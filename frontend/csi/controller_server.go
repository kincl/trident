// Copyright 2019 NetApp, Inc. All Rights Reserved.

package csi

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/golang/protobuf/ptypes/timestamp"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	v1 "k8s.io/api/core/v1"

	tridentconfig "github.com/netapp/trident/config"
	"github.com/netapp/trident/core"
	"github.com/netapp/trident/frontend/csi/helpers"
	"github.com/netapp/trident/storage"
	"github.com/netapp/trident/utils"
)

func (p *Plugin) CreateVolume(
	ctx context.Context, req *csi.CreateVolumeRequest,
) (*csi.CreateVolumeResponse, error) {

	fields := log.Fields{"Method": "CreateVolume", "Type": "CSI_Controller", "name": req.Name}
	log.WithFields(fields).Debug(">>>> CreateVolume")
	defer log.WithFields(fields).Debug("<<<< CreateVolume")

	if _, ok := p.opCache[req.Name]; ok {
		log.WithFields(fields).Debug("Create already in progress, returning DeadlineExceeded.")
		return nil, status.Error(codes.DeadlineExceeded, "create already in progress")
	} else {
		p.opCache[req.Name] = true
		defer delete(p.opCache, req.Name)
	}

	// Check arguments
	if len(req.GetName()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "volume name missing in request")
	}
	if req.GetVolumeCapabilities() == nil {
		return nil, status.Error(codes.InvalidArgument, "volume capabilities missing in request")
	}

	// Check for pre-existing volume with the same name
	existingVolume, err := p.orchestrator.GetVolume(req.Name)
	if err != nil && !core.IsNotFoundError(err) {
		return nil, p.getCSIErrorForOrchestratorError(err)
	}

	// If pre-existing volume found, check for the requested capacity and already allocated capacity
	if existingVolume != nil {

		// Check if the size of existing volume is compatible with the new request
		existingSize, _ := strconv.ParseInt(existingVolume.Config.Size, 10, 64)
		if existingSize < int64(req.GetCapacityRange().GetRequiredBytes()) {
			return nil, status.Error(
				codes.AlreadyExists,
				fmt.Sprintf("volume %s (but with different size) already exists", req.GetName()))
		}

		// Request matches existing volume, so just return it
		csiVolume, err := p.getCSIVolumeFromTridentVolume(existingVolume)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}

		return &csi.CreateVolumeResponse{Volume: csiVolume}, nil
	}

	// Check for matching volume capabilities
	log.Debugf("Volume capabilities (%d): %v", len(req.GetVolumeCapabilities()), req.GetVolumeCapabilities())
	protocol := tridentconfig.ProtocolAny
	accessMode := tridentconfig.ModeAny
	fsType := ""
	//var mountFlags []string

	if req.GetVolumeCapabilities() != nil {
		for _, capability := range req.GetVolumeCapabilities() {

			// Ensure access type is "MountVolume"
			if block := capability.GetBlock(); block != nil {
				return nil, status.Error(codes.InvalidArgument, "block access type not supported")
			}

			// See if we have a backend for the specified access mode
			accessMode = p.getAccessForCSIAccessMode(capability.GetAccessMode().Mode)
			protocol = p.getProtocolForCSIAccessMode(capability.GetAccessMode().Mode)
			if !p.hasBackendForProtocol(protocol) {
				return nil, status.Error(codes.InvalidArgument, "no available storage for access mode")
			}

			// See if fsType was specified
			if mount := capability.GetMount(); mount != nil {
				fsType = mount.GetFsType()
				//mountFlags = mount.GetMountFlags()
			}
		}
	}

	var sizeBytes int64
	if req.CapacityRange != nil {
		sizeBytes = req.CapacityRange.RequiredBytes
	}

	// Convert volume creation options into a Trident volume config
	volConfig, err := p.helper.GetVolumeConfig(req.Name, sizeBytes, req.Parameters, protocol, accessMode, fsType)
	if err != nil {
		p.helper.RecordVolumeEvent(req.Name, helpers.EventTypeNormal, "ProvisioningFailed", err.Error())
		return nil, p.getCSIErrorForOrchestratorError(err)
	}

	// Check if CSI asked for a clone (overrides trident.netapp.io/cloneFromPVC PVC annotation, if present)
	if req.VolumeContentSource != nil {
		switch contentSource := req.VolumeContentSource.Type.(type) {

		case *csi.VolumeContentSource_Volume:
			volumeID := contentSource.Volume.VolumeId
			if volumeID == "" {
				return nil, status.Error(codes.InvalidArgument, "content source volume ID missing in request")
			}
			volConfig.CloneSourceVolume = volumeID

		case *csi.VolumeContentSource_Snapshot:
			snapshotID := contentSource.Snapshot.SnapshotId
			if snapshotID == "" {
				return nil, status.Error(codes.InvalidArgument, "content source snapshot ID missing in request")
			}
			if cloneSourceVolume, cloneSourceSnapshot, err := storage.ParseSnapshotID(snapshotID); err != nil {
				log.WithFields(log.Fields{
					"volumeName": req.Name,
					"snapshotID": contentSource.Snapshot.SnapshotId,
				}).Error("Cannot create clone, invalid snapshot ID.")
				return nil, status.Error(codes.InvalidArgument, "invalid snapshot ID")
			} else {
				volConfig.CloneSourceVolume = cloneSourceVolume
				volConfig.CloneSourceSnapshot = cloneSourceSnapshot
			}
		}
	}

	// Invoke the orchestrator to create or clone the new volume
	var newVolume *storage.VolumeExternal
	if volConfig.CloneSourceVolume == "" {
		newVolume, err = p.orchestrator.AddVolume(volConfig)
	} else {
		newVolume, err = p.orchestrator.CloneVolume(volConfig)
	}

	if err != nil {
		p.helper.RecordVolumeEvent(req.Name, helpers.EventTypeNormal, "ProvisioningFailed", err.Error())
		return nil, p.getCSIErrorForOrchestratorError(err)
	} else {
		p.helper.RecordVolumeEvent(req.Name, v1.EventTypeNormal, "ProvisioningSuccess", "provisioned a volume")
	}

	csiVolume, err := p.getCSIVolumeFromTridentVolume(newVolume)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &csi.CreateVolumeResponse{Volume: csiVolume}, nil
}

func (p *Plugin) DeleteVolume(
	ctx context.Context, req *csi.DeleteVolumeRequest,
) (*csi.DeleteVolumeResponse, error) {

	fields := log.Fields{"Method": "DeleteVolume", "Type": "CSI_Controller"}
	log.WithFields(fields).Debug(">>>> DeleteVolume")
	defer log.WithFields(fields).Debug("<<<< DeleteVolume")

	if req.GetVolumeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "no volume ID provided")
	}

	if err := p.orchestrator.DeleteVolume(req.VolumeId); err != nil {

		log.WithFields(log.Fields{
			"volumeName": req.VolumeId,
			"error":      err,
		}).Debugf("Could not delete volume.")

		// In CSI, delete is idempotent, so don't return an error if the volume doesn't exist
		if !core.IsNotFoundError(err) {
			return nil, p.getCSIErrorForOrchestratorError(err)
		}
	}

	return &csi.DeleteVolumeResponse{}, nil
}

func stashIscsiTargetPortals(publishInfo map[string]string, accessInfo utils.VolumeAccessInfo) {

	count := 1 + len(accessInfo.IscsiPortals)
	publishInfo["iscsiTargetPortalCount"] = strconv.Itoa(count)
	publishInfo["p1"] = accessInfo.IscsiTargetPortal
	for i, p := range accessInfo.IscsiPortals {
		key := fmt.Sprintf("p%d", i+2)
		publishInfo[key] = p
	}
}

func (p *Plugin) ControllerPublishVolume(
	ctx context.Context, req *csi.ControllerPublishVolumeRequest,
) (*csi.ControllerPublishVolumeResponse, error) {

	fields := log.Fields{"Method": "ControllerPublishVolume", "Type": "CSI_Controller"}
	log.WithFields(fields).Debug(">>>> ControllerPublishVolume")
	defer log.WithFields(fields).Debug("<<<< ControllerPublishVolume")

	volumeID := req.GetVolumeId()
	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument, "no volume ID provided")
	}

	nodeID := req.GetNodeId()
	if nodeID == "" {
		return nil, status.Error(codes.InvalidArgument, "no node ID provided")
	}

	if req.GetVolumeCapability() == nil {
		return nil, status.Error(codes.InvalidArgument, "no volume capabilities provided")
	}

	// Make sure volume exists
	volume, err := p.orchestrator.GetVolume(volumeID)
	if err != nil {
		return nil, p.getCSIErrorForOrchestratorError(err)
	}

	// Get node attributes from the node ID
	nodeInfo, err := p.orchestrator.GetNode(nodeID)
	if err != nil {
		log.WithField("node", nodeID).Error("Node info not found.")
		return nil, status.Error(codes.NotFound, err.Error())
	}

	// Set up volume publish info with what we know about the node
	volumePublishInfo := &utils.VolumePublishInfo{
		Localhost: false,
		HostIQN:   []string{nodeInfo.IQN},
		HostIP:    []string{},
		HostName:  nodeInfo.Name,
	}

	// Update NFS export rules (?), add node IQN to igroup, etc.
	err = p.orchestrator.PublishVolume(volume.Config.Name, volumePublishInfo)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	mount := req.VolumeCapability.GetMount()
	if len(mount.MountFlags) > 0 {
		volumePublishInfo.MountOptions = strings.Join(mount.MountFlags, ",")
	}

	// Build CSI controller publish info from volume publish info
	publishInfo := map[string]string{
		"protocol": string(volume.Config.Protocol),
	}

	publishInfo["mountOptions"] = volumePublishInfo.MountOptions
	if volume.Config.Protocol == tridentconfig.File {
		publishInfo["nfsServerIp"] = volume.Config.AccessInfo.NfsServerIP
		publishInfo["nfsPath"] = volume.Config.AccessInfo.NfsPath
	} else if volume.Config.Protocol == tridentconfig.Block {
		stashIscsiTargetPortals(publishInfo, volume.Config.AccessInfo)
		publishInfo["iscsiTargetIqn"] = volume.Config.AccessInfo.IscsiTargetIQN
		publishInfo["iscsiLunNumber"] = strconv.Itoa(int(volume.Config.AccessInfo.IscsiLunNumber))
		publishInfo["iscsiInterface"] = volume.Config.AccessInfo.IscsiInterface
		publishInfo["iscsiIgroup"] = volume.Config.AccessInfo.IscsiIgroup
		publishInfo["iscsiUsername"] = volume.Config.AccessInfo.IscsiUsername
		publishInfo["iscsiInitiatorSecret"] = volume.Config.AccessInfo.IscsiInitiatorSecret
		publishInfo["iscsiTargetSecret"] = volume.Config.AccessInfo.IscsiTargetSecret
		publishInfo["filesystemType"] = volumePublishInfo.FilesystemType
		publishInfo["useCHAP"] = strconv.FormatBool(volumePublishInfo.UseCHAP)
		publishInfo["sharedTarget"] = strconv.FormatBool(volumePublishInfo.SharedTarget)
	}

	return &csi.ControllerPublishVolumeResponse{PublishContext: publishInfo}, nil
}

func (p *Plugin) ControllerUnpublishVolume(
	ctx context.Context, req *csi.ControllerUnpublishVolumeRequest,
) (*csi.ControllerUnpublishVolumeResponse, error) {

	fields := log.Fields{"Method": "ControllerUnpublishVolume", "Type": "CSI_Controller"}
	log.WithFields(fields).Debug(">>>> ControllerUnpublishVolume")
	defer log.WithFields(fields).Debug("<<<< ControllerUnpublishVolume")

	volumeID := req.GetVolumeId()
	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument, "no volume ID provided")
	}

	// Make sure volume exists
	if _, err := p.orchestrator.GetVolume(volumeID); err != nil {
		return nil, p.getCSIErrorForOrchestratorError(err)
	}

	// Apart from validation, Trident has nothing to do for this entry point
	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (p *Plugin) ValidateVolumeCapabilities(
	ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest,
) (*csi.ValidateVolumeCapabilitiesResponse, error) {

	volumeID := req.GetVolumeId()

	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument, "no volume ID provided")
	}
	if req.GetVolumeCapabilities() == nil {
		return nil, status.Error(codes.InvalidArgument, "no volume capabilities provided")
	}

	volume, err := p.orchestrator.GetVolume(volumeID)
	if err != nil {
		return nil, status.Error(codes.NotFound, "volume not found")
	}

	resp := &csi.ValidateVolumeCapabilitiesResponse{}

	for _, v := range req.GetVolumeCapabilities() {
		if volume.Config.AccessMode != p.getAccessForCSIAccessMode(v.GetAccessMode().Mode) {
			resp.Message = "Could not satisfy one or more access modes."
			return resp, nil
		}
		if block := v.GetBlock(); block != nil {
			if volume.Config.Protocol != tridentconfig.Block {
				resp.Message = "Could not satisfy block protocol."
				return resp, nil
			}
		} else {
			if volume.Config.Protocol != tridentconfig.File {
				resp.Message = "Could not satisfy file protocol."
				return resp, nil
			}
		}
	}

	confirmed := &csi.ValidateVolumeCapabilitiesResponse_Confirmed{}
	confirmed.VolumeCapabilities = req.GetVolumeCapabilities()

	resp.Confirmed = confirmed

	return resp, nil
}

func (p *Plugin) ListVolumes(
	ctx context.Context, req *csi.ListVolumesRequest,
) (*csi.ListVolumesResponse, error) {

	fields := log.Fields{"Method": "ListVolumes", "Type": "CSI_Controller"}
	log.WithFields(fields).Debug(">>>> ListVolumes")
	defer log.WithFields(fields).Debug("<<<< ListVolumes")

	volumes, err := p.orchestrator.ListVolumes()
	if err != nil {
		return nil, p.getCSIErrorForOrchestratorError(err)
	}

	entries := make([]*csi.ListVolumesResponse_Entry, 0)

	for _, volume := range volumes {
		if csiVolume, err := p.getCSIVolumeFromTridentVolume(volume); err == nil {
			entries = append(entries, &csi.ListVolumesResponse_Entry{Volume: csiVolume})
		}
	}

	return &csi.ListVolumesResponse{Entries: entries}, nil
}

func (p *Plugin) GetCapacity(
	ctx context.Context, req *csi.GetCapacityRequest,
) (*csi.GetCapacityResponse, error) {

	// Trident doesn't report pool capacities
	return nil, status.Error(codes.Unimplemented, "")
}

func (p *Plugin) ControllerGetCapabilities(
	ctx context.Context, req *csi.ControllerGetCapabilitiesRequest,
) (*csi.ControllerGetCapabilitiesResponse, error) {

	fields := log.Fields{"Method": "ControllerGetCapabilities", "Type": "CSI_Controller"}
	log.WithFields(fields).Debug(">>>> ControllerGetCapabilities")
	defer log.WithFields(fields).Debug("<<<< ControllerGetCapabilities")

	return &csi.ControllerGetCapabilitiesResponse{Capabilities: p.csCap}, nil
}

func (p *Plugin) CreateSnapshot(
	ctx context.Context, req *csi.CreateSnapshotRequest,
) (*csi.CreateSnapshotResponse, error) {

	fields := log.Fields{"Method": "CreateSnapshot", "Type": "CSI_Controller"}
	log.WithFields(fields).Debug(">>>> CreateSnapshot")
	defer log.WithFields(fields).Debug("<<<< CreateSnapshot")

	volumeName := req.GetSourceVolumeId()
	if volumeName == "" {
		return nil, status.Error(codes.InvalidArgument, "no volume ID provided")
	}

	snapshotName := req.GetName()
	if snapshotName == "" {
		return nil, status.Error(codes.InvalidArgument, "no snapshot name provided")
	}

	// Check for pre-existing snapshot with the same name on the same volume
	existingSnapshot, err := p.orchestrator.GetSnapshot(volumeName, snapshotName)
	if err != nil && !core.IsNotFoundError(err) {
		return nil, p.getCSIErrorForOrchestratorError(err)
	}

	// If pre-existing snapshot found, just return it
	if existingSnapshot != nil {
		if csiSnapshot, err := p.getCSISnapshotFromTridentSnapshot(existingSnapshot); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		} else {
			return &csi.CreateSnapshotResponse{Snapshot: csiSnapshot}, nil
		}
	}

	// Check for pre-existing snapshot with the same name on a different volume
	if existingSnapshots, err := p.orchestrator.ListSnapshotsByName(snapshotName); err != nil {
		return nil, p.getCSIErrorForOrchestratorError(err)
	} else if len(existingSnapshots) > 0 {
		for _, s := range existingSnapshots {
			log.Errorf("Found existing snapshot %s in another volume %s.", s.Config.Name, s.Config.VolumeName)
		}
		// We already handled the same name / same volume case, so getting here has to mean a different volume
		return nil, status.Error(codes.AlreadyExists, "snapshot exists on a different volume")
	} else {
		log.Debugf("Found no existing snapshot %s in other volumes.", snapshotName)
	}

	// Convert snapshot creation options into a Trident snapshot config
	snapshotConfig, err := p.helper.GetSnapshotConfig(volumeName, snapshotName)
	if err != nil {
		p.helper.RecordVolumeEvent(req.Name, helpers.EventTypeNormal, "ProvisioningFailed", err.Error())
		return nil, p.getCSIErrorForOrchestratorError(err)
	}

	// Create the snapshot
	newSnapshot, err := p.orchestrator.CreateSnapshot(snapshotConfig)
	if err != nil {
		if core.IsNotFoundError(err) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	if csiSnapshot, err := p.getCSISnapshotFromTridentSnapshot(newSnapshot); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	} else {
		return &csi.CreateSnapshotResponse{Snapshot: csiSnapshot}, nil
	}
}

func (p *Plugin) DeleteSnapshot(
	ctx context.Context, req *csi.DeleteSnapshotRequest,
) (*csi.DeleteSnapshotResponse, error) {

	fields := log.Fields{"Method": "DeleteSnapshot", "Type": "CSI_Controller"}
	log.WithFields(fields).Debug(">>>> DeleteSnapshot")
	defer log.WithFields(fields).Debug("<<<< DeleteSnapshot")

	snapshotID := req.GetSnapshotId()
	if snapshotID == "" {
		return nil, status.Error(codes.InvalidArgument, "no snapshot ID provided")
	}

	volumeName, snapshotName, err := storage.ParseSnapshotID(snapshotID)
	if err != nil {
		// An invalid ID is treated an a non-existent snapshot, so we log the error and return success
		log.Error(err)
		return &csi.DeleteSnapshotResponse{}, nil
	}

	// Delete the snapshot
	if err = p.orchestrator.DeleteSnapshot(volumeName, snapshotName); err != nil {

		log.WithFields(log.Fields{
			"volumeName":   volumeName,
			"snapshotName": snapshotName,
			"error":        err,
		}).Debugf("Could not delete snapshot.")

		// In CSI, delete is idempotent, so don't return an error if the snapshot doesn't exist
		if !core.IsNotFoundError(err) {
			return nil, p.getCSIErrorForOrchestratorError(err)
		}
	}

	return &csi.DeleteSnapshotResponse{}, nil
}

func (p *Plugin) ListSnapshots(
	ctx context.Context, req *csi.ListSnapshotsRequest,
) (*csi.ListSnapshotsResponse, error) {

	// Trident doesn't support snapshots
	return nil, status.Error(codes.Unimplemented, "")
}

func (p *Plugin) ControllerExpandVolume(
	ctx context.Context, in *csi.ControllerExpandVolumeRequest,
) (*csi.ControllerExpandVolumeResponse, error) {

	// Trident doesn't support expansion via CSI
	return nil, status.Error(codes.Unimplemented, "")
}

func (p *Plugin) getCSIVolumeFromTridentVolume(volume *storage.VolumeExternal) (*csi.Volume, error) {

	capacity, err := strconv.ParseInt(volume.Config.Size, 10, 64)
	if err != nil {
		log.WithFields(log.Fields{
			"volume": volume.Config.InternalName,
			"size":   volume.Config.Size,
		}).Error("Could not parse volume size.")
		capacity = 0
	}

	attributes := map[string]string{
		"backendUUID":  volume.BackendUUID,
		"name":         volume.Config.Name,
		"internalName": volume.Config.InternalName,
		"protocol":     string(volume.Config.Protocol),
	}

	return &csi.Volume{
		CapacityBytes: capacity,
		VolumeId:      volume.Config.Name,
		VolumeContext: attributes,
	}, nil
}

func (p *Plugin) getCSISnapshotFromTridentSnapshot(snapshot *storage.SnapshotExternal) (*csi.Snapshot, error) {

	createdSeconds, err := time.Parse(time.RFC3339, snapshot.Created)
	if err != nil {
		log.WithField("time", snapshot.Created).Error("Could not parse RFC3339 snapshot time.")
		createdSeconds = time.Now()
	}

	return &csi.Snapshot{
		SizeBytes:      snapshot.SizeBytes,
		SnapshotId:     storage.MakeSnapshotID(snapshot.Config.VolumeName, snapshot.Config.Name),
		SourceVolumeId: snapshot.Config.VolumeName,
		CreationTime:   &timestamp.Timestamp{Seconds: createdSeconds.Unix()},
		ReadyToUse:     true,
	}, nil
}

func (p *Plugin) getAccessForCSIAccessMode(accessMode csi.VolumeCapability_AccessMode_Mode) tridentconfig.AccessMode {
	switch accessMode {
	case csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER:
		return tridentconfig.ReadWriteOnce
	case csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY:
		return tridentconfig.ReadWriteOnce
	case csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY:
		return tridentconfig.ReadOnlyMany
	case csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER:
		return tridentconfig.ReadWriteMany
	case csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER:
		return tridentconfig.ReadWriteMany
	default:
		return tridentconfig.ModeAny
	}
}

func (p *Plugin) getProtocolForCSIAccessMode(accessMode csi.VolumeCapability_AccessMode_Mode) tridentconfig.Protocol {
	switch accessMode {
	case csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER: // block or file OK
		return tridentconfig.ProtocolAny
	case csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY: // block or file OK
		return tridentconfig.ProtocolAny
	case csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY: // block or file OK
		return tridentconfig.ProtocolAny
	case csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER: // block or file OK
		return tridentconfig.ProtocolAny
	case csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER: // file required
		return tridentconfig.File
	default:
		return tridentconfig.ProtocolAny
	}
}

func (p *Plugin) hasBackendForProtocol(protocol tridentconfig.Protocol) bool {

	backends, err := p.orchestrator.ListBackends()
	if err != nil || backends == nil || len(backends) == 0 {
		return false
	}

	if protocol == tridentconfig.ProtocolAny {
		return true
	}

	for _, b := range backends {
		if b.Protocol == tridentconfig.ProtocolAny || b.Protocol == protocol {
			return true
		}
	}

	return false
}
