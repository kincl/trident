package kubernetes

import (
	"fmt"
	"io/ioutil"
	"regexp"
	"time"

	"github.com/cenkalti/backoff"
	log "github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"
	k8sstoragev1 "k8s.io/api/storage/v1"
	k8sstoragev1beta "k8s.io/api/storage/v1beta1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sversion "k8s.io/apimachinery/pkg/version"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/record"

	clik8sclient "github.com/netapp/trident/cli/k8s_client"
	"github.com/netapp/trident/config"
	"github.com/netapp/trident/core"
	"github.com/netapp/trident/frontend/csi"
	"github.com/netapp/trident/frontend/csi/helpers"
	storageattribute "github.com/netapp/trident/storage_attribute"
	storageclass "github.com/netapp/trident/storage_class"
)

const (
	uidIndex = "uid"

	eventAdd    = "add"
	eventUpdate = "update"
	eventDelete = "delete"
)

var (
	uidRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	pvcRegex = regexp.MustCompile(
		`^pvc-(?P<uid>[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})$`)
)

type Plugin struct {
	orchestrator  core.Orchestrator
	kubeConfig    rest.Config
	kubeClient    kubernetes.Interface
	kubeVersion   *k8sversion.Info
	namespace     string
	eventRecorder record.EventRecorder

	pvcIndexer            cache.Indexer
	pvcController         cache.SharedIndexInformer
	pvcControllerStopChan chan struct{}
	pvcSource             cache.ListerWatcher

	pvIndexer            cache.Indexer
	pvController         cache.SharedIndexInformer
	pvControllerStopChan chan struct{}
	pvSource             cache.ListerWatcher

	scIndexer            cache.Indexer
	scController         cache.SharedIndexInformer
	scControllerStopChan chan struct{}
	scSource             cache.ListerWatcher
}

// NewPlugin instantiates this plugin when running outside a pod.
func NewPlugin(o core.Orchestrator, apiServerIP, kubeConfigPath string) (*Plugin, error) {

	kubeConfig, err := clientcmd.BuildConfigFromFlags(apiServerIP, kubeConfigPath)
	if err != nil {
		return nil, err
	}

	// Create the CLI-based Kubernetes client
	client, err := clik8sclient.NewKubectlClient("")
	if err != nil {
		return nil, fmt.Errorf("could not initialize Kubernetes client: %v", err)
	}

	// When running in binary mode, we use the current namespace as determined by the CLI client
	return newKubernetesPlugin(o, kubeConfig, client.Namespace())
}

// NewPluginInCluster instantiates this plugin when running inside a pod.
func NewPluginInCluster(o core.Orchestrator) (*Plugin, error) {

	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	// When running in a pod, we use the Trident pod's namespace
	namespaceBytes, err := ioutil.ReadFile(config.TridentNamespaceFile)
	if err != nil {
		log.WithFields(log.Fields{
			"error":         err,
			"namespaceFile": config.TridentNamespaceFile,
		}).Error("K8S helper failed to obtain Trident's namespace!")
		return nil, err
	}

	return newKubernetesPlugin(o, kubeConfig, string(namespaceBytes))
}

// newKubernetesPlugin initializes this plugin, checks the K8S verison, and sets up the watchers for
// various Kubernetes objects.
func newKubernetesPlugin(orchestrator core.Orchestrator, kubeConfig *rest.Config, namespace string) (*Plugin, error) {

	log.WithField("namespace", namespace).Info("Initializing K8S helper frontend.")

	// Create the Kubernetes client
	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return nil, err
	}

	// Get the Kubernetes version
	kubeVersion, err := kubeClient.Discovery().ServerVersion()
	if err != nil {
		return nil, fmt.Errorf("K8S helper frontend could not retrieve API server's version: %v", err)
	}

	p := &Plugin{
		orchestrator:          orchestrator,
		kubeConfig:            *kubeConfig,
		kubeClient:            kubeClient,
		kubeVersion:           kubeVersion,
		pvcControllerStopChan: make(chan struct{}),
		pvControllerStopChan:  make(chan struct{}),
		scControllerStopChan:  make(chan struct{}),
		namespace:             namespace,
	}

	log.WithFields(log.Fields{
		"version":    p.kubeVersion.Major + "." + p.kubeVersion.Minor,
		"gitVersion": p.kubeVersion.GitVersion,
	}).Info("K8S helper determined the container orchestrator version.")

	if err = p.validateKubeVersion(); err != nil {
		return nil, fmt.Errorf("K8S helper frontend could not validate Kubernetes version: %v", err)
	}

	// Set up event broadcaster
	broadcaster := record.NewBroadcaster()
	broadcaster.StartRecordingToSink(&corev1.EventSinkImpl{Interface: kubeClient.CoreV1().Events("")})
	p.eventRecorder = broadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: csi.Provisioner})

	// Set up a watch for PVCs
	p.pvcSource = &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			return kubeClient.CoreV1().PersistentVolumeClaims(v1.NamespaceAll).List(options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			return kubeClient.CoreV1().PersistentVolumeClaims(v1.NamespaceAll).Watch(options)
		},
	}

	// Set up the PVC indexing controller
	p.pvcController = cache.NewSharedIndexInformer(
		p.pvcSource,
		&v1.PersistentVolumeClaim{},
		CacheSyncPeriod,
		cache.Indexers{uidIndex: MetaUIDKeyFunc},
	)
	p.pvcIndexer = p.pvcController.GetIndexer()

	// Add handlers for CSI-provisioned PVCs
	p.pvcController.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    p.addPVC,
			UpdateFunc: p.updatePVC,
			DeleteFunc: p.deletePVC,
		},
	)
	p.pvcController.AddEventHandlerWithResyncPeriod(
		cache.ResourceEventHandlerFuncs{
			UpdateFunc: p.updatePVCResize,
		},
		ResizeSyncPeriod,
	)

	// Set up a watch for PVs
	p.pvSource = &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			return kubeClient.CoreV1().PersistentVolumes().List(options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			return kubeClient.CoreV1().PersistentVolumes().Watch(options)
		},
	}

	// Set up the PV indexing controller
	p.pvController = cache.NewSharedIndexInformer(
		p.pvSource,
		&v1.PersistentVolume{},
		CacheSyncPeriod,
		cache.Indexers{uidIndex: MetaUIDKeyFunc},
	)
	p.pvIndexer = p.pvController.GetIndexer()

	// Add handler for deleting legacy PVs
	p.pvController.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			UpdateFunc: p.updateLegacyPV,
		},
	)

	// Set up a watch for storage classes
	p.scSource = &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			return kubeClient.StorageV1().StorageClasses().List(options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			return kubeClient.StorageV1().StorageClasses().Watch(options)
		},
	}

	// Set up the storage class indexing controller
	p.scController = cache.NewSharedIndexInformer(
		p.scSource,
		&k8sstoragev1.StorageClass{},
		CacheSyncPeriod,
		cache.Indexers{uidIndex: MetaUIDKeyFunc},
	)
	p.scIndexer = p.scController.GetIndexer()

	// Add handler for registering storage classes with Trident
	p.scController.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    p.addStorageClass,
			UpdateFunc: p.updateStorageClass,
			DeleteFunc: p.deleteStorageClass,
		},
	)

	// Add handler for replacing legacy storage classes
	p.scController.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    p.addLegacyStorageClass,
			UpdateFunc: p.updateLegacyStorageClass,
			DeleteFunc: p.deleteLegacyStorageClass,
		},
	)

	return p, nil
}

// MetaUIDKeyFunc is a KeyFunc which knows how to make keys for API objects
// which implement meta.Interface.  The key is the object's UID.
func MetaUIDKeyFunc(obj interface{}) ([]string, error) {
	if key, ok := obj.(string); ok && uidRegex.MatchString(key) {
		return []string{string(key)}, nil
	}
	objectMeta, err := meta.Accessor(obj)
	if err != nil {
		return []string{""}, fmt.Errorf("object has no meta: %v", err)
	}
	if len(objectMeta.GetUID()) == 0 {
		return []string{""}, fmt.Errorf("object has no UID: %v", err)
	}
	return []string{string(objectMeta.GetUID())}, nil
}

// Activate starts this Trident frontend.
func (p *Plugin) Activate() error {
	log.Info("Activating K8S helper frontend.")
	go p.pvcController.Run(p.pvcControllerStopChan)
	go p.pvController.Run(p.pvControllerStopChan)
	go p.scController.Run(p.scControllerStopChan)

	// Configure telemetry
	config.OrchestratorTelemetry.Platform = string(config.PlatformKubernetes)
	config.OrchestratorTelemetry.PlatformVersion = p.Version()

	return nil
}

// Deactivate stops this Trident frontend.
func (p *Plugin) Deactivate() error {
	log.Info("Deactivating K8S helper frontend.")
	close(p.pvcControllerStopChan)
	close(p.pvControllerStopChan)
	close(p.scControllerStopChan)
	return nil
}

// GetName returns the name of this Trident frontend.
func (p *Plugin) GetName() string {
	return string(helpers.KubernetesHelper)
}

// Version returns the version of this Trident frontend (the detected K8S version).
func (p *Plugin) Version() string {
	return p.kubeVersion.GitVersion
}

// addPVC is the add handler for the PVC watcher.
func (p *Plugin) addPVC(obj interface{}) {
	switch pvc := obj.(type) {
	case *v1.PersistentVolumeClaim:
		p.processPVC(pvc, eventAdd)
	default:
		log.Errorf("K8S helper expected PVC; got %v", obj)
	}
}

// updatePVC is the update handler for the PVC watcher.
func (p *Plugin) updatePVC(oldObj, newObj interface{}) {
	switch pvc := newObj.(type) {
	case *v1.PersistentVolumeClaim:
		p.processPVC(pvc, eventUpdate)
	default:
		log.Errorf("K8S helper expected PVC; got %v", newObj)
	}
}

// deletePVC is the delete handler for the PVC watcher.
func (p *Plugin) deletePVC(obj interface{}) {
	switch pvc := obj.(type) {
	case *v1.PersistentVolumeClaim:
		p.processPVC(pvc, eventDelete)
	default:
		log.Errorf("K8S helper expected PVC; got %v", obj)
	}
}

// processPVC logs the add/update/delete PVC events.
func (p *Plugin) processPVC(pvc *v1.PersistentVolumeClaim, eventType string) {

	// Validate the PVC
	size, ok := pvc.Spec.Resources.Requests[v1.ResourceStorage]
	if !ok {
		log.WithField("name", pvc.Name).Debug("Rejecting PVC, no size specified.")
		return
	}

	logFields := log.Fields{
		"name":         pvc.Name,
		"phase":        pvc.Status.Phase,
		"size":         size.String(),
		"uid":          pvc.UID,
		"storageClass": getStorageClassForPVC(pvc),
		"accessModes":  pvc.Spec.AccessModes,
		"pv":           pvc.Spec.VolumeName,
	}

	switch eventType {
	case eventAdd:
		log.WithFields(logFields).Debug("PVC added to cache.")
	case eventUpdate:
		log.WithFields(logFields).Debug("PVC updated in cache.")
	case eventDelete:
		log.WithFields(logFields).Debug("PVC deleted from cache.")
	}
}

// getCachedPVCByName returns a PVC (identified by namespace/name) from the client's cache,
// or an error if not found.  In most cases it may be better to call waitForCachedPVCByName().
func (p *Plugin) getCachedPVCByName(name, namespace string) (*v1.PersistentVolumeClaim, error) {

	logFields := log.Fields{"name": name, "namespace": namespace}

	item, exists, err := p.pvcIndexer.GetByKey(namespace + "/" + name)
	if err != nil {
		log.WithFields(logFields).Error("Could not search cache for PVC by name.")
		return nil, fmt.Errorf("could not search cache for PVC %s/%s: %v", namespace, name, err)
	} else if !exists {
		log.WithFields(logFields).Debug("PVC object not found in cache by name.")
		return nil, fmt.Errorf("PVC %s/%s not found in cache", namespace, name)
	} else if pvc, ok := item.(*v1.PersistentVolumeClaim); !ok {
		log.WithFields(logFields).Error("Non-PVC cached object found by name.")
		return nil, fmt.Errorf("non-PVC object %s/%s found in cache", namespace, name)
	} else {
		log.WithFields(logFields).Debug("Found cached PVC by name.")
		return pvc, nil
	}
}

// waitForCachedPVCByUID returns a PVC (identified by namespace/name) from the client's cache, waiting in a
// backoff loop for the specified duration for the PVC to become available.
func (p *Plugin) waitForCachedPVCByName(
	name, namespace string, maxElapsedTime time.Duration,
) (*v1.PersistentVolumeClaim, error) {

	var pvc *v1.PersistentVolumeClaim

	checkForCachedPVC := func() error {
		var pvcError error
		pvc, pvcError = p.getCachedPVCByName(name, namespace)
		return pvcError
	}
	pvcNotify := func(err error, duration time.Duration) {
		log.WithFields(log.Fields{
			"name":      name,
			"namespace": namespace,
			"increment": duration,
		}).Debugf("PVC not yet in cache, waiting.")
	}
	pvcBackoff := backoff.NewExponentialBackOff()
	pvcBackoff.InitialInterval = CacheBackoffInitialInterval
	pvcBackoff.RandomizationFactor = CacheBackoffRandomizationFactor
	pvcBackoff.Multiplier = CacheBackoffMultiplier
	pvcBackoff.MaxInterval = CacheBackoffMaxInterval
	pvcBackoff.MaxElapsedTime = maxElapsedTime

	if err := backoff.RetryNotify(checkForCachedPVC, pvcBackoff, pvcNotify); err != nil {
		return nil, fmt.Errorf("PVC %s/%s was not cache after %3.2f seconds",
			namespace, name, maxElapsedTime.Seconds())
	}

	return pvc, nil
}

// getCachedPVCByUID returns a PVC (identified by UID) from the client's cache,
// or an error if not found.  In most cases it may be better to call waitForCachedPVCByUID().
func (p *Plugin) getCachedPVCByUID(uid string) (*v1.PersistentVolumeClaim, error) {

	items, err := p.pvcIndexer.ByIndex(uidIndex, uid)
	if err != nil {
		log.WithField("error", err).Error("Could not search cache for PVC by UID.")
		return nil, fmt.Errorf("could not search cache for PVC with UID %s: %v", uid, err)
	} else if len(items) == 0 {
		log.WithField("uid", uid).Debug("PVC object not found in cache by UID.")
		return nil, fmt.Errorf("PVC with UID %s not found in cache", uid)
	} else if len(items) > 1 {
		log.WithField("uid", uid).Error("Multiple cached PVC objects found by UID.")
		return nil, fmt.Errorf("multiple PVC objects with UID %s found in cache", uid)
	} else if pvc, ok := items[0].(*v1.PersistentVolumeClaim); !ok {
		log.WithField("uid", uid).Error("Non-PVC cached object found by UID.")
		return nil, fmt.Errorf("non-PVC object with UID %s found in cache", uid)
	} else {
		log.WithFields(log.Fields{
			"name":      pvc.Name,
			"namespace": pvc.Namespace,
			"uid":       pvc.UID,
		}).Debug("Found cached PVC by UID.")
		return pvc, nil
	}
}

// waitForCachedPVCByUID returns a PVC (identified by UID) from the client's cache, waiting in a
// backoff loop for the specified duration for the PVC to become available.
func (p *Plugin) waitForCachedPVCByUID(uid string, maxElapsedTime time.Duration) (*v1.PersistentVolumeClaim, error) {

	var pvc *v1.PersistentVolumeClaim

	checkForCachedPVC := func() error {
		var pvcError error
		pvc, pvcError = p.getCachedPVCByUID(uid)
		return pvcError
	}
	pvcNotify := func(err error, duration time.Duration) {
		log.WithFields(log.Fields{
			"uid":       uid,
			"increment": duration,
		}).Debugf("PVC not yet in cache, waiting.")
	}
	pvcBackoff := backoff.NewExponentialBackOff()
	pvcBackoff.InitialInterval = CacheBackoffInitialInterval
	pvcBackoff.RandomizationFactor = CacheBackoffRandomizationFactor
	pvcBackoff.Multiplier = CacheBackoffMultiplier
	pvcBackoff.MaxInterval = CacheBackoffMaxInterval
	pvcBackoff.MaxElapsedTime = maxElapsedTime

	if err := backoff.RetryNotify(checkForCachedPVC, pvcBackoff, pvcNotify); err != nil {
		return nil, fmt.Errorf("PVC %s was not cache after %3.2f seconds", uid, maxElapsedTime.Seconds())
	}

	return pvc, nil
}

// addStorageClass is the add handler for the storage class watcher.
func (p *Plugin) addStorageClass(obj interface{}) {
	switch sc := obj.(type) {
	case *k8sstoragev1beta.StorageClass:
		p.processStorageClass(convertStorageClassV1BetaToV1(sc), eventAdd)
	case *k8sstoragev1.StorageClass:
		p.processStorageClass(sc, eventAdd)
	default:
		log.Errorf("K8S helper expected storage.k8s.io/v1beta1 or storage.k8s.io/v1 storage class; got %v", obj)
	}
}

// updateStorageClass is the update handler for the storage class watcher.
func (p *Plugin) updateStorageClass(oldObj, newObj interface{}) {
	switch sc := newObj.(type) {
	case *k8sstoragev1beta.StorageClass:
		p.processStorageClass(convertStorageClassV1BetaToV1(sc), eventUpdate)
	case *k8sstoragev1.StorageClass:
		p.processStorageClass(sc, eventUpdate)
	default:
		log.Errorf("K8S helper expected storage.k8s.io/v1beta1 or storage.k8s.io/v1 storage class; got %v", newObj)
	}
}

// deleteStorageClass is the delete handler for the storage class watcher.
func (p *Plugin) deleteStorageClass(obj interface{}) {
	switch sc := obj.(type) {
	case *k8sstoragev1beta.StorageClass:
		p.processStorageClass(convertStorageClassV1BetaToV1(sc), eventDelete)
	case *k8sstoragev1.StorageClass:
		p.processStorageClass(sc, eventDelete)
	default:
		log.Errorf("K8S helper expected storage.k8s.io/v1beta1 or storage.k8s.io/v1 storage class; got %v", obj)
	}
}

// processStorageClass logs and handles add/update/delete events for CSI Trident storage classes.
func (p *Plugin) processStorageClass(sc *k8sstoragev1.StorageClass, eventType string) {

	// Validate the storage class
	if sc.Provisioner != csi.Provisioner {
		return
	}

	logFields := log.Fields{
		"name":        sc.Name,
		"provisioner": sc.Provisioner,
		"parameters":  sc.Parameters,
	}

	switch eventType {
	case eventAdd:
		log.WithFields(logFields).Debug("Storage class added to cache.")
		p.processAddedStorageClass(sc)
	case eventUpdate:
		log.WithFields(logFields).Debug("Storage class updated in cache.")
		// Make sure Trident has a record of this storage class.
		if storageClass, _ := p.orchestrator.GetStorageClass(sc.Name); storageClass == nil {
			log.WithFields(logFields).Warn("K8S helper has no record of the updated " +
				"storage class; instead it will try to create it.")
			p.processAddedStorageClass(sc)
		}
	case eventDelete:
		log.WithFields(logFields).Debug("Storage class deleted from cache.")
		p.processDeletedStorageClass(sc)
	}
}

// processAddedStorageClass informs the orchestrator of a new storage class.
func (p *Plugin) processAddedStorageClass(sc *k8sstoragev1.StorageClass) {

	scConfig := new(storageclass.Config)
	scConfig.Name = sc.Name
	scConfig.Attributes = make(map[string]storageattribute.Request)

	// Populate storage class config attributes and backend storage pools
	for k, v := range sc.Parameters {
		switch k {
		case K8sFsType:
			// Ignore Kubernetes-defined storage class parameters handled by CSI

		case storageattribute.RequiredStorage, storageattribute.AdditionalStoragePools:
			// format:  additionalStoragePools: "backend1:pool1,pool2;backend2:pool1"
			additionalPools, err := storageattribute.CreateBackendStoragePoolsMapFromEncodedString(v)
			if err != nil {
				log.WithFields(log.Fields{
					"name":        sc.Name,
					"provisioner": sc.Provisioner,
					"parameters":  sc.Parameters,
					"error":       err,
				}).Errorf("K8S helper could not process the storage class parameter %s", k)
			}
			scConfig.AdditionalPools = additionalPools

		case storageattribute.ExcludeStoragePools:
			// format:  excludeStoragePools: "backend1:pool1,pool2;backend2:pool1"
			excludeStoragePools, err := storageattribute.CreateBackendStoragePoolsMapFromEncodedString(v)
			if err != nil {
				log.WithFields(log.Fields{
					"name":        sc.Name,
					"provisioner": sc.Provisioner,
					"parameters":  sc.Parameters,
					"error":       err,
				}).Errorf("K8S helper could not process the storage class parameter %s", k)
			}
			scConfig.ExcludePools = excludeStoragePools

		case storageattribute.StoragePools:
			// format:  storagePools: "backend1:pool1,pool2;backend2:pool1"
			pools, err := storageattribute.CreateBackendStoragePoolsMapFromEncodedString(v)
			if err != nil {
				log.WithFields(log.Fields{
					"name":        sc.Name,
					"provisioner": sc.Provisioner,
					"parameters":  sc.Parameters,
					"error":       err,
				}).Errorf("K8S helper could not process the storage class parameter %s", k)
			}
			scConfig.Pools = pools

		default:
			// format:  attribute: "value"
			req, err := storageattribute.CreateAttributeRequestFromAttributeValue(k, v)
			if err != nil {
				log.WithFields(log.Fields{
					"name":        sc.Name,
					"provisioner": sc.Provisioner,
					"parameters":  sc.Parameters,
					"error":       err,
				}).Errorf("K8S helper could not process the storage class attribute %s", k)
				return
			}
			scConfig.Attributes[k] = req
		}
	}

	// Add the storage class
	if _, err := p.orchestrator.AddStorageClass(scConfig); err != nil {
		log.WithFields(log.Fields{
			"name":        sc.Name,
			"provisioner": sc.Provisioner,
			"parameters":  sc.Parameters,
		}).Warningf("K8S helper could not add a storage class: %s", err)
		return
	}

	log.WithFields(log.Fields{
		"name":        sc.Name,
		"provisioner": sc.Provisioner,
		"parameters":  sc.Parameters,
	}).Info("K8S helper added a storage class.")
}

// processDeletedStorageClass informs the orchestrator of a deleted storage class.
func (p *Plugin) processDeletedStorageClass(sc *k8sstoragev1.StorageClass) {

	logFields := log.Fields{"name": sc.Name}

	// Delete the storage class from Trident
	err := p.orchestrator.DeleteStorageClass(sc.Name)
	if err != nil {
		log.WithFields(logFields).Errorf("K8S helper could not delete the storage class: %v", err)
	} else {
		log.WithFields(logFields).Info("K8S helper deleted the storage class.")
	}
}

// getCachedStorageClassByName returns a storage class (identified by name) from the client's cache,
// or an error if not found.  In most cases it may be better to call waitForCachedStorageClassByName().
func (p *Plugin) getCachedStorageClassByName(name string) (*k8sstoragev1.StorageClass, error) {

	logFields := log.Fields{"name": name}

	item, exists, err := p.scIndexer.GetByKey(name)
	if err != nil {
		log.WithFields(logFields).Error("Could not search cache for storage class by name.")
		return nil, fmt.Errorf("could not search cache for storage class %s: %v", name, err)
	} else if !exists {
		log.WithFields(logFields).Debug("storage class object not found in cache by name.")
		return nil, fmt.Errorf("storage class %s not found in cache", name)
	} else if sc, ok := item.(*k8sstoragev1.StorageClass); !ok {
		log.WithFields(logFields).Error("Non-SC cached object found by name.")
		return nil, fmt.Errorf("non-SC object %s found in cache", name)
	} else {
		log.WithFields(logFields).Debug("Found cached storage class by name.")
		return sc, nil
	}
}

// waitForCachedStorageClassByName returns a storage class (identified by name) from the client's cache,
// waiting in a backoff loop for the specified duration for the storage class to become available.
func (p *Plugin) waitForCachedStorageClassByName(
	name string, maxElapsedTime time.Duration,
) (*k8sstoragev1.StorageClass, error) {

	var sc *k8sstoragev1.StorageClass

	checkForCachedSC := func() error {
		var scError error
		sc, scError = p.getCachedStorageClassByName(name)
		return scError
	}
	scNotify := func(err error, duration time.Duration) {
		log.WithFields(log.Fields{
			"name":      name,
			"increment": duration,
		}).Debugf("Storage class not yet in cache, waiting.")
	}
	scBackoff := backoff.NewExponentialBackOff()
	scBackoff.InitialInterval = CacheBackoffInitialInterval
	scBackoff.RandomizationFactor = CacheBackoffRandomizationFactor
	scBackoff.Multiplier = CacheBackoffMultiplier
	scBackoff.MaxInterval = CacheBackoffMaxInterval
	scBackoff.MaxElapsedTime = maxElapsedTime

	if err := backoff.RetryNotify(checkForCachedSC, scBackoff, scNotify); err != nil {
		return nil, fmt.Errorf("storage class %s was not cache after %3.2f seconds", name, maxElapsedTime.Seconds())
	}

	return sc, nil
}
