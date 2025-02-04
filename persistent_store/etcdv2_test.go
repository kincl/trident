// Copyright 2019 NetApp, Inc. All Rights Reserved.

package persistentstore

import (
	"encoding/json"
	"flag"
	"fmt"
	"reflect"
	"strconv"
	"testing"
	"time"

	uuid "github.com/google/uuid"
	log "github.com/sirupsen/logrus"

	"github.com/netapp/trident/config"
	"github.com/netapp/trident/storage"
	"github.com/netapp/trident/storage_attribute"
	"github.com/netapp/trident/storage_class"
	drivers "github.com/netapp/trident/storage_drivers"
	"github.com/netapp/trident/storage_drivers/ontap"
	"github.com/netapp/trident/storage_drivers/solidfire"
	"github.com/netapp/trident/utils"
)

var (
	etcdV2 = flag.String("etcd_v2", "http://127.0.0.1:8001", "etcd server (v2 API)")
	debug  = flag.Bool("debug", false, "Enable debugging output")

	storagePool = "aggr1"
)

func init() {
	flag.Parse()
	if *debug {
		log.SetLevel(log.DebugLevel)
	}
}

func TestNewEtcdClientV2(t *testing.T) {
	p, err := NewEtcdClientV2(*etcdV2)
	if p == nil || err != nil {
		t.Errorf("Failed to create a working etcdv2 client in %v!",
			config.PersistentStoreBootstrapTimeout)
	}
}

func TestEtcdv2InvalidClient(t *testing.T) {
	_, err := NewEtcdClientV2("http://127.0.0.1:9999")
	if err != nil && MatchUnavailableClusterErr(err) {
	} else {
		t.Error("Invalid client wasn't caught!")
	}
}

func TestEtcdv2CRUD(t *testing.T) {
	p, err := NewEtcdClientV2(*etcdV2)

	// Testing Create
	err = p.Create("key1", "val1")
	if err != nil {
		t.Error(err.Error())
		t.FailNow()
	}

	// Testing Read
	var val string
	val, err = p.Read("key1")
	if err != nil {
		t.Error(err.Error())
	}

	// Testing Update
	err = p.Update("key1", "val2")
	if err != nil {
		t.Error(err.Error())
	}
	val, err = p.Read("key1")
	if err != nil {
		t.Error(err.Error())
	} else if val != "val2" {
		t.Error("Update failed!")
	}

	// Testing Delete
	err = p.Delete("key1")
	if err != nil {
		t.Error(err.Error())
	}
}

func TestEtcdv2ReadDeleteKeys(t *testing.T) {
	p, err := NewEtcdClientV2(*etcdV2)

	for i := 1; i <= 5; i++ {
		err = p.Create("/volume/"+"vol"+strconv.Itoa(i), "val"+strconv.Itoa(i))
		if err != nil {
			t.Error(err.Error())
			t.FailNow()
		}
	}

	var keys []string
	keys, err = p.ReadKeys("/volume")
	if err != nil {
		t.Error(err.Error())
		t.FailNow()
	}
	if len(keys) != 5 {
		t.Error("Reading all the keys failed!")
	}
	for i := 1; i <= 5; i++ {
		key := "/volume/" + "vol" + strconv.Itoa(i)
		if keys[i-1] != key {
			t.Error("")
		}
	}
	if err = p.DeleteKeys("/volume"); err != nil {
		t.Error("Deleting the keys failed!")
	}
	_, err = p.ReadKeys("/volume")
	if err != nil && MatchKeyNotFoundErr(err) {
	} else {
		t.Error(err.Error())
		t.FailNow()
	}
}

func TestEtcdv2RecursiveReadDelete(t *testing.T) {
	p, err := NewEtcdClientV2(*etcdV2)

	for i := 1; i <= 5; i++ {
		err = p.Set(fmt.Sprintf("/trident_etcd/v%d/volume/vol%d", i, i),
			"VOLUME"+strconv.Itoa(i))
		if err != nil {
			t.Error(err.Error())
			t.FailNow()
		}
	}
	for i := 1; i <= 3; i++ {
		err = p.Set(fmt.Sprintf("/trident_etcd/v%d/backend/backend%d", i, i),
			"BACKEND"+strconv.Itoa(i))
		if err != nil {
			t.Error(err.Error())
			t.FailNow()
		}
	}

	var keys []string
	keys, err = p.ReadKeys("/trident_etcd")
	if err != nil {
		t.Error(err.Error())
		t.FailNow()
	}
	if len(keys) != 8 {
		t.Error("Reading all the keys failed!")
	}
	keys, err = p.ReadKeys("/trident_etcd/v1")
	if err != nil {
		t.Error(err.Error())
		t.FailNow()
	}
	if len(keys) != 2 {
		t.Error("Reading all the keys failed!")
	}

	if err = p.DeleteKeys("/trident_etcd"); err != nil {
		t.Errorf("Failed to delete the keys: %v", err)
	}

	// Validate delete
	_, err = p.ReadKeys("/trident_etcd")
	if err != nil && MatchKeyNotFoundErr(err) {
	} else {
		t.Fatal("Failed to delete the keys: ", err)
	}
}

func TestEtcdv2NonexistentReadKeys(t *testing.T) {
	p, err := NewEtcdClientV2(*etcdV2)
	_, err = p.ReadKeys("/trident_102983471023")
	if err != nil && MatchKeyNotFoundErr(err) {
	} else {
		t.Fatal("Failed to return an error for the nonexistent keys: ", err)
	}
}

func TestEtcdv2NonexistentDeleteKeys(t *testing.T) {
	p, err := NewEtcdClientV2(*etcdV2)
	err = p.DeleteKeys("/trident_102983471023")
	if err != nil && MatchKeyNotFoundErr(err) {
	} else {
		t.Fatal("Failed to return an error for the nonexistent keys: ", err)
	}
}

func TestEtcdv2CRUDFailure(t *testing.T) {
	p, err := NewEtcdClientV2(*etcdV2)
	if err != nil {
		t.Error(err.Error())
	}
	var backend string
	backend, err = p.Read("/backend/nonexistent_server_10.0.0.1")
	if backend != "" || !(err != nil && MatchKeyNotFoundErr(err)) {
		t.Error("Failed to catch an invalid read!")
	}
	if err = p.Update("/backend/nonexistent_server_10.0.0.1", ""); !(err != nil && MatchKeyNotFoundErr(err)) {
		t.Error("Failed to catch an invalid update!")
	}
	if err = p.Delete("/backend/nonexistent_server_10.0.0.1"); !(err != nil && MatchKeyNotFoundErr(err)) {
		t.Error("Failed to catch an invalid delete!")
	}
}

func TestEtcdv2Backend(t *testing.T) {
	p, err := NewEtcdClientV2(*etcdV2)

	// Adding storage backend
	nfsServerConfig := drivers.OntapStorageDriverConfig{
		CommonStorageDriverConfig: &drivers.CommonStorageDriverConfig{
			StorageDriverName: drivers.OntapNASStorageDriverName,
		},
		ManagementLIF: "10.0.0.4",
		DataLIF:       "10.0.0.100",
		SVM:           "svm1",
		Username:      "admin",
		Password:      "netapp",
	}
	nfsDriver := ontap.NASStorageDriver{
		Config: nfsServerConfig,
	}
	nfsServer := &storage.Backend{
		Driver: &nfsDriver,
		Name:   "nfs_server_1-" + nfsServerConfig.ManagementLIF,
	}
	err = p.AddBackend(nfsServer)
	if err != nil {
		t.Error(err.Error())
		t.FailNow()
	}

	// Getting a storage backend
	//var recoveredBackend *storage.BackendPersistent
	var ontapConfig drivers.OntapStorageDriverConfig
	recoveredBackend, err := p.GetBackend(nfsServer.Name)
	if err != nil {
		t.Error(err.Error())
	}
	configJSON, err := recoveredBackend.MarshalConfig()
	if err != nil {
		t.Fatal("Unable to marshal recovered backend config: ", err)
	}
	if err = json.Unmarshal([]byte(configJSON), &ontapConfig); err != nil {
		t.Error("Unable to unmarshal backend into ontap configuration: ", err)
	} else if ontapConfig.SVM != nfsServerConfig.SVM {
		t.Error("Backend state doesn't match!")
	}

	// Updating a storage backend
	nfsServerNewConfig := drivers.OntapStorageDriverConfig{
		CommonStorageDriverConfig: &drivers.CommonStorageDriverConfig{
			StorageDriverName: drivers.OntapNASStorageDriverName,
		},
		ManagementLIF: "10.0.0.4",
		DataLIF:       "10.0.0.100",
		SVM:           "svm1",
		Username:      "admin",
		Password:      "NETAPP",
	}
	nfsDriver.Config = nfsServerNewConfig
	err = p.UpdateBackend(nfsServer)
	if err != nil {
		t.Error(err.Error())
	}
	recoveredBackend, err = p.GetBackend(nfsServer.Name)
	if err != nil {
		t.Error(err.Error())
	}
	configJSON, err = recoveredBackend.MarshalConfig()
	if err != nil {
		t.Fatal("Unable to marshal recovered backend config: ", err)
	}
	if err = json.Unmarshal([]byte(configJSON), &ontapConfig); err != nil {
		t.Error("Unable to unmarshal backend into ontap configuration: ", err)
	} else if ontapConfig.SVM != nfsServerConfig.SVM {
		t.Error("Backend state doesn't match!")
	}

	// Deleting a storage backend
	err = p.DeleteBackend(nfsServer)
	if err != nil {
		t.Error(err.Error())
	}
}

func TestEtcdv2Backends(t *testing.T) {
	var backends []*storage.BackendPersistent
	p, err := NewEtcdClientV2(*etcdV2)
	if backends, err = p.GetBackends(); err != nil {
		t.Fatal("Unable to list backends at test start: ", err)
	}
	initialCount := len(backends)

	// Adding storage backends
	for i := 1; i <= 5; i++ {
		nfsServerConfig := drivers.OntapStorageDriverConfig{
			CommonStorageDriverConfig: &drivers.CommonStorageDriverConfig{
				StorageDriverName: drivers.OntapNASStorageDriverName,
			},
			ManagementLIF: "10.0.0." + strconv.Itoa(i),
			DataLIF:       "10.0.0.100",
			SVM:           "svm" + strconv.Itoa(i),
			Username:      "admin",
			Password:      "netapp",
		}
		nfsServer := &storage.Backend{
			Driver: &ontap.NASStorageDriver{
				Config: nfsServerConfig,
			},
			Name: "nfs_server_" + strconv.Itoa(i) + "-" + nfsServerConfig.ManagementLIF,
		}
		err = p.AddBackend(nfsServer)
	}

	// Retreiving all backends
	if backends, err = p.GetBackends(); err != nil {
		t.Error(err.Error())
		t.FailNow()
	}
	if len(backends)-initialCount != 5 {
		t.Error("Didn't retrieve all the backends!")
	}

	// Deleting all backends
	if err = p.DeleteBackends(); err != nil {
		t.Error(err.Error())
	}
	if backends, err = p.GetBackends(); err != nil {
		t.Error(err.Error())
	} else if len(backends) != 0 {
		t.Error("Deleting backends failed!")
	}
}

func TestEtcdv2DuplicateBackend(t *testing.T) {
	p, err := NewEtcdClientV2(*etcdV2)

	nfsServerConfig := drivers.OntapStorageDriverConfig{
		CommonStorageDriverConfig: &drivers.CommonStorageDriverConfig{
			StorageDriverName: drivers.OntapNASStorageDriverName,
		},
		ManagementLIF: "10.0.0.4",
		DataLIF:       "10.0.0.100",
		SVM:           "svm1",
		Username:      "admin",
		Password:      "netapp",
	}
	nfsServer := &storage.Backend{
		Driver: &ontap.NASStorageDriver{
			Config: nfsServerConfig,
		},
		Name: "nfs_server_1-" + nfsServerConfig.ManagementLIF,
	}
	err = p.AddBackend(nfsServer)
	if err != nil {
		t.Error(err.Error())
		t.FailNow()
	}
	err = p.AddBackend(nfsServer)
	if err == nil {
		t.Error("Second Create should have failed!")
	}
	err = p.DeleteBackend(nfsServer)
	if err != nil {
		t.Error(err.Error())
	}
}

func TestEtcdv2Volume(t *testing.T) {
	p, err := NewEtcdClientV2(*etcdV2)

	// Adding a volume
	nfsServerConfig := drivers.OntapStorageDriverConfig{
		CommonStorageDriverConfig: &drivers.CommonStorageDriverConfig{
			StorageDriverName: drivers.OntapNASStorageDriverName,
		},
		ManagementLIF: "10.0.0.4",
		DataLIF:       "10.0.0.100",
		SVM:           "svm1",
		Username:      "admin",
		Password:      "netapp",
	}
	nfsServer := &storage.Backend{
		Driver: &ontap.NASStorageDriver{
			Config: nfsServerConfig,
		},
		Name: "nfs_server-" + nfsServerConfig.ManagementLIF,
	}
	nfsServer.BackendUUID = uuid.New().String()
	vol1Config := storage.VolumeConfig{
		Version:      string(config.OrchestratorAPIVersion),
		Name:         "vol1",
		Size:         "1GB",
		Protocol:     config.File,
		StorageClass: "gold",
	}
	vol1 := &storage.Volume{
		Config:      &vol1Config,
		BackendUUID: nfsServer.BackendUUID,
		Pool:        storagePool,
	}
	err = p.AddVolume(vol1)
	if err != nil {
		t.Error(err.Error())
		t.FailNow()
	}

	// Getting a volume
	var recoveredVolume *storage.VolumeExternal
	recoveredVolume, err = p.GetVolume(vol1.Config.Name)
	if err != nil {
		t.Error(err.Error())
	}
	if recoveredVolume.BackendUUID != vol1.BackendUUID ||
		recoveredVolume.Config.Size != vol1.Config.Size {
		t.Error("Recovered volume does not match!")
	}

	// Updating a volume
	vol1Config.Size = "2GB"
	err = p.UpdateVolume(vol1)
	if err != nil {
		t.Error(err.Error())
	}
	recoveredVolume, err = p.GetVolume(vol1.Config.Name)
	if err != nil {
		t.Error(err.Error())
	}
	if recoveredVolume.Config.Size != vol1Config.Size {
		t.Error("Volume update failed!")
	}

	// Deleting a volume
	err = p.DeleteVolume(vol1)
	if err != nil {
		t.Error(err.Error())
	}
}

func TestEtcdv2Volumes(t *testing.T) {
	var (
		newVolumeCount  = 5
		expectedVolumes = newVolumeCount
	)
	p, err := NewEtcdClientV2(*etcdV2)

	// Because we don't reset database state between tests, get an initial
	// count of the existing volumes
	if initialVols, err := p.GetVolumes(); err != nil {
		t.Errorf("Unable to get initial volumes.")
	} else {
		expectedVolumes += len(initialVols)
	}

	// Adding volumes
	nfsServerConfig := drivers.OntapStorageDriverConfig{
		CommonStorageDriverConfig: &drivers.CommonStorageDriverConfig{
			StorageDriverName: drivers.OntapNASStorageDriverName,
		},
		ManagementLIF: "10.0.0.4",
		DataLIF:       "10.0.0.100",
		SVM:           "svm1",
		Username:      "admin",
		Password:      "netapp",
	}
	nfsServer := &storage.Backend{
		Driver: &ontap.NASStorageDriver{
			Config: nfsServerConfig,
		},
		Name: "nfs_server-" + nfsServerConfig.ManagementLIF,
	}
	nfsServer.BackendUUID = uuid.New().String()

	for i := 1; i <= newVolumeCount; i++ {
		volConfig := storage.VolumeConfig{
			Version:      string(config.OrchestratorAPIVersion),
			Name:         "vol" + strconv.Itoa(i),
			Size:         strconv.Itoa(i) + "GB",
			Protocol:     config.File,
			StorageClass: "gold",
		}
		vol := &storage.Volume{
			Config:      &volConfig,
			BackendUUID: nfsServer.BackendUUID,
			Pool:        storagePool,
		}
		err = p.AddVolume(vol)
		if err != nil {
			t.Error(err.Error())
			t.FailNow()
		}
	}

	// Retreiving all volumes
	var volumes []*storage.VolumeExternal
	if volumes, err = p.GetVolumes(); err != nil {
		t.Error(err.Error())
		t.FailNow()
	}
	if len(volumes) != expectedVolumes {
		t.Errorf("Expected %d volumes; retrieved %d", expectedVolumes,
			len(volumes))
	}

	// Deleting all volumes
	if err = p.DeleteVolumes(); err != nil {
		t.Error(err.Error())
	}
	if volumes, err = p.GetVolumes(); err != nil {
		t.Error(err.Error())
	} else if len(volumes) != 0 {
		t.Error("Deleting volumes failed!")
	}
}

func TestEtcdv2VolumeTransactions(t *testing.T) {
	p, err := NewEtcdClientV2(*etcdV2)

	// Adding volume transactions
	for i := 1; i <= 5; i++ {
		volConfig := storage.VolumeConfig{
			Version:      string(config.OrchestratorAPIVersion),
			Name:         "vol" + strconv.Itoa(i),
			Size:         strconv.Itoa(i) + "GB",
			Protocol:     config.File,
			StorageClass: "gold",
		}
		volTxn := &VolumeTransaction{
			Config: &volConfig,
			Op:     AddVolume,
		}
		if err = p.AddVolumeTransaction(volTxn); err != nil {
			t.Error(err.Error())
			t.FailNow()
		}
	}

	// Retrieving volume transactions
	volTxns, err := p.GetVolumeTransactions()
	if err != nil {
		t.Error(err.Error())
		t.FailNow()
	}
	if len(volTxns) != 5 {
		t.Error("Did n't retrieve all volume transactions!")
	}

	// Getting and Deleting volume transactions
	for _, v := range volTxns {
		txn, err := p.GetExistingVolumeTransaction(v)
		if err != nil {
			t.Errorf("Unable to get existing volume transaction %s:  %v",
				v.Config.Name, err)
		}
		if !reflect.DeepEqual(txn, v) {
			t.Errorf("Got incorrect volume transaction for %s (got %s)",
				v.Config.Name, txn.Config.Name)
		}
		p.DeleteVolumeTransaction(v)
	}
	volTxns, err = p.GetVolumeTransactions()
	if err != nil {
		t.Error(err.Error())
		t.FailNow()
	}
	if len(volTxns) != 0 {
		t.Error("Didn't delete all volume transactions!")
	}
}

func TestEtcdv2DuplicateVolumeTransaction(t *testing.T) {
	firstTxn := &VolumeTransaction{
		Config: &storage.VolumeConfig{
			Version:      string(config.OrchestratorAPIVersion),
			Name:         "testVol",
			Size:         "1 GB",
			Protocol:     config.File,
			StorageClass: "gold",
		},
		Op: AddVolume,
	}
	secondTxn := &VolumeTransaction{
		Config: &storage.VolumeConfig{
			Version:      string(config.OrchestratorAPIVersion),
			Name:         "testVol",
			Size:         "1 GB",
			Protocol:     config.File,
			StorageClass: "silver",
		},
		Op: AddVolume,
	}
	p, err := NewEtcdClientV2(*etcdV2)
	if err != nil {
		t.Fatal("Unable to create persistent store client:  ", err)
	}
	if err = p.AddVolumeTransaction(firstTxn); err != nil {
		t.Error("Unable to add first volume transaction: ", err)
	}
	if err = p.AddVolumeTransaction(secondTxn); err != nil {
		t.Error("Unable to add second volume transaction: ", err)
	}
	volTxns, err := p.GetVolumeTransactions()
	if err != nil {
		t.Fatal("Unable to retrieve volume transactions: ", err)
	}
	if len(volTxns) != 1 {
		t.Errorf("Expected one volume transaction; got %d", len(volTxns))
	} else if volTxns[0].Config.StorageClass != "silver" {
		t.Errorf("Vol transaction not updated.  Expected storage class silver;"+
			" got %s.", volTxns[0].Config.StorageClass)
	}
	for i, volTxn := range volTxns {
		if err = p.DeleteVolumeTransaction(volTxn); err != nil {
			t.Errorf("Unable to delete volume transaction %d:  %v", i+1, err)
		}
	}
}

func TestEtcdv2AddSolidFireBackend(t *testing.T) {
	p, err := NewEtcdClientV2(*etcdV2)

	commonConfigDefaults := drivers.CommonStorageDriverConfigDefaults{Size: "1GiB"}
	solidfireConfigDefaults := drivers.SolidfireStorageDriverConfigDefaults{
		CommonStorageDriverConfigDefaults: commonConfigDefaults,
	}

	sfConfig := drivers.SolidfireStorageDriverConfig{
		CommonStorageDriverConfig: &drivers.CommonStorageDriverConfig{
			StorageDriverName: drivers.SolidfireSANStorageDriverName,
		},
		SolidfireStorageDriverPool: drivers.SolidfireStorageDriverPool{
			Labels: map[string]string{"performance": "bronze", "cost": "1"},
			Region: "us-east",
			Zone:   "us-east-1",
			SolidfireStorageDriverConfigDefaults: solidfireConfigDefaults,
		},
		TenantName: "docker",
	}
	sfBackend := &storage.Backend{
		Driver: &solidfire.SANStorageDriver{
			Config: sfConfig,
		},
		Name: "solidfire" + "_10.0.0.9",
	}
	if err = p.AddBackend(sfBackend); err != nil {
		t.Fatal(err.Error())
	}

	var retrievedConfig drivers.SolidfireStorageDriverConfig
	recoveredBackend, err := p.GetBackend(sfBackend.Name)
	if err != nil {
		t.Error(err.Error())
	}
	configJSON, err := recoveredBackend.MarshalConfig()
	if err != nil {
		t.Fatal("Unable to marshal recovered backend config: ", err)
	}
	if err = json.Unmarshal([]byte(configJSON), &retrievedConfig); err != nil {
		t.Error("Unable to unmarshal backend into ontap configuration: ", err)
	} else if retrievedConfig.Size != sfConfig.Size {
		t.Errorf("Backend state doesn't match: %v != %v",
			retrievedConfig.Size, sfConfig.Size)
	}

	if err = p.DeleteBackend(sfBackend); err != nil {
		t.Fatal(err.Error())
	}
}

func TestEtcdv2AddStorageClass(t *testing.T) {
	p, err := NewEtcdClientV2(*etcdV2)
	bronzeConfig := &storageclass.Config{
		Name:            "bronze",
		Attributes:      make(map[string]storageattribute.Request),
		AdditionalPools: make(map[string][]string),
	}
	bronzeConfig.Attributes["media"] = storageattribute.NewStringRequest("hdd")
	bronzeConfig.AdditionalPools["ontapnas_10.0.207.101"] = []string{"aggr1"}
	bronzeConfig.AdditionalPools["ontapsan_10.0.207.103"] = []string{"aggr1"}
	bronzeClass := storageclass.New(bronzeConfig)

	if err := p.AddStorageClass(bronzeClass); err != nil {
		t.Fatal(err.Error())
	}

	retrievedSC, err := p.GetStorageClass(bronzeConfig.Name)
	if err != nil {
		t.Error(err.Error())
	}

	sc := storageclass.NewFromPersistent(retrievedSC)
	// Validating correct retrieval of storage class attributes
	retrievedAttrs := sc.GetAttributes()
	if _, ok := retrievedAttrs["media"]; !ok {
		t.Error("Could not find storage class attribute!")
	}
	if retrievedAttrs["media"].Value().(string) != "hdd" || retrievedAttrs["media"].GetType() != "string" {
		t.Error("Could not retrieve storage class attribute!")
	}

	// Validating correct retrieval of storage pools in a storage class
	backendVCs := sc.GetAdditionalStoragePools()
	for k, v := range bronzeConfig.AdditionalPools {
		if vcs, ok := backendVCs[k]; !ok {
			t.Errorf("Could not find backend %s for the storage class!", k)
		} else {
			if len(vcs) != len(v) {
				t.Errorf("Could not retrieve the correct number of storage pools!")
			} else {
				for i, vc := range vcs {
					if vc != v[i] {
						t.Errorf("Could not find storage pools %s for the storage class!", vc)
					}
				}
			}
		}
	}

	if err := p.DeleteStorageClass(bronzeClass); err != nil {
		t.Fatal(err.Error())
	}
}

func TestEtcdv2AddGetDeleteNode(t *testing.T) {
	p, err := NewEtcdClientV2(*etcdV2)
	initialNode := &utils.Node{
		Name: "testNode",
		IQN:  "myIQN",
		IPs:  []string{"1.1.1.1", "2.2.2.2"},
	}

	if err := p.AddOrUpdateNode(initialNode); err != nil {
		t.Fatal(err.Error())
	}

	retrievedNode, err := p.GetNode(initialNode.Name)
	if err != nil {
		t.Error(err.Error())
	}

	if !reflect.DeepEqual(retrievedNode, initialNode) {
		t.Errorf("Incorrect node added to persistence; expected %v, found %v", initialNode, retrievedNode)
	}

	if err := p.DeleteNode(initialNode); err != nil {
		t.Fatal(err.Error())
	}
}

func TestEtcdv2UpdateNode(t *testing.T) {
	p, err := NewEtcdClientV2(*etcdV2)
	initialNode := &utils.Node{
		Name: "testNode",
		IQN:  "myIQN",
		IPs:  []string{"1.1.1.1", "2.2.2.2"},
	}

	if err := p.AddOrUpdateNode(initialNode); err != nil {
		t.Fatal(err.Error())
	}
	defer p.DeleteNode(initialNode)

	updateNode := &utils.Node{
		Name: "testNode",
		IQN:  "myOtherIQN",
		IPs:  []string{"3.3.3.3", "4.4.4.4"},
	}

	if err := p.AddOrUpdateNode(updateNode); err != nil {
		t.Fatal(err.Error())
	}

	retrievedNode, err := p.GetNode(initialNode.Name)
	if err != nil {
		t.Error(err.Error())
	}

	if !reflect.DeepEqual(retrievedNode, updateNode) {
		t.Errorf("Incorrect node added to persistence; expected %v, found %v", updateNode, retrievedNode)
	}
}

func TestEtcdv2GetNodes(t *testing.T) {
	p, err := NewEtcdClientV2(*etcdV2)
	initialNodes := make([]*utils.Node, 0)
	initialNode1 := &utils.Node{
		Name: "testNode",
		IQN:  "myIQN",
		IPs:  []string{"1.1.1.1", "2.2.2.2"},
	}

	if err := p.AddOrUpdateNode(initialNode1); err != nil {
		t.Fatal(err.Error())
	}
	defer p.DeleteNode(initialNode1)
	initialNodes = append(initialNodes, initialNode1)

	initialNode2 := &utils.Node{
		Name: "testNode2",
		IQN:  "myOtherIQN",
		IPs:  []string{"3.3.3.3", "4.4.4.4"},
	}

	if err := p.AddOrUpdateNode(initialNode2); err != nil {
		t.Fatal(err.Error())
	}
	defer p.DeleteNode(initialNode2)
	initialNodes = append(initialNodes, initialNode2)

	retrievedNodes, err := p.GetNodes()
	if err != nil {
		t.Error(err.Error())
	}

	if !unorderedNodeSlicesEqual(retrievedNodes, initialNodes) {
		t.Error("Incorrect nodes returned from persistence")
	}
}

func TestEtcdv2AddGetSnapshot(t *testing.T) {
	p, _ := NewEtcdClientV2(*etcdV2)

	// Adding a volume
	nfsServerConfig := drivers.OntapStorageDriverConfig{
		CommonStorageDriverConfig: &drivers.CommonStorageDriverConfig{
			StorageDriverName: drivers.OntapNASStorageDriverName,
		},
		ManagementLIF: "10.0.0.4",
		DataLIF:       "10.0.0.100",
		SVM:           "svm1",
		Username:      "admin",
		Password:      "netapp",
	}
	nfsServer := &storage.Backend{
		Driver: &ontap.NASStorageDriver{
			Config: nfsServerConfig,
		},
		Name:        "nfs_server-" + nfsServerConfig.ManagementLIF,
		BackendUUID: uuid.New().String(),
	}
	vol1Config := storage.VolumeConfig{
		Version:      string(config.OrchestratorAPIVersion),
		Name:         "vol1",
		Size:         "1GB",
		Protocol:     config.File,
		StorageClass: "gold",
	}
	vol1 := &storage.Volume{
		Config:      &vol1Config,
		BackendUUID: nfsServer.BackendUUID,
		Pool:        storagePool,
	}
	err := p.AddVolume(vol1)
	if err != nil {
		t.Error(err.Error())
		t.FailNow()
	}

	// Adding a snapshot
	snap1config := &storage.SnapshotConfig{
		Version:            config.OrchestratorAPIVersion,
		Name:               "snapA",
		InternalName:       "snapA",
		VolumeName:         "vol1",
		VolumeInternalName: "trident_vol1",
	}
	snapA := &storage.Snapshot{
		Config:    snap1config,
		Created:   time.Now().UTC().Format(storage.SnapshotTimestampFormat),
		SizeBytes: 1000000000,
	}
	err = p.AddSnapshot(snapA)
	if err != nil {
		t.Error(err.Error())
		t.FailNow()
	}

	// Getting a snapshot
	var recoveredSnapshot *storage.SnapshotPersistent
	recoveredSnapshot, err = p.GetSnapshot(snapA.Config.VolumeName, snapA.Config.Name)
	if err != nil {
		t.Error(err.Error())
	}
	if !reflect.DeepEqual(snapA.ConstructPersistent(), recoveredSnapshot) {
		t.Error("Recovered snapshot does not match!")
	}

	// Deleting a snapshot
	err = p.DeleteSnapshot(snapA)
	if err != nil {
		t.Error(err.Error())
	}

	recoveredSnapshot, err = p.GetSnapshot(snapA.Config.VolumeName, snapA.Config.Name)
	if err != nil && !MatchKeyNotFoundErr(err) {
		t.Error(err.Error())
		t.FailNow()
	} else if recoveredSnapshot != nil {
		t.Error("Didn't delete snapshot!")
	}

	// Deleting a volume
	err = p.DeleteVolume(vol1)
	if err != nil {
		t.Error(err.Error())
	}
}

func TestEtcdv2AddGetSnapshots(t *testing.T) {
	p, _ := NewEtcdClientV2(*etcdV2)

	// Adding a volume
	nfsServerConfig := drivers.OntapStorageDriverConfig{
		CommonStorageDriverConfig: &drivers.CommonStorageDriverConfig{
			StorageDriverName: drivers.OntapNASStorageDriverName,
		},
		ManagementLIF: "10.0.0.4",
		DataLIF:       "10.0.0.100",
		SVM:           "svm1",
		Username:      "admin",
		Password:      "netapp",
	}
	nfsServer := &storage.Backend{
		Driver: &ontap.NASStorageDriver{
			Config: nfsServerConfig,
		},
		Name:        "nfs_server-" + nfsServerConfig.ManagementLIF,
		BackendUUID: uuid.New().String(),
	}
	vol1Config := storage.VolumeConfig{
		Version:      string(config.OrchestratorAPIVersion),
		Name:         "vol1",
		Size:         "1GB",
		Protocol:     config.File,
		StorageClass: "gold",
	}
	vol1 := &storage.Volume{
		Config:      &vol1Config,
		BackendUUID: nfsServer.BackendUUID,
		Pool:        storagePool,
	}
	err := p.AddVolume(vol1)
	if err != nil {
		t.Error(err.Error())
		t.FailNow()
	}

	// Adding a snapshot
	snap1config := &storage.SnapshotConfig{
		Version:            config.OrchestratorAPIVersion,
		Name:               "snap1",
		InternalName:       "snap1",
		VolumeName:         "vol1",
		VolumeInternalName: "trident_vol1",
	}
	snap1 := &storage.Snapshot{
		Config:    snap1config,
		Created:   time.Now().UTC().Format(storage.SnapshotTimestampFormat),
		SizeBytes: 1000000000,
	}
	err = p.AddSnapshot(snap1)
	if err != nil {
		t.Error(err.Error())
		t.FailNow()
	}

	// Adding another snapshot
	snap2config := &storage.SnapshotConfig{
		Version:            config.OrchestratorAPIVersion,
		Name:               "snap2",
		InternalName:       "snap2",
		VolumeName:         "vol1",
		VolumeInternalName: "trident_vol1",
	}
	snap2 := &storage.Snapshot{
		Config:    snap2config,
		Created:   time.Now().UTC().Format(storage.SnapshotTimestampFormat),
		SizeBytes: 2000000000,
	}
	err = p.AddSnapshot(snap2)
	if err != nil {
		t.Error(err.Error())
		t.FailNow()
	}

	// Getting snapshots
	var recoveredSnapshots []*storage.SnapshotPersistent
	recoveredSnapshots, err = p.GetSnapshots()
	if err != nil {
		t.Error(err.Error())
	}
	for _, snap := range recoveredSnapshots {
		switch snap.Config.Name {
		case "snap1":
			if !reflect.DeepEqual(snap1.ConstructPersistent(), snap) {
				t.Error("Recovered snapshot does not match!")
			}
		case "snap2":
			if !reflect.DeepEqual(snap2.ConstructPersistent(), snap) {
				t.Error("Recovered snapshot does not match!")
			}
		}
	}

	// Deleting snapshots
	err = p.DeleteSnapshots()
	if err != nil {
		t.Error(err.Error())
	}

	recoveredSnapshots, err = p.GetSnapshots()
	if err != nil {
		t.Error(err.Error())
		t.FailNow()
	} else if recoveredSnapshots != nil && len(recoveredSnapshots) != 0 {
		t.Error("Didn't delete snapshots!")
	}

	// Deleting a volume
	err = p.DeleteVolume(vol1)
	if err != nil {
		t.Error(err.Error())
	}
}
