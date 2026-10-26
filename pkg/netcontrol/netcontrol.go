package netcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	danmtypes "github.com/danm-cni/danm/crd/apis/danm/v1"
	nadtypes "github.com/danm-cni/danm/crd/apis/k8s.cni.cncf.io/v1"
	danmclientset "github.com/danm-cni/danm/crd/client/clientset/versioned"
	danminformers "github.com/danm-cni/danm/crd/client/informers/externalversions"
	nadclientset "github.com/danm-cni/danm/crd/client/nad/clientset/versioned"
	nadinformers "github.com/danm-cni/danm/crd/client/nad/informers/externalversions"
	"github.com/danm-cni/danm/pkg/datastructs"
	"github.com/vishvananda/netlink"
	multustypes "gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/types"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

const (
	DanmNetKind             = "DanmNet"
	TenantNetworkKind       = "TenantNetwork"
	ClusterNetworkKind      = "ClusterNetwork"
	NadKind                 = "NetworkAttachmentDefinition"
	ResyncPeriod            = 1 * time.Minute
	DiscoveryTimeout        = 1 * time.Minute
	DiscoveryRequestTimeout = 10 * time.Second
	DiscoveryRetryInterval  = 5 * time.Second
	CacheSyncTimeout        = 5 * time.Minute
	APIRequestTimeout       = 30 * time.Second
	RetryBaseDelay          = time.Second
	RetryMaxDelay           = 5 * time.Minute
	WorkerCount             = 4
	NetworkIDIndex          = "networkID"
	//NadParentAnnotation stores the real parent device of a NAD whose master NetWatcher has overwritten with its VLAN/VxLAN host interface
	NadParentAnnotation = "danm.io/parent-device"
	//NadPatchedMasterAnnotation stores the master NetWatcher has written into the NAD, to tell it apart from a master set by the user
	NadPatchedMasterAnnotation = "danm.io/patched-master"
)

// NetWatcher represents an object watching the K8s API for changes in all three network management API paths
// Upon the reception of a notification it queues the NetworkIDs affected, and its workers handle the related VxLAN/VLAN/RT creation/deletions on the host
// Host interfaces are named after the NetworkID, so a NetworkID is the unit of reconciliation, possibly shared by multiple network objects
type NetWatcher struct {
	DanmClient  danmclientset.Interface
	DanmFactory danminformers.SharedInformerFactory
	NadClient   nadclientset.Interface
	NadFactory  nadinformers.SharedInformerFactory
	Informers   map[string]cache.SharedIndexInformer
	Config      datastructs.NetwatcherConfig
	queue       workqueue.TypedRateLimitingInterface[string]
	workers     sync.WaitGroup
}

// NewWatcher initializes and returns a new NetWatcher object
// Informers are only created for the network management APIs served by the cluster, as an Informer of a missing API would never sync
// Discovery is retried until DiscoveryTimeout, because a transient API server failure must not make NetWatcher run without some of its Controllers
func NewWatcher(ctx context.Context, cfg datastructs.NetwatcherConfig) (*NetWatcher, error) {
	danmClient, err := danmclientset.NewForConfig(cfg.RestConfig)
	if err != nil {
		return nil, err
	}
	nadClient, err := nadclientset.NewForConfig(cfg.RestConfig)
	if err != nil {
		return nil, err
	}
	discoveryConfig := rest.CopyConfig(cfg.RestConfig)
	discoveryConfig.Timeout = DiscoveryRequestTimeout
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(discoveryConfig)
	if err != nil {
		return nil, err
	}
	netWatcher := &NetWatcher{
		DanmClient:  danmClient,
		DanmFactory: danminformers.NewSharedInformerFactory(danmClient, ResyncPeriod),
		NadClient:   nadClient,
		NadFactory:  nadinformers.NewSharedInformerFactory(nadClient, ResyncPeriod),
		Informers:   make(map[string]cache.SharedIndexInformer),
		Config:      cfg,
		queue: workqueue.NewTypedRateLimitingQueueWithConfig(
			workqueue.NewTypedItemExponentialFailureRateLimiter[string](RetryBaseDelay, RetryMaxDelay),
			workqueue.TypedRateLimitingQueueConfig[string]{Name: "netwatcher"}),
	}
	danmResources, err := discoverResources(ctx, discoveryClient, danmtypes.SchemeGroupVersion.String())
	if err != nil {
		return nil, err
	}
	nadResources, err := discoverResources(ctx, discoveryClient, nadtypes.SchemeGroupVersion.String())
	if err != nil {
		return nil, err
	}
	if danmResources["danmnets"] {
		err = netWatcher.registerInformer(DanmNetKind, netWatcher.DanmFactory.Danm().V1().DanmNets().Informer())
		if err != nil {
			return nil, err
		}
	}
	//Production APIs work in tandem, Netwatcher shall not start their Controllers if only one or the other would be served
	if danmResources["tenantnetworks"] && danmResources["clusternetworks"] {
		err = netWatcher.registerInformer(TenantNetworkKind, netWatcher.DanmFactory.Danm().V1().TenantNetworks().Informer())
		if err != nil {
			return nil, err
		}
		err = netWatcher.registerInformer(ClusterNetworkKind, netWatcher.DanmFactory.Danm().V1().ClusterNetworks().Informer())
		if err != nil {
			return nil, err
		}
	} else if danmResources["tenantnetworks"] || danmResources["clusternetworks"] {
		return nil, errors.New("only one of the TenantNetwork and ClusterNetwork APIs is currently served by the cluster, please install the missing CRD!")
	}
	if nadResources["network-attachment-definitions"] {
		err = netWatcher.registerInformer(NadKind, netWatcher.NadFactory.K8sCniCncfIo().V1().NetworkAttachmentDefinitions().Informer())
		if err != nil {
			return nil, err
		}
	}
	if len(netWatcher.Informers) == 0 {
		return nil, errors.New("no network management APIs are installed in the cluster, netwatcher cannot start!")
	}
	log.Println("Number of watchers started for recognized APIs:" + strconv.Itoa(len(netWatcher.Informers)))
	return netWatcher, nil
}

func discoverResources(ctx context.Context, discoveryClient discovery.DiscoveryInterface, groupVersion string) (map[string]bool, error) {
	resources := make(map[string]bool)
	var lastErr error
	err := wait.PollUntilContextTimeout(ctx, DiscoveryRetryInterval, DiscoveryTimeout, true, func(ctx context.Context) (bool, error) {
		log.Println("INFO: Trying to discover " + groupVersion + " APIs in the cluster...")
		resourceList, err := discoveryClient.ServerResourcesForGroupVersion(groupVersion)
		if apierrors.IsNotFound(err) {
			log.Println("INFO: " + groupVersion + " APIs are not installed in the cluster")
			return true, nil
		}
		if err != nil {
			lastErr = err
			log.Println("INFO: Discovery of " + groupVersion + " APIs failed with error:" + err.Error())
			return false, nil
		}
		for _, resource := range resourceList.APIResources {
			resources[resource.Name] = true
		}
		return true, nil
	})
	if err != nil {
		return nil, fmt.Errorf("discovery of %s APIs did not succeed within %s: %w", groupVersion, DiscoveryTimeout, errors.Join(err, lastErr))
	}
	return resources, nil
}

// registerInformer makes the Informer queue the NetworkIDs affected by its events, and index its objects by NetworkID
// An update queues the old NetworkID too, so its interfaces are removed when the NetworkID of a network changes
// An update to an invalid network is ignored altogether, so a typo in a NAD does not tear down the interfaces of its previous, valid version
func (netWatcher *NetWatcher) registerInformer(kind string, informer cache.SharedIndexInformer) error {
	err := informer.AddIndexers(cache.Indexers{NetworkIDIndex: networkIDIndexFunc})
	if err != nil {
		return errors.New("could not register NetworkID indexer of " + kind + " Controller:" + err.Error())
	}
	_, err = informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) { netWatcher.enqueue(kind, obj) },
		UpdateFunc: func(oldObj, newObj interface{}) {
			if netWatcher.enqueue(kind, newObj) {
				netWatcher.enqueue(kind, oldObj)
			}
		},
		DeleteFunc: func(obj interface{}) { netWatcher.enqueue(kind, obj) },
	})
	if err != nil {
		return errors.New("could not register event handler of " + kind + " Controller:" + err.Error())
	}
	err = informer.SetWatchErrorHandlerWithContext(WatchErrorHandler)
	if err != nil {
		return errors.New("could not register watch error handler of " + kind + " Controller:" + err.Error())
	}
	log.Println("INFO: " + kind + " API is served by the cluster, starting " + kind + " Controller!")
	netWatcher.Informers[kind] = informer
	return nil
}

// Run starts all Informers, and once their caches are synced it starts the workers processing the queued networks,
// and the periodic garbage collection of the host interfaces tagged by NetWatcher
// An Informer not syncing within CacheSyncTimeout (e.g. due to missing RBAC permissions) is reported as an error, as it would never deliver any events
// The Informers and the workers keep running until ctx is cancelled
func (netWatcher *NetWatcher) Run(ctx context.Context) error {
	netWatcher.DanmFactory.StartWithContext(ctx)
	netWatcher.NadFactory.StartWithContext(ctx)
	syncCtx, cancel := context.WithTimeoutCause(ctx, CacheSyncTimeout, errors.New("Informer caches did not sync within "+CacheSyncTimeout.String()))
	defer cancel()
	err := netWatcher.DanmFactory.WaitForCacheSyncWithContext(syncCtx).AsError()
	if err != nil {
		return err
	}
	err = netWatcher.NadFactory.WaitForCacheSyncWithContext(syncCtx).AsError()
	if err != nil {
		return err
	}
	for i := 0; i < WorkerCount; i++ {
		netWatcher.workers.Go(func() {
			wait.UntilWithContext(ctx, netWatcher.runWorker, time.Second)
		})
	}
	netWatcher.workers.Go(func() {
		wait.UntilWithContext(ctx, netWatcher.enqueueTaggedNetworks, ResyncPeriod)
	})
	go func() {
		<-ctx.Done()
		netWatcher.queue.ShutDown()
	}()
	return nil
}

// Shutdown waits until all workers and Informers are stopped, the ctx passed to Run needs to be cancelled first
// Waiting is bounded by shutdownCtx, as a worker can be stuck in a netlink call which cannot be interrupted
func (netWatcher *NetWatcher) Shutdown(shutdownCtx context.Context) error {
	netWatcher.queue.ShutDown()
	stopped := make(chan struct{})
	go func() {
		netWatcher.workers.Wait()
		netWatcher.DanmFactory.Shutdown()
		netWatcher.NadFactory.Shutdown()
		close(stopped)
	}()
	select {
	case <-stopped:
		return nil
	case <-shutdownCtx.Done():
		return context.Cause(shutdownCtx)
	}
}

// WatchErrorHandler only logs the error, as the Reflector itself keeps retrying List and Watch with backoff for as long as its Informer runs
// Expired resource versions and closed connections are part of normal watch operations, so they are not logged
func WatchErrorHandler(_ context.Context, r *cache.Reflector, err error) {
	if apierrors.IsResourceExpired(err) || apierrors.IsGone(err) || errors.Is(err, io.EOF) {
		return
	}
	log.Println("WARNING: " + r.TypeDescription() + " watcher failed with error:" + err.Error() + ", re-establishing connection")
}

func (netWatcher *NetWatcher) enqueue(kind string, obj interface{}) bool {
	if tombStone, isTombStone := obj.(cache.DeletedFinalStateUnknown); isTombStone {
		obj = tombStone.Obj
	}
	dnet, err := toDanmNet(obj)
	if err != nil {
		key, _ := cache.MetaNamespaceKeyFunc(obj)
		log.Println("ERROR: Can't manage host interfaces for " + kind + ":" + key + " because:" + err.Error())
		return false
	}
	if dnet.Spec.NetworkID != "" {
		netWatcher.queue.Add(dnet.Spec.NetworkID)
	}
	return true
}

// enqueueTaggedNetworks queues the NetworkIDs of all host interfaces tagged by NetWatcher
// This is how interfaces of networks deleted while NetWatcher was not running get cleaned up, as no event refers to their NetworkID anymore
// It must only run once the Informer caches are synced, otherwise the interfaces of all networks would look unnecessary
func (netWatcher *NetWatcher) enqueueTaggedNetworks(_ context.Context) {
	links, err := netlink.LinkList()
	if err != nil {
		log.Println("WARNING: Listing host interfaces for garbage collection failed with error:" + err.Error())
		return
	}
	for _, networkID := range taggedNetworkIDs(links) {
		netWatcher.queue.Add(networkID)
	}
}

func networkIDIndexFunc(obj interface{}) ([]string, error) {
	dnet, err := toDanmNet(obj)
	if err != nil || dnet.Spec.NetworkID == "" {
		return nil, nil
	}
	return []string{dnet.Spec.NetworkID}, nil
}

func (netWatcher *NetWatcher) runWorker(ctx context.Context) {
	for netWatcher.processNextWorkItem(ctx) {
	}
}

func (netWatcher *NetWatcher) processNextWorkItem(ctx context.Context) bool {
	networkID, shutdown := netWatcher.queue.Get()
	if shutdown {
		return false
	}
	defer netWatcher.queue.Done(networkID)
	err := netWatcher.reconcile(ctx, networkID)
	if err == nil {
		netWatcher.queue.Forget(networkID)
		return true
	}
	log.Println("INFO: Managing host interfaces for NetworkID:" + networkID + " failed with error:" + err.Error() + " , retrying")
	netWatcher.queue.AddRateLimited(networkID)
	return true
}

// reconcile brings the host interfaces of a NetworkID in line with all network objects using it in the Informer caches
// The interfaces present on the host are the reference: any interface of the NetworkID not required by a network is deleted,
// regardless of whether it belongs to a deleted network, to a previous version of a network, or was left behind while NetWatcher was not running
func (netWatcher *NetWatcher) reconcile(ctx context.Context, networkID string) error {
	networks, err := netWatcher.networksWithID(networkID)
	if err != nil {
		return err
	}
	links, err := netlink.LinkList()
	if err != nil {
		return errors.New("cannot cleanup dangling interfaces because listing host interfaces failed:" + err.Error())
	}
	hostIfaces := findHostInterfaces(networkID, links)
	for _, dnet := range networks {
		if dnet.TypeMeta.Kind == NadKind {
			resolveNadParentDevice(dnet, hostIfaces, links)
		}
	}
	errs := cleanupDanglingIfaces(networkID, networks, hostIfaces, links)
	for _, dnet := range networks {
		err = setupHost(dnet, netWatcher.Config.SourceLearning)
		if err != nil {
			errs = append(errs, errors.New("creating host interfaces for "+dnet.TypeMeta.Kind+":"+dnet.ObjectMeta.Name+" failed:"+err.Error()))
			continue
		}
		if dnet.TypeMeta.Kind == NadKind {
			err = netWatcher.patchNadParentDevice(ctx, dnet)
			if err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func cleanupDanglingIfaces(networkID string, networks []*danmtypes.DanmNet, hostIfaces []hostInterface, links []netlink.Link) []error {
	var errs []error
	toTag, toDelete := planHostInterfaces(networks, hostIfaces, links)
	for _, hostIface := range toTag {
		err := tagHostInterface(hostIface.link, networkID)
		if err != nil {
			errs = append(errs, err)
		}
	}
	for _, hostIface := range toDelete {
		log.Println("INFO: Deleting tagged host interface:" + hostIface.link.Attrs().Name + " as none of the networks require it")
		err := netlink.LinkDel(hostIface.link)
		if err != nil {
			errs = append(errs, errors.New("deletion of host interface:"+hostIface.link.Attrs().Name+" failed:"+err.Error()))
		}
	}
	return errs
}

// planHostInterfaces returns the untagged interfaces required by a network, which need to be tagged,
// and the tagged interfaces not required by any network, which need to be deleted
// Untagged interfaces not required by any network are left alone, as they might have been created by someone else
func planHostInterfaces(networks []*danmtypes.DanmNet, hostIfaces []hostInterface, links []netlink.Link) ([]hostInterface, []hostInterface) {
	var toTag, toDelete []hostInterface
	for _, hostIface := range hostIfaces {
		isDesired := isHostInterfaceDesired(hostIface, networks, links)
		if isDesired && !hostIface.isTagged {
			toTag = append(toTag, hostIface)
		} else if !isDesired && hostIface.isTagged {
			toDelete = append(toDelete, hostIface)
		}
	}
	return toTag, toDelete
}

func (netWatcher *NetWatcher) networksWithID(networkID string) ([]*danmtypes.DanmNet, error) {
	var networks []*danmtypes.DanmNet
	for _, informer := range netWatcher.Informers {
		objs, err := informer.GetIndexer().ByIndex(NetworkIDIndex, networkID)
		if err != nil {
			return nil, err
		}
		for _, obj := range objs {
			dnet, err := toDanmNet(obj)
			if err != nil {
				return nil, err
			}
			networks = append(networks, dnet)
		}
	}
	return networks, nil
}

func toDanmNet(obj interface{}) (*danmtypes.DanmNet, error) {
	switch network := obj.(type) {
	case *danmtypes.DanmNet:
		dnet := *network
		dnet.TypeMeta.Kind = DanmNetKind
		return &dnet, nil
	case *danmtypes.TenantNetwork:
		return ConvertTnetToDnet(network), nil
	case *danmtypes.ClusterNetwork:
		return ConvertCnetToDnet(network), nil
	case *nadtypes.NetworkAttachmentDefinition:
		return convertNadToDnet(network)
	}
	return nil, fmt.Errorf("unexpected object type %T", obj)
}

func resolveNadParentDevice(dnet *danmtypes.DanmNet, hostIfaces []hostInterface, links []netlink.Link) {
	parent, isParentStored := dnet.ObjectMeta.Annotations[NadParentAnnotation]
	if isParentStored && dnet.Spec.Options.Device == dnet.ObjectMeta.Annotations[NadPatchedMasterAnnotation] {
		dnet.Spec.Options.Device = parent
		return
	}
	for _, hostIface := range hostIfaces {
		if hostIface.link.Attrs().Name == dnet.Spec.Options.Device {
			if parent = linkName(links, hostIface.parentIndex); parent != "" {
				dnet.Spec.Options.Device = parent
			}
			return
		}
	}
}

func (netWatcher *NetWatcher) patchNadParentDevice(ctx context.Context, dnet *danmtypes.DanmNet) error {
	obj, exists, err := netWatcher.Informers[NadKind].GetIndexer().GetByKey(dnet.ObjectMeta.Namespace + "/" + dnet.ObjectMeta.Name)
	if err != nil || !exists {
		return err
	}
	nad := obj.(*nadtypes.NetworkAttachmentDefinition)
	var netConf datastructs.NetConf
	err = json.Unmarshal([]byte(nad.Spec.Config), &netConf)
	if err != nil {
		return errors.New("could not parse CNI config of NetworkAttachmentDefinition:" + err.Error())
	}
	hostDevice := DetermineHostDeviceName(dnet)
	//CNI plugin conf isn't compatible with Netwatcher's assumptions
	if dnet.Spec.Options.Device == "" {
		return nil
	}
	isParentPatched := hostDevice != dnet.Spec.Options.Device
	parent, isParentStored := nad.ObjectMeta.Annotations[NadParentAnnotation]
	patchedMaster, isPatchedMasterStored := nad.ObjectMeta.Annotations[NadPatchedMasterAnnotation]
	areAnnotationsUpToDate := !isParentStored && !isPatchedMasterStored
	if isParentPatched {
		areAnnotationsUpToDate = isParentStored && parent == dnet.Spec.Options.Device && isPatchedMasterStored && patchedMaster == hostDevice
	}
	if netConf.Master == hostDevice && areAnnotationsUpToDate {
		return nil
	}
	patchedNad := nad.DeepCopy()
	patchedNad.Spec.Config = string(PatchCniConf([]byte(nad.Spec.Config), "master", hostDevice))
	if isParentPatched {
		if patchedNad.ObjectMeta.Annotations == nil {
			patchedNad.ObjectMeta.Annotations = make(map[string]string)
		}
		patchedNad.ObjectMeta.Annotations[NadParentAnnotation] = dnet.Spec.Options.Device
		patchedNad.ObjectMeta.Annotations[NadPatchedMasterAnnotation] = hostDevice
	} else {
		delete(patchedNad.ObjectMeta.Annotations, NadParentAnnotation)
		delete(patchedNad.ObjectMeta.Annotations, NadPatchedMasterAnnotation)
	}
	reqCtx, cancel := context.WithTimeout(ctx, APIRequestTimeout)
	defer cancel()
	_, err = netWatcher.NadClient.K8sCniCncfIoV1().NetworkAttachmentDefinitions(nad.ObjectMeta.Namespace).Update(reqCtx, patchedNad, meta_v1.UpdateOptions{})
	if err != nil {
		return errors.New("could not update NetworkAttachmentDefinition with the new parent interface name because:" + err.Error())
	}
	return nil
}

func ConvertTnetToDnet(tnet *danmtypes.TenantNetwork) *danmtypes.DanmNet {
	dnet := danmtypes.DanmNet{
		TypeMeta:   tnet.TypeMeta,
		ObjectMeta: tnet.ObjectMeta,
		Spec:       tnet.Spec,
		Status:     tnet.Status,
	}
	dnet.TypeMeta.Kind = TenantNetworkKind
	return &dnet
}

func ConvertCnetToDnet(cnet *danmtypes.ClusterNetwork) *danmtypes.DanmNet {
	dnet := danmtypes.DanmNet{
		TypeMeta:   cnet.TypeMeta,
		ObjectMeta: cnet.ObjectMeta,
		Spec:       cnet.Spec,
		Status:     cnet.Status,
	}
	dnet.TypeMeta.Kind = ClusterNetworkKind
	return &dnet
}

func convertNadToDnet(nad *nadtypes.NetworkAttachmentDefinition) (*danmtypes.DanmNet, error) {
	dnet := danmtypes.DanmNet{
		TypeMeta:   nad.TypeMeta,
		ObjectMeta: nad.ObjectMeta,
	}
	dnet.TypeMeta.Kind = NadKind
	delegateConf, err := multustypes.LoadDelegateNetConf([]byte(nad.Spec.Config), nil, "", "")
	if err != nil {
		return &dnet, errors.New("could not parse CNI config from Nad.Spec.Config into delegate type because:" + err.Error())
	}
	//TODO: support conflist type
	if delegateConf.Conf.Type == "" {
		return &dnet, nil
	}
	var netConf datastructs.NetConf
	err = json.Unmarshal([]byte(nad.Spec.Config), &netConf)
	if err != nil {
		return &dnet, errors.New("could not parse CNI config from Nad.Spec.Config into netconf type because:" + err.Error())
	}
	spec := danmtypes.DanmNetSpec{
		NetworkID:   netConf.Name,
		NetworkType: netConf.Type,
		Options: danmtypes.DanmNetOption{
			Device: netConf.Master,
			Vlan:   netConf.Vlan,
			Vxlan:  netConf.Vxlan,
		},
	}
	dnet.Spec = spec
	return &dnet, nil
}

func ConvertDnetToTnet(dnet *danmtypes.DanmNet) *danmtypes.TenantNetwork {
	return &danmtypes.TenantNetwork{
		TypeMeta:   dnet.TypeMeta,
		ObjectMeta: dnet.ObjectMeta,
		Spec:       dnet.Spec,
		Status:     dnet.Status,
	}
}

func ConvertDnetToCnet(dnet *danmtypes.DanmNet) *danmtypes.ClusterNetwork {
	return &danmtypes.ClusterNetwork{
		TypeMeta:   dnet.TypeMeta,
		ObjectMeta: dnet.ObjectMeta,
		Spec:       dnet.Spec,
		Status:     dnet.Status,
	}
}

func PutNetwork(danmClient danmclientset.Interface, dnet *danmtypes.DanmNet) (bool, error) {
	var err error
	var wasResourceAlreadyUpdated bool
	switch dnet.TypeMeta.Kind {
	case DanmNetKind, "":
		_, err = danmClient.DanmV1().DanmNets(dnet.ObjectMeta.Namespace).Update(context.TODO(), dnet, meta_v1.UpdateOptions{})
	case TenantNetworkKind:
		tn := ConvertDnetToTnet(dnet)
		_, err = danmClient.DanmV1().TenantNetworks(dnet.ObjectMeta.Namespace).Update(context.TODO(), tn, meta_v1.UpdateOptions{})
	case ClusterNetworkKind:
		cn := ConvertDnetToCnet(dnet)
		_, err = danmClient.DanmV1().ClusterNetworks().Update(context.TODO(), cn, meta_v1.UpdateOptions{})
	default:
		return wasResourceAlreadyUpdated, errors.New("can't refresh network object because it has an invalid type:" + dnet.TypeMeta.Kind)
	}
	if err != nil {
		if strings.Contains(err.Error(), datastructs.OptimisticLockErrorMsg) {
			wasResourceAlreadyUpdated = true
			return wasResourceAlreadyUpdated, nil
		}
		return wasResourceAlreadyUpdated, err
	}
	return wasResourceAlreadyUpdated, nil
}

func UpdateNetStatus(danmClient danmclientset.Interface, dnet *danmtypes.DanmNet) (bool, error) {
	var err error
	var wasResourceAlreadyUpdated bool
	switch dnet.TypeMeta.Kind {
	//Status field only exists in the production APIs
	case DanmNetKind, "":
		return wasResourceAlreadyUpdated, nil
	case TenantNetworkKind:
		tn := ConvertDnetToTnet(dnet)
		_, err = danmClient.DanmV1().TenantNetworks(dnet.ObjectMeta.Namespace).UpdateStatus(context.TODO(), tn, meta_v1.UpdateOptions{})
	case ClusterNetworkKind:
		cn := ConvertDnetToCnet(dnet)
		_, err = danmClient.DanmV1().ClusterNetworks().UpdateStatus(context.TODO(), cn, meta_v1.UpdateOptions{})
	default:
		return wasResourceAlreadyUpdated, errors.New("can't refresh network object because it has an invalid type:" + dnet.TypeMeta.Kind)
	}
	if err != nil {
		if strings.Contains(err.Error(), datastructs.OptimisticLockErrorMsg) {
			wasResourceAlreadyUpdated = true
			return wasResourceAlreadyUpdated, nil
		}
		return wasResourceAlreadyUpdated, err
	}
	return wasResourceAlreadyUpdated, nil
}

func GetDefaultNetwork(danmClient danmclientset.Interface, defaultNetworkName, nameSpace string) (*danmtypes.DanmNet, error) {
	dnet, err := danmClient.DanmV1().DanmNets(nameSpace).Get(context.TODO(), defaultNetworkName, meta_v1.GetOptions{})
	if err == nil && dnet.ObjectMeta.Name == defaultNetworkName {
		return dnet, nil
	}
	tnet, err := danmClient.DanmV1().TenantNetworks(nameSpace).Get(context.TODO(), defaultNetworkName, meta_v1.GetOptions{})
	if err == nil && tnet.ObjectMeta.Name == defaultNetworkName {
		dn := ConvertTnetToDnet(tnet)
		return dn, nil
	}
	cnet, err := danmClient.DanmV1().ClusterNetworks().Get(context.TODO(), defaultNetworkName, meta_v1.GetOptions{})
	if err == nil && cnet.ObjectMeta.Name == defaultNetworkName {
		dn := ConvertCnetToDnet(cnet)
		return dn, nil
	}
	return nil, errors.New("none of DANM APIs have a suitable default network configured")
}

func GetNetworkFromInterface(danmClient danmclientset.Interface, iface datastructs.Interface, nameSpace string) (*danmtypes.DanmNet, error) {
	var err error
	if iface.Network != "" {
		var dnet *danmtypes.DanmNet
		dnet, err = danmClient.DanmV1().DanmNets(nameSpace).Get(context.TODO(), iface.Network, meta_v1.GetOptions{})
		if err == nil {
			dnet.TypeMeta.Kind = DanmNetKind
			return dnet, nil
		}
	} else if iface.TenantNetwork != "" {
		var tnet *danmtypes.TenantNetwork
		tnet, err = danmClient.DanmV1().TenantNetworks(nameSpace).Get(context.TODO(), iface.TenantNetwork, meta_v1.GetOptions{})
		if err == nil {
			dnet := ConvertTnetToDnet(tnet)
			return dnet, nil
		}
	} else if iface.ClusterNetwork != "" {
		var cnet *danmtypes.ClusterNetwork
		cnet, err = danmClient.DanmV1().ClusterNetworks().Get(context.TODO(), iface.ClusterNetwork, meta_v1.GetOptions{})
		if err == nil {
			dnet := ConvertCnetToDnet(cnet)
			return dnet, nil
		}
	}
	if err == nil {
		err = errors.New("network connection request did not contain a valid type from [DanmNet, TenantNetwork, ClusterNetwork]")
	}
	return nil, err
}

func GetNetworkFromEp(danmClient danmclientset.Interface, ep *danmtypes.DanmEp) (*danmtypes.DanmNet, error) {
	dummyIface := datastructs.Interface{}
	if ep.Spec.ApiType == DanmNetKind || ep.Spec.ApiType == "" {
		dummyIface.Network = ep.Spec.NetworkName
	}
	if ep.Spec.ApiType == TenantNetworkKind {
		dummyIface.TenantNetwork = ep.Spec.NetworkName
	}
	if ep.Spec.ApiType == ClusterNetworkKind {
		dummyIface.ClusterNetwork = ep.Spec.NetworkName
	}
	return GetNetworkFromInterface(danmClient, dummyIface, ep.ObjectMeta.Namespace)
}

func GetNetworkFromReference(danmClient danmclientset.Interface, name, namespace, kind string) (*danmtypes.DanmNet, error) {
	dummyIface := datastructs.Interface{}
	if kind == "" || kind == TenantNetworkKind {
		dummyIface.TenantNetwork = name
	}
	if kind == ClusterNetworkKind {
		dummyIface.ClusterNetwork = name
	}
	if dummyIface.TenantNetwork == "" && dummyIface.ClusterNetwork == "" {
		return nil, fmt.Errorf(("network reference did not contain a valid type from [TenantNetwork, ClusterNetwork]"))
	}
	return GetNetworkFromInterface(danmClient, dummyIface, namespace)
}

func RefreshNetwork(danmClient danmclientset.Interface, netInfo danmtypes.DanmNet) (*danmtypes.DanmNet, error) {
	dummyIface := datastructs.Interface{}
	if netInfo.TypeMeta.Kind == DanmNetKind || netInfo.TypeMeta.Kind == "" {
		dummyIface.Network = netInfo.ObjectMeta.Name
	}
	if netInfo.TypeMeta.Kind == TenantNetworkKind {
		dummyIface.TenantNetwork = netInfo.ObjectMeta.Name
	}
	if netInfo.TypeMeta.Kind == ClusterNetworkKind {
		dummyIface.ClusterNetwork = netInfo.ObjectMeta.Name
	}
	return GetNetworkFromInterface(danmClient, dummyIface, netInfo.ObjectMeta.Namespace)
}

func DetermineHostDeviceName(dnet *danmtypes.DanmNet) string {
	var device string
	isVlanDefined := (dnet.Spec.Options.Vlan != 0)
	isVxlanDefined := (dnet.Spec.Options.Vxlan != 0)
	if isVxlanDefined {
		device = "vx_" + dnet.Spec.NetworkID
	} else if isVlanDefined {
		vlanId := strconv.Itoa(dnet.Spec.Options.Vlan)
		device = dnet.Spec.NetworkID + "." + vlanId
	} else {
		device = dnet.Spec.Options.Device
	}
	return device
}

// TODO: one day we will break this import cycle between PatchCniConf(cnidel) and DetermineHostDevice(netcontrol) but today is not that day
func PatchCniConf(rawConf []byte, patchKey string, patchValue interface{}) []byte {
	transparentCniConf := map[string]interface{}{}
	json.Unmarshal(rawConf, &transparentCniConf)
	transparentCniConf[patchKey] = patchValue
	moddedCniConf, _ := json.Marshal(transparentCniConf)
	return moddedCniConf
}
