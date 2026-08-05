package ipam

import (
	"encoding/binary"
	"errors"
	"math"
	"math/big"
	"net"
	"reflect"
	"strconv"
	"strings"

	"github.com/apparentlymart/go-cidr/cidr"
	danmtypes "github.com/danm-cni/danm/crd/apis/danm/v1"
	danmclientset "github.com/danm-cni/danm/crd/client/clientset/versioned"
	"github.com/danm-cni/danm/pkg/bitarray"
	"github.com/danm-cni/danm/pkg/datastructs"
	"github.com/danm-cni/danm/pkg/netcontrol"
)

const (
	NoneAllocType    = "none"
	DynamicAllocType = "dynamic"
)

// Reserve inspects the network object received as an input, and allocates an IPv4 or IPv6 address from the appropriate allocation pool
// In case static IP allocation is requested, it will try to reserve the requested address. If it is not possible, it returns an error
// The reserved IP addresses are represented by setting a bit in the network's BitArray type allocation matrices
// The refreshed network object is modified in the K8s API server at the end
func Reserve(danmClient danmclientset.Interface, netInfo danmtypes.DanmNet, req4, req6 string) (string, string, error) {
	origSpec := netInfo.Spec
	tempNet := netInfo
	for {
		ip4, ip6, err := allocateIps(&tempNet, req4, req6)
		if err != nil {
			return "", "", errors.New("failed to allocate IP address for network:" + netInfo.ObjectMeta.Name + " with error:" + err.Error())
		}
		//There is nothing to update in the API server if the network is unchanged after IP reservation
		if reflect.DeepEqual(origSpec, tempNet.Spec) {
			return ip4, ip6, nil
		}
		retryNeeded, newNetSpec, err := updateIpAllocation(danmClient, tempNet)
		if err != nil {
			return "", "", err
		}
		if retryNeeded {
			tempNet = newNetSpec
			continue
		}
		return ip4, ip6, nil
	}
}

// Free inspects the network object received as an input, and releases an IPv4 or IPv6 address from the appropriate allocation pool
// The IP address liberation is represented by unsetting a bit in the network's BitArray type allocation matrix
// The refreshed network object is modified in the K8s API server at the end
func Free(danmClient danmclientset.Interface, netInfo danmtypes.DanmNet, rip string) error {
	if rip == NoneAllocType || rip == "" {
		return nil
	}
	ripParts := strings.Split(rip, "/")
	ip := net.ParseIP(ripParts[0])
	tempNet := netInfo
	origSpec := netInfo.Spec
	for {
		if ip.To4() != nil {
			tempNet.Spec.Options.Alloc = resetIp(tempNet.Spec.Options.Alloc, tempNet.Spec.Options.Cidr, ip)
		} else {
			tempNet.Spec.Options.Alloc6 = resetIp(tempNet.Spec.Options.Alloc6, tempNet.Spec.Options.Pool6.Cidr, ip)
		}
		//There is nothing to update in the API server if the network is unchanged after freeing the IP
		if reflect.DeepEqual(origSpec, tempNet.Spec) {
			return nil
		}
		retryNeeded, newNet, err := updateIpAllocation(danmClient, tempNet)
		if err != nil {
			return err
		}
		if retryNeeded {
			tempNet = newNet
			continue
		}
		return nil
	}
}

func allocateIps(netInfo *danmtypes.DanmNet, req4, req6 string) (string, string, error) {
	ip4 := ""
	ip6 := ""
	var err error
	if req4 != "" {
		netInfo.Spec.Options.Alloc, ip4, err = allocateAddress(&netInfo.Spec.Options.Pool, netInfo.Spec.Options.Alloc, req4, netInfo.Spec.Options.Cidr, netInfo.Spec.Options.Cidr, netInfo.Spec.Options.Routes)
		if err != nil {
			return "", "", err
		}
	}
	if req6 != "" {
		if netInfo.Spec.Options.Net6 != "" && netInfo.Spec.Options.Pool6.Cidr == "" {
			InitV6AllocFields(netInfo)
		}
		//TODO: to have a real uniform handling both V4 and V6 pool definition should be uniform, meaning, V4 pools should also have a separare allocation CIDR
		tempPool6 := danmtypes.IpPool{Start: netInfo.Spec.Options.Pool6.Start, End: netInfo.Spec.Options.Pool6.End, LastIp: netInfo.Spec.Options.Pool6.LastIp}
		netInfo.Spec.Options.Alloc6, ip6, err = allocateAddress(&tempPool6, netInfo.Spec.Options.Alloc6, req6, netInfo.Spec.Options.Pool6.Cidr, netInfo.Spec.Options.Net6, netInfo.Spec.Options.Routes6)
		if err != nil {
			return "", "", err
		}
		netInfo.Spec.Options.Pool6.LastIp = tempPool6.LastIp
	}
	return ip4, ip6, err
}

func InitV6AllocFields(netInfo *danmtypes.DanmNet) {
	InitV6PoolCidr(netInfo)
	netInfo.Spec.Options.Pool6.Start, netInfo.Spec.Options.Pool6.End, netInfo.Spec.Options.Alloc6 =
		InitAllocPool(netInfo.Spec.Options.Pool6.Cidr, netInfo.Spec.Options.Pool6.Start, netInfo.Spec.Options.Pool6.End, netInfo.Spec.Options.Alloc6, netInfo.Spec.Options.Routes6)
}

func allocateAddress(pool *danmtypes.IpPool, alloc, reqType, allocCidr, netCidr string, routes map[string]string) (string, string, error) {
	if reqType == NoneAllocType {
		return alloc, NoneAllocType, nil
	}
	if alloc == "" {
		return alloc, "", errors.New("IP address cannot be allocated for an L2 network!")
	}
	ba := bitarray.NewBitArrayFromBase64(alloc)
	var allocatedIp string
	_, allocSubnet, _ := net.ParseCIDR(allocCidr)
	_, netSubnet, _ := net.ParseCIDR(netCidr)
	if reqType == DynamicAllocType {
		begin, end := getAllocRangeBasedOnCidr(pool, allocSubnet)
		var lastIpIndex uint32
		if pool.LastIp != "" {
			lastIp := net.ParseIP(pool.LastIp)
			lastIpIndex = GetIndexOfIp(lastIp, allocSubnet)
		}
		if lastIpIndex >= end || lastIpIndex == 0 {
			lastIpIndex = begin
		}
		var doesAnyFreeIpExist bool
		var allocatedIndex uint32
		for i := lastIpIndex; i <= end; i++ {
			if !ba.Get(i) && !isIpAGwIp(i, routes, ba, allocSubnet) {
				ba.Set(i)
				allocatedIndex = i
				doesAnyFreeIpExist = true
				break
			}
			//Now let's look from the beginning until LastIp
			if i == end && end != lastIpIndex {
				i = begin
				end = lastIpIndex
			}
		}
		if !doesAnyFreeIpExist {
			return alloc, "", errors.New("IP address cannot be dynamically allocated, all addresses are reserved!")
		}
		allocatedIp = getIpFromIndex(allocatedIndex, allocSubnet, netSubnet)
		pool.LastIp = allocatedIp
	} else {
		//I guess we are doing backward compatibility now :)
		//You used to be able to define a static IP in CIDR format, so now we need to trim the suffix if it is unnecessarily there
		requestParts := strings.Split(reqType, "/")
		ip := net.ParseIP(requestParts[0])
		if ip == nil {
			return alloc, "", errors.New("static IP allocation failed, requested static IP:" + reqType + " is not a valid IP")
		}
		if !(netSubnet.Contains(ip)) {
			return alloc, "", errors.New("static IP allocation failed, requested static IP:" + reqType + " is outside the network's CIDR:" + netCidr)
		}
		prefix, _ := netSubnet.Mask.Size()
		allocatedIp = requestParts[0] + "/" + strconv.Itoa(prefix)
		//Static IP allocations can come from the network's CIDR, outside of the dynamic allocation pool
		//But if the static IP does belong to the allocation pool, we need to reserve its place as usual
		if allocSubnet.Contains(ip) {
			allocatedIndex := GetIndexOfIp(ip, allocSubnet)
			//TODO: we should throw the same error if static IP is outside the allocation pool, but was already assigned to a DanmEp
			if ba.Get(allocatedIndex) {
				return alloc, "", errors.New("static IP allocation failed, requested IP address:" + reqType + " is already in use")
			}
			ba.Set(allocatedIndex)
		}
	}
	return ba.Encode(), allocatedIp, nil
}

func getAllocRangeBasedOnCidr(pool *danmtypes.IpPool, cidr *net.IPNet) (uint32, uint32) {
	var beginAsInt, endAsInt uint32
	if cidr.IP.To4() != nil {
		firstIpAsInt := Ip2int(cidr.IP)
		beginAsInt = Ip2int(net.ParseIP(pool.Start)) - firstIpAsInt
		endAsInt = Ip2int(net.ParseIP(pool.End)) - firstIpAsInt
	} else {
		firstIpAsBigInt := Ip62int(cidr.IP)
		beginAsBigInt := Ip62int(net.ParseIP(pool.Start))
		endAsBigInt := Ip62int(net.ParseIP(pool.End))
		beginAsInt = uint32(beginAsBigInt.Sub(beginAsBigInt, firstIpAsBigInt).Uint64())
		endAsInt = uint32(endAsBigInt.Sub(endAsBigInt, firstIpAsBigInt).Uint64())
	}
	return beginAsInt, endAsInt
}

func GetIndexOfIp(ip net.IP, subnet *net.IPNet) uint32 {
	var index uint32
	if ip == nil {
		return index
	}
	if ip.To4() != nil {
		firstIpAsInt := Ip2int(subnet.IP)
		ipAsInt := Ip2int(ip)
		index = ipAsInt - firstIpAsInt
	} else {
		firstIpAsBigInt := Ip62int(subnet.IP)
		ipAsBigInt := Ip62int(ip)
		index = uint32(ipAsBigInt.Sub(ipAsBigInt, firstIpAsBigInt).Uint64())
	}
	return index
}

func getIpFromIndex(index uint32, allocSubnet, netSubnet *net.IPNet) string {
	var ip net.IP
	if allocSubnet.IP.To4() != nil {
		firstIpAsInt := Ip2int(allocSubnet.IP)
		ip = Int2ip(firstIpAsInt + index)
	} else {
		firstIpAsBigInt := Ip62int(allocSubnet.IP)
		indexAsBigInt := new(big.Int).SetUint64(uint64(index))
		ip = Int2ip6(firstIpAsBigInt.Add(firstIpAsBigInt, indexAsBigInt))
	}
	prefix, _ := netSubnet.Mask.Size()
	return ip.String() + "/" + strconv.Itoa(prefix)
}

func updateIpAllocation(danmClient danmclientset.Interface, netInfo danmtypes.DanmNet) (bool, danmtypes.DanmNet, error) {
	resourceConflicted, err := netcontrol.PutNetwork(danmClient, &netInfo)
	if err != nil {
		return false, danmtypes.DanmNet{}, errors.New("DanmNet update failed with error:" + err.Error())
	}
	if resourceConflicted {
		newNetSpec, err := netcontrol.RefreshNetwork(danmClient, netInfo)
		if err != nil {
			return false, danmtypes.DanmNet{}, errors.New("After IP address reservation conflict, network cannot be read again!")
		}
		return true, *newNetSpec, nil
	}
	return false, danmtypes.DanmNet{}, nil
}

func resetIp(alloc, cidr string, rip net.IP) string {
	ba := bitarray.NewBitArrayFromBase64(alloc)
	_, subnet, _ := net.ParseCIDR(cidr)
	if rip == nil || subnet == nil || !subnet.Contains(rip) {
		//Invalid IP, nothing to do here. Resetting would crash if we wouldn't return
		return alloc
	}
	allocatedIndex := GetIndexOfIp(rip, subnet)
	ba.Reset(allocatedIndex)
	return ba.Encode()
}

func GarbageCollectIps(danmClient danmclientset.Interface, netInfo *danmtypes.DanmNet, ip4, ip6 string) error {
	err := Free(danmClient, *netInfo, ip4)
	if err != nil {
		return err
	}
	err = Free(danmClient, *netInfo, ip6)
	return err
}

// Ip2int converts an IP address stored according to the Golang net package to a native Golang big endian, 32-bit integer
func Ip2int(ip net.IP) uint32 {
	if len(ip) == 16 {
		return binary.BigEndian.Uint32(ip[12:16])
	}
	return binary.BigEndian.Uint32(ip)
}

// Ip62int converts an IPv6 address stored according to the Golang net package to a native Golang big endian, 64-bit integer
func Ip62int(ip6 net.IP) *big.Int {
	Ip6Int := big.NewInt(0)
	Ip6Int.SetBytes(ip6.To16())
	return Ip6Int
}

// Int2ip converts an IP address stored as a native Golang big endian, 32-bit integer to an IP
// represented according to the Golang net package
func Int2ip(nn uint32) net.IP {
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, nn)
	return ip
}

// Int2ip6 converts an IP address stored as a native Golang big endian, 64-bit integer to an IP
// represented according to the Golang net package
func Int2ip6(nn *big.Int) net.IP {
	ip := nn.Bytes()
	return ip
}

func CreateAllocationArray(subnet *net.IPNet, routes map[string]string) string {
	bitArray, _ := bitarray.CreateBitArrayFromIpnet(subnet)
	return bitArray.Encode()
}

func isIpAGwIp(ipPosition uint32, routes map[string]string, bitArray *bitarray.BitArray, subnet *net.IPNet) bool {
	for _, gw := range routes {
		gatewayPosition := GetIndexOfIp(net.ParseIP(gw), subnet)
		if gatewayPosition <= bitArray.Len() && gatewayPosition == ipPosition {
			return true
		}
	}
	return false
}

func DoV6CidrsIntersect(masterCidr, subCidr *net.IPNet) bool {
	firstAllocIp, lastAllocIp := cidr.AddressRange(subCidr)
	//Brute force: if the Alloc6 CIDR's first, and last IP both belongs to Net6, we assume the whole CIDR also does
	if masterCidr.Contains(firstAllocIp) && masterCidr.Contains(lastAllocIp) {
		return true
	}
	return false
}

func GetMaxUsableV6Prefix(dnet *danmtypes.DanmNet) int {
	if dnet.Spec.Options.Cidr == "" {
		return datastructs.MaxV6PrefixLength
	}
	_, v4Cidr, _ := net.ParseCIDR(dnet.Spec.Options.Cidr)
	sizeOfV4AllocPool := cidr.AddressCount(v4Cidr)
	maxRemainingCapacity := uint64(math.Pow(2, float64(bitarray.MaxSupportedAllocLength))) - sizeOfV4AllocPool
	maxUsableV6Prefix := datastructs.MinV6PrefixLength
	for pref := datastructs.MaxV6PrefixLength; pref <= datastructs.MinV6PrefixLength; pref++ {
		_, testCidr, _ := net.ParseCIDR("2a00:8a00:a000:1193:f816:3eff:fe24:e348/" + strconv.Itoa(pref))
		if cidr.AddressCount(testCidr) < maxRemainingCapacity {
			maxUsableV6Prefix = pref
			break
		}
	}
	return maxUsableV6Prefix
}

func InitV6PoolCidr(netInfo *danmtypes.DanmNet) {
	if netInfo.Spec.Options.Net6 == "" || netInfo.Spec.Options.Pool6.Cidr != "" {
		return
	}
	_, netCidr, _ := net.ParseCIDR(netInfo.Spec.Options.Net6)
	baseCidrStart := netCidr.IP
	pool6CidrPrefix := GetMaxUsableV6Prefix(netInfo)
	//If the subnet of the whole network is smaller than the maximum remaining capacity, use only that amount
	net6Prefix, _ := netCidr.Mask.Size()
	if pool6CidrPrefix < net6Prefix {
		pool6CidrPrefix = net6Prefix
	}
	maskedV6AllocCidrBase := net.CIDRMask(pool6CidrPrefix, 128)
	maskedV6AllocCidr := net.IPNet{IP: baseCidrStart, Mask: maskedV6AllocCidrBase}
	netInfo.Spec.Options.Pool6.Cidr = maskedV6AllocCidr.String()
}

func InitAllocPool(netCidr, start, end, alloc string, routes map[string]string) (string, string, string) {
	if netCidr == "" {
		return start, end, alloc
	}
	_, allocCidr, _ := net.ParseCIDR(netCidr)
	if start == "" {
		start = cidr.Inc(allocCidr.IP).String()
	}
	if end == "" {
		end = cidr.Dec(GetBroadcastAddress(allocCidr)).String()
	}
	if alloc == "" {
		alloc = CreateAllocationArray(allocCidr, routes)
	}
	return start, end, alloc
}

func GetBroadcastAddress(subnet *net.IPNet) net.IP {
	_, lastIp := cidr.AddressRange(subnet)
	return lastIp
}

func WasIpAllocatedByDanm(ip, cidr string) bool {
	_, subnet, _ := net.ParseCIDR(cidr)
	parsedIp := net.ParseIP(ip)
	if parsedIp == nil {
		parsedIp, _, _ = net.ParseCIDR(ip)
	}
	if parsedIp != nil && (subnet != nil && subnet.Contains(parsedIp)) {
		return true
	}
	return false
}
