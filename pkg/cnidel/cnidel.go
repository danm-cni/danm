package cnidel

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/containernetworking/cni/pkg/invoke"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/cni/pkg/version"
	danmtypes "github.com/danm-cni/danm/crd/apis/danm/v1"
	"github.com/danm-cni/danm/pkg/datastructs"
	"github.com/danm-cni/danm/pkg/ipam"
)

const (
	CniAddOp = "ADD"
	CniDelOp = "DEL"
)

var (
	ipamType       = "fakeipam"
	defaultDataDir = "/var/lib/cni/networks"
	flannelBridge  = GetEnv("FLANNEL_BRIDGE", "cbr0")
)

// IsDelegationRequired decides if the interface creation operations should be delegated to a 3rd party CNI, or can be handled by DANM
// Decision is made based on the NetworkType parameter of the network object
func IsDelegationRequired(netInfo *danmtypes.DanmNet) bool {
	neType := strings.ToLower(netInfo.Spec.NetworkType)
	if neType == "ipvlan" || neType == "" {
		return false
	}
	return true
}

// DelegateInterfaceSetup delegates K8s Pod network interface setup task to the input 3rd party CNI plugin
// Returns the CNI compatible result object, or an error if interface creation was unsuccessful, or if the 3rd party CNI config could not be loaded
// TODO: I hate myself for the bool input parameter, but that's what we are going with for the time being. Could be this information cleverly defaulted from existing DanmEp spec in all cases?
func DelegateInterfaceSetup(netConf *datastructs.NetConf, wasIpReservedByDanmIpam bool, netInfo *danmtypes.DanmNet, ep *danmtypes.DanmEp) (*current.Result, error) {
	var (
		err         error
		ipamOptions datastructs.IpamConfig
	)
	if wasIpReservedByDanmIpam {
		ipamOptions = getCniIpamConfig(netInfo, ep.Spec.Iface.Address, ep.Spec.Iface.AddressIPv6)
	}
	rawConfig, err := getCniPluginConfig(netConf, netInfo, ipamOptions, ep)
	if err != nil {
		return nil, err
	}
	cniType := netInfo.Spec.NetworkType
	cniResult, err := execCniPlugin(cniType, CniAddOp, netInfo, rawConfig, ep)
	if err != nil {
		return nil, errors.New("Error delegating ADD to CNI plugin:" + cniType + " because:" + err.Error())
	}
	if cniResult != nil {
		setEpIfaceAddress(cniResult, &ep.Spec.Iface)
	}
	return cniResult, nil
}

func IsDanmIpamNeededForDelegation(iface datastructs.Interface, netInfo *danmtypes.DanmNet) bool {
	if cni, ok := SupportedNativeCnis[strings.ToLower(netInfo.Spec.NetworkType)]; ok {
		return cni.IpamNeeded
	}
	//For static delegates we should only overwrite the original IPAM if an IP was explicitly "requested" from the Pod, and the request "makes sense"
	//Requested includes "none" allocation scheme as well, which can happen for L2 networks too
	//When a real IP is asked from DANM it only makes sense to overwrite if there is really a CIDR to allocate it from
	//BEWARE, because once DANM takes over IP allocation, it takes over for both IPv4, and IPv6!
	//TODO: Can we have partial IPAM allocations? Is chaining IPAM CNIs allowed, e.g. IPv6 from DANM, IPv4 from host-ipam etc.?
	if iface.Ip == ipam.NoneAllocType ||
		iface.Ip6 == ipam.NoneAllocType ||
		(iface.Ip != "" && iface.Ip != ipam.NoneAllocType && netInfo.Spec.Options.Cidr != "") ||
		(iface.Ip6 != "" && iface.Ip6 != ipam.NoneAllocType && netInfo.Spec.Options.Pool6.Cidr != "") {
		return true
	}
	return false
}

func IsDeviceNeeded(cniType string) bool {
	if cni, ok := SupportedNativeCnis[strings.ToLower(cniType)]; ok {
		return cni.DeviceNeeded
	} else {
		return false
	}
}

func getCniIpamConfig(netinfo *danmtypes.DanmNet, ip4, ip6 string) datastructs.IpamConfig {
	var ipSlice = []datastructs.IpamIp{}
	if ip4 != "" && ip4 != ipam.NoneAllocType {
		ipSlice = append(ipSlice, datastructs.IpamIp{
			IpCidr:  ip4,
			Version: 4,
		})
	}
	if ip6 != "" && ip6 != ipam.NoneAllocType {
		ipSlice = append(ipSlice, datastructs.IpamIp{
			IpCidr:  ip6,
			Version: 6,
		})
	}
	return datastructs.IpamConfig{
		Type: ipamType,
		Ips:  ipSlice,
	}
}

func getCniPluginConfig(netConf *datastructs.NetConf, netInfo *danmtypes.DanmNet, ipamOptions datastructs.IpamConfig, ep *danmtypes.DanmEp) ([]byte, error) {
	if cni, ok := SupportedNativeCnis[strings.ToLower(netInfo.Spec.NetworkType)]; ok {
		return cni.ReadConfig(netInfo, ipamOptions, ep, cni.CNIVersion)
	} else {
		return readCniConfigFile(netConf.CniConfigDir, netInfo, ipamOptions)
	}
}

func execCniPlugin(cniType, cniOpType string, netInfo *danmtypes.DanmNet, rawConfig []byte, ep *danmtypes.DanmEp) (*current.Result, error) {
	cniPath, cniArgs, err := getExecCniParams(cniType, cniOpType, netInfo, ep)
	if err != nil {
		return nil, errors.New("exec CNI params couldn't be gathered:" + err.Error())
	}
	exec := invoke.RawExec{Stderr: os.Stderr}
	rawResult, err := exec.ExecPlugin(context.Background(), cniPath, rawConfig, cniArgs)
	if err != nil {
		return nil, errors.New("OS exec call failed:" + err.Error())
	}
	versionDecoder := &version.ConfigDecoder{}
	confVersion, err := versionDecoder.Decode(rawResult)
	if err != nil || rawResult == nil {
		return &current.Result{}, nil
	}
	convertedResult, err := version.NewResult(confVersion, rawResult)
	if err != nil || convertedResult == nil {
		return &current.Result{}, nil
	}
	finalResult := convertCniResult(convertedResult)
	return finalResult, nil
}

func getExecCniParams(cniType, cniOpType string, netInfo *danmtypes.DanmNet, ep *danmtypes.DanmEp) (string, []string, error) {
	cniPaths := filepath.SplitList(os.Getenv("CNI_PATH"))
	cniPath, err := invoke.FindInPath(cniType, cniPaths)
	if err != nil {
		return "", nil, err
	}
	cniArgs := []string{
		"CNI_COMMAND=" + cniOpType,
		"CNI_CONTAINERID=" + os.Getenv("CNI_CONTAINERID"),
		"CNI_NETNS=" + os.Getenv("CNI_NETNS"),
		"CNI_IFNAME=" + ep.Spec.Iface.Name,
		"CNI_ARGS=" + os.Getenv("CNI_ARGS"),
		"CNI_PATH=" + os.Getenv("CNI_PATH"),
		"PATH=" + os.Getenv("PATH"),
	}
	return cniPath, cniArgs, nil
}

func setEpIfaceAddress(cniResult *current.Result, epIface *danmtypes.DanmEpIface) error {
	for _, ip := range cniResult.IPs {
		if ip.Version == "4" {
			epIface.Address = ip.Address.String()
		} else {
			epIface.AddressIPv6 = ip.Address.String()
		}
	}
	return nil
}

// DelegateInterfaceDelete delegates Ks8 Pod network interface delete task to the input 3rd party CNI plugin
// Returns an error if interface creation was unsuccessful, or if the 3rd party CNI config could not be loaded
func DelegateInterfaceDelete(netConf *datastructs.NetConf, netInfo *danmtypes.DanmNet, ep *danmtypes.DanmEp) error {
	var ip4, ip6 string
	if ipam.WasIpAllocatedByDanm(ep.Spec.Iface.Address, netInfo.Spec.Options.Cidr) {
		ip4 = ep.Spec.Iface.Address
	}
	if ipam.WasIpAllocatedByDanm(ep.Spec.Iface.AddressIPv6, netInfo.Spec.Options.Net6) {
		ip6 = ep.Spec.Iface.AddressIPv6
	}
	ipamForDelete := getCniIpamConfig(netInfo, ip4, ip6)
	rawConfig, err := getCniPluginConfig(netConf, netInfo, ipamForDelete, ep)
	if err != nil {
		FreeDelegatedIps(netInfo, ep.Spec.Iface.Address, ep.Spec.Iface.AddressIPv6)
		return err
	}
	cniType := netInfo.Spec.NetworkType
	_, err = execCniPlugin(cniType, CniDelOp, netInfo, rawConfig, ep)
	if err != nil {
		FreeDelegatedIps(netInfo, ep.Spec.Iface.Address, ep.Spec.Iface.AddressIPv6)
		return errors.New("Error delegating DEL to CNI plugin:" + cniType + " because:" + err.Error())
	}
	return FreeDelegatedIps(netInfo, ep.Spec.Iface.Address, ep.Spec.Iface.AddressIPv6)
}

func FreeDelegatedIps(netInfo *danmtypes.DanmNet, ip4, ip6 string) error {
	err4 := freeDelegatedIp(netInfo, ip4)
	err6 := freeDelegatedIp(netInfo, ip6)
	if err4 != nil {
		return err4
	}
	return err6
}

func freeDelegatedIp(netInfo *danmtypes.DanmNet, ip string) error {
	if netInfo.Spec.NetworkType == "flannel" && ip != "" && ip != ipam.NoneAllocType {
		flannelIpExhaustionWorkaround(ip)
	}
	return nil
}

// Host-local IPAM management plugin uses disk-local Store by default.
// Right now it is buggy in a sense that it does not try to free IPs if the container being deleted does not exist already.
// But it should!
// Exception handling 101 dear readers: ALWAYS try and reset your environment to the best of your ability during an exception
// TODO: remove this once the problem is solved upstream
func flannelIpExhaustionWorkaround(ip string) {
	var dataDir = filepath.Join(defaultDataDir, flannelBridge)
	os.Remove(filepath.Join(dataDir, ip))
}

// ConvertCniResult converts a CNI result from an older API version to the latest format
// Returns nil if conversion is unsuccessful
func convertCniResult(rawCniResult types.Result) *current.Result {
	convertedResult, err := current.NewResultFromResult(rawCniResult)
	if err != nil {
		log.Println("Delegated CNI result could not be converted:" + err.Error())
		return nil
	}
	return convertedResult
}

func GetEnv(key, fallback string) string {
	if value, doesExist := os.LookupEnv(key); doesExist {
		return value
	}
	return fallback
}
