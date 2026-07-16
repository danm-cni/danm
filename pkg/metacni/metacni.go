package metacni

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	current "github.com/containernetworking/cni/pkg/types/040"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/containernetworking/plugins/pkg/utils/sysctl"
	danmtypes "github.com/danm-cni/danm/crd/apis/danm/v1"
	danmclientset "github.com/danm-cni/danm/crd/client/clientset/versioned"
	"github.com/danm-cni/danm/pkg/cnidel"
	"github.com/danm-cni/danm/pkg/danmep"
	"github.com/danm-cni/danm/pkg/datastructs"
	"github.com/danm-cni/danm/pkg/ipam"
	"github.com/danm-cni/danm/pkg/netcontrol"
	"github.com/danm-cni/danm/pkg/syncher"
	podresclient "gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/kubeletclient"
	multus_types "gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/types"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	danmApiPath            = "danm.io"
	danmIfDefinitionSyntax = danmApiPath + "/interfaces"
	v1Endpoint             = "/api/v1/"
	cniVersion             = "0.3.1"
	defaultNetworkName     = "default"
	defaultIfName          = "eth"
	DefaultCniDir          = "/etc/cni/net.d"
)

var (
	apiHost    = os.Getenv("API_SERVERS")
	DanmConfig *datastructs.NetConf
)

// K8sArgs is the valid CNI_ARGS type used to parse K8s CNI event calls (thanks Multus)
type K8sArgs struct {
	types.CommonArgs
	IP                         net.IP
	K8S_POD_NAME               types.UnmarshallableString
	K8S_POD_NAMESPACE          types.UnmarshallableString
	K8S_POD_INFRA_CONTAINER_ID types.UnmarshallableString
}

func CreateInterfaces(args *skel.CmdArgs) error {
	cniArgs, err := extractCniArgs(args)
	if err != nil {
		log.Println("ERROR: ADD: CNI args cannot be loaded with error:" + err.Error())
		return fmt.Errorf("CNI args cannot be loaded with error: %v", err)
	}
	log.Println("CNI ADD invoked with: ns:" + cniArgs.Namespace + " for Pod:" + cniArgs.PodName + " CID: " + cniArgs.ContainerId)
	err = loadNetConf(cniArgs.StdIn)
	if err != nil {
		return errors.New("ERROR: ADD: cannot load DANM CNI config due to error:" + err.Error())
	}
	err = getPod(cniArgs)
	if err != nil {
		log.Println("ERROR: ADD: Pod manifest could not be parsed with error:" + err.Error())
		return fmt.Errorf("Pod manifest could not be parsed with error: %v", err)
	}
	err = extractConnections(cniArgs)
	if err != nil {
		log.Println("ERROR: ADD: DANM annotation cannot be parsed:" + err.Error())
		return fmt.Errorf("DANM annotation cannot be parsed: %v", err)
	}
	if len(cniArgs.Interfaces) == 0 {
		danmClient, err := CreateDanmClient(DanmConfig.Kubeconfig)
		if err != nil {
			log.Println("ERROR: cannot instantiate K8s client, because:" + err.Error())
			return fmt.Errorf("ERROR: cannot instantiate K8s client: %v", err)
		}
		defaultNet, err := netcontrol.GetDefaultNetwork(danmClient, defaultNetworkName, cniArgs.Pod.ObjectMeta.Namespace)
		if err != nil {
			log.Println("ERROR: there are no network connections defined for Pod:" + cniArgs.Pod.ObjectMeta.Name + ", and there is no suitable default network configured in the cluster!")
			return errors.New("there are no network connections defined, and there is no suitable default network configured in the cluster")
		}
		cniArgs.DefaultNetwork = defaultNet
	}
	cniResult, err := setupNetworking(cniArgs)
	if err != nil {
		//Best effort cleanup - not interested in possible errors, anyway could not do anything with them
		os.Setenv("CNI_COMMAND", "DEL")
		DeleteInterfaces(args)
		log.Println("ERROR: ADD: CNI network could not be set up with error:" + err.Error())
		return fmt.Errorf("CNI network could not be set up: %v", err)
	}
	return types.PrintResult(cniResult, cniVersion)
}

func CreateDanmClient(kubeConfig string) (danmclientset.Interface, error) {
	config, err := getClientConfig(kubeConfig)
	if err != nil {
		return nil, errors.New("Parsing kubeconfig failed with error:" + err.Error())
	}
	client, err := danmclientset.NewForConfig(config)
	if err != nil {
		return nil, errors.New("Creation of K8s Danm REST client failed with error:" + err.Error())
	}
	return client, nil
}

func getClientConfig(kubeConfig string) (*rest.Config, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeConfig)
	if err != nil {
		return nil, err
	}
	return config, nil
}

func loadNetConf(bytes []byte) error {
	netconf := &datastructs.NetConf{}
	err := json.Unmarshal(bytes, netconf)
	if err != nil {
		return errors.New("Failed to parse DANM's CNI config file:" + err.Error())
	}
	DanmConfig = netconf
	if DanmConfig.CniConfigDir == "" {
		DanmConfig.CniConfigDir = DefaultCniDir
	}
	return nil
}

func extractCniArgs(args *skel.CmdArgs) (*datastructs.CniArgs, error) {
	kubeArgs := K8sArgs{}
	err := types.LoadArgs(args.Args, &kubeArgs)
	if err != nil {
		return nil, err
	}
	cmdArgs := datastructs.CniArgs{
		Namespace:      string(kubeArgs.K8S_POD_NAMESPACE),
		Netns:          args.Netns,
		PodName:        string(kubeArgs.K8S_POD_NAME),
		ContainerId:    string(kubeArgs.K8S_POD_INFRA_CONTAINER_ID),
		StdIn:          args.StdinData,
		Interfaces:     nil,
		Pod:            nil,
		DefaultNetwork: nil,
	}
	return &cmdArgs, nil
}

func getPod(args *datastructs.CniArgs) error {
	k8sClient, err := createK8sClient(DanmConfig.Kubeconfig)
	if err != nil {
		return errors.New("cannot create K8s REST client due to error:" + err.Error())
	}
	pod, err := k8sClient.CoreV1().Pods(string(args.Namespace)).Get(context.TODO(), string(args.PodName), meta_v1.GetOptions{})
	if err != nil {
		return errors.New("failed to get Pod info from K8s API server due to:" + err.Error())
	}
	args.Pod = pod
	return nil
}

func createK8sClient(kubeconfig string) (kubernetes.Interface, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(config)
}

func extractConnections(args *datastructs.CniArgs) error {
	var ifaces []datastructs.Interface
	for key, val := range args.Pod.Annotations {
		if strings.Contains(key, danmIfDefinitionSyntax) {
			decoder := json.NewDecoder(bytes.NewReader([]byte(val)))
			//We are using Decoder interface, because it can notify us if any unknown fields were put into the object
			decoder.DisallowUnknownFields()
			err := decoder.Decode(&ifaces)
			if err != nil {
				return errors.New("Can't create network interfaces for Pod: " + args.Pod.ObjectMeta.Name + " due to badly formatted " + danmIfDefinitionSyntax + " definition in Pod annotation:" + err.Error())
			}
			break
		}
	}
	if err := validateAnnotation(ifaces); err != nil {
		return errors.New("DANM annotation is invalid for Pod: " + args.Pod.ObjectMeta.Name + ", because:" + err.Error())
	}
	args.Interfaces = ifaces
	return nil
}

func validateAnnotation(ifaces []datastructs.Interface) error {
	for ifaceId, iface := range ifaces {
		var definedNetworks int
		if iface.Network != "" {
			definedNetworks++
		}
		if iface.TenantNetwork != "" {
			definedNetworks++
		}
		if iface.ClusterNetwork != "" {
			definedNetworks++
		}
		if definedNetworks != 1 {
			return errors.New("network connection no.:" + strconv.Itoa(ifaceId) + " contains invalid number of network references:" + strconv.Itoa(definedNetworks))
		}
	}
	return nil
}

func setupNetworking(args *datastructs.CniArgs) (*current.Result, error) {
	err := preparePodForIpv6(args)
	if err != nil {
		return nil, errors.New("failed to prepare Pod for IPv6 due to:" + err.Error())
	}
	allocatedDevices := make(map[string]*[]string)
	syncher := syncher.NewSyncher(len(args.Interfaces))
	danmClient, err := CreateDanmClient(DanmConfig.Kubeconfig)
	if err != nil {
		return nil, err
	}
	cleanOutdatedAllocations(danmClient, args)
	if args.DefaultNetwork != nil {
		syncher.ExpectedNumOfResults++
		defParam := datastructs.Interface{SequenceId: 0, Ip: "dynamic"}
		err = createIface(args, danmClient, args.DefaultNetwork, defParam, syncher, allocatedDevices)
		if err != nil {
			syncher.PushResult(args.DefaultNetwork.ObjectMeta.Name, err, nil, "")
		}
	}
	for nicID, nicParams := range args.Interfaces {
		nicParams.SequenceId = nicID
		nicParams.DefaultIfaceName = defaultIfName
		netInfo, err := netcontrol.GetNetworkFromInterface(danmClient, nicParams, args.Pod.ObjectMeta.Namespace)
		if err != nil {
			syncher.PushResult("", errors.New("failed to get network object for Pod:"+args.Pod.ObjectMeta.Name+
				"'s connection no.:"+strconv.Itoa(nicID)+" due to:"+err.Error()), nil, "")
			continue
		}
		err = createIface(args, danmClient, netInfo, nicParams, syncher, allocatedDevices)
		if err != nil {
			syncher.PushResult(netInfo.ObjectMeta.Name, err, nil, "")
			continue
		}
	}
	err = syncher.GetAggregatedResult()
	return syncher.MergeCniResults(), err
}

func preparePodForIpv6(args *datastructs.CniArgs) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// save the current namespace
	origNs, err := ns.GetCurrentNS()
	if err != nil {
		return errors.New("failed to get current network namespace due to:" + err.Error())
	}
	// enter to the Pod's network namespace
	podNs, err := ns.GetNS(args.Netns)
	if err != nil {
		return errors.New("failed to get Pod's network namespace due to:" + err.Error())
	}
	err = podNs.Set()
	if err != nil {
		return errors.New("failed to switch to Pod's network namespace due to:" + err.Error())
	}
	defer func() {
		podNs.Close()
		origNs.Set()
	}()
	// set sysctl
	_, err = sysctl.Sysctl("net.ipv6.conf.all.disable_ipv6", "0")
	if err != nil {
		return errors.New("failed to set sysctl due to:" + err.Error())
	}
	return nil
}

func createIface(args *datastructs.CniArgs, danmClient danmclientset.Interface, netInfo *danmtypes.DanmNet, nicParams datastructs.Interface, syncher *syncher.Syncher, allocatedDevices map[string]*[]string) error {
	if !isTenantAllowed(args, netInfo) {
		return errors.New("Pod:" + args.PodName + "'s namespace:" + args.Namespace + " is not in the AllowedTenants whitelist of network:" + netInfo.ObjectMeta.Name)
	}
	var err error
	if cnidel.IsDeviceNeeded(netInfo.Spec.NetworkType) {
		if _, ok := allocatedDevices[netInfo.Spec.Options.DevicePool]; !ok {
			presClient, err := podresclient.GetResourceClient("")
			if err != nil {
				return errors.New("failed to instantiate Kubelet Pord Resource client due to:" + err.Error())
			}
			allocatedDevices[netInfo.Spec.Options.DevicePool], err = getAllocatedDevices(args, presClient, netInfo.Spec.Options.DevicePool)
			if err != nil {
				return errors.New("failed to get allocated devices due to:" + err.Error())
			}
		}
		nicParams.Device, err = popDevice(netInfo.Spec.Options.DevicePool, allocatedDevices)
		if err != nil {
			return errors.New("failed to pop devices due to:" + err.Error())
		}
	}
	go createNic(syncher, danmClient, nicParams, netInfo, args)
	return nil
}

func isTenantAllowed(args *datastructs.CniArgs, netInfo *danmtypes.DanmNet) bool {
	if len(netInfo.Spec.AllowedTenants) == 0 {
		return true
	}
	var isTenantAllowed bool
	for _, tenantName := range netInfo.Spec.AllowedTenants {
		if tenantName == args.Pod.ObjectMeta.Namespace {
			isTenantAllowed = true
			break
		}
	}
	return isTenantAllowed
}

func getAllocatedDevices(args *datastructs.CniArgs, presClient multus_types.ResourceClient, devicePool string) (*[]string, error) {
	resourceMap, err := presClient.GetPodResourceMap(args.Pod)
	if err != nil {
		return nil, errors.New("failed to retrieve Pod info from Kubelet Pod Resource client due to:" + err.Error())
	}
	if len(resourceMap) == 0 {
		return nil, errors.New("there were no Devices allocated for the Pod")
	}
	if _, ok := resourceMap[devicePool]; !ok {
		return nil, errors.New("failed to retrieve resources of DevicePool")
	}
	return &resourceMap[devicePool].DeviceIDs, nil
}

func popDevice(devicePool string, allocatedDevices map[string]*[]string) (string, error) {
	if len(allocatedDevices) == 0 {
		return "", errors.New("allocatedDevices is empty")
	}
	devices := (*allocatedDevices[devicePool])
	if len(devices) == 0 {
		return "", errors.New("devicePool is empty")
	}
	device, devices := devices[len(devices)-1], devices[:len(devices)-1]
	allocatedDevices[devicePool] = &devices
	return device, nil
}

func createNic(syncher *syncher.Syncher, danmClient danmclientset.Interface, iface datastructs.Interface, netInfo *danmtypes.DanmNet, args *datastructs.CniArgs) {
	isIpReservationNeeded := cnidel.IsDanmIpamNeededForDelegation(iface, netInfo) || netInfo.Spec.NetworkType == "ipvlan"
	ep, netInfo, err := danmep.CreateDanmEp(danmClient, DanmConfig.NamingScheme, isIpReservationNeeded, netInfo, iface, args)
	if err != nil {
		if ep != nil {
			danmep.DeleteDanmEp(danmClient, ep, netInfo)
		}
		syncher.PushResult(netInfo.ObjectMeta.Name, err, nil, "")
		return
	}
	var cniResult *current.Result
	if cnidel.IsDelegationRequired(netInfo) {
		cniResult, err = createDelegatedInterface(danmClient, isIpReservationNeeded, ep, netInfo, args)
	} else {
		cniResult, err = createDanmInterface(danmClient, ep, netInfo, args)
	}
	if err != nil {
		danmep.DeleteDanmEp(danmClient, ep, netInfo)
		syncher.PushResult(ep.Spec.NetworkName, err, cniResult, "")
		return
	}
	err = danmep.PostProcessInterface(ep, netInfo)
	if err != nil {
		danmep.DeleteDanmEp(danmClient, ep, netInfo)
		syncher.PushResult(ep.Spec.NetworkName, errors.New("Post-processing failed for interface:"+ep.Spec.Iface.Name+" because:"+err.Error()), nil, "")
		return
	}
	syncher.PushResult(ep.Spec.NetworkName, nil, cniResult, ep.Spec.Iface.Name)
}

func createDelegatedInterface(danmClient danmclientset.Interface, wasIpReservedByDanmIpam bool, ep *danmtypes.DanmEp, netInfo *danmtypes.DanmNet, args *datastructs.CniArgs) (*current.Result, error) {
	origV4Address := ep.Spec.Iface.Address
	origV6Address := ep.Spec.Iface.AddressIPv6
	delegatedResult, err := cnidel.DelegateInterfaceSetup(DanmConfig, wasIpReservedByDanmIpam, netInfo, ep)
	if err != nil {
		//TODO: is this -basically only host-ipam related- stuff really needed, or is just legacy residue?
		cnidel.FreeDelegatedIps(netInfo, ep.Spec.Iface.Address, ep.Spec.Iface.AddressIPv6)
		return delegatedResult, errors.New("CNI delegation failed due to error:" + err.Error())
	}
	if (origV4Address != ep.Spec.Iface.Address && origV4Address != ipam.NoneAllocType) ||
		(origV6Address != ep.Spec.Iface.AddressIPv6 && origV6Address != ipam.NoneAllocType) {
		err = danmep.UpdateDanmEp(danmClient, ep)
		if err != nil {
			//TODO: is this -basically only host-ipam related- stuff really needed, or is just legacy residue?
			cnidel.FreeDelegatedIps(netInfo, ep.Spec.Iface.Address, ep.Spec.Iface.AddressIPv6)
			return delegatedResult, errors.New("could not update DanmEp:" + ep.ObjectMeta.Name + " in namespace:" + ep.ObjectMeta.Namespace +
				" with the result returned by CNI plugin:" + netInfo.Spec.NetworkType + " because:" + err.Error())
		}
	}
	return delegatedResult, nil
}

func createDanmInterface(danmClient danmclientset.Interface, ep *danmtypes.DanmEp, netInfo *danmtypes.DanmNet, args *datastructs.CniArgs) (*current.Result, error) {
	err := danmep.AddIpvlanInterface(netInfo, ep)
	if err != nil {
		return nil, errors.New("IPVLAN interface could not be created due to error:" + err.Error())
	}
	danmResult := &current.Result{}
	AddIfaceToResult(ep.Spec.Iface.Name, args.ContainerId, danmResult)
	AddIpToResult(ep.Spec.Iface.Address, "4", danmResult)
	AddIpToResult(ep.Spec.Iface.AddressIPv6, "6", danmResult)
	return danmResult, nil
}

func AddIfaceToResult(ifName string, sandBox string, cniResult *current.Result) {
	iface := &current.Interface{
		Name:    ifName,
		Sandbox: sandBox,
	}
	cniResult.Interfaces = append(cniResult.Interfaces, iface)
}

func AddIpToResult(ip string, version string, cniResult *current.Result) {
	if ip != "" && ip != ipam.NoneAllocType {
		ip, _ := types.ParseCIDR(ip)
		ipConf := &current.IPConfig{
			Version: version,
			Address: *ip,
		}
		cniResult.IPs = append(cniResult.IPs, ipConf)
	}
}

func DeleteInterfaces(args *skel.CmdArgs) error {
	cniArgs, err := extractCniArgs(args)
	log.Println("CNI DEL invoked with: ns:" + cniArgs.Namespace + " for Pod:" + cniArgs.PodName + " CID: " + cniArgs.ContainerId)
	if err != nil {
		log.Println("INFO: DEL: CNI args could not be loaded because" + err.Error())
		return nil
	}
	err = loadNetConf(cniArgs.StdIn)
	if err != nil {
		log.Println("INFO: DEL: cannot load DANM CNI config due to error:" + err.Error())
		return nil
	}
	danmClient, err := CreateDanmClient(DanmConfig.Kubeconfig)
	if err != nil {
		log.Println("INFO: DEL: DanmEp REST client could not be created because" + err.Error())
		return nil
	}
	eplist, err := danmep.FindByCid(danmClient, cniArgs.ContainerId)
	if err != nil {
		log.Println("INFO: DEL: Could not interrogate DanmEps from K8s API server because" + err.Error())
		return nil
	}
	syncher := syncher.NewSyncher(len(eplist))
	//Note to self: NEVER change this to pass-by-pointer. It totally breaks CNI DEL for all but one interface
	for _, ep := range eplist {
		go deleteInterface(danmClient, cniArgs, syncher, ep)
	}
	deleteErrors := syncher.GetAggregatedResult()
	if deleteErrors != nil {
		log.Println("INFO: DEL: Following errors happened during interface deletion:" + deleteErrors.Error())
	}
	return nil
}

func deleteInterface(danmClient danmclientset.Interface, args *datastructs.CniArgs, syncher *syncher.Syncher, ep danmtypes.DanmEp) {
	//During delete we are not that interested in errors, but we also can't just return yet.
	//We need to try and clean-up as many remaining resources as possible
	var aggregatedError string
	netInfo, err := netcontrol.GetNetworkFromEp(danmClient, &ep)
	if err != nil {
		aggregatedError += "failed to get network:" + err.Error() + "; "
	}
	if netInfo != nil {
		err = deleteNic(netInfo, &ep)
		if err != nil {
			aggregatedError += "failed to delete container NIC:" + err.Error() + "; "
		}
	}
	err = danmep.DeleteDanmEp(danmClient, &ep, netInfo)
	if err != nil {
		aggregatedError += "failed to delete DanmEp:" + err.Error() + "; "
	}
	if aggregatedError != "" {
		syncher.PushResult(ep.Spec.NetworkName, errors.New(aggregatedError), nil, "")
	} else {
		syncher.PushResult(ep.Spec.NetworkName, nil, nil, ep.Spec.Iface.Name)
	}
}

func deleteNic(netInfo *danmtypes.DanmNet, ep *danmtypes.DanmEp) error {
	var err error
	if ep.Spec.NetworkType != "ipvlan" {
		err = cnidel.DelegateInterfaceDelete(DanmConfig, netInfo, ep)
	} else {
		err = danmep.DeleteIpvlanInterface(ep)
	}
	return err
}

func GetInterfaces(args *skel.CmdArgs) error {
	return nil
}

// I'm tired of cleaning up after Kubelet, but what can we do? :)
// After a full cluster restart Kubelet invokes a CNI_ADD for the same Pod, with the same UID.
// We need to take care of clearing old, invalid allocations for the same UID ourselves during ADD.
func cleanOutdatedAllocations(danmClient danmclientset.Interface, args *datastructs.CniArgs) {
	deps, _ := danmep.FindByPodName(danmClient, args.Pod.ObjectMeta.Name, args.Pod.ObjectMeta.Namespace)
	for _, dep := range deps {
		if dep.Spec.PodUID == args.Pod.ObjectMeta.UID {
			dnet, _ := netcontrol.GetNetworkFromEp(danmClient, &dep)
			danmep.DeleteDanmEp(danmClient, &dep, dnet)
			log.Println("WARNING: DANM needed to reconcile inconsistent cluster state during CNI ADD, as DanmEps already existed for Pod:" + args.Pod.ObjectMeta.Name + " in namespace:" + args.Pod.ObjectMeta.Namespace)
		}
	}
}
