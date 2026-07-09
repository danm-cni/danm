package danmep

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/containernetworking/plugins/pkg/utils/sysctl"
	danmtypes "github.com/danm-cni/danm/crd/apis/danm/v1"
	danmclientset "github.com/danm-cni/danm/crd/client/clientset/versioned"
	"github.com/danm-cni/danm/pkg/datastructs"
	"github.com/danm-cni/danm/pkg/ipam"
	"github.com/danm-cni/danm/pkg/netcontrol"
	sriov_utils "github.com/intel/sriov-cni/pkg/utils"
	uuid "github.com/satori/go.uuid"
	"github.com/vishvananda/netlink"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type sysctlFunction func(*danmtypes.DanmEp) bool
type sysctlObject struct {
	sysctlName  string
	sysctlValue string
}
type sysctlTask struct {
	sysctlFunc sysctlFunction
	sysctlData []sysctlObject
}

var sysctls = []sysctlTask{
	{
		sysctlFunc: isIPv6Needed,
		sysctlData: []sysctlObject{
			{"net.ipv6.conf.%s.disable_ipv6", "0"},
			{"net.ipv6.conf.%s.autoconf", "0"},
			{"net.ipv6.conf.%s.accept_ra", "0"},
			{"net.ipv6.conf.%s.ndisc_notify", "1"},
		},
	},
	{
		sysctlFunc: isIPv6NotNeeded,
		sysctlData: []sysctlObject{
			{"net.ipv6.conf.%s.disable_ipv6", "1"},
		},
	},
}

const (
	MaxRetryCount = 10
	RetryInterval = 100
)

// DeleteIpvlanInterface deletes a Pod's IPVLAN network interface based on the related DanmEp
func DeleteIpvlanInterface(ep *danmtypes.DanmEp) error {
	return deleteEp(ep)
}

// FindByCid returns a map of DanmEps which belongs to the same infra container ID
func FindByCid(client danmclientset.Interface, cid string) ([]danmtypes.DanmEp, error) {
	var err error
	var result *danmtypes.DanmEpList
	//Critical CNI_DEL calls depends on this function, so we will re-try for one sec to be able to cope with temporary network disruptions
	for i := 0; i < MaxRetryCount; i++ {
		result, err = client.DanmV1().DanmEps("").List(context.TODO(), meta_v1.ListOptions{})
		if err == nil {
			break
		}
		time.Sleep(RetryInterval * time.Millisecond)
	}
	if err != nil {
		return nil, errors.New("cannot list DanmEps because:" + err.Error())
	}
	ret := make([]danmtypes.DanmEp, 0)
	if result == nil {
		return ret, nil
	}
	eplist := result.Items
	for _, ep := range eplist {
		if ep.Spec.CID == cid {
			ret = append(ret, ep)
		}
	}
	return ret, nil
}

// CidsByHost returns a map of Eps
// The Eps in the map are indexed with the name of the K8s host their Pods are running on
func CidsByHost(client danmclientset.Interface, host string) (map[string]danmtypes.DanmEp, error) {
	result, err := client.DanmV1().DanmEps("").List(context.TODO(), meta_v1.ListOptions{})
	if err != nil {
		return nil, errors.New("cannot list DanmEps because:" + err.Error())
	}
	ret := make(map[string]danmtypes.DanmEp, 0)
	if result == nil {
		return ret, nil
	}
	eplist := result.Items
	for _, ep := range eplist {
		if ep.Spec.Host == host {
			ret[ep.Spec.CID] = ep
		}
	}
	return ret, nil
}

// FindByPodName returns a map of DanmEps which belong to the same Pod in a given namespace
// If no Pod name is provided, function returns no DanmEps
func FindByPodName(client danmclientset.Interface, podName, ns string) ([]danmtypes.DanmEp, error) {
	result, err := client.DanmV1().DanmEps(ns).List(context.TODO(), meta_v1.ListOptions{})
	if err != nil {
		return nil, errors.New("cannot list DanmEps because:" + err.Error())
	}
	ret := make([]danmtypes.DanmEp, 0)
	if result == nil {
		return ret, nil
	}
	eplist := result.Items
	for _, ep := range eplist {
		if podName != "" && ep.Spec.Pod != podName {
			continue
		}
		ret = append(ret, ep)
	}
	return ret, nil
}

// FindByPodUid returns a map of DanmEps which belong to the same Pod instance in a given namespace
// If no Pod name is provided, function returns no DanmEps
func FindByPodUid(client danmclientset.Interface, podUid, ns string) ([]danmtypes.DanmEp, error) {
	result, err := client.DanmV1().DanmEps(ns).List(context.TODO(), meta_v1.ListOptions{})
	if err != nil {
		return nil, errors.New("cannot list DanmEps because:" + err.Error())
	}
	ret := make([]danmtypes.DanmEp, 0)
	if result == nil {
		return ret, nil
	}
	eplist := result.Items
	for _, ep := range eplist {
		if podUid != "" && string(ep.Spec.PodUID) != podUid {
			continue
		}
		ret = append(ret, ep)
	}
	return ret, nil
}

func AddIpvlanInterface(dnet *danmtypes.DanmNet, ep *danmtypes.DanmEp) error {
	if ep.Spec.NetworkType != "ipvlan" {
		return nil
	}
	return createIpvlanInterface(dnet, ep)
}

func PostProcessInterface(ep *danmtypes.DanmEp, dnet *danmtypes.DanmNet) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	origNs, err := ns.GetCurrentNS()
	if err != nil {
		return errors.New("getting current namespace failed")
	}
	hns, err := ns.GetNS(ep.Spec.Netns)
	if err != nil {
		return errors.New("cannot open network namespace:" + ep.Spec.Netns)
	}
	defer func() {
		hns.Close()
		err = origNs.Set()
		if err != nil {
			log.Println("Could not switch back to default ns during IP route provisioning operation:" + err.Error())
		}
	}()
	err = hns.Set()
	if err != nil {
		return errors.New("failed to enter network namespace of CID:" + ep.Spec.Netns + " with error:" + err.Error())
	}
	isVfAttachedToDpdkDriver, _ := sriov_utils.HasDpdkDriver(ep.Spec.Iface.DeviceID)
	if isVfAttachedToDpdkDriver {
		err = createDummyInterface(ep, dnet)
		if err != nil {
			return errors.New("failed to create dummy kernel interface for " + ep.Spec.Iface.Name + " because:" + err.Error())
		}
	}
	link, err := netlink.LinkByName(ep.Spec.Iface.Name)
	if err != nil {
		log.Println("WARNING: Interface post-processing was skipped for Pod:" + ep.Spec.Pod + " and link:" + ep.Spec.Iface.Name + " because it does not exist in the kernel. If it is not a user space interface, you should investigate!!!")
		return nil
	}
	err = setDanmEpSysctls(ep)
	if err != nil {
		return errors.New("failed to set kernel configs for interface" + ep.Spec.Iface.Name + " because:" + err.Error())
	}
	err = disableDadOnIface(link, ep)
	if err != nil {
		return errors.New("failed to disable DAD for address" + ep.Spec.Iface.AddressIPv6 + " because:" + err.Error())
	}
	return addIpRoutes(link, ep, dnet)
}

func setDanmEpSysctls(ep *danmtypes.DanmEp) error {
	var err error
	for _, s := range sysctls {
		if s.sysctlFunc(ep) {
			for _, ss := range s.sysctlData {
				sss := fmt.Sprintf(ss.sysctlName, ep.Spec.Iface.Name)
				_, err = sysctl.Sysctl(sss, ss.sysctlValue)
				if err != nil {
					return errors.New("failed to set sysctl due to:" + err.Error())
				}
			}
		}
	}
	return nil
}

func isIPv6Needed(ep *danmtypes.DanmEp) bool {
	if ep.Spec.Iface.AddressIPv6 != "" {
		return true
	}
	return false
}

func isIPv6NotNeeded(ep *danmtypes.DanmEp) bool {
	if ep.Spec.Iface.AddressIPv6 == "" {
		return true
	}
	return false
}

// ArePodsConnectedToNetwork checks if there are any Pods currently in the system using the particular network.
// If there is at least, it returns true, and the spec of the first matching DanmEp.
func ArePodsConnectedToNetwork(client danmclientset.Interface, dnet *danmtypes.DanmNet) (bool, danmtypes.DanmEp, error) {
	result, err := client.DanmV1().DanmEps("").List(context.TODO(), meta_v1.ListOptions{})
	if err != nil {
		return false, danmtypes.DanmEp{}, errors.New("cannot list DanmEps because:" + err.Error())
	}
	if result == nil {
		return false, danmtypes.DanmEp{}, nil
	}
	eplist := result.Items
	for _, ep := range eplist {
		if (ep.Spec.ApiType == dnet.TypeMeta.Kind && ep.Spec.NetworkName == dnet.ObjectMeta.Name) &&
			(dnet.TypeMeta.Kind == "ClusterNetwork" || ep.ObjectMeta.Namespace == dnet.ObjectMeta.Namespace) {
			return true, ep, nil
		}
	}
	return false, danmtypes.DanmEp{}, nil
}

// CreateDanmEp is a RAII-like API to automatically reserve IP allocations whenever an object holding these allocations is created
// It helps making sure IPs are for sure universally reserved upon DanmEp creation itself
// TODO: I hate myself for the bool input parameter, but ipam absolutely should not depend on cnidel. Could be changed to cleverly defaulting iface attributes to sthing?
func CreateDanmEp(danmClient danmclientset.Interface, namingScheme string, isIpReservationNeeded bool, netInfo *danmtypes.DanmNet, iface datastructs.Interface, args *datastructs.CniArgs) (*danmtypes.DanmEp, *danmtypes.DanmNet, error) {
	var (
		ip4 = iface.Ip
		ip6 = iface.Ip6
		err error
	)
	if isIpReservationNeeded {
		ip4, ip6, err = ipam.Reserve(danmClient, *netInfo, iface.Ip, iface.Ip6)
		if err != nil {
			return nil, netInfo, errors.New("IP address reservation failed for network:" + netInfo.ObjectMeta.Name + " with error:" + err.Error())
		}
	}
	epSpec := danmtypes.DanmEpIface{
		Name:        calculateIfaceName(namingScheme, netInfo.Spec.Options.Prefix, iface.DefaultIfaceName, iface.SequenceId),
		Address:     ip4,
		AddressIPv6: ip6,
		Proutes:     iface.Proutes,
		Proutes6:    iface.Proutes6,
		DeviceID:    iface.Device,
	}
	var hwAddress net.HardwareAddr
	if iface.Device != "" {
		hwAddress = getVfMac(iface.Device)
		if hwAddress.String() != "" {
			epSpec.MacAddress = hwAddress.String()
		}
	}
	ep, err := createDanmEp(danmClient, epSpec, netInfo, args)
	if err != nil {
		return nil, netInfo, errors.New("DanmEp object could not be created due to error:" + err.Error())
	}
	//As netInfo is only copied to IPAM above, the IP allocation is not refreshed in the original copy.
	//Without re-reading the network body we risk leaking IPs if an error happens later on within the same thread!
	dnet, err := netcontrol.GetNetworkFromEp(danmClient, ep)
	if err != nil {
		return ep, dnet, errors.New("network manifest could not be refreshed after IP allocations due to error:" + err.Error())
	}
	return ep, dnet, nil
}

// CalculateIfaceName decides what should be the name of a container's interface.
// If a name is explicitly set in the related network API object, the NIC will be named accordingly.
// If a name is not explicitly set, then DANM names the interface ethX where X=sequence number of the interface
// When legacy naming scheme is configured container_prefix behaves as the exact name of an interface, rather than its name suggest
func calculateIfaceName(namingScheme, chosenName, defaultName string, sequenceId int) string {
	//Kubelet expects the first interface to be literally named "eth0", so...
	if sequenceId == 0 {
		return "eth0"
	}
	if chosenName != "" {
		if namingScheme != datastructs.LegacyNamingScheme {
			chosenName += strconv.Itoa(sequenceId)
		}
		return chosenName
	}
	return defaultName + strconv.Itoa(sequenceId)
}

func createDanmEp(danmClient danmclientset.Interface, epInput danmtypes.DanmEpIface, netInfo *danmtypes.DanmNet, args *datastructs.CniArgs) (*danmtypes.DanmEp, error) {
	epidInt, err := uuid.NewV4()
	if err != nil {
		return nil, errors.New("uuid.NewV4 returned error during EP creation:" + err.Error())
	}
	epid := epidInt.String()
	host, err := os.Hostname()
	if err != nil {
		return nil, errors.New("OS.Hostname returned error during EP creation:" + err.Error())
	}
	epSpec := danmtypes.DanmEpSpec{
		NetworkName: netInfo.ObjectMeta.Name,
		NetworkType: netInfo.Spec.NetworkType,
		EndpointID:  epid,
		Iface:       epInput,
		Host:        host,
		Pod:         args.PodName,
		PodUID:      args.Pod.ObjectMeta.UID,
		CID:         args.ContainerId,
		Netns:       args.Netns,
		ApiType:     netInfo.TypeMeta.Kind,
	}
	meta := meta_v1.ObjectMeta{
		Name:            epid,
		Namespace:       args.Namespace,
		ResourceVersion: "",
		Labels:          args.Pod.Labels,
	}
	typeMeta := meta_v1.TypeMeta{
		APIVersion: danmtypes.SchemeGroupVersion.String(),
		Kind:       "DanmEp",
	}
	ep := danmtypes.DanmEp{
		TypeMeta:   typeMeta,
		ObjectMeta: meta,
		Spec:       epSpec,
	}
	newEp, err := danmClient.DanmV1().DanmEps(ep.Namespace).Create(context.TODO(), &ep, meta_v1.CreateOptions{})
	if err != nil {
		return newEp, errors.New("DanmEp object could not be PUT to K8s API server due to error:" + err.Error())
	}
	return newEp, nil
}

// UpdateDanmEp is a more network outage resilient version of the one provided by the base K8s client
func UpdateDanmEp(client danmclientset.Interface, ep *danmtypes.DanmEp) error {
	var err error
	for i := 0; i < MaxRetryCount; i++ {
		_, err = client.DanmV1().DanmEps(ep.Namespace).Update(context.TODO(), ep, meta_v1.UpdateOptions{})
		if err == nil {
			break
		}
		time.Sleep(RetryInterval * time.Millisecond)
	}
	return err
}

// DeleteDanmEp is a RAII-like API to automatically free IP allocations whenever the resource holding these allocations is deleted
// It helps making sure IPs are always and only freed when a DanmEp is indeed deleted
func DeleteDanmEp(danmClient danmclientset.Interface, ep *danmtypes.DanmEp, dnet *danmtypes.DanmNet) error {
	var err error
	if (ep.Spec.Iface.Address != "" || ep.Spec.Iface.AddressIPv6 != "") && dnet == nil {
		return errors.New("DanmEp:" + ep.ObjectMeta.Name + " cannot be safely deleted because its linked network is not available to free DANM IPAM allocated IPs")
	}
	//We only need to Free an IP if it was allocated by DANM IPAM, and it was allocated by DANM only if it falls into any of the defined subnets
	if ipam.WasIpAllocatedByDanm(ep.Spec.Iface.Address, dnet.Spec.Options.Cidr) || ipam.WasIpAllocatedByDanm(ep.Spec.Iface.AddressIPv6, dnet.Spec.Options.Pool6.Cidr) {
		err = ipam.GarbageCollectIps(danmClient, dnet, ep.Spec.Iface.Address, ep.Spec.Iface.AddressIPv6)
		if err != nil {
			return errors.New("DanmEp:" + ep.ObjectMeta.Name + " cannot be safely deleted because freeing its reserved IP addresses failed with error:" + err.Error())
		}
	}
	return danmClient.DanmV1().DanmEps(ep.ObjectMeta.Namespace).Delete(context.TODO(), ep.ObjectMeta.Name, meta_v1.DeleteOptions{})
}

func getVfMac(pciId string) net.HardwareAddr {
	pfName, _ := sriov_utils.GetPfName(pciId)
	vfId, err := sriov_utils.GetVfid(pciId, pfName)
	if err != nil {
		return net.HardwareAddr{}
	}
	pfLink, err := netlink.LinkByName(pfName)
	if err != nil {
		return net.HardwareAddr{}
	}
	if pfLink.Attrs() != nil && len(pfLink.Attrs().Vfs) >= vfId {
		return pfLink.Attrs().Vfs[vfId].Mac
	}
	return net.HardwareAddr{}
}
