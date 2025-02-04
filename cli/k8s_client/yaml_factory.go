// Copyright 2019 NetApp, Inc. All Rights Reserved.

package k8sclient

import (
	"fmt"
	"strings"

	"github.com/netapp/trident/utils"
)

func GetNamespaceYAML(namespace string) string {
	return strings.Replace(namespaceYAMLTemplate, "{NAMESPACE}", namespace, 1)
}

const namespaceYAMLTemplate = `---
apiVersion: v1
kind: Namespace
metadata:
  name: {NAMESPACE}
`

func GetServiceAccountYAML(csi bool) string {

	if csi {
		return strings.Replace(serviceAccountYAML, "{NAME}", "trident-csi", 1)
	} else {
		return strings.Replace(serviceAccountYAML, "{NAME}", "trident", 1)
	}
}

const serviceAccountYAML = `---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {NAME}
`

func GetClusterRoleYAML(flavor OrchestratorFlavor, csi bool) string {

	var clusterRoleYAML string

	if csi {
		clusterRoleYAML = clusterRoleCSIYAMLTemplate
	} else {
		clusterRoleYAML = clusterRoleYAMLTemplate
	}

	switch flavor {
	case FlavorOpenShift:
		clusterRoleYAML = strings.Replace(clusterRoleYAML, "{API_VERSION}", "authorization.openshift.io/v1", 1)
	default:
		fallthrough
	case FlavorKubernetes:
		clusterRoleYAML = strings.Replace(clusterRoleYAML, "{API_VERSION}", "rbac.authorization.k8s.io/v1", 1)
	}

	return clusterRoleYAML
}

const clusterRoleYAMLTemplate = `---
kind: ClusterRole
apiVersion: {API_VERSION}
metadata:
  name: trident
rules:
  - apiGroups: [""]
    resources: ["persistentvolumes", "persistentvolumeclaims"]
    verbs: ["get", "list", "watch", "create", "delete", "update", "patch"]
  - apiGroups: [""]
    resources: ["persistentvolumeclaims/status"]
    verbs: ["update", "patch"]
  - apiGroups: ["storage.k8s.io"]
    resources: ["storageclasses"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["watch", "create", "update", "patch"]
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get", "list", "watch", "create", "delete"]
  - apiGroups: ["apiextensions.k8s.io"]
    resources: ["customresourcedefinitions"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["trident.netapp.io"]
    resources: ["tridentversions", "tridentbackends", "tridentstorageclasses", "tridentvolumes","tridentnodes", "tridenttransactions", "tridentsnapshots"]
    verbs: ["get", "list", "watch", "create", "delete", "update", "patch"]
`

const clusterRoleCSIYAMLTemplate = `---
kind: ClusterRole
apiVersion: {API_VERSION}
metadata:
  name: trident-csi
rules:
  - apiGroups: [""]
    resources: ["persistentvolumes", "persistentvolumeclaims"]
    verbs: ["get", "list", "watch", "create", "delete", "update", "patch"]
  - apiGroups: [""]
    resources: ["persistentvolumeclaims/status"]
    verbs: ["update", "patch"]
  - apiGroups: ["storage.k8s.io"]
    resources: ["storageclasses"]
    verbs: ["get", "list", "watch", "create", "delete", "update", "patch"]
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["get", "list", "watch", "create", "update", "patch"]
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get", "list", "watch", "create", "delete"]
  - apiGroups: [""]
    resources: ["nodes"]
    verbs: ["get", "list", "watch", "update"]
  - apiGroups: ["storage.k8s.io"]
    resources: ["volumeattachments"]
    verbs: ["get", "list", "watch", "update"]
  - apiGroups: ["snapshot.storage.k8s.io"]
    resources: ["volumesnapshots", "volumesnapshotclasses"]
    verbs: ["get", "list", "watch", "update", "patch"]
  - apiGroups: ["snapshot.storage.k8s.io"]
    resources: ["volumesnapshots/status"]
    verbs: ["update", "patch"]
  - apiGroups: ["snapshot.storage.k8s.io"]
    resources: ["volumesnapshotcontents"]
    verbs: ["get", "list", "watch", "create", "delete", "update", "patch"]
  - apiGroups: ["csi.storage.k8s.io"]
    resources: ["csidrivers", "csinodeinfos"]
    verbs: ["get", "list", "watch", "create", "delete", "update", "patch"]
  - apiGroups: ["storage.k8s.io"]
    resources: ["csidrivers", "csinodes"]
    verbs: ["get", "list", "watch", "create", "delete", "update", "patch"]
  - apiGroups: ["apiextensions.k8s.io"]
    resources: ["customresourcedefinitions"]
    verbs: ["get", "list", "watch", "create", "delete", "update", "patch"]
  - apiGroups: ["trident.netapp.io"]
    resources: ["tridentversions", "tridentbackends", "tridentstorageclasses", "tridentvolumes","tridentnodes", "tridenttransactions", "tridentsnapshots"]
    verbs: ["get", "list", "watch", "create", "delete", "update", "patch"]
`

func GetClusterRoleBindingYAML(namespace string, flavor OrchestratorFlavor, csi bool) string {

	var name string
	var crbYAML string

	if csi {
		name = "trident-csi"
	} else {
		name = "trident"
	}

	switch flavor {
	case FlavorOpenShift:
		crbYAML = clusterRoleBindingOpenShiftYAMLTemplate
	default:
		fallthrough
	case FlavorKubernetes:
		crbYAML = clusterRoleBindingKubernetesV1YAMLTemplate
	}

	crbYAML = strings.Replace(crbYAML, "{NAMESPACE}", namespace, 1)
	crbYAML = strings.Replace(crbYAML, "{NAME}", name, -1)
	return crbYAML
}

const clusterRoleBindingOpenShiftYAMLTemplate = `---
kind: ClusterRoleBinding
apiVersion: authorization.openshift.io/v1
metadata:
  name: {NAME}
subjects:
  - kind: ServiceAccount
    name: {NAME}
    namespace: {NAMESPACE}
roleRef:
  name: {NAME}
`

const clusterRoleBindingKubernetesV1YAMLTemplate = `---
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: {NAME}
subjects:
  - kind: ServiceAccount
    name: {NAME}
    namespace: {NAMESPACE}
roleRef:
  kind: ClusterRole
  name: {NAME}
  apiGroup: rbac.authorization.k8s.io
`

func GetDeploymentYAML(tridentImage, label string, debug bool) string {

	var debugLine string
	if debug {
		debugLine = "- -debug"
	} else {
		debugLine = "#- -debug"
	}

	deploymentYAML := strings.Replace(deploymentYAMLTemplate, "{TRIDENT_IMAGE}", tridentImage, 1)
	deploymentYAML = strings.Replace(deploymentYAML, "{DEBUG}", debugLine, 1)
	deploymentYAML = strings.Replace(deploymentYAML, "{LABEL}", label, -1)
	return deploymentYAML
}

const deploymentYAMLTemplate = `---
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: trident
  labels:
    app: {LABEL}
spec:
  replicas: 1
  template:
    metadata:
      labels:
        app: {LABEL}
    spec:
      serviceAccount: trident
      containers:
      - name: trident-main
        image: {TRIDENT_IMAGE}
        command:
        - /usr/local/bin/trident_orchestrator
        args:
        - "--crd_persistence"
        - "--k8s_pod"
        {DEBUG}
        livenessProbe:
          exec:
            command:
            - tridentctl
            - -s
            - 127.0.0.1:8000
            - get
            - backend
          failureThreshold: 2
          initialDelaySeconds: 120
          periodSeconds: 120
          timeoutSeconds: 90
`

func GetCSIServiceYAML(label string) string {

	serviceYAML := strings.Replace(serviceYAMLTemplate, "{LABEL}", label, -1)
	return serviceYAML
}

const serviceYAMLTemplate = `---
apiVersion: v1
kind: Service
metadata:
  name: trident-csi
  labels:
    app: {LABEL}
spec:
  selector:
    app: {LABEL}
  ports:
    - protocol: TCP
      port: 34571
      targetPort: 8443
`

func GetCSIDeploymentYAML(tridentImage, label string, debug bool, version *utils.Version) string {

	var debugLine string
	if debug {
		debugLine = "- -debug"
	} else {
		debugLine = "#- -debug"
	}

	var deploymentYAML string
	if version.MajorVersion() == 1 && version.MinorVersion() == 13 {
		deploymentYAML = csiDeployment113YAMLTemplate
	} else {
		deploymentYAML = csiDeployment114YAMLTemplate
	}

	deploymentYAML = strings.Replace(deploymentYAML, "{TRIDENT_IMAGE}", tridentImage, 1)
	deploymentYAML = strings.Replace(deploymentYAML, "{DEBUG}", debugLine, 1)
	deploymentYAML = strings.Replace(deploymentYAML, "{LABEL}", label, -1)
	return deploymentYAML
}

const csiDeployment113YAMLTemplate = `---
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: trident-csi
  labels:
    app: {LABEL}
spec:
  replicas: 1
  strategy:
    type: Recreate
  template:
    metadata:
      labels:
        app: {LABEL}
    spec:
      serviceAccount: trident-csi
      containers:
      - name: trident-main
        image: {TRIDENT_IMAGE}
        ports:
        - containerPort: 8443
        command:
        - /usr/local/bin/trident_orchestrator
        args:
        - "--crd_persistence"
        - "--k8s_pod"
        - "--https_rest"
        - "--https_port=8443"
        - "--csi_node_name=$(KUBE_NODE_NAME)"
        - "--csi_endpoint=$(CSI_ENDPOINT)"
        - "--csi_role=controller"
        {DEBUG}
        livenessProbe:
          exec:
            command:
            - tridentctl
            - -s
            - 127.0.0.1:8000
            - get
            - backend
          failureThreshold: 2
          initialDelaySeconds: 120
          periodSeconds: 120
          timeoutSeconds: 90
        env:
        - name: KUBE_NODE_NAME
          valueFrom:
            fieldRef:
              apiVersion: v1
              fieldPath: spec.nodeName
        - name: CSI_ENDPOINT
          value: unix://plugin/csi.sock
        volumeMounts:
        - name: socket-dir
          mountPath: /plugin
        - name: certs
          mountPath: /certs
          readOnly: true
      - name: csi-provisioner
        image: quay.io/k8scsi/csi-provisioner:v1.0.1
        args:
        - "--v=9"
        - "--connection-timeout=24h"
        - "--csi-address=$(ADDRESS)"
        env:
        - name: ADDRESS
          value: /var/lib/csi/sockets/pluginproxy/csi.sock
        volumeMounts:
        - name: socket-dir
          mountPath: /var/lib/csi/sockets/pluginproxy/
      - name: csi-attacher
        image: quay.io/k8scsi/csi-attacher:v1.0.1
        args:
        - "--v=9"
        - "--connection-timeout=24h"
        - "--timeout=60s"
        - "--csi-address=$(ADDRESS)"
        env:
        - name: ADDRESS
          value: /var/lib/csi/sockets/pluginproxy/csi.sock
        volumeMounts:
        - name: socket-dir
          mountPath: /var/lib/csi/sockets/pluginproxy/
      - name: csi-snapshotter
        image: quay.io/k8scsi/csi-snapshotter:v1.0.1
        args:
        - "--v=9"
        - "--connection-timeout=24h"
        - "--csi-address=$(ADDRESS)"
        env:
        - name: ADDRESS
          value: /var/lib/csi/sockets/pluginproxy/csi.sock
        volumeMounts:
        - name: socket-dir
          mountPath: /var/lib/csi/sockets/pluginproxy/
      - name: csi-cluster-driver-registrar
        image: quay.io/k8scsi/csi-cluster-driver-registrar:v1.0.1
        args:
        - "--v=9"
        - "--connection-timeout=24h"
        - "--csi-address=$(ADDRESS)"
        env:
        - name: ADDRESS
          value: /var/lib/csi/sockets/pluginproxy/csi.sock
        volumeMounts:
        - name: socket-dir
          mountPath: /var/lib/csi/sockets/pluginproxy/
      volumes:
      - name: socket-dir
        emptyDir:
      - name: certs
        secret:
          secretName: trident-csi
`

const csiDeployment114YAMLTemplate = `---
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: trident-csi
  labels:
    app: {LABEL}
spec:
  replicas: 1
  strategy:
    type: Recreate
  template:
    metadata:
      labels:
        app: {LABEL}
    spec:
      serviceAccount: trident-csi
      containers:
      - name: trident-main
        image: {TRIDENT_IMAGE}
        ports:
        - containerPort: 8443
        command:
        - /usr/local/bin/trident_orchestrator
        args:
        - "--crd_persistence"
        - "--k8s_pod"
        - "--https_rest"
        - "--https_port=8443"
        - "--csi_node_name=$(KUBE_NODE_NAME)"
        - "--csi_endpoint=$(CSI_ENDPOINT)"
        - "--csi_role=controller"
        {DEBUG}
        livenessProbe:
          exec:
            command:
            - tridentctl
            - -s
            - 127.0.0.1:8000
            - get
            - backend
          failureThreshold: 2
          initialDelaySeconds: 120
          periodSeconds: 120
          timeoutSeconds: 90
        env:
        - name: KUBE_NODE_NAME
          valueFrom:
            fieldRef:
              apiVersion: v1
              fieldPath: spec.nodeName
        - name: CSI_ENDPOINT
          value: unix://plugin/csi.sock
        volumeMounts:
        - name: socket-dir
          mountPath: /plugin
        - name: certs
          mountPath: /certs
          readOnly: true
      - name: csi-provisioner
        image: quay.io/k8scsi/csi-provisioner:v1.2.1
        args:
        - "--v=9"
        - "--timeout=300s"
        - "--csi-address=$(ADDRESS)"
        env:
        - name: ADDRESS
          value: /var/lib/csi/sockets/pluginproxy/csi.sock
        volumeMounts:
        - name: socket-dir
          mountPath: /var/lib/csi/sockets/pluginproxy/
      - name: csi-attacher
        image: quay.io/k8scsi/csi-attacher:v1.1.1
        args:
        - "--v=9"
        - "--timeout=60s"
        - "--csi-address=$(ADDRESS)"
        env:
        - name: ADDRESS
          value: /var/lib/csi/sockets/pluginproxy/csi.sock
        volumeMounts:
        - name: socket-dir
          mountPath: /var/lib/csi/sockets/pluginproxy/
      - name: csi-snapshotter
        image: quay.io/k8scsi/csi-snapshotter:v1.2.0
        args:
        - "--v=9"
        - "--timeout=60s"
        - "--csi-address=$(ADDRESS)"
        env:
        - name: ADDRESS
          value: /var/lib/csi/sockets/pluginproxy/csi.sock
        volumeMounts:
        - name: socket-dir
          mountPath: /var/lib/csi/sockets/pluginproxy/
      volumes:
      - name: socket-dir
        emptyDir:
      - name: certs
        secret:
          secretName: trident-csi
`

func GetCSIDaemonSetYAML(tridentImage, label string, debug bool, version *utils.Version) string {

	var debugLine string

	if debug {
		debugLine = "- -debug"
	} else {
		debugLine = "#- -debug"
	}

	var daemonSetYAML string
	if version.MajorVersion() == 1 && version.MinorVersion() == 13 {
		daemonSetYAML = daemonSet113YAMLTemplate
	} else {
		daemonSetYAML = daemonSet114YAMLTemplate
	}

	daemonSetYAML = strings.Replace(daemonSetYAML, "{TRIDENT_IMAGE}", tridentImage, 1)
	daemonSetYAML = strings.Replace(daemonSetYAML, "{LABEL}", label, -1)
	daemonSetYAML = strings.Replace(daemonSetYAML, "{DEBUG}", debugLine, 1)
	return daemonSetYAML
}

const daemonSet113YAMLTemplate = `---
apiVersion: apps/v1beta2
kind: DaemonSet
metadata:
  name: trident-csi
  labels:
    app: {LABEL}
spec:
  selector:
    matchLabels:
      app: {LABEL}
  template:
    metadata:
      labels:
        app: {LABEL}
    spec:
      serviceAccount: trident-csi
      hostNetwork: true
      hostIPC: true
      dnsPolicy: ClusterFirstWithHostNet
      containers:
      - name: trident-main
        securityContext:
          privileged: true
          capabilities:
            add: ["SYS_ADMIN"]
          allowPrivilegeEscalation: true
        image: {TRIDENT_IMAGE}
        command:
        - /usr/local/bin/trident_orchestrator
        args:
        - "--no_persistence"
        - "--rest=false"
        - "--csi_node_name=$(KUBE_NODE_NAME)"
        - "--csi_endpoint=$(CSI_ENDPOINT)"
        - "--csi_role=node"
        {DEBUG}
        env:
        - name: KUBE_NODE_NAME
          valueFrom:
            fieldRef:
              apiVersion: v1
              fieldPath: spec.nodeName
        - name: CSI_ENDPOINT
          value: unix://plugin/csi.sock
        - name: PATH
          value: /netapp:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
        volumeMounts:
        - name: plugin-dir
          mountPath: /plugin
        - name: plugins-mount-dir
          mountPath: /var/lib/kubelet/plugins
        - name: pods-mount-dir
          mountPath: /var/lib/kubelet/pods
          mountPropagation: "Bidirectional"
        - name: dev-dir
          mountPath: /dev
        - name: sys-dir
          mountPath: /sys
        - name: host-dir
          mountPath: /host
          mountPropagation: "Bidirectional"
        - name: certs
          mountPath: /certs
          readOnly: true
      - name: driver-registrar
        image: quay.io/k8scsi/csi-node-driver-registrar:v1.0.2
        args:
        - "--v=9"
        - "--connection-timeout=24h"
        - "--csi-address=$(ADDRESS)"
        - "--kubelet-registration-path=$(REGISTRATION_PATH)"
        env:
        - name: ADDRESS
          value: /plugin/csi.sock
        - name: REGISTRATION_PATH
          value: "/var/lib/kubelet/plugins/csi.trident.netapp.io/csi.sock"
        - name: KUBE_NODE_NAME
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
        volumeMounts:
        - name: plugin-dir
          mountPath: /plugin
        - name: registration-dir
          mountPath: /registration
      volumes:
      - name: plugin-dir
        hostPath:
          path: /var/lib/kubelet/plugins/csi.trident.netapp.io/
          type: DirectoryOrCreate
      - name: registration-dir
        hostPath:
          path: /var/lib/kubelet/plugins_registry/
          type: Directory
      - name: plugins-mount-dir
        hostPath:
          path: /var/lib/kubelet/plugins
          type: DirectoryOrCreate
      - name: pods-mount-dir
        hostPath:
          path: /var/lib/kubelet/pods
          type: DirectoryOrCreate
      - name: dev-dir
        hostPath:
          path: /dev
          type: Directory
      - name: sys-dir
        hostPath:
          path: /sys
          type: Directory
      - name: host-dir
        hostPath:
          path: /
          type: Directory
      - name: certs
        secret:
          secretName: trident-csi
`

const daemonSet114YAMLTemplate = `---
apiVersion: apps/v1beta2
kind: DaemonSet
metadata:
  name: trident-csi
  labels:
    app: {LABEL}
spec:
  selector:
    matchLabels:
      app: {LABEL}
  template:
    metadata:
      labels:
        app: {LABEL}
    spec:
      serviceAccount: trident-csi
      hostNetwork: true
      hostIPC: true
      dnsPolicy: ClusterFirstWithHostNet
      containers:
      - name: trident-main
        securityContext:
          privileged: true
          capabilities:
            add: ["SYS_ADMIN"]
          allowPrivilegeEscalation: true
        image: {TRIDENT_IMAGE}
        command:
        - /usr/local/bin/trident_orchestrator
        args:
        - "--no_persistence"
        - "--rest=false"
        - "--csi_node_name=$(KUBE_NODE_NAME)"
        - "--csi_endpoint=$(CSI_ENDPOINT)"
        - "--csi_role=node"
        {DEBUG}
        env:
        - name: KUBE_NODE_NAME
          valueFrom:
            fieldRef:
              apiVersion: v1
              fieldPath: spec.nodeName
        - name: CSI_ENDPOINT
          value: unix://plugin/csi.sock
        - name: PATH
          value: /netapp:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
        volumeMounts:
        - name: plugin-dir
          mountPath: /plugin
        - name: plugins-mount-dir
          mountPath: /var/lib/kubelet/plugins
        - name: pods-mount-dir
          mountPath: /var/lib/kubelet/pods
          mountPropagation: "Bidirectional"
        - name: dev-dir
          mountPath: /dev
        - name: sys-dir
          mountPath: /sys
        - name: host-dir
          mountPath: /host
          mountPropagation: "Bidirectional"
        - name: certs
          mountPath: /certs
          readOnly: true
      - name: driver-registrar
        image: quay.io/k8scsi/csi-node-driver-registrar:v1.1.0
        args:
        - "--v=9"
        - "--csi-address=$(ADDRESS)"
        - "--kubelet-registration-path=$(REGISTRATION_PATH)"
        env:
        - name: ADDRESS
          value: /plugin/csi.sock
        - name: REGISTRATION_PATH
          value: "/var/lib/kubelet/plugins/csi.trident.netapp.io/csi.sock"
        - name: KUBE_NODE_NAME
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
        volumeMounts:
        - name: plugin-dir
          mountPath: /plugin
        - name: registration-dir
          mountPath: /registration
      volumes:
      - name: plugin-dir
        hostPath:
          path: /var/lib/kubelet/plugins/csi.trident.netapp.io/
          type: DirectoryOrCreate
      - name: registration-dir
        hostPath:
          path: /var/lib/kubelet/plugins_registry/
          type: Directory
      - name: plugins-mount-dir
        hostPath:
          path: /var/lib/kubelet/plugins
          type: DirectoryOrCreate
      - name: pods-mount-dir
        hostPath:
          path: /var/lib/kubelet/pods
          type: DirectoryOrCreate
      - name: dev-dir
        hostPath:
          path: /dev
          type: Directory
      - name: sys-dir
        hostPath:
          path: /sys
          type: Directory
      - name: host-dir
        hostPath:
          path: /
          type: Directory
      - name: certs
        secret:
          secretName: trident-csi
`

func GetInstallerServiceAccountYAML() string {

	return installerServiceAccountYAML
}

const installerServiceAccountYAML = `---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: trident-installer
`

func GetInstallerClusterRoleYAML(flavor OrchestratorFlavor) string {
	switch flavor {
	case FlavorOpenShift:
		return installerClusterRoleOpenShiftYAML
	default:
		fallthrough
	case FlavorKubernetes:
		return installerClusterRoleKubernetesYAMLTemplate
	}
}

const installerClusterRoleOpenShiftYAML = `---
kind: ClusterRole
apiVersion: "authorization.openshift.io/v1"
metadata:
  name: trident-installer
rules:
  - apiGroups: [""]
    resources: ["namespaces", "pods", "pods/exec", "pods/log", "persistentvolumes", "persistentvolumeclaims", "persistentvolumeclaims/status", "secrets", "serviceaccounts", "services", "events", "nodes", "configmaps"]
    verbs: ["*"]
  - apiGroups: ["extensions"]
    resources: ["deployments", "daemonsets"]
    verbs: ["*"]
  - apiGroups: ["apps"]
    resources: ["statefulsets", daemonsets", "deployments"]
    verbs: ["*"]
  - apiGroups: ["authorization.openshift.io", "rbac.authorization.k8s.io"]
    resources: ["clusterroles", "clusterrolebindings"]
    verbs: ["*"]
  - apiGroups: ["storage.k8s.io"]
    resources: ["storageclasses", "volumeattachments"]
    verbs: ["*"]
  - apiGroups: ["security.openshift.io"]
    resources: ["securitycontextconstraints"]
    verbs: ["*"]
  - apiGroups: ["apiextensions.k8s.io"]
    resources: ["customresourcedefinitions"]
    verbs: ["*"]
  - apiGroups: ["trident.netapp.io"]
    resources: ["tridentversions", "tridentbackends", "tridentstorageclasses", "tridentvolumes","tridentnodes", "tridenttransactions", "tridentsnapshots"]
    verbs: ["*"]
`

const installerClusterRoleKubernetesYAMLTemplate = `---
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: trident-installer
rules:
  - apiGroups: [""]
    resources: ["namespaces", "pods", "pods/exec", "pods/log", "persistentvolumes", "persistentvolumeclaims", "persistentvolumeclaims/status", "secrets", "serviceaccounts", "services", "events", "nodes", "configmaps"]
    verbs: ["*"]
  - apiGroups: ["extensions"]
    resources: ["deployments", "daemonsets"]
    verbs: ["*"]
  - apiGroups: ["apps"]
    resources: ["statefulsets", "daemonsets", "deployments"]
    verbs: ["*"]
  - apiGroups: ["rbac.authorization.k8s.io"]
    resources: ["clusterroles", "clusterrolebindings"]
    verbs: ["*"]
  - apiGroups: ["storage.k8s.io"]
    resources: ["storageclasses", "volumeattachments", "csidrivers", "csinodes"]
    verbs: ["*"]
  - apiGroups: ["snapshot.storage.k8s.io"]
    resources: ["volumesnapshots", "volumesnapshotclasses", "volumesnapshotcontents"]
    verbs: ["*"]
  - apiGroups: ["apiextensions.k8s.io"]
    resources: ["customresourcedefinitions"]
    verbs: ["*"]
  - apiGroups: ["csi.storage.k8s.io"]
    resources: ["csidrivers", "csinodeinfos"]
    verbs: ["*"]
  - apiGroups: ["trident.netapp.io"]
    resources: ["tridentversions", "tridentbackends", "tridentstorageclasses", "tridentvolumes","tridentnodes", "tridenttransactions", "tridentsnapshots"]
    verbs: ["*"]
`

func GetInstallerClusterRoleBindingYAML(namespace string, flavor OrchestratorFlavor) string {

	var crbYAML string

	switch flavor {
	case FlavorOpenShift:
		crbYAML = installerClusterRoleBindingOpenShiftYAMLTemplate
	default:
		fallthrough
	case FlavorKubernetes:
		crbYAML = installerClusterRoleBindingKubernetesV1YAMLTemplate
	}

	crbYAML = strings.Replace(crbYAML, "{NAMESPACE}", namespace, 1)
	return crbYAML
}

const installerClusterRoleBindingOpenShiftYAMLTemplate = `---
kind: ClusterRoleBinding
apiVersion: authorization.openshift.io/v1
metadata:
  name: trident-installer
subjects:
  - kind: ServiceAccount
    name: trident-installer
    namespace: {NAMESPACE}
roleRef:
  name: trident-installer
`

const installerClusterRoleBindingKubernetesV1YAMLTemplate = `---
kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: trident-installer
subjects:
  - kind: ServiceAccount
    name: trident-installer
    namespace: {NAMESPACE}
roleRef:
  kind: ClusterRole
  name: trident-installer
  apiGroup: rbac.authorization.k8s.io
`

func GetMigratorPodYAML(pvcName, tridentImage, etcdImage, label string, csi bool, commandArgs []string) string {

	command := `["` + strings.Join(commandArgs, `", "`) + `"]`

	podYAML := strings.Replace(migratorPodYAMLTemplate, "{TRIDENT_IMAGE}", tridentImage, 1)
	podYAML = strings.Replace(podYAML, "{ETCD_IMAGE}", etcdImage, 1)
	podYAML = strings.Replace(podYAML, "{PVC_NAME}", pvcName, 1)
	podYAML = strings.Replace(podYAML, "{LABEL}", label, 1)
	podYAML = strings.Replace(podYAML, "{COMMAND}", command, 1)

	if csi {
		podYAML = strings.Replace(podYAML, "{SERVICE_ACCOUNT}", "trident-csi", 1)
	} else {
		podYAML = strings.Replace(podYAML, "{SERVICE_ACCOUNT}", "trident", 1)
	}

	return podYAML
}

const migratorPodYAMLTemplate = `---
apiVersion: v1
kind: Pod
metadata:
  name: trident-migrator
  labels:
    app: {LABEL}
spec:
  serviceAccount: {SERVICE_ACCOUNT}
  restartPolicy: Never
  containers:
  - name: trident-migrator
    image: {TRIDENT_IMAGE}
    command: {COMMAND}
  - name: etcd
    image: {ETCD_IMAGE}
    command:
    - /usr/local/bin/etcd
    args:
    - "--name=etcd1"
    - "--advertise-client-urls=http://127.0.0.1:8001"
    - "--listen-client-urls=http://127.0.0.1:8001"
    - "--initial-advertise-peer-urls=http://127.0.0.1:8002"
    - "--listen-peer-urls=http://127.0.0.1:8002"
    - "--data-dir=/var/etcd/data"
    - "--initial-cluster=etcd1=http://127.0.0.1:8002"
    volumeMounts:
    - name: etcd-vol
      mountPath: /var/etcd/data
  volumes:
  - name: etcd-vol
    persistentVolumeClaim:
      claimName: {PVC_NAME}
`

func GetInstallerPodYAML(label, tridentImage string, commandArgs []string) string {

	command := `["` + strings.Join(commandArgs, `", "`) + `"]`

	jobYAML := strings.Replace(installerPodTemplate, "{LABEL}", label, 1)
	jobYAML = strings.Replace(jobYAML, "{TRIDENT_IMAGE}", tridentImage, 1)
	jobYAML = strings.Replace(jobYAML, "{COMMAND}", command, 1)
	return jobYAML
}

const installerPodTemplate = `---
apiVersion: v1
kind: Pod
metadata:
  name: trident-installer
  labels:
    app: {LABEL}
spec:
  serviceAccount: trident-installer
  containers:
  - name: trident-installer
    image: {TRIDENT_IMAGE}
    workingDir: /
    command: {COMMAND}
    volumeMounts:
    - name: setup-dir
      mountPath: /setup
  restartPolicy: Never
  volumes:
  - name: setup-dir
    configMap:
      name: trident-installer
`

func GetUninstallerPodYAML(label, tridentImage string, commandArgs []string) string {

	command := `["` + strings.Join(commandArgs, `", "`) + `"]`

	jobYAML := strings.Replace(uninstallerPodTemplate, "{LABEL}", label, 1)
	jobYAML = strings.Replace(jobYAML, "{TRIDENT_IMAGE}", tridentImage, 1)
	jobYAML = strings.Replace(jobYAML, "{COMMAND}", command, 1)
	return jobYAML
}

const uninstallerPodTemplate = `---
apiVersion: v1
kind: Pod
metadata:
  name: trident-installer
  labels:
    app: {LABEL}
spec:
  serviceAccount: trident-installer
  containers:
  - name: trident-installer
    image: {TRIDENT_IMAGE}
    workingDir: /
    command: {COMMAND}
  restartPolicy: Never
`

func GetOpenShiftSCCQueryYAML(scc string) string {
	return strings.Replace(openShiftSCCQueryYAMLTemplate, "{SCC}", scc, 1)
}

const openShiftSCCQueryYAMLTemplate = `
---
kind: SecurityContextConstraints
apiVersion: security.openshift.io/v1
metadata:
  name: {SCC}
`

func GetSecretYAML(secretName, namespace, label string, secretData map[string]string) string {

	secretYAML := strings.Replace(secretYAMLTemplate, "{SECRET_NAME}", secretName, 1)
	secretYAML = strings.Replace(secretYAML, "{NAMESPACE}", namespace, 1)
	secretYAML = strings.Replace(secretYAML, "{LABEL}", label, 1)

	for key, value := range secretData {
		secretYAML += fmt.Sprintf("  %s: %s\n", key, value)
	}

	return secretYAML
}

const secretYAMLTemplate = `
apiVersion: v1
kind: Secret
metadata:
  name: {SECRET_NAME}
  namespace: {NAMESPACE}
  labels:
    app: {LABEL}
data:
`

func GetCRDsYAML() string {
	return customResourceDefinitionYAML
}

/*
kubectl delete crd tridentversions.trident.netapp.io --wait=false
kubectl delete crd tridentbackends.trident.netapp.io --wait=false
kubectl delete crd tridentstorageclasses.trident.netapp.io --wait=false
kubectl delete crd tridentvolumes.trident.netapp.io --wait=false
kubectl delete crd tridentnodes.trident.netapp.io --wait=false
kubectl delete crd tridenttransactions.trident.netapp.io --wait=false
kubectl delete crd tridentsnapshots.trident.netapp.io --wait=false

kubectl patch crd tridentversions.trident.netapp.io -p '{"metadata":{"finalizers": []}}' --type=merge
kubectl patch crd tridentbackends.trident.netapp.io -p '{"metadata":{"finalizers": []}}' --type=merge
kubectl patch crd tridentstorageclasses.trident.netapp.io -p '{"metadata":{"finalizers": []}}' --type=merge
kubectl patch crd tridentvolumes.trident.netapp.io -p '{"metadata":{"finalizers": []}}' --type=merge
kubectl patch crd tridentnodes.trident.netapp.io -p '{"metadata":{"finalizers": []}}' --type=merge
kubectl patch crd tridenttransactions.trident.netapp.io -p '{"metadata":{"finalizers": []}}' --type=merge
kubectl patch crd tridentsnapshots.trident.netapp.io -p '{"metadata":{"finalizers": []}}' --type=merge

kubectl delete crd tridentversions.trident.netapp.io
kubectl delete crd tridentbackends.trident.netapp.io
kubectl delete crd tridentstorageclasses.trident.netapp.io
kubectl delete crd tridentvolumes.trident.netapp.io
kubectl delete crd tridentnodes.trident.netapp.io
kubectl delete crd tridenttransactions.trident.netapp.io
kubectl delete crd tridentsnapshots.trident.netapp.io
*/

const customResourceDefinitionYAML = `
apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: tridentversions.trident.netapp.io
spec:
  group: trident.netapp.io
  version: v1
  versions:
    - name: v1
      served: true
      storage: true
  scope: Namespaced
  names:
    plural: tridentversions
    singular: tridentversion
    kind: TridentVersion
    shortNames:
    - tver
    - tversion
    categories:
    - trident
    - trident-internal
  additionalPrinterColumns:
    - name: Version
      type: string
      description: The Trident version
      priority: 0
      JSONPath: .trident_version
---
apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: tridentbackends.trident.netapp.io
spec:
  group: trident.netapp.io
  version: v1
  versions:
    - name: v1
      served: true
      storage: true
  scope: Namespaced
  names:
    plural: tridentbackends
    singular: tridentbackend
    kind: TridentBackend
    shortNames:
    - tbe
    - tbackend
    categories:
    - trident
    - trident-internal
  additionalPrinterColumns:
    - name: Backend
      type: string
      description: The backend name
      priority: 0
      JSONPath: .backendName
    - name: Backend UUID
      type: string
      description: The backend UUID
      priority: 0
      JSONPath: .backendUUID
---
apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: tridentstorageclasses.trident.netapp.io
spec:
  group: trident.netapp.io
  version: v1
  versions:
    - name: v1
      served: true
      storage: true
  scope: Namespaced
  names:
    plural: tridentstorageclasses
    singular: tridentstorageclass
    kind: TridentStorageClass
    shortNames:
    - tsc
    - tstorageclass
    categories:
    - trident
    - trident-internal
---
apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: tridentvolumes.trident.netapp.io
spec:
  group: trident.netapp.io
  version: v1
  versions:
    - name: v1
      served: true
      storage: true
  scope: Namespaced
  names:
    plural: tridentvolumes
    singular: tridentvolume
    kind: TridentVolume
    shortNames:
    - tvol
    - tvolume
    categories:
    - trident
    - trident-internal
  additionalPrinterColumns:
    - name: Age
      type: date
      priority: 0
      JSONPath: .metadata.creationTimestamp
    - name: Size
      type: string
      description: The volume's size
      priority: 1
      JSONPath: .config.size
    - name: Storage Class
      type: string
      description: The volume's storage class
      priority: 1
      JSONPath: .config.storageClass
    - name: State
      type: string
      description: The volume's state
      priority: 1
      JSONPath: .state
    - name: Protocol
      type: string
      description: The volume's protocol
      priority: 1
      JSONPath: .config.protocol
    - name: Backend UUID
      type: string
      description: The volume's backend UUID
      priority: 1
      JSONPath: .backendUUID
    - name: Pool
      type: string
      description: The volume's pool
      priority: 1
      JSONPath: .pool
---
apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: tridentnodes.trident.netapp.io
spec:
  group: trident.netapp.io
  version: v1
  versions:
    - name: v1
      served: true
      storage: true
  scope: Namespaced
  names:
    plural: tridentnodes
    singular: tridentnode
    kind: TridentNode
    shortNames:
    - tnode
    categories:
    - trident
    - trident-internal
---
apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: tridenttransactions.trident.netapp.io
spec:
  group: trident.netapp.io
  version: v1
  versions:
    - name: v1
      served: true
      storage: true
  scope: Namespaced
  names:
    plural: tridenttransactions
    singular: tridenttransaction
    kind: TridentTransaction
    shortNames:
    - ttx
    - ttransaction
    categories:
    - trident-internal
---
apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: tridentsnapshots.trident.netapp.io
spec:
  group: trident.netapp.io
  version: v1
  versions:
    - name: v1
      served: true
      storage: true
  scope: Namespaced
  names:
    plural: tridentsnapshots
    singular: tridentsnapshot
    kind: TridentSnapshot
    shortNames:
    - tss
    - tsnap
    - tsnapshot
    categories:
    - trident
    - trident-internal
`

func GetCSIDriverCRDYAML() string {
	return CSIDriverCRDYAML
}

const CSIDriverCRDYAML = `
apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: csidrivers.csi.storage.k8s.io
  labels:
    addonmanager.kubernetes.io/mode: Reconcile
spec:
  group: csi.storage.k8s.io
  names:
    kind: CSIDriver
    plural: csidrivers
  scope: Cluster
  validation:
    openAPIV3Schema:
      properties:
        spec:
          description: Specification of the CSI Driver.
          properties:
            attachRequired:
              description: Indicates this CSI volume driver requires an attach operation,
                and that Kubernetes should call attach and wait for any attach operation
                to complete before proceeding to mount.
              type: boolean
            podInfoOnMountVersion:
              description: Indicates this CSI volume driver requires additional pod
                information (like podName, podUID, etc.) during mount operations.
              type: string
  version: v1alpha1
`

func GetCSINodeInfoCRDYAML() string {
	return CSINodeInfoCRDYAML
}

const CSINodeInfoCRDYAML = `
apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: csinodeinfos.csi.storage.k8s.io
  labels:
    addonmanager.kubernetes.io/mode: Reconcile
spec:
  group: csi.storage.k8s.io
  names:
    kind: CSINodeInfo
    plural: csinodeinfos
  scope: Cluster
  validation:
    openAPIV3Schema:
      properties:
        spec:
          description: Specification of CSINodeInfo
          properties:
            drivers:
              description: List of CSI drivers running on the node and their specs.
              type: array
              items:
                properties:
                  name:
                    description: The CSI driver that this object refers to.
                    type: string
                  nodeID:
                    description: The node from the driver point of view.
                    type: string
                  topologyKeys:
                    description: List of keys supported by the driver.
                    items:
                      type: string
                    type: array
        status:
          description: Status of CSINodeInfo
          properties:
            drivers:
              description: List of CSI drivers running on the node and their statuses.
              type: array
              items:
                properties:
                  name:
                    description: The CSI driver that this object refers to.
                    type: string
                  available:
                    description: Whether the CSI driver is installed.
                    type: boolean
                  volumePluginMechanism:
                    description: Indicates to external components the required mechanism
                      to use for any in-tree plugins replaced by this driver.
                    pattern: in-tree|csi
                    type: string
  version: v1alpha1
`

func GetCSIDriverCRYAML() string {
	return CSIDriverCRYAML
}

const CSIDriverCRYAML = `
apiVersion: storage.k8s.io/v1beta1
kind: CSIDriver
metadata:
  name: csi.trident.netapp.io
spec:
  attachRequired: true
`
