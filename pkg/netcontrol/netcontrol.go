package netcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	danmtypes "github.com/danm-cni/danm/crd/apis/danm/v1"
	nadtypes "github.com/danm-cni/danm/crd/apis/k8s.cni.cncf.io/v1"
	danmclientset "github.com/danm-cni/danm/crd/client/clientset/versioned"
	danminformers "github.com/danm-cni/danm/crd/client/informers/externalversions"
	nadclientset "github.com/danm-cni/danm/crd/client/nad/clientset/versioned"
	nadinformers "github.com/danm-cni/danm/crd/client/nad/informers/externalversions"
	"github.com/danm-cni/danm/pkg/datastructs"
	multustypes "gopkg.in/k8snetworkplumbingwg/multus-cni.v3/pkg/types"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

const (
	MaxRetryCount      = 5
	RetryInterval      = 100
	DanmNetKind        = "DanmNet"
	TenantNetworkKind  = "TenantNetwork"
	ClusterNetworkKind = "ClusterNetwork"
	NadKind            = "NetworkAttachmentDefinition"
)

// NetWatcher represents an object watching the K8s API for changes in all three network management API paths
// Upon the reception of a notification it handles the related VxLAN/VLAN/RT creation/deletions on the host
type NetWatcher struct {
	DanmFactories map[string]danminformers.SharedInformerFactory
	DanmClients   map[string]danmclientset.Interface
	NadFactory    nadinformers.SharedInformerFactory
	NadClient     nadclientset.Interface
	Controllers   map[string]cache.Controller
	StopChan      *chan struct{}
}

// NewWatcher initializes and returns a new NetWatcher object
// Upon the reception of a notification it performs host network management operations
// Watcher stores all K8s Clients, Factories, and Informeres of the DANM network management APIs
func NewWatcher(cfg *rest.Config, stopChan *chan struct{}) (*NetWatcher, error) {
	netWatcher := &NetWatcher{
		DanmFactories: make(map[string]danminformers.SharedInformerFactory),
		DanmClients:   make(map[string]danmclientset.Interface),
		Controllers:   make(map[string]cache.Controller),
		StopChan:      stopChan,
	}
	//this is how we test if the specific API is used within the cluster, or not
	//we can only create an Informer for an existing API, otherwise we get errors
	dnetClient, err := danmclientset.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	for i := 0; i < MaxRetryCount; i++ {
		log.Println("INFO: Trying to discover DanmNet API in the cluster...")
		_, err = dnetClient.DanmV1().DanmNets("").List(context.TODO(), meta_v1.ListOptions{})
		if err != nil {
			log.Println("INFO: DanmNet discovery query failed with error:" + err.Error())
			time.Sleep(RetryInterval * time.Millisecond)
		} else {
			log.Println("INFO: DanmNet API seems to be installed in the cluster!")
			netWatcher.createDnetInformer(dnetClient)
			break
		}
	}
	tnetClient, err := danmclientset.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	for i := 0; i < MaxRetryCount; i++ {
		log.Println("INFO: Trying to discover TenantNetwork API in the cluster...")
		_, err = tnetClient.DanmV1().TenantNetworks("").List(context.TODO(), meta_v1.ListOptions{})
		if err != nil {
			log.Println("INFO: TenantNetwork discovery query failed with error:" + err.Error())
			time.Sleep(RetryInterval * time.Millisecond)
		} else {
			log.Println("INFO: TenantNetwork API seems to be installed in the cluster!")
			netWatcher.createTnetInformer(tnetClient)
			break
		}
	}
	cnetClient, err := danmclientset.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	for i := 0; i < MaxRetryCount; i++ {
		log.Println("INFO: Trying to discover ClusterNetwork API in the cluster...")
		_, err = cnetClient.DanmV1().ClusterNetworks().List(context.TODO(), meta_v1.ListOptions{})
		if err != nil {
			log.Println("INFO: ClusterNetwork discovery query failed with error:" + err.Error())
			time.Sleep(RetryInterval * time.Millisecond)
		} else {
			log.Println("INFO: ClusterNetwork API seems to be installed in the cluster!")
			netWatcher.createCnetInformer(cnetClient)
			break
		}
	}
	nadClient, err := nadclientset.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	for i := 0; i < MaxRetryCount; i++ {
		log.Println("INFO: Trying to discover NetworkAttachmentDefinition API in the cluster...")
		_, err = nadClient.K8sCniCncfIoV1().NetworkAttachmentDefinitions("").List(context.TODO(), meta_v1.ListOptions{})
		if err != nil {
			log.Println("INFO: NetworkAttachmentDefinition discovery query failed with error:" + err.Error())
			time.Sleep(RetryInterval * time.Millisecond)
		} else {
			log.Println("INFO: NetworkAttachmentDefinition API seems to be installed in the cluster!")
			netWatcher.createNadInformer(nadClient)
			break
		}
	}
	if len(netWatcher.Controllers) == 0 {
		return nil, errors.New("no network management APIs are installed in the cluster, netwatcher cannot start!")
	}
	log.Println("Number of watchers started for recognized APIs:" + strconv.Itoa(len(netWatcher.Controllers)))
	return netWatcher, nil
}

func (netWatcher *NetWatcher) Run(stopCh *chan struct{}) {
	for _, controller := range netWatcher.Controllers {
		go controller.Run(*stopCh)
	}
}

func (netWatcher *NetWatcher) WatchErrorHandler(r *cache.Reflector, err error) {
	if apierrors.IsResourceExpired(err) || apierrors.IsGone(err) || err == io.EOF {
		log.Println("INFO: One of the API watchers closed gracefully, re-establishing connection")
		return
	}
	//The default K8s client retry mechanism expires after a certain amount of time, and just gives-up
	//It is better to shutdown the whole process now and freshly re-build the watchers, rather than risking becoming a permanent zombie
	*netWatcher.StopChan <- struct{}{}
	//Give some time for gracefully terminating the connections
	time.Sleep(5 * time.Second)
	log.Println("ERROR: One of the API watchers closed unexpectedly with error:" + err.Error() + " shutting down NetWatcher!")
	os.Exit(0)
}

func (netWatcher *NetWatcher) createDnetInformer(dnetClient danmclientset.Interface) {
	netWatcher.DanmClients[DanmNetKind] = dnetClient
	dnetInformerFactory := danminformers.NewSharedInformerFactory(dnetClient, time.Minute*10)
	netWatcher.DanmFactories[DanmNetKind] = dnetInformerFactory
	dnetController := dnetInformerFactory.Danm().V1().DanmNets().Informer()
	dnetController.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    AddDanmNet,
		UpdateFunc: UpdateDanmNet,
		DeleteFunc: DeleteDanmNet,
	})
	dnetController.SetWatchErrorHandler(netWatcher.WatchErrorHandler)
	netWatcher.Controllers[DanmNetKind] = dnetController
}

func (netWatcher *NetWatcher) createTnetInformer(tnetClient danmclientset.Interface) {
	netWatcher.DanmClients[TenantNetworkKind] = tnetClient
	tnetInformerFactory := danminformers.NewSharedInformerFactory(tnetClient, time.Minute*10)
	netWatcher.DanmFactories[TenantNetworkKind] = tnetInformerFactory
	tnetController := tnetInformerFactory.Danm().V1().TenantNetworks().Informer()
	tnetController.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    AddTenantNetwork,
		UpdateFunc: UpdateTenantNetwork,
		DeleteFunc: DeleteTenantNetwork,
	})
	tnetController.SetWatchErrorHandler(netWatcher.WatchErrorHandler)
	netWatcher.Controllers[TenantNetworkKind] = tnetController
}

func (netWatcher *NetWatcher) createCnetInformer(cnetClient danmclientset.Interface) {
	netWatcher.DanmClients[ClusterNetworkKind] = cnetClient
	cnetInformerFactory := danminformers.NewSharedInformerFactory(cnetClient, time.Minute*10)
	netWatcher.DanmFactories[ClusterNetworkKind] = cnetInformerFactory
	cnetController := cnetInformerFactory.Danm().V1().ClusterNetworks().Informer()
	cnetController.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    AddClusterNetwork,
		UpdateFunc: UpdateClusterNetwork,
		DeleteFunc: DeleteClusterNetwork,
	})
	cnetController.SetWatchErrorHandler(netWatcher.WatchErrorHandler)
	netWatcher.Controllers[ClusterNetworkKind] = cnetController
}

func (netWatcher *NetWatcher) createNadInformer(nadClient nadclientset.Interface) {
	netWatcher.NadClient = nadClient
	nadInformerFactory := nadinformers.NewSharedInformerFactory(nadClient, time.Minute*10)
	netWatcher.NadFactory = nadInformerFactory
	nadController := nadInformerFactory.K8sCniCncfIo().V1().NetworkAttachmentDefinitions().Informer()
	nadController.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    netWatcher.AddNad,
		UpdateFunc: netWatcher.UpdateNad,
		DeleteFunc: DeleteNad,
	})
	nadController.SetWatchErrorHandler(netWatcher.WatchErrorHandler)
	netWatcher.Controllers[NadKind] = nadController
}

func AddDanmNet(obj interface{}) {
	dn, isNetwork := obj.(*danmtypes.DanmNet)
	if !isNetwork {
		log.Println("ERROR: Can't create interfaces for DanmNet, 'cause we have received an invalid object from the K8s API server")
		return
	}
	err := setupHost(dn)
	if err != nil {
		log.Println("INFO: Creating host interfaces for DanmNet:" + dn.ObjectMeta.Name + " failed with error:" + err.Error())
	}
}

func UpdateDanmNet(oldObj, newObj interface{}) {
	oldDn, isNetwork := oldObj.(*danmtypes.DanmNet)
	if !isNetwork {
		log.Println("ERROR: Can't update interfaces for DanmNet change, 'cause we have received an invalid old object from the K8s API server")
		return
	}
	newdDn, isNetwork := newObj.(*danmtypes.DanmNet)
	if !isNetwork {
		log.Println("ERROR: Can't update interfaces for DanmNet change, 'cause we have received an invalid new object from the K8s API server")
		return
	}
	zeroVnis(oldDn, newdDn)
	err := deleteNetworks(oldDn)
	if err != nil {
		log.Println("INFO: Deletion of old host interfaces for DanmNet:" + oldDn.ObjectMeta.Name + " after update failed with error:" + err.Error())
	}
	err = setupHost(newdDn)
	if err != nil {
		log.Println("INFO: Creating host interfaces for new DanmNet:" + newdDn.ObjectMeta.Name + " after update failed with error:" + err.Error())
	}
}

func DeleteDanmNet(obj interface{}) {
	dn, isNetwork := obj.(*danmtypes.DanmNet)
	if !isNetwork {
		tombStone, objIsTombstone := obj.(cache.DeletedFinalStateUnknown)
		if !objIsTombstone {
			log.Println("ERROR: Can't delete interfaces for DanmNet, 'cause we have received an invalid object from the K8s API server")
			return
		}
		var isObjectInTombStoneNetwork bool
		dn, isObjectInTombStoneNetwork = tombStone.Obj.(*danmtypes.DanmNet)
		if !isObjectInTombStoneNetwork {
			log.Println("ERROR: Can't delete interfaces for DanmNet, 'cause we have received an invalid object from the K8s API server in the Event tombstone")
			return
		}
	}
	err := deleteNetworks(dn)
	if err != nil {
		log.Println("INFO: Deletion of host interfaces for DanmNet:" + dn.ObjectMeta.Name + " failed with error:" + err.Error())
	}
}

func AddTenantNetwork(obj interface{}) {
	tn, isNetwork := obj.(*danmtypes.TenantNetwork)
	if !isNetwork {
		log.Println("ERROR: Can't create interfaces for TenantNetwork, 'cause we have received an invalid object from the K8s API server")
		return
	}
	dnet := ConvertTnetToDnet(tn)
	err := setupHost(dnet)
	if err != nil {
		log.Println("INFO: Creating host interfaces for TenantNetwork:" + dnet.ObjectMeta.Name + " failed with error:" + err.Error())
	}
}

func UpdateTenantNetwork(oldObj, newObj interface{}) {
	oldTn, isNetwork := oldObj.(*danmtypes.TenantNetwork)
	if !isNetwork {
		log.Println("ERROR: Can't update interfaces for TenantNetwork change, 'cause we have received an invalid old object from the K8s API server")
		return
	}
	newTn, isNetwork := newObj.(*danmtypes.TenantNetwork)
	if !isNetwork {
		log.Println("ERROR: Can't update interfaces for TenantNetwork change, 'cause we have received an invalid new object from the K8s API server")
		return
	}
	oldDn := ConvertTnetToDnet(oldTn)
	newdDn := ConvertTnetToDnet(newTn)
	zeroVnis(oldDn, newdDn)
	err := deleteNetworks(oldDn)
	if err != nil {
		log.Println("INFO: Deletion of old host interfaces for TenantNetwork:" + oldDn.ObjectMeta.Name + " after update failed with error:" + err.Error())
	}
	err = setupHost(newdDn)
	if err != nil {
		log.Println("INFO: Creating host interfaces for new TenantNetwork:" + newdDn.ObjectMeta.Name + " after update failed with error:" + err.Error())
	}
}

func DeleteTenantNetwork(obj interface{}) {
	tn, isNetwork := obj.(*danmtypes.TenantNetwork)
	if !isNetwork {
		tombStone, objIsTombstone := obj.(cache.DeletedFinalStateUnknown)
		if !objIsTombstone {
			log.Println("ERROR: Can't delete interfaces for TenantNetwork, 'cause we have received an invalid object from the K8s API server")
			return
		}
		var isObjectInTombStoneNetwork bool
		tn, isObjectInTombStoneNetwork = tombStone.Obj.(*danmtypes.TenantNetwork)
		if !isObjectInTombStoneNetwork {
			log.Println("ERROR: Can't delete interfaces for TenantNetwork, 'cause we have received an invalid object from the K8s API server in the Event tombstone")
			return
		}
	}
	dn := ConvertTnetToDnet(tn)
	err := deleteNetworks(dn)
	if err != nil {
		log.Println("INFO: Deletion of host interfaces for TenantNetwork:" + dn.ObjectMeta.Name + " failed with error:" + err.Error())
	}
}

func AddClusterNetwork(obj interface{}) {
	cn, isNetwork := obj.(*danmtypes.ClusterNetwork)
	if !isNetwork {
		log.Println("ERROR: Can't create interfaces for ClusterNetwork, 'cause we have received an invalid object from the K8s API server")
		return
	}
	dnet := ConvertCnetToDnet(cn)
	err := setupHost(dnet)
	if err != nil {
		log.Println("INFO: Creating host interfaces for ClusterNetwork:" + dnet.ObjectMeta.Name + " failed with error:" + err.Error())
	}
}

func UpdateClusterNetwork(oldObj, newObj interface{}) {
	oldCn, isNetwork := oldObj.(*danmtypes.ClusterNetwork)
	if !isNetwork {
		log.Println("ERROR: Can't update interfaces for ClusterNetwork change, 'cause we have received an invalid old object from the K8s API server")
		return
	}
	newCn, isNetwork := newObj.(*danmtypes.ClusterNetwork)
	if !isNetwork {
		log.Println("ERROR: Can't update interfaces for ClusterNetwork change, 'cause we have received an invalid new object from the K8s API server")
		return
	}
	oldDn := ConvertCnetToDnet(oldCn)
	newdDn := ConvertCnetToDnet(newCn)
	zeroVnis(oldDn, newdDn)
	err := deleteNetworks(oldDn)
	if err != nil {
		log.Println("INFO: Deletion of old host interfaces for ClusterNetwork:" + oldDn.ObjectMeta.Name + " after update failed with error:" + err.Error())
	}
	err = setupHost(newdDn)
	if err != nil {
		log.Println("INFO: Creating host interfaces for new ClusterNetwork:" + newdDn.ObjectMeta.Name + " after update failed with error:" + err.Error())
	}
}

func DeleteClusterNetwork(obj interface{}) {
	cn, isNetwork := obj.(*danmtypes.ClusterNetwork)
	if !isNetwork {
		tombStone, objIsTombstone := obj.(cache.DeletedFinalStateUnknown)
		if !objIsTombstone {
			log.Println("ERROR: Can't delete interfaces for ClusterNetwork, 'cause we have received an invalid object from the K8s API server")
			return
		}
		var isObjectInTombStoneNetwork bool
		cn, isObjectInTombStoneNetwork = tombStone.Obj.(*danmtypes.ClusterNetwork)
		if !isObjectInTombStoneNetwork {
			log.Println("ERROR: Can't delete interfaces for ClusterNetwork, 'cause we have received an invalid object from the K8s API server in the Event tombstone")
			return
		}
	}
	dn := ConvertCnetToDnet(cn)
	err := deleteNetworks(dn)
	if err != nil {
		log.Println("INFO: Deletion of host interfaces for ClusterNetwork:" + dn.ObjectMeta.Name + " failed with error:" + err.Error())
	}
}

func (netWatcher *NetWatcher) AddNad(obj interface{}) {
	nad, isNetwork := obj.(*nadtypes.NetworkAttachmentDefinition)
	if !isNetwork {
		log.Println("ERROR: Can't create interfaces for NetworkAttachmentDefinition, 'cause we have received an invalid object from the K8s API server")
		return
	}
	dnet, err := convertNadToDnet(nad)
	if err != nil {
		log.Println("INFO: Creating host interfaces for NetworkAttachmentDefinition:" + nad.ObjectMeta.Name + " failed with error:" + err.Error())
		return
	}
	err = setupHost(dnet)
	if err != nil {
		log.Println("INFO: Creating host interfaces for NetworkAttachmentDefinition:" + nad.ObjectMeta.Name + " failed with error:" + err.Error())
		return
	}
	//Upstream IPVLAN/MACVLAN plugins are dumb animals, so we need to modify parent device in their NAD to the exact VLAN/VxLAN host interface
	//TODO: on one hand this would make much more sense to be done in an admission controller, on the other one it makes sense for netwatcher to be self-containing
	//      Let's see if this causes issues in production. A random initial Pod restart here and there when the network and a Pod using it are created the same time we can live with IMO
	if dnet.Spec.Options.Vlan != 0 || dnet.Spec.Options.Vxlan != 0 {
		nad.Spec.Config = string(PatchCniConf([]byte(nad.Spec.Config), "master", DetermineHostDeviceName(dnet)))
		_, err = netWatcher.NadClient.K8sCniCncfIoV1().NetworkAttachmentDefinitions(nad.ObjectMeta.Namespace).Update(context.TODO(), nad, meta_v1.UpdateOptions{})
		if err != nil {
			log.Println("INFO: Could not update NetworkAttachmentDefinition:" + nad.ObjectMeta.Name + " with the new parent interface name because:" + err.Error())
		}
	}
}

func (netWatcher *NetWatcher) UpdateNad(oldObj, newObj interface{}) {
	oldNad, isNetwork := oldObj.(*nadtypes.NetworkAttachmentDefinition)
	if !isNetwork {
		log.Println("ERROR: Can't update interfaces for NetworkAttachmentDefinition change, 'cause we have received an invalid old object from the K8s API server")
		return
	}
	newNad, isNetwork := newObj.(*nadtypes.NetworkAttachmentDefinition)
	if !isNetwork {
		log.Println("ERROR: Can't update interfaces for NetworkAttachmentDefinition change, 'cause we have received an invalid new object from the K8s API server")
		return
	}
	oldDn, err := convertNadToDnet(oldNad)
	if err != nil {
		log.Println("INFO: Modifying host interfaces for NetworkAttachmentDefinition:" + oldNad.ObjectMeta.Name + " failed with error:" + err.Error())
		return
	}
	newdDn, err := convertNadToDnet(newNad)
	if err != nil {
		log.Println("INFO: Modifying host interfaces for NetworkAttachmentDefinition:" + newNad.ObjectMeta.Name + " failed with error:" + err.Error())
		return
	}
	parentUpdateNeeded := (DetermineHostDeviceName(oldDn) != DetermineHostDeviceName(newdDn))
	zeroVnis(oldDn, newdDn)
	err = deleteNetworks(oldDn)
	if err != nil {
		log.Println("INFO: Deletion of old host interfaces for NetworkAttachmentDefinition:" + oldNad.ObjectMeta.Name + " after update failed with error:" + err.Error())
	}
	err = setupHost(newdDn)
	if err != nil {
		log.Println("INFO: Creating host interfaces for modified NetworkAttachmentDefinition:" + newNad.ObjectMeta.Name + " after update failed with error:" + err.Error())
	}
	if parentUpdateNeeded {
		newNad.Spec.Config = string(PatchCniConf([]byte(newNad.Spec.Config), "master", DetermineHostDeviceName(newdDn)))
		_, err = netWatcher.NadClient.K8sCniCncfIoV1().NetworkAttachmentDefinitions(newNad.ObjectMeta.Namespace).Update(context.TODO(), newNad, meta_v1.UpdateOptions{})
		if err != nil {
			log.Println("INFO: Could not update NetworkAttachmentDefinition:" + newNad.ObjectMeta.Name + " with the new parent interface name because:" + err.Error())
		}
	}
}

func DeleteNad(obj interface{}) {
	nad, isNetwork := obj.(*nadtypes.NetworkAttachmentDefinition)
	if !isNetwork {
		tombStone, objIsTombstone := obj.(cache.DeletedFinalStateUnknown)
		if !objIsTombstone {
			log.Println("ERROR: Can't delete interfaces for NetworkAttachmentDefinition, 'cause we have received an invalid object from the K8s API server")
			return
		}
		var isObjectInTombStoneNetwork bool
		nad, isObjectInTombStoneNetwork = tombStone.Obj.(*nadtypes.NetworkAttachmentDefinition)
		if !isObjectInTombStoneNetwork {
			log.Println("ERROR: Can't delete interfaces for NetworkAttachmentDefinition, 'cause we have received an invalid object from the K8s API server in the Event tombstone")
			return
		}
	}
	dnet, err := convertNadToDnet(nad)
	if err != nil {
		log.Println("INFO: Deleting host interfaces for NetworkAttachmentDefinition:" + nad.ObjectMeta.Name + " failed with error:" + err.Error())
		return
	}
	err = deleteNetworks(dnet)
	if err != nil {
		log.Println("INFO: Deletion of host interfaces for NetworkAttachmentDefinition:" + nad.ObjectMeta.Name + " failed with error:" + err.Error())
	}
}

func ConvertTnetToDnet(tnet *danmtypes.TenantNetwork) *danmtypes.DanmNet {
	dnet := danmtypes.DanmNet{
		TypeMeta:   tnet.TypeMeta,
		ObjectMeta: tnet.ObjectMeta,
		Spec:       tnet.Spec,
	}
	//Why do I need to set this, you could ask?
	//Well, don't: https://github.com/kubernetes/client-go/issues/308
	dnet.TypeMeta.Kind = TenantNetworkKind
	return &dnet
}

func ConvertCnetToDnet(cnet *danmtypes.ClusterNetwork) *danmtypes.DanmNet {
	dnet := danmtypes.DanmNet{
		TypeMeta:   cnet.TypeMeta,
		ObjectMeta: cnet.ObjectMeta,
		Spec:       cnet.Spec,
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
	}
}

func ConvertDnetToCnet(dnet *danmtypes.DanmNet) *danmtypes.ClusterNetwork {
	return &danmtypes.ClusterNetwork{
		TypeMeta:   dnet.TypeMeta,
		ObjectMeta: dnet.ObjectMeta,
		Spec:       dnet.Spec,
	}
}

func PutNetwork(danmClient danmclientset.Interface, dnet *danmtypes.DanmNet) (bool, error) {
	var err error
	var wasResourceAlreadyUpdated bool
	if dnet.TypeMeta.Kind == DanmNetKind || dnet.TypeMeta.Kind == "" {
		_, err = danmClient.DanmV1().DanmNets(dnet.ObjectMeta.Namespace).Update(context.TODO(), dnet, meta_v1.UpdateOptions{})
	} else if dnet.TypeMeta.Kind == TenantNetworkKind {
		tn := ConvertDnetToTnet(dnet)
		_, err = danmClient.DanmV1().TenantNetworks(dnet.ObjectMeta.Namespace).Update(context.TODO(), tn, meta_v1.UpdateOptions{})
	} else if dnet.TypeMeta.Kind == ClusterNetworkKind {
		cn := ConvertDnetToCnet(dnet)
		_, err = danmClient.DanmV1().ClusterNetworks().Update(context.TODO(), cn, meta_v1.UpdateOptions{})
	} else {
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
	var netName, netType string
	if iface.Network != "" {
		netName = iface.Network
		netType = DanmNetKind
		dnet, err := danmClient.DanmV1().DanmNets(nameSpace).Get(context.TODO(), iface.Network, meta_v1.GetOptions{})
		if err == nil && dnet.ObjectMeta.Name == iface.Network {
			dnet.TypeMeta.Kind = netType
			return dnet, nil
		}
	} else if iface.TenantNetwork != "" {
		netName = iface.TenantNetwork
		netType = TenantNetworkKind
		tnet, err := danmClient.DanmV1().TenantNetworks(nameSpace).Get(context.TODO(), iface.TenantNetwork, meta_v1.GetOptions{})
		if err == nil && tnet.ObjectMeta.Name == iface.TenantNetwork {
			dnet := ConvertTnetToDnet(tnet)
			return dnet, nil
		}
	} else if iface.ClusterNetwork != "" {
		netName = iface.ClusterNetwork
		netType = ClusterNetworkKind
		cnet, err := danmClient.DanmV1().ClusterNetworks().Get(context.TODO(), iface.ClusterNetwork, meta_v1.GetOptions{})
		if err == nil && cnet.ObjectMeta.Name == iface.ClusterNetwork {
			dnet := ConvertCnetToDnet(cnet)
			return dnet, nil
		}
	}
	return nil, errors.New("requested network:" + netName + " of type:" + netType + " in namespace:" + nameSpace + " does not exist")
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

func PatchCniConf(rawConf []byte, patchKey string, patchValue interface{}) []byte {
	transparentCniConf := map[string]interface{}{}
	json.Unmarshal(rawConf, &transparentCniConf)
	transparentCniConf[patchKey] = patchValue
	moddedCniConf, _ := json.Marshal(transparentCniConf)
	return moddedCniConf
}

// Little trickery: if there was no change in the VNI+host_device combo during the update we set it to 0 in the manifests.
// Thus we avoid unnecessarily recreating host interfaces.
func zeroVnis(oldDn, newDn *danmtypes.DanmNet) {
	if oldDn.Spec.Options.Vlan == newDn.Spec.Options.Vlan && oldDn.Spec.Options.Device == newDn.Spec.Options.Device {
		oldDn.Spec.Options.Vlan = 0
		newDn.Spec.Options.Vlan = 0
	}
	if oldDn.Spec.Options.Vxlan == newDn.Spec.Options.Vxlan && oldDn.Spec.Options.Device == newDn.Spec.Options.Device {
		oldDn.Spec.Options.Vxlan = 0
		newDn.Spec.Options.Vxlan = 0
	}
}
