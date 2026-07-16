package admit

import (
	"errors"
	"net"
	"strconv"

	danmtypes "github.com/danm-cni/danm/crd/apis/danm/v1"
	danmclientset "github.com/danm-cni/danm/crd/client/clientset/versioned"
	"github.com/danm-cni/danm/pkg/danmep"
	"github.com/danm-cni/danm/pkg/datastructs"
	"github.com/danm-cni/danm/pkg/ipam"
	admissionv1 "k8s.io/api/admission/v1beta1"
	"k8s.io/utils/cpuset"
)

const (
	MaxNidLength = 10
)

var (
	DanmNetMapping       = []ValidatorFunc{validateIpv4Fields, validateIpv6Fields, validateAllocationPools, validateVids, validateNetworkId, validateAbsenceOfAllowedTenants, validateNeType, validateVniChange}
	ClusterNetMapping    = []ValidatorFunc{validateIpv4Fields, validateIpv6Fields, validateAllocationPools, validateVids, validateNetworkId, validateNeType, validateVniChange}
	TenantNetMapping     = []ValidatorFunc{validateIpv4Fields, validateIpv6Fields, validateAllocationPools, validateAbsenceOfAllowedTenants, validateTenantNetRules, validateNeType}
	danmValidationConfig = map[string]ValidatorMapping{
		"DanmNet":        DanmNetMapping,
		"ClusterNetwork": ClusterNetMapping,
		"TenantNetwork":  TenantNetMapping,
	}
)

type ValidatorFunc func(oldManifest, newManifest *danmtypes.DanmNet, opType admissionv1.Operation, client danmclientset.Interface) error
type ValidatorMapping []ValidatorFunc

func filterVnis(origSet cpuset.CPUSet) cpuset.CPUSet {
	var newSet string
	origIds := origSet.List()
	for cpu := range origIds {
		if cpu > MaxAllowedVni {
			newSet = newSet + "," + string(cpu)
		}
	}
	finalVnis, _ := cpuset.Parse(newSet)
	return finalVnis
}

func validateIpv4Fields(oldManifest, newManifest *danmtypes.DanmNet, opType admissionv1.Operation, client danmclientset.Interface) error {
	return validateIpFields(newManifest.Spec.Options.Cidr, newManifest.Spec.Options.Routes)
}

func validateIpv6Fields(oldManifest, newManifest *danmtypes.DanmNet, opType admissionv1.Operation, client danmclientset.Interface) error {
	return validateIpFields(newManifest.Spec.Options.Net6, newManifest.Spec.Options.Routes6)
}

func validateIpFields(cidr string, routes map[string]string) error {
	if cidr == "" {
		if routes != nil {
			return errors.New("IP routes cannot be defined for a L2 network")
		}
		return nil
	}
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return errors.New("Invalid CIDR: " + cidr)
	}
	for _, gw := range routes {
		if !ipnet.Contains(net.ParseIP(gw)) {
			return errors.New("Specified GW address:" + gw + " is not part of CIDR:" + cidr)
		}
	}
	return nil
}

func validateAllocationPools(oldManifest, newManifest *danmtypes.DanmNet, opType admissionv1.Operation, client danmclientset.Interface) error {
	if opType == admissionv1.Create &&
		(newManifest.Spec.Options.Alloc != "" || newManifest.Spec.Options.Alloc6 != "") {
		return errors.New("Allocation bitmasks shall not be manually defined upon creation!")
	}
	err := validateAllocV4(newManifest)
	if err != nil {
		return err
	}
	err = validateAllocV6(newManifest)
	if err != nil {
		return err
	}
	return nil
}

func validateAllocV4(newManifest *danmtypes.DanmNet) error {
	cidrV4 := newManifest.Spec.Options.Cidr
	if cidrV4 == "" {
		if newManifest.Spec.Options.Pool.Start != "" || newManifest.Spec.Options.Pool.End != "" {
			return errors.New("V4 Allocation pool cannot be defined without CIDR!")
		}
		return nil
	}
	_, ipnet, _ := net.ParseCIDR(cidrV4)
	if ipnet.IP.To4() == nil {
		return errors.New("Options.CIDR is not a valid V4 subnet!")
	}
	netMaskSize, _ := ipnet.Mask.Size()
	if netMaskSize < datastructs.MaxV4MaskLength {
		return errors.New("Netmask of the IPv4 CIDR is bigger than the maximum allowed /" + strconv.Itoa(datastructs.MaxV4MaskLength))
	}
	newManifest.Spec.Options.Pool.Start, newManifest.Spec.Options.Pool.End, newManifest.Spec.Options.Alloc =
		ipam.InitAllocPool(newManifest.Spec.Options.Cidr, newManifest.Spec.Options.Pool.Start, newManifest.Spec.Options.Pool.End, newManifest.Spec.Options.Alloc, newManifest.Spec.Options.Routes)
	if !ipnet.Contains(net.ParseIP(newManifest.Spec.Options.Pool.Start)) || !ipnet.Contains(net.ParseIP(newManifest.Spec.Options.Pool.End)) {
		return errors.New("Allocation pool is outside of defined CIDR!")
	}
	if ipam.Ip2int(net.ParseIP(newManifest.Spec.Options.Pool.End)) <= ipam.Ip2int(net.ParseIP(newManifest.Spec.Options.Pool.Start)) {
		return errors.New("Allocation pool start:" + newManifest.Spec.Options.Pool.Start + " is bigger than or equal to allocation pool end:" + newManifest.Spec.Options.Pool.End)
	}
	return nil
}

func validateAllocV6(newManifest *danmtypes.DanmNet) error {
	net6 := newManifest.Spec.Options.Net6
	if net6 == "" {
		if newManifest.Spec.Options.Pool6.Start != "" ||
			newManifest.Spec.Options.Pool6.End != "" ||
			newManifest.Spec.Options.Pool6.Cidr != "" {
			return errors.New("IPv6 allocation pool cannot be defined without Net6!")
		}
		return nil
	}
	_, netCidr, _ := net.ParseCIDR(net6)
	if netCidr.IP.To4() != nil {
		return errors.New("spec.Options.Net6 is not a valid V6 subnet!")
	}
	// The limit of the current storage algorithm and etcd 3.4.X is ~8M addresses per network.
	// This means that the summarized size of the IPv4, and IPv6 allocation pools shall not go over this threshold.
	// Therefore we need to calculate the maximum usable prefix for our V6 pool, discounting the space we have already reserved for the V4 pool.
	maxV6AllocPrefix := ipam.GetMaxUsableV6Prefix(newManifest)
	ipam.InitV6PoolCidr(newManifest)
	_, allocCidr, err := net.ParseCIDR(newManifest.Spec.Options.Pool6.Cidr)
	if err != nil {
		return errors.New("spec.Options.Pool6.CIDR is invalid!")
	}
	if allocCidr.IP.To4() != nil {
		return errors.New("spec.Options.Allocation_Pool_V6.Cidr is not a valid V6 subnet!")
	}
	netMaskSize, _ := allocCidr.Mask.Size()
	// We don't have enough storage space left for storing IPv6 allocations
	if netMaskSize < maxV6AllocPrefix || netMaskSize == datastructs.MinV6PrefixLength {
		return errors.New("The defined IPv6 allocation pool exceeds the maximum - 8M-size(IPv4 allocation pool) - storage capacity!")
	}
	if (newManifest.Spec.Options.Pool6.Start != "" && !allocCidr.Contains(net.ParseIP(newManifest.Spec.Options.Pool6.Start))) ||
		(newManifest.Spec.Options.Pool6.End != "" && !allocCidr.Contains(net.ParseIP(newManifest.Spec.Options.Pool6.End))) ||
		(!ipam.DoV6CidrsIntersect(netCidr, allocCidr)) {
		return errors.New("IPv6 allocation pool is outside of the defined IPv6 subnet!")
	}
	newManifest.Spec.Options.Pool6.Start, newManifest.Spec.Options.Pool6.End, newManifest.Spec.Options.Alloc6 =
		ipam.InitAllocPool(newManifest.Spec.Options.Pool6.Cidr, newManifest.Spec.Options.Pool6.Start, newManifest.Spec.Options.Pool6.End, newManifest.Spec.Options.Alloc6, newManifest.Spec.Options.Routes6)
	if ipam.Ip62int(net.ParseIP(newManifest.Spec.Options.Pool6.End)).Cmp(ipam.Ip62int(net.ParseIP(newManifest.Spec.Options.Pool6.Start))) <= 0 {
		return errors.New("Allocation pool start:" + newManifest.Spec.Options.Pool6.Start + " is bigger than or equal to allocation pool end:" + newManifest.Spec.Options.Pool6.End)
	}
	return nil
}

func validateVids(oldManifest, newManifest *danmtypes.DanmNet, opType admissionv1.Operation, client danmclientset.Interface) error {
	isVlanDefined := (newManifest.Spec.Options.Vlan != 0)
	isVxlanDefined := (newManifest.Spec.Options.Vxlan != 0)
	if isVlanDefined && isVxlanDefined {
		return errors.New("VLAN ID and VxLAN ID parameters are mutually exclusive")
	}
	return nil
}

func validateNetworkId(oldManifest, newManifest *danmtypes.DanmNet, opType admissionv1.Operation, client danmclientset.Interface) error {
	if newManifest.Spec.NetworkID == "" {
		return errors.New("Spec.NetworkID mandatory parameter is missing!")
	}
	if len(newManifest.Spec.NetworkID) > MaxNidLength && IsTypeDynamic(newManifest.Spec.NetworkType) &&
		(newManifest.Spec.Options.Vxlan != 0 || newManifest.Spec.Options.Vlan != 0) {
		return errors.New("Spec.NetworkID cannot be longer than " + strconv.Itoa(MaxNidLength) + " characters (otherwise VLAN and VxLAN host interface creation might fail)!")
	}
	return nil
}

func validateAbsenceOfAllowedTenants(oldManifest, newManifest *danmtypes.DanmNet, opType admissionv1.Operation, client danmclientset.Interface) error {
	if newManifest.Spec.AllowedTenants != nil {
		return errors.New("AllowedTenants attribute is only valid for the ClusterNetwork API!")
	}
	return nil
}

func validateTenantNetRules(oldManifest, newManifest *danmtypes.DanmNet, opType admissionv1.Operation, client danmclientset.Interface) error {
	if opType == admissionv1.Create &&
		(newManifest.Spec.Options.Vxlan != 0 ||
			newManifest.Spec.Options.Vlan != 0) {
		return errors.New("Manually configuring Spec.Options.vlan, or Spec.Options.vxlan attributes is not allowed for TenantNetworks!")
	}
	if opType == admissionv1.Update &&
		(newManifest.Spec.Options.Device != oldManifest.Spec.Options.Device ||
			newManifest.Spec.Options.DevicePool != oldManifest.Spec.Options.DevicePool ||
			newManifest.Spec.Options.Vxlan != oldManifest.Spec.Options.Vxlan ||
			newManifest.Spec.Options.Vlan != oldManifest.Spec.Options.Vlan) {
		return errors.New("Manually changing any one of Spec.Options. host_device, device_pool, vlan, or vxlan attributes is not allowed for TenantNetworks!")
	}
	return nil
}

func validateTenantconfig(oldManifest, newManifest *danmtypes.TenantConfig, opType admissionv1.Operation) error {
	if len(newManifest.HostDevices) == 0 && len(newManifest.NetworkIds) == 0 {
		return errors.New("Either hostDevices, or networkIds must be provided!")
	}
	var err error
	for _, ifaceConf := range newManifest.HostDevices {
		err = validateIfaceConfig(ifaceConf, opType)
		if err != nil {
			return err
		}
	}
	for nType, nId := range newManifest.NetworkIds {
		if nType == "" || nId == "" {
			return errors.New("neither NetworkID, nor NetworkType can be empty in a NetworkID mapping!")
		}
		if len(nId) > MaxNidLength && IsTypeDynamic(nType) {
			return errors.New("NetworkID:" + nId + " cannot be longer than " + strconv.Itoa(MaxNidLength) + " characters (otherwise VLAN and VxLAN host interface creation might fail)!")
		}
	}
	return nil
}

func validateIfaceConfig(ifaceConf danmtypes.IfaceProfile, opType admissionv1.Operation) error {
	if ifaceConf.Name == "" {
		return errors.New("name attribute of a hostDevice must not be empty!")
	}
	if (ifaceConf.VniType == "" && ifaceConf.VniRange != "") ||
		(ifaceConf.VniRange == "" && ifaceConf.VniType != "") {
		return errors.New("vniRange and vniType attributes must be provided together for interface:" + ifaceConf.Name)
	}
	if ifaceConf.VniType != "" && ifaceConf.VniType != "vlan" && ifaceConf.VniType != "vxlan" {
		return errors.New(ifaceConf.VniType + " is not in allowed vniType values: {vlan,vxlan} for interface:" + ifaceConf.Name)
	}
	if opType == admissionv1.Create && ifaceConf.Alloc != "" {
		return errors.New("Allocation bitmask for interface: " + ifaceConf.Name + " shall not be manually defined upon creation!")
	}
	//I know this type is for CPU sets, but isn't it just perfect for handling arbitrarily defined integer ranges?
	vniSet, err := cpuset.Parse(ifaceConf.VniRange)
	if err != nil {
		return errors.New("vniRange for interface:" + ifaceConf.Name + " must be improperly formatted because its parsing fails with:" + err.Error())
	}
	filteredSet := filterVnis(vniSet)
	if filteredSet.Size() > 0 {
		return errors.New("vniRange for interface:" + ifaceConf.Name + " is invalid, because it cannot contain VNIs over the maximum supported number that is:" + strconv.Itoa(MaxAllowedVni))
	}
	return nil
}

func validateNeType(oldManifest, newManifest *danmtypes.DanmNet, opType admissionv1.Operation, client danmclientset.Interface) error {
	if newManifest.Spec.NetworkType == "sriov" {
		if newManifest.Spec.Options.DevicePool == "" || newManifest.Spec.Options.Device != "" {
			return errors.New("Spec.Options.device_pool must, and Spec.Options.host_device cannot be provided for SR-IOV networks!")
		}
	} else if newManifest.Spec.Options.Device != "" && newManifest.Spec.Options.DevicePool != "" {
		return errors.New("Spec.Options.device_pool and Spec.Options.host_device cannot be provided together!")
	}
	return nil
}

func validateVniChange(oldManifest, newManifest *danmtypes.DanmNet, opType admissionv1.Operation, client danmclientset.Interface) error {
	if opType != admissionv1.Update {
		return nil
	}
	isAnyPodConnectedToNetwork, connectedEp, err := danmep.ArePodsConnectedToNetwork(client, oldManifest)
	if err != nil {
		return errors.New("no way to tell if Pods are still using the network due to:" + err.Error())
	}
	if !isAnyPodConnectedToNetwork {
		return nil
	}
	if (oldManifest.Spec.Options.Vlan != 0 && (oldManifest.Spec.Options.Vlan != newManifest.Spec.Options.Vlan || oldManifest.Spec.Options.Device != newManifest.Spec.Options.Device)) ||
		(oldManifest.Spec.Options.Vxlan != 0 && (oldManifest.Spec.Options.Vxlan != newManifest.Spec.Options.Vxlan || oldManifest.Spec.Options.Device != newManifest.Spec.Options.Device)) {
		return errors.New("cannot change VNI/host_device of a network which having any Pods connected to it e.g. Pod:" + connectedEp.Spec.Pod + " in namespace:" + connectedEp.ObjectMeta.Namespace)
	}
	return nil
}
