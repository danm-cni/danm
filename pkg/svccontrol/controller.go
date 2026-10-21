package svccontrol

import (
	"context"
	"errors"
	"fmt"
	"log"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	danmv1 "github.com/danm-cni/danm/crd/apis/danm/v1"
	danmclientset "github.com/danm-cni/danm/crd/client/clientset/versioned"
	danmscheme "github.com/danm-cni/danm/crd/client/clientset/versioned/scheme"
	danminformers "github.com/danm-cni/danm/crd/client/informers/externalversions/danm/v1"
	danmlisters "github.com/danm-cni/danm/crd/client/listers/danm/v1"
	"github.com/danm-cni/danm/pkg/ipam"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

// errSkipped is returned by the sync operations when svcwatcher shall not touch the cluster state anymore
var errSkipped = errors.New("controller is shutting down, or it is not the leader anymore")

type Controller struct {
	isLeader        atomic.Bool
	stopCh          <-chan struct{}
	kubeclient      kubernetes.Interface
	danmclient      danmclientset.Interface
	podLister       corelisters.PodLister
	podInformer     cache.SharedIndexInformer
	podSynced       cache.InformerSynced
	serviceLister   corelisters.ServiceLister
	serviceInformer cache.SharedIndexInformer
	serviceSynced   cache.InformerSynced
	epsLister       corelisters.EndpointsLister
	epsInformer     cache.SharedIndexInformer
	epsSynced       cache.InformerSynced
	danmepLister    danmlisters.DanmEpLister
	danmepInformer  cache.SharedIndexInformer
	danmepSynced    cache.InformerSynced
	workqueue       workqueue.TypedRateLimitingInterface[any]
	deletedDepStore map[string]*danmv1.DanmEp
	deletedDepLock  sync.Mutex
	idx             struct {
		Network string
		NS      string
		Pod     string
	}
}

//nolint:staticcheck // Endpoints is deprecated but still supported, refactor it at some point though
func NewController(
	stopCh <-chan struct{},
	kubeclient kubernetes.Interface,
	danmclient danmclientset.Interface,
	podInformer coreinformers.PodInformer,
	serviceInformer coreinformers.ServiceInformer,
	epsInformer coreinformers.EndpointsInformer,
	danmepInformer danminformers.DanmEpInformer) (*Controller, error) {

	danmscheme.AddToScheme(scheme.Scheme)
	log.Println("INFO: Creating svcwatcher controller")

	controller := &Controller{
		stopCh:          stopCh,
		kubeclient:      kubeclient,
		danmclient:      danmclient,
		podLister:       podInformer.Lister(),
		podInformer:     podInformer.Informer(),
		podSynced:       podInformer.Informer().HasSynced,
		serviceLister:   serviceInformer.Lister(),
		serviceInformer: serviceInformer.Informer(),
		serviceSynced:   serviceInformer.Informer().HasSynced,
		epsLister:       epsInformer.Lister(),
		epsInformer:     epsInformer.Informer(),
		epsSynced:       epsInformer.Informer().HasSynced,
		danmepLister:    danmepInformer.Lister(),
		danmepInformer:  danmepInformer.Informer(),
		danmepSynced:    danmepInformer.Informer().HasSynced,
		workqueue:       workqueue.NewTypedRateLimitingQueueWithConfig(workqueue.DefaultTypedControllerRateLimiter[any](), workqueue.TypedRateLimitingQueueConfig[any]{Name: "Endpoints"}),
	}

	controller.idx.Network = "networkIndex"
	controller.idx.NS = "nsIndex"
	controller.idx.Pod = "podIndex"

	controller.deletedDepStore = make(map[string]*danmv1.DanmEp)

	if err := controller.danmepInformer.AddIndexers(cache.Indexers{
		controller.idx.Network: func(obj interface{}) ([]string, error) {
			dep, ok := obj.(*danmv1.DanmEp)
			if !ok {
				return []string{}, nil
			}
			net := dep.Spec.NetworkName
			if net == "default" {
				return []string{}, nil
			}
			apitype := dep.Spec.ApiType
			return []string{dep.Namespace + "/" + apitype + "/" + net}, nil
		},
		controller.idx.Pod: func(obj interface{}) ([]string, error) {
			dep, ok := obj.(*danmv1.DanmEp)
			if !ok {
				return []string{}, nil
			}
			net := dep.Spec.NetworkName
			if net == "default" {
				return []string{}, nil
			}
			pod := dep.Spec.Pod
			return []string{dep.Namespace + "/" + pod}, nil
		},
	}); err != nil {
		return nil, fmt.Errorf("DanmEp indexers could not be added to the informer because: %w", err)
	}

	if err := controller.serviceInformer.AddIndexers(cache.Indexers{
		controller.idx.Network: func(obj interface{}) ([]string, error) {
			svc, ok := obj.(*corev1.Service)
			if !ok {
				return []string{}, nil
			}
			ann := svc.Annotations
			if ann == nil {
				return []string{}, nil
			}
			_, svcNets, err := GetDanmSvcAnnotations(ann)
			if err != nil || len(svcNets) != 1 {
				return []string{}, nil
			}
			var apitype string
			var net string
			for k, v := range svcNets {
				apitype = k
				net = v
			}
			return []string{svc.Namespace + "/" + apitype + "/" + net}, nil
		},
	}); err != nil {
		return nil, fmt.Errorf("Service indexers could not be added to the informer because: %w", err)
	}

	if err := controller.epsInformer.AddIndexers(cache.Indexers{
		controller.idx.Network: func(obj interface{}) ([]string, error) {
			eps, ok := obj.(*corev1.Endpoints)
			if !ok {
				return []string{}, nil
			}
			ann := eps.Annotations
			if ann == nil {
				return []string{}, nil
			}
			_, svcNets, err := GetDanmSvcAnnotations(ann)
			if err != nil || len(svcNets) != 1 {
				return []string{}, nil
			}
			var apitype string
			var net string
			for k, v := range svcNets {
				apitype = k
				net = v
			}
			return []string{eps.Namespace + "/" + apitype + "/" + net}, nil
		},
		controller.idx.NS: func(obj interface{}) ([]string, error) {
			eps, ok := obj.(*corev1.Endpoints)
			if !ok {
				return []string{}, nil
			}
			ann := eps.Annotations
			_, svcNets, err := GetDanmSvcAnnotations(ann)
			if err != nil || len(svcNets) != 1 {
				return []string{}, nil
			}
			return []string{eps.Namespace}, nil
		},
	}); err != nil {
		return nil, fmt.Errorf("Endpoints indexers could not be added to the informer because: %w", err)
	}

	log.Println("INFO: Setting up event handlers")

	controller.podInformer.AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: isPodRelevant,
		Handler: cache.ResourceEventHandlerFuncs{
			// pod is updated: can change its status, label, rv, but ip address remains
			UpdateFunc: func(oldObj, newObj interface{}) {
				if !controller.isLeader.Load() {
					return
				}
				if pod, ok := newObj.(*corev1.Pod); ok {
					key, _ := cache.MetaNamespaceKeyFunc(pod)
					controller.workqueue.Add("pod/" + key)
				}
			},
		},
	})

	controller.serviceInformer.AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: isSvcRelevant,
		Handler: cache.ResourceEventHandlerFuncs{
			// eps should be created when service is created
			AddFunc: func(obj interface{}) {
				if !controller.isLeader.Load() {
					return
				}
				if svc, ok := obj.(*corev1.Service); ok {
					key, _ := cache.MetaNamespaceKeyFunc(svc)
					controller.workqueue.Add("svc/" + key)
				}
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				if !controller.isLeader.Load() {
					return
				}
				if svc, ok := newObj.(*corev1.Service); ok {
					key, _ := cache.MetaNamespaceKeyFunc(svc)
					controller.workqueue.Add("svc/" + key)
				}
			},
		},
	})

	controller.danmepInformer.AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: isDanmEpRelevant,
		Handler: cache.ResourceEventHandlerFuncs{
			// danmep created by cni when pod created
			AddFunc: func(obj interface{}) {
				if !controller.isLeader.Load() {
					return
				}
				if dep, ok := obj.(*danmv1.DanmEp); ok {
					key, _ := cache.MetaNamespaceKeyFunc(dep)
					controller.workqueue.Add("dep/" + key)
				}
			},
			// danmep deleted by cni when pod is deleted
			DeleteFunc: func(obj interface{}) {
				if !controller.isLeader.Load() {
					return
				}
				var dep *danmv1.DanmEp
				switch t := obj.(type) {
				case *danmv1.DanmEp:
					dep = t
				case cache.DeletedFinalStateUnknown:
					if castDep, ok := t.Obj.(*danmv1.DanmEp); ok {
						dep = castDep
					}
				default:
					dep = nil
				}
				if dep == nil {
					return
				}
				key, _ := cache.MetaNamespaceKeyFunc(dep)
				controller.deletedDepLock.Lock()
				controller.deletedDepStore[key] = dep.DeepCopy()
				controller.deletedDepLock.Unlock()
				controller.workqueue.Add("delDep/" + key)
			},
		},
	})

	return controller, nil
}

// isPodRelevant tells if a Pod needs to be watched by svcwatcher: only DANM managed Pods are of interest
func isPodRelevant(obj interface{}) bool {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return false
	}
	_, ok = pod.Annotations[danmIfDefinitionSyntax]
	return ok
}

// isSvcRelevant tells if a Service needs to be watched by svcwatcher: only selectorless Services carrying a DANM Pod selector are of interest
func isSvcRelevant(obj interface{}) bool {
	svc, ok := obj.(*corev1.Service)
	if !ok {
		return false
	}
	_, ok = svc.Annotations[PodSelector]
	return ok
}

// isDanmEpRelevant tells if a DanmEp needs to be watched by svcwatcher: interfaces connected to the default network are handled by vanilla K8s
func isDanmEpRelevant(obj interface{}) bool {
	dep, ok := obj.(*danmv1.DanmEp)
	if !ok {
		return false
	}
	return dep.Spec.NetworkName != "default"
}

func (c *Controller) SetLeader(leader bool) {
	c.isLeader.Store(leader)
}

func (c *Controller) WarmUp() error {
	if ok := cache.WaitForCacheSync(c.stopCh, c.serviceSynced, c.epsSynced, c.podSynced, c.danmepSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}
	return nil
}

func (c *Controller) Run(threadiness int) error {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	log.Println("INFO: Starting svcwatcher controller")

	log.Println("INFO: Starting workers")
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, c.stopCh)
	}
	log.Println("INFO: Started workers")

	log.Println("INFO: Starting periodic reconciliation")
	go wait.Until(c.reconcileAllEndpoints, 60*time.Second, c.stopCh)
	log.Println("INFO: Started periodic reconciliation")

	<-c.stopCh
	log.Println("INFO: Shutting down workers")

	return nil
}

func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

func (c *Controller) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.workqueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.workqueue.Forget(obj)
			runtime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.syncHandler(key); err != nil {
			if errors.Is(err, errSkipped) {
				c.workqueue.Forget(obj)
				return nil
			}
			c.workqueue.AddRateLimited(obj)
			return fmt.Errorf("workqueue item %s could not be synced because: %w", key, err)
		}
		c.workqueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		runtime.HandleError(err)
		return true
	}

	return true
}

func (c *Controller) reconcileAllEndpoints() {
	if c.shouldSkip() {
		return
	}
	svcList, err := c.serviceLister.List(labels.Everything())
	if err != nil {
		runtime.HandleError(fmt.Errorf("periodic reconciliation could not list Services because: %w", err))
		return
	}
	for _, svc := range svcList {
		if isSvcRelevant(svc) {
			key := svc.Namespace + "/" + svc.Name
			c.workqueue.Add("svc/" + key)
		}
	}
}

func (c *Controller) syncHandler(key string) error {
	if strings.HasPrefix(key, "pod/") {
		k := strings.TrimPrefix(key, "pod/")
		ns, name, err := cache.SplitMetaNamespaceKey(k)
		if err != nil {
			return fmt.Errorf("Pod key %s in the workqueue is invalid because: %w", k, err)
		}
		return c.syncPod(ns, name)
	} else if strings.HasPrefix(key, "svc/") {
		k := strings.TrimPrefix(key, "svc/")
		ns, name, err := cache.SplitMetaNamespaceKey(k)
		if err != nil {
			return fmt.Errorf("Service key %s in the workqueue is invalid because: %w", k, err)
		}
		return c.syncSvc(ns, name)
	} else if strings.HasPrefix(key, "dep/") {
		k := strings.TrimPrefix(key, "dep/")
		ns, name, err := cache.SplitMetaNamespaceKey(k)
		if err != nil {
			return fmt.Errorf("DanmEp key %s in the workqueue is invalid because: %w", k, err)
		}
		return c.syncDanmEp(ns, name)
	} else if strings.HasPrefix(key, "delDep/") {
		k := strings.TrimPrefix(key, "delDep/")
		ns, name, err := cache.SplitMetaNamespaceKey(k)
		if err != nil {
			return fmt.Errorf("deleted DanmEp key %s in the workqueue is invalid because: %w", k, err)
		}
		return c.syncDanmEpDelete(ns, name)
	}
	return fmt.Errorf("workqueue item %s has an unknown key prefix", key)
}

func (c *Controller) syncPod(ns, name string) error {
	pod, err := c.podLister.Pods(ns).Get(name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("Pod %s/%s could not be read from the cache because: %w", ns, name, err)
	}
	var syncErrs []error
	// update pod in eps
	err = func() error {
		if c.shouldSkip() {
			return errSkipped
		}
		idxKey := ns
		objs, ierr := c.epsInformer.GetIndexer().ByIndex(c.idx.NS, idxKey)
		if ierr != nil {
			return fmt.Errorf("Endpoints of namespace %s could not be read from the index because: %w", idxKey, ierr)
		}
		epList, uerr := c.updatePodInEps(objs, pod)
		if len(epList) == 0 {
			return uerr
		}
		if updErr := c.updateEndpointsList(epList); updErr != nil {
			return updErr
		}
		return uerr
	}()
	if err != nil {
		syncErrs = append(syncErrs, fmt.Errorf("status of Pod %s/%s could not be updated in the Endpoints of its Services because: %w", ns, name, err))
	}
	// update labels
	err = func() error {
		if c.shouldSkip() {
			return errSkipped
		}
		idxKey := ns + "/" + name
		objs, ierr := c.danmepInformer.GetIndexer().ByIndex(c.idx.Pod, idxKey)
		if ierr != nil {
			return fmt.Errorf("DanmEps of Pod %s could not be read from the index because: %w", idxKey, ierr)
		}
		for _, o := range objs {
			de := o.(*danmv1.DanmEp)
			if de.Spec.Pod == name && de.Spec.PodUID == pod.UID {
				if reflect.DeepEqual(de.Labels, pod.Labels) {
					continue
				}
				deNew := de.DeepCopy()
				deNew.SetLabels(pod.Labels)
				_, ierr = c.danmclient.DanmV1().DanmEps(ns).Update(context.TODO(), deNew, meta_v1.UpdateOptions{})
				if ierr != nil {
					return fmt.Errorf("DanmEp %s/%s could not be updated because: %w", ns, de.ObjectMeta.Name, ierr)
				}
			}
		}
		return nil
	}()
	if err != nil {
		syncErrs = append(syncErrs, fmt.Errorf("labels of Pod %s/%s could not be synced to its DanmEps because: %w", ns, name, err))
	}
	return errors.Join(syncErrs...)
}

func (c *Controller) syncSvc(ns, name string) error {
	svc, err := c.serviceLister.Services(ns).Get(name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("Service %s/%s could not be read from the cache because: %w", ns, name, err)
	}
	annotations := svc.Annotations
	selectorMap, svcNets, err := GetDanmSvcAnnotations(annotations)
	if err != nil {
		return fmt.Errorf("annotations of Service %s/%s could not be parsed because: %w", ns, name, err)
	}
	if len(selectorMap) == 0 || len(svcNets) == 0 {
		return nil
	}
	if len(svcNets) != 1 {
		return fmt.Errorf("Service %s/%s selects %d networks, but exactly one network selector is supported", ns, name, len(svcNets))
	}
	var apitype, net string
	for k, v := range svcNets {
		apitype = k
		net = v
	}
	idxKey := ns + "/" + apitype + "/" + net
	desObjs, err := c.danmepInformer.GetIndexer().ByIndex(c.idx.Network, idxKey)
	if err != nil {
		return fmt.Errorf("DanmEps of network %s could not be read from the index because: %w", idxKey, err)
	}
	matchingDesList := SelectDesMatchLabels(desObjs, selectorMap, svcNets, ns)
	if c.shouldSkip() {
		return errSkipped
	}
	eps, err := c.epsLister.Endpoints(ns).Get(name)
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("Endpoints %s/%s could not be read from the cache because: %w", ns, name, err)
	}
	if err = c.createModifyEndpoints(svc, eps, matchingDesList); err != nil {
		return fmt.Errorf("Endpoints of Service %s/%s could not be reconciled because: %w", ns, name, err)
	}
	return nil
}

func (c *Controller) syncDanmEp(ns, name string) error {
	dep, err := c.danmepLister.DanmEps(ns).Get(name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("DanmEp %s/%s could not be read from the cache because: %w", ns, name, err)
	}
	ipAddr, ip6Addr := getIpsFromDanmEp(dep)
	idxKey := dep.Namespace + "/" + dep.Spec.ApiType + "/" + dep.Spec.NetworkName
	objs, err := c.serviceInformer.GetIndexer().ByIndex(c.idx.Network, idxKey)
	if err != nil {
		return fmt.Errorf("Services of network %s could not be read from the index because: %w", idxKey, err)
	}
	matchedSvcList := MatchExistingSvc(dep, objs)
	if len(matchedSvcList) == 0 {
		return nil
	}
	pod, err := c.podLister.Pods(dep.Namespace).Get(dep.Spec.Pod)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("Pod %s/%s of DanmEp %s could not be read from the cache because: %w", dep.Namespace, dep.Spec.Pod, name, err)
	}
	var syncErrs []error
	for _, svc := range matchedSvcList {
		svcNs := svc.ObjectMeta.Namespace
		svcName := svc.ObjectMeta.Name
		err := func() error {
			if c.shouldSkip() {
				return errSkipped
			}
			eps, getErr := c.epsLister.Endpoints(svcNs).Get(svcName)
			if getErr != nil && !apierrors.IsNotFound(getErr) {
				return fmt.Errorf("Endpoints %s/%s could not be read from the cache because: %w", svcNs, svcName, getErr)
			}
			if eps == nil || len(eps.Subsets) == 0 {
				desList := []*danmv1.DanmEp{dep}
				return c.createModifyEndpoints(svc, eps, desList)
			}
			early := (svc.Annotations[TolerateUnreadyEps] == "true")
			return c.epCheckUpdate(ipAddr, ip6Addr, eps.DeepCopy(), pod, early)
		}()
		if err != nil {
			syncErrs = append(syncErrs, fmt.Errorf("DanmEp %s could not be processed for Service %s/%s because: %w", dep.ObjectMeta.Name, svcNs, svcName, err))
		}
	}
	return errors.Join(syncErrs...)
}

//nolint:staticcheck // Endpoints is deprecated but still supported, refactor it at some point though
func (c *Controller) syncDanmEpDelete(ns, name string) error {
	key := ns + "/" + name
	c.deletedDepLock.Lock()
	dep, ok := c.deletedDepStore[key]
	c.deletedDepLock.Unlock()
	if !ok {
		return nil
	}
	if err := c.cleanDeletedDepFromEps(dep); err != nil {
		return fmt.Errorf("deletion of DanmEp %s/%s could not be processed because: %w", ns, name, err)
	}
	c.deletedDepLock.Lock()
	delete(c.deletedDepStore, key)
	c.deletedDepLock.Unlock()
	return nil
}

// cleanDeletedDepFromEps removes the IP addresses of an already deleted DanmEp from the Endpoints of every Service selecting it
//
//nolint:staticcheck // Endpoints is deprecated but still supported, refactor it at some point though
func (c *Controller) cleanDeletedDepFromEps(dep *danmv1.DanmEp) error {
	ipAddr, ip6Addr := getIpsFromDanmEp(dep)
	idxKey := dep.Namespace + "/" + dep.Spec.ApiType + "/" + dep.Spec.NetworkName
	objs, err := c.epsInformer.GetIndexer().ByIndex(c.idx.Network, idxKey)
	if err != nil {
		return fmt.Errorf("Endpoints of network %s could not be read from the index because: %w", idxKey, err)
	}
	var syncErrs []error
	var epList []*corev1.Endpoints
	for _, o := range objs {
		ep := o.(*corev1.Endpoints)
		if len(ep.Subsets) == 0 {
			continue
		}
		annotations := ep.GetAnnotations()
		selectorMap, svcNets, err := GetDanmSvcAnnotations(annotations)
		if err != nil {
			syncErrs = append(syncErrs, fmt.Errorf("annotations of Endpoints %s/%s could not be parsed because: %w", ep.ObjectMeta.Namespace, ep.ObjectMeta.Name, err))
			continue
		}
		if len(selectorMap) == 0 || !isDepSelectedBySvc(dep, svcNets) {
			continue
		}
		deFit := IsContain(dep.GetLabels(), selectorMap)
		if !deFit {
			continue
		}
		epNew := ep.DeepCopy()
		epNew.Subsets[0].Addresses = deleteFromEpAddressList(ipAddr, ip6Addr, epNew.Subsets[0].Addresses, dep.Spec.PodUID)
		epNew.Subsets[0].NotReadyAddresses = deleteFromEpAddressList(ipAddr, ip6Addr, epNew.Subsets[0].NotReadyAddresses, dep.Spec.PodUID)
		if len(epNew.Subsets[0].Addresses) != len(ep.Subsets[0].Addresses) || len(epNew.Subsets[0].NotReadyAddresses) != len(ep.Subsets[0].NotReadyAddresses) {
			epList = append(epList, epNew)
		}
	}
	if len(epList) == 0 {
		return errors.Join(syncErrs...)
	}
	if c.shouldSkip() {
		return errors.Join(append(syncErrs, errSkipped)...)
	}
	if err := c.updateEndpointsList(epList); err != nil {
		syncErrs = append(syncErrs, err)
	}
	return errors.Join(syncErrs...)
}

////////////////////////
//                    //
//                    //
// Instance functions //
//                    //
//                    //
////////////////////////

func (c *Controller) shouldSkip() bool {
	select {
	case <-c.stopCh:
		return true
	default:
	}
	return !c.isLeader.Load()
}

//nolint:staticcheck // Endpoints is deprecated but still supported, refactor it at some point though
func (c *Controller) epCheckUpdate(ipAddr, ip6Addr string, eps *corev1.Endpoints, pod *corev1.Pod, early bool) error {
	if (isIpInEp(ipAddr, eps) || !isRoutableAddress(ipAddr)) &&
		(isIpInEp(ip6Addr, eps) || !isRoutableAddress(ip6Addr)) {
		return nil
	}
	targetRef := &corev1.ObjectReference{
		Kind:            "Pod",
		Namespace:       pod.ObjectMeta.Namespace,
		Name:            pod.ObjectMeta.Name,
		ResourceVersion: pod.ObjectMeta.ResourceVersion,
		UID:             pod.ObjectMeta.UID,
	}
	if PodReady(pod) || early {
		eps.Subsets[0].Addresses = createChangedEpAddressList(ipAddr, ip6Addr, pod, eps, targetRef, eps.Subsets[0].Addresses)
	} else {
		eps.Subsets[0].NotReadyAddresses = createChangedEpAddressList(ipAddr, ip6Addr, pod, eps, targetRef, eps.Subsets[0].NotReadyAddresses)
	}
	return c.updateEndpoints(eps)
}

//nolint:staticcheck // Endpoints is deprecated but still supported, refactor it at some point though
func (c *Controller) updateEndpoints(eps *corev1.Endpoints) error {
	shrinkSubsets(eps)
	if _, err := c.kubeclient.CoreV1().Endpoints(eps.Namespace).Update(context.TODO(), eps, meta_v1.UpdateOptions{}); err != nil {
		return fmt.Errorf("Endpoints %s/%s could not be updated because: %w", eps.ObjectMeta.Namespace, eps.ObjectMeta.Name, err)
	}
	return nil
}

//nolint:staticcheck // Endpoints is deprecated but still supported, refactor it at some point though
func (c *Controller) updateEndpointsList(epList []*corev1.Endpoints) error {
	var err error
	for _, eps := range epList {
		err = c.updateEndpoints(eps)
		if err != nil {
			return err
		}
	}
	return nil
}

//nolint:staticcheck // Endpoints is deprecated but still supported, refactor it at some point though
func (c *Controller) createModifyEndpoints(svc *corev1.Service, eps *corev1.Endpoints, des []*danmv1.DanmEp) error {
	var err error
	epNew, epErr := c.makeNewEps(svc, des)
	shrinkSubsets(&epNew)
	if eps == nil {
		if _, err = c.kubeclient.CoreV1().Endpoints(svc.Namespace).Create(context.TODO(), &epNew, meta_v1.CreateOptions{}); err != nil {
			err = fmt.Errorf("Endpoints %s/%s could not be created because: %w", svc.ObjectMeta.Namespace, svc.ObjectMeta.Name, err)
		}
		return errors.Join(epErr, err)
	}
	if reflect.DeepEqual(normalizeSubsets(eps.Subsets), normalizeSubsets(epNew.Subsets)) {
		return epErr
	}
	epUpdate := eps.DeepCopy()
	epUpdate.Annotations = epNew.Annotations
	epUpdate.Subsets = epNew.Subsets
	if _, err = c.kubeclient.CoreV1().Endpoints(svc.Namespace).Update(context.TODO(), epUpdate, meta_v1.UpdateOptions{}); err != nil {
		err = fmt.Errorf("Endpoints %s/%s could not be updated because: %w", svc.ObjectMeta.Namespace, svc.ObjectMeta.Name, err)
	}
	return errors.Join(epErr, err)
}

//nolint:staticcheck // Endpoints is deprecated but still supported, refactor it at some point though
func (c *Controller) updatePodInEps(objs []interface{}, pod *corev1.Pod) ([]*corev1.Endpoints, error) {
	var epList []*corev1.Endpoints
	var updateErrs []error
	podReady := PodReady(pod)
	for _, o := range objs {
		eps := o.(*corev1.Endpoints).DeepCopy()
		selectorMap, _, err := GetDanmSvcAnnotations(eps.Annotations)
		if err != nil {
			updateErrs = append(updateErrs, fmt.Errorf("annotations of Endpoints %s/%s could not be parsed because: %w", eps.ObjectMeta.Namespace, eps.ObjectMeta.Name, err))
			continue
		}
		podLabelFit := IsContain(pod.GetLabels(), selectorMap)
		if !podLabelFit {
			continue
		}
		change := false
		svc, err := c.serviceLister.Services(eps.Namespace).Get(eps.Name)
		if err != nil {
			if !apierrors.IsNotFound(err) {
				updateErrs = append(updateErrs, fmt.Errorf("Service %s/%s of the Endpoints could not be read from the cache because: %w", eps.ObjectMeta.Namespace, eps.ObjectMeta.Name, err))
			}
			continue
		}
		if len(eps.Subsets) == 0 {
			continue
		}
		// Make map for addresses
		readyAddrs := map[string]corev1.EndpointAddress{}
		notReadyAddrs := map[string]corev1.EndpointAddress{}
		for _, a := range eps.Subsets[0].Addresses {
			readyAddrs[a.IP] = a
		}
		for _, a := range eps.Subsets[0].NotReadyAddresses {
			notReadyAddrs[a.IP] = a
		}
		early := (svc.Annotations[TolerateUnreadyEps] == "true")
		if !podReady && !early {
			for _, a := range eps.Subsets[0].Addresses {
				if isAddressOfPod(a, pod) {
					a.TargetRef.ResourceVersion = pod.ResourceVersion
					change = true
					delete(readyAddrs, a.IP)
					notReadyAddrs[a.IP] = a
				}
			}
		}
		if !change && (podReady || early) {
			for _, a := range eps.Subsets[0].NotReadyAddresses {
				if isAddressOfPod(a, pod) {
					a.TargetRef.ResourceVersion = pod.ResourceVersion
					change = true
					delete(notReadyAddrs, a.IP)
					readyAddrs[a.IP] = a
				}
			}
		}
		if change {
			eps.Subsets[0].Addresses = nil
			eps.Subsets[0].NotReadyAddresses = nil
			for _, a := range readyAddrs {
				eps.Subsets[0].Addresses = append(eps.Subsets[0].Addresses, a)
			}
			for _, a := range notReadyAddrs {
				eps.Subsets[0].NotReadyAddresses = append(eps.Subsets[0].NotReadyAddresses, a)
			}
			epList = append(epList, eps)
		}
	}
	return epList, errors.Join(updateErrs...)
}

//nolint:staticcheck // Endpoints is deprecated but still supported, refactor it at some point though
func (c *Controller) makeNewEps(svc *corev1.Service, des []*danmv1.DanmEp) (corev1.Endpoints, error) {
	epNew := corev1.Endpoints{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:        svc.ObjectMeta.Name,
			Namespace:   svc.ObjectMeta.Namespace,
			Annotations: svc.DeepCopy().GetAnnotations(),
		},
	}
	if len(des) == 0 {
		return epNew, nil
	}
	var epErrs []error
	var readyEpAddrs []corev1.EndpointAddress
	var notReadyEpAddrs []corev1.EndpointAddress
	for _, de := range des {
		pod, err := c.podLister.Pods(de.Namespace).Get(de.Spec.Pod)
		if err != nil {
			// a DanmEp outliving its Pod simply has no address to advertise anymore
			if !apierrors.IsNotFound(err) {
				epErrs = append(epErrs, fmt.Errorf("Pod %s/%s of DanmEp %s could not be read from the cache because: %w", de.Namespace, de.Spec.Pod, de.ObjectMeta.Name, err))
			}
			continue
		}
		targetRef := &corev1.ObjectReference{
			Kind:            "Pod",
			Namespace:       pod.ObjectMeta.Namespace,
			Name:            pod.ObjectMeta.Name,
			ResourceVersion: pod.ObjectMeta.ResourceVersion,
			UID:             pod.ObjectMeta.UID,
		}
		if PodReady(pod) || svc.Annotations[TolerateUnreadyEps] == "true" {
			readyEpAddrs = createChangedEpAddressList(strings.Split(de.Spec.Iface.Address, "/")[0], strings.Split(de.Spec.Iface.AddressIPv6, "/")[0], pod, nil, targetRef, readyEpAddrs)
		} else {
			notReadyEpAddrs = createChangedEpAddressList(strings.Split(de.Spec.Iface.Address, "/")[0], strings.Split(de.Spec.Iface.AddressIPv6, "/")[0], pod, nil, targetRef, notReadyEpAddrs)
		}
	}
	var epPorts []corev1.EndpointPort
	for _, svcPort := range svc.Spec.Ports {
		ep := corev1.EndpointPort{
			Name:     svcPort.Name,
			Port:     svcPort.Port,
			Protocol: svcPort.Protocol,
		}
		epPorts = append(epPorts, ep)
	}
	//nolint:staticcheck // EndpointSubset is deprecated but still supported, refactor it at some point though
	subsets := []corev1.EndpointSubset{
		{
			Addresses:         readyEpAddrs,
			NotReadyAddresses: notReadyEpAddrs,
			Ports:             epPorts,
		},
	}
	epNew.Subsets = subsets
	return epNew, errors.Join(epErrs...)
}

// isAddressOfPod tells if an Endpoints address belongs to the given Pod
//
//nolint:staticcheck // Endpoints is deprecated but still supported, refactor it at some point though
func isAddressOfPod(addr corev1.EndpointAddress, pod *corev1.Pod) bool {
	return addr.TargetRef != nil &&
		addr.TargetRef.Name == pod.ObjectMeta.Name &&
		addr.TargetRef.Namespace == pod.ObjectMeta.Namespace &&
		addr.TargetRef.UID == pod.ObjectMeta.UID
}

// isIpInEp tells if an IP address is already present in the Endpoints. Both the ready and the not ready
// list has to be searched: an address sitting in either of them is already represented in the Endpoints,
// and appending it a second time would only duplicate it
//
//nolint:staticcheck // Endpoints is deprecated but still supported, refactor it at some point though
func isIpInEp(ip string, eps *corev1.Endpoints) bool {
	if len(eps.Subsets) == 0 {
		return false
	}
	for _, a := range eps.Subsets[0].Addresses {
		if a.IP == ip {
			return true
		}
	}
	for _, a := range eps.Subsets[0].NotReadyAddresses {
		if a.IP == ip {
			return true
		}
	}
	return false
}

//nolint:staticcheck // Endpoints is deprecated but still supported, refactor it at some point though
func createChangedEpAddressList(v4Address, v6Address string, pod *corev1.Pod, eps *corev1.Endpoints, targetRef *corev1.ObjectReference, epAddrs []corev1.EndpointAddress) []corev1.EndpointAddress {
	nodeNameCopy := pod.Spec.NodeName
	if isRoutableAddress(v4Address) && (eps == nil || !isIpInEp(v4Address, eps)) {
		epAddrs = append(epAddrs, corev1.EndpointAddress{IP: v4Address, Hostname: pod.Spec.Hostname, NodeName: &nodeNameCopy, TargetRef: targetRef})
	}
	if isRoutableAddress(v6Address) && (eps == nil || !isIpInEp(v6Address, eps)) {
		epAddrs = append(epAddrs, corev1.EndpointAddress{IP: v6Address, Hostname: pod.Spec.Hostname, NodeName: &nodeNameCopy, TargetRef: targetRef})
	}
	return epAddrs
}

// isRoutableAddress tells if a DanmEp interface address can be put into an Endpoints at all:
// unset, "none", and "dynamic" IPAM markers are not real addresses
func isRoutableAddress(address string) bool {
	return address != "" && address != ipam.NoneAllocType && address != ipam.DynamicAllocType
}

func getIpsFromDanmEp(de *danmv1.DanmEp) (string, string) {
	var ipAddr, ip6Addr string
	if de.Spec.Iface.Address != "" {
		ipAddr = strings.Split(de.Spec.Iface.Address, "/")[0]
	}
	if de.Spec.Iface.AddressIPv6 != "" {
		ip6Addr = strings.Split(de.Spec.Iface.AddressIPv6, "/")[0]
	}
	return ipAddr, ip6Addr
}

//nolint:staticcheck // Endpoints is deprecated but still supported, refactor it at some point though
func deleteFromEpAddressList(v4Address, v6Address string, epAddrs []corev1.EndpointAddress, podUid types.UID) []corev1.EndpointAddress {
	retVal := epAddrs[:0]
	for _, addr := range epAddrs {
		if (addr.IP == v4Address || addr.IP == v6Address) && addr.TargetRef != nil && addr.TargetRef.UID == podUid {
			continue
		}
		retVal = append(retVal, addr)
	}
	return retVal
}
