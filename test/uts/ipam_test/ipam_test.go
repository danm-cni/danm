package ipam_test

import (
	"os"
	"strconv"
	"testing"

	danmtypes "github.com/danm-cni/danm/crd/apis/danm/v1"
	"github.com/danm-cni/danm/pkg/ipam"
	stubs "github.com/danm-cni/danm/test/stubs/danm"
	"github.com/danm-cni/danm/test/utils"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var testNets = []danmtypes.DanmNet{
	{ObjectMeta: meta_v1.ObjectMeta{Name: "l2"}, Spec: danmtypes.DanmNetSpec{NetworkID: "l2"}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "cidr"}, Spec: danmtypes.DanmNetSpec{NetworkID: "cidr", Options: danmtypes.DanmNetOption{Cidr: "192.168.1.64/26"}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "fullIpv4"}, Spec: danmtypes.DanmNetSpec{NetworkID: "fullIpv4", Options: danmtypes.DanmNetOption{Cidr: "192.168.1.0/30"}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "net6"}, Spec: danmtypes.DanmNetSpec{NetworkID: "net6", Options: danmtypes.DanmNetOption{Net6: "2a00:8a00:a000:1193::/64", Cidr: "192.168.1.64/26"}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "smallNet6"}, Spec: danmtypes.DanmNetSpec{NetworkID: "smallNet6", Options: danmtypes.DanmNetOption{Net6: "2a00:8a00:a000:1193::/120"}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "conflict"}, Spec: danmtypes.DanmNetSpec{NetworkID: "conflict", Options: danmtypes.DanmNetOption{Cidr: "192.168.1.64/26"}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "conflicterror"}, Spec: danmtypes.DanmNetSpec{NetworkID: "conflicterror", Options: danmtypes.DanmNetOption{Cidr: "192.168.1.64/26"}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "fullconflictFree"}, Spec: danmtypes.DanmNetSpec{NetworkID: "fullconflictFree", Options: danmtypes.DanmNetOption{Cidr: "192.168.1.64/26"}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "fullconflicterrorFree"}, Spec: danmtypes.DanmNetSpec{NetworkID: "fullconflicterrorFree", Options: danmtypes.DanmNetOption{Cidr: "192.168.1.64/26"}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "fullerror"}, Spec: danmtypes.DanmNetSpec{NetworkID: "error", Options: danmtypes.DanmNetOption{Cidr: "192.168.1.64/26"}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "error"}, Spec: danmtypes.DanmNetSpec{NetworkID: "error", Options: danmtypes.DanmNetOption{Cidr: "192.168.1.64/26"}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "staticFirst"}, Spec: danmtypes.DanmNetSpec{NetworkID: "cidr", Options: danmtypes.DanmNetOption{Cidr: "192.168.1.64/26"}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "fullinitv6"}, Spec: danmtypes.DanmNetSpec{NetworkID: "net6", Options: danmtypes.DanmNetOption{Cidr: "192.168.1.64/26", Net6: "2a00:8a00:a000:1193::/64"}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "v4RestrictedPool"}, Spec: danmtypes.DanmNetSpec{NetworkID: "v4RestrictedPool", Options: danmtypes.DanmNetOption{Cidr: "192.168.1.64/26", Pool: danmtypes.IpPool{Start: "192.168.1.70", End: "192.168.1.80"}}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "v4RestrictedPoolWithLastIp"}, Spec: danmtypes.DanmNetSpec{NetworkID: "v4RestrictedPoolWithLastIp", Options: danmtypes.DanmNetOption{Cidr: "192.168.1.64/26", Pool: danmtypes.IpPool{Start: "192.168.1.70", End: "192.168.1.80", LastIp: "192.168.1.80"}}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "nogws"}, Spec: danmtypes.DanmNetSpec{NetworkID: "nogws", Options: danmtypes.DanmNetOption{Cidr: "192.168.1.64/26", Net6: "2a00:8a00:a000:1193::/64"}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "gws"}, Spec: danmtypes.DanmNetSpec{NetworkID: "gws", Options: danmtypes.DanmNetOption{Cidr: "192.168.1.64/26", Net6: "2a00:8a00:a000:1193::/64", Routes: map[string]string{"10.20.20.0/24": "192.168.1.65"}, Routes6: map[string]string{"2a00:8a00:a000:1194:/64": "2a00:8a00:a000:1193::1"}}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "fullinitv6Gws"}, Spec: danmtypes.DanmNetSpec{NetworkID: "gwsReserved", Options: danmtypes.DanmNetOption{Cidr: "192.168.1.64/26", Net6: "2a00:8a00:a000:1193::/64", Routes: map[string]string{"10.20.20.0/24": "192.168.1.65"}, Routes6: map[string]string{"2a00:8a00:a000:1194:/64": "2a00:8a00:a000:1193::1"}}}},
}

var reserveTcs = []struct {
	netName                   string
	netIndex                  int
	requestedIp4              string
	requestedIp6              string
	expectedIp4               string
	expectedIp6               string
	isErrorExpected           bool
	timesUpdateShouldBeCalled int
}{
	{"noIpsRequested", 0, "", "", "", "", false, 0},
	{"noneIPv4", 0, "none", "", "none", "", false, 0},
	{"noneIPv6", 0, "", "none", "", "none", false, 0},
	{"noneDualStack", 0, "none", "none", "none", "none", false, 0},
	{"dynamicErrorIPv4", 0, "dynamic", "", "", "", true, 0},
	{"dynamicErrorIPv6", 0, "", "dynamic", "", "", true, 0},
	{"dynamicErrorDualStack", 0, "dynamic", "dynamic", "", "", true, 0},
	{"dynamicIPv4Success", 1, "dynamic", "", "192.168.1.65/26", "", false, 1},
	{"dynamicIPv4Exhausted", 2, "dynamic", "", "", "", true, 0},
	{"staticInvalidIPv4", 2, "hululululu", "", "", "", true, 0},
	{"staticInvalidNoCidrIPv4", 2, "192.168.1.1", "", "", "", true, 0},
	{"staticL2IPv4", 0, "192.168.1.1/26", "", "", "", true, 0},
	{"staticNetmaskMismatchIPv4", 2, "192.168.1.1/32", "", "", "", true, 0},
	{"staticAlreadyUsedIPv4", 2, "192.168.1.2/30", "", "", "", true, 0},
	{"staticSuccessLastIPv4", 1, "192.168.1.126/26", "", "192.168.1.126/26", "", false, 1},
	{"staticSuccessFirstIPv4", 11, "192.168.1.65/26", "", "192.168.1.65/26", "", false, 1},
	{"staticFailAfterLastIPv4", 1, "192.168.1.127/26", "", "", "", true, 0},
	{"staticFailBeforeFirstIPv4", 1, "192.168.1.64/26", "", "", "", true, 0},
	{"dynamicIPv6Success", 3, "", "dynamic", "", "2a00:8a00:a000:1193::1/64", false, 1},
	{"dynamicIPv6SmallCidrSizeSuccess", 4, "", "dynamic", "", "2a00:8a00:a000:1193::1/120", false, 1},
	{"staticL2IPv6", 2, "", "2a00:8a00:a000:1193:f816:3eff:fe24:e348/64", "", "", true, 0},
	{"staticInvalidIPv6", 3, "", "2a00:8a00:a000:1193:hulu:lulu:lulu:lulu/64", "", "", true, 0},
	{"staticIPv/OutsideOfAllocCidr", 3, "", "2a00:8a00:a000:2193:f816:3eff:fe24:e348/64", "", "", true, 0},
	{"staticIPv6Success", 3, "", "2a00:8a00:a000:1193::3e:1010", "", "2a00:8a00:a000:1193::3e:1010/64", false, 1},
	{"dynamicDualStackSuccess", 3, "dynamic", "dynamic", "192.168.1.65/26", "2a00:8a00:a000:1193::2/64", false, 1},
	{"staticDualStackSuccess", 3, "192.168.1.115", "2a00:8a00:a000:1193::3e:2002", "192.168.1.115/26", "2a00:8a00:a000:1193::3e:2002/64", false, 1},
	{"resolvedConflictDuringUpdate", 5, "dynamic", "", "192.168.1.65/26", "", false, 2},
	{"unresolvedConflictAfterUpdate", 6, "dynamic", "", "", "", true, 1},
	{"errorUpdate", 10, "dynamic", "", "", "", true, 1},
	{"dyanmicV4FromAllocationPool", 13, "dynamic", "", "192.168.1.70/26", "", false, 1},
	{"dyanmicV4FromAllocationPoolWithLastIpSet", 14, "dynamic", "", "192.168.1.70/26", "", false, 1},
	{"dyanmicV4FirstIpIsNotAGw", 15, "dynamic", "", "192.168.1.65/26", "", false, 1},
	{"dyanmicV6FirstIpIsNotAGw", 15, "", "dynamic", "", "2a00:8a00:a000:1193::1/64", false, 1},
	{"dyanmicV4FirstIpIsGw", 16, "dynamic", "", "192.168.1.66/26", "", false, 1},
	{"dyanmicV6FirstIpIsGw", 16, "", "dynamic", "", "2a00:8a00:a000:1193::2/64", false, 1},
	{"staticV4GwSuccess", 16, "192.168.1.65/26", "", "192.168.1.65/26", "", false, 1},
	{"staticV6GwSuccess", 16, "", "2a00:8a00:a000:1193::1", "", "2a00:8a00:a000:1193::1/64", false, 1},
	{"staticV4GwFailIpReserved", 17, "192.168.1.65/26", "", "", "", true, 0},
	{"staticV6GwFailIpReserved", 17, "", "2a00:8a00:a000:1193::1", "", "", true, 0},
}

var freeTcs = []struct {
	netName                   string
	netIndex                  int
	allocatedIp               string
	isErrorExpected           bool
	timesUpdateShouldBeCalled int
}{
	{"l2Network", 0, "192.168.1.126/26", false, 0},
	{"noAssignedIp", 1, "", false, 0},
	{"successfulFree", 2, "192.168.1.2/30", false, 1},
	{"noNetmask", 2, "192.168.1.2", false, 0},
	{"outOfRange", 2, "192.168.1.10/30", false, 0},
	{"invalidIp", 2, "192.168.hululu/30", false, 0},
	{"ipv6OutsideOfCidr", 3, "2a00:8a00:a000:1193:f816:3eff:fe24:e348/64", false, 0},
	{"resolvedConflictDuringUpdate", 7, "192.168.1.69/26", false, 2},
	{"unresolvedConflictAfterUpdate", 8, "192.168.1.69/26", true, 1},
	{"errorUpdate", 9, "192.168.1.69/26", true, 1},
	{"ipv6SuccesfulFree", 12, "2a00:8a00:a000:1193::1/106", false, 1},
}

var gcTcs = []struct {
	netName      string
	netIndex     int
	allocatedIp4 string
	allocatedIp6 string
}{
	{"ip4OnlyGc", 12, "192.168.1.110/26", ""},
	{"ip6OnlyGc", 12, "", "2a00:8a00:a000:1193::1/106"},
	{"dualStackGc", 12, "192.168.1.115", "2a00:8a00:a000:1193::5"},
}

func TestReserve(t *testing.T) {
	err := utils.SetupAllocationPools(testNets)
	if err != nil {
		t.Errorf("Allocation pool for testnets could not be set-up because:%v", err)
	}
	for _, tc := range reserveTcs {
		t.Run(tc.netName, func(t *testing.T) {
			var ips []utils.ReservedIpsList
			ips = utils.AppendIpToExpectedAllocsList(ips, tc.expectedIp4, true, testNets[tc.netIndex].Spec.NetworkID)
			ips = utils.AppendIpToExpectedAllocsList(ips, tc.expectedIp6, true, testNets[tc.netIndex].Spec.NetworkID)
			testArtifacts := utils.TestArtifacts{TestNets: testNets, ReservedIps: ips}
			netClientStub := stubs.NewClientSetStub(testArtifacts)
			ip4, ip6, err := ipam.Reserve(netClientStub, testNets[tc.netIndex], tc.requestedIp4, tc.requestedIp6)
			if (err != nil && !tc.isErrorExpected) || (err == nil && tc.isErrorExpected) {
				t.Errorf("Received error:%v does not match with expectation", err)
				return
			}
			if ip4 != tc.expectedIp4 {
				t.Errorf("Allocated IP4 address:%s does not match with expected:%s", ip4, tc.expectedIp4)
			}
			if ip6 != tc.expectedIp6 {
				t.Errorf("Allocated IP6 address:%s does not match with the expected:%s", ip6, tc.expectedIp6)
			}
			var timesUpdateWasCalled int
			if netClientStub.DanmClient.NetClient != nil {
				timesUpdateWasCalled = netClientStub.DanmClient.NetClient.TimesUpdateWasCalled
			}
			if tc.timesUpdateShouldBeCalled != timesUpdateWasCalled {
				t.Errorf("%s", "Network manifest should have been updated:"+strconv.Itoa(tc.timesUpdateShouldBeCalled)+" times, but it happened:"+strconv.Itoa(timesUpdateWasCalled)+" times instead")
			}
		})
	}
}

func TestFree(t *testing.T) {
	err := utils.SetupAllocationPools(testNets)
	if err != nil {
		t.Errorf("Allocation pool for testnets could not be set-up because:%v", err)
	}
	for _, tc := range freeTcs {
		t.Run(tc.netName, func(t *testing.T) {
			var ips []utils.ReservedIpsList
			ips = utils.AppendIpToExpectedAllocsList(ips, tc.allocatedIp, false, testNets[tc.netIndex].Spec.NetworkID)
			testArtifacts := utils.TestArtifacts{TestNets: testNets, ReservedIps: ips}
			netClientStub := stubs.NewClientSetStub(testArtifacts)
			err := ipam.Free(netClientStub, testNets[tc.netIndex], tc.allocatedIp)
			if (err != nil && !tc.isErrorExpected) || (err == nil && tc.isErrorExpected) {
				t.Errorf("Received error:%v does not match with expectation", err)
				return
			}
			var timesUpdateWasCalled int
			if netClientStub.DanmClient.NetClient != nil {
				timesUpdateWasCalled = netClientStub.DanmClient.NetClient.TimesUpdateWasCalled
			}
			if tc.timesUpdateShouldBeCalled != timesUpdateWasCalled {
				t.Errorf("Network manifest should have been updated:" + strconv.Itoa(tc.timesUpdateShouldBeCalled) + " times, but it happened:" + strconv.Itoa(timesUpdateWasCalled) + " times instead")
			}
		})
	}
}

func TestGarbageCollectIps(t *testing.T) {
	err := utils.SetupAllocationPools(testNets)
	if err != nil {
		t.Errorf("Allocation pool for testnets could not be set-up because:%v", err)
	}
	for _, tc := range gcTcs {
		t.Run(tc.netName, func(t *testing.T) {
			var ips []utils.ReservedIpsList
			ips = utils.AppendIpToExpectedAllocsList(ips, tc.allocatedIp4, false, testNets[tc.netIndex].Spec.NetworkID)
			ips = utils.AppendIpToExpectedAllocsList(ips, tc.allocatedIp6, false, testNets[tc.netIndex].Spec.NetworkID)
			testArtifacts := utils.TestArtifacts{TestNets: testNets, ReservedIps: ips}
			netClientStub := stubs.NewClientSetStub(testArtifacts)
			ipam.GarbageCollectIps(netClientStub, &testNets[tc.netIndex], tc.allocatedIp4, tc.allocatedIp6)
		})
	}
}

func TestMain(m *testing.M) {
	code := m.Run()
	os.Exit(code)
}
