package confman_test

import (
	"strconv"
	"testing"

	danmtypes "github.com/danm-cni/danm/crd/apis/danm/v1"
	"github.com/danm-cni/danm/pkg/bitarray"
	"github.com/danm-cni/danm/pkg/confman"
	stubs "github.com/danm-cni/danm/test/stubs/danm"
	"github.com/danm-cni/danm/test/utils"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type TconfSets struct {
	sets []TconfSet
}

type TconfSet struct {
	name   string
	tconfs []danmtypes.TenantConfig
}

var (
	emptyTconfs []danmtypes.TenantConfig
	errorTconfs = []danmtypes.TenantConfig{
		danmtypes.TenantConfig{
			ObjectMeta: meta_v1.ObjectMeta{Name: "error"},
			HostDevices: []danmtypes.IfaceProfile{
				danmtypes.IfaceProfile{Name: "ens4", VniType: "vxlan", VniRange: "700-710", Alloc: utils.AllocFor5k},
			},
		},
	}
	multipleTconfs = []danmtypes.TenantConfig{
		danmtypes.TenantConfig{ObjectMeta: meta_v1.ObjectMeta{Name: "firstConf"}},
		danmtypes.TenantConfig{ObjectMeta: meta_v1.ObjectMeta{Name: "secondConf"}},
	}
	reserveConfs = []danmtypes.TenantConfig{
		danmtypes.TenantConfig{
			ObjectMeta: meta_v1.ObjectMeta{Name: "tconf"},
			HostDevices: []danmtypes.IfaceProfile{
				danmtypes.IfaceProfile{Name: "ens4", VniType: "vxlan", VniRange: "700-710", Alloc: utils.AllocFor5k},
				danmtypes.IfaceProfile{Name: "ens4", VniType: "vlan", VniRange: "200,500-510", Alloc: utils.AllocFor5k},
				danmtypes.IfaceProfile{Name: "ens6", VniType: "vxlan", VniRange: "1200-1300", Alloc: utils.AllocFor5k},
				danmtypes.IfaceProfile{Name: "nokia.k8s.io/sriov_ens1f0", VniType: "vlan", VniRange: "1500-1550", Alloc: utils.AllocFor5k},
				danmtypes.IfaceProfile{Name: "nokia.k8s.io/sriov_ens1f0", VniType: "vxlan", VniRange: "1600-1650", Alloc: utils.AllocFor5k},
			},
		},
		danmtypes.TenantConfig{
			ObjectMeta: meta_v1.ObjectMeta{Name: "error"},
			HostDevices: []danmtypes.IfaceProfile{
				danmtypes.IfaceProfile{Name: "ens4", VniType: "vxlan", VniRange: "800-810", Alloc: utils.AllocFor5k},
			},
		},
		danmtypes.TenantConfig{
			ObjectMeta: meta_v1.ObjectMeta{Name: "corrupt"},
			HostDevices: []danmtypes.IfaceProfile{
				danmtypes.IfaceProfile{Name: "corrupt", VniType: "vxlan", VniRange: "700-710", Alloc: ""},
			},
		},
		danmtypes.TenantConfig{
			ObjectMeta: meta_v1.ObjectMeta{Name: "conflict"},
			HostDevices: []danmtypes.IfaceProfile{
				danmtypes.IfaceProfile{Name: "conflict", VniType: "vxlan", VniRange: "700-710", Alloc: utils.AllocFor5k},
			},
		},
		danmtypes.TenantConfig{
			ObjectMeta: meta_v1.ObjectMeta{Name: "conflicterror"},
			HostDevices: []danmtypes.IfaceProfile{
				danmtypes.IfaceProfile{Name: "conflict", VniType: "vxlan", VniRange: "700-710", Alloc: utils.AllocFor5k},
			},
		},
	}
	reserveIfaces = []danmtypes.IfaceProfile{
		danmtypes.IfaceProfile{Name: "invalidVni", VniRange: "invalid"},
		danmtypes.IfaceProfile{Name: "ens4", VniType: "vxlan", VniRange: "700-710", Alloc: utils.AllocFor5k},
		danmtypes.IfaceProfile{Name: "ens4", VniType: "vlan", VniRange: "200,500-510", Alloc: utils.AllocFor5k},
		danmtypes.IfaceProfile{Name: "hupak", VniType: "vlan", VniRange: "1000,1001", Alloc: utils.AllocFor5k},
		danmtypes.IfaceProfile{Name: "corrupt", VniType: "vxlan", VniRange: "700-710", Alloc: ""},
		danmtypes.IfaceProfile{Name: "conflict", VniType: "vxlan", VniRange: "700-710", Alloc: utils.AllocFor5k},
	}
	tconfSets = []TconfSet{
		TconfSet{name: "emptyTcs", tconfs: emptyTconfs},
		TconfSet{name: "errorTconfs", tconfs: errorTconfs},
		TconfSet{name: "multipleConfigs", tconfs: multipleTconfs},
	}
	testConfigs = TconfSets{sets: tconfSets}
	testNets    = []danmtypes.DanmNet{
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "ens5"},
			Spec:       danmtypes.DanmNetSpec{NetworkID: "int", Options: danmtypes.DanmNetOption{Device: "ens5", Vlan: 1200}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "ens6"},
			Spec:       danmtypes.DanmNetSpec{NetworkID: "int", Options: danmtypes.DanmNetOption{Device: "ens6", Vlan: 1200}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "ipvlan_vlan"},
			Spec:       danmtypes.DanmNetSpec{NetworkID: "int", NetworkType: "ipvlan", Options: danmtypes.DanmNetOption{Device: "ens4", Vlan: 510}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "ipvlan_vxlan"},
			Spec:       danmtypes.DanmNetSpec{NetworkID: "int", NetworkType: "ipvlan", Options: danmtypes.DanmNetOption{Device: "ens4", Vxlan: 700}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "sriov_vlan"},
			Spec:       danmtypes.DanmNetSpec{NetworkID: "ext", NetworkType: "sriov", Options: danmtypes.DanmNetOption{DevicePool: "nokia.k8s.io/sriov_ens1f0", Vlan: 1540}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "sriov_vxlan"},
			Spec:       danmtypes.DanmNetSpec{NetworkID: "ext", NetworkType: "sriov", Options: danmtypes.DanmNetOption{DevicePool: "nokia.k8s.io/sriov_ens1f0", Vxlan: 1600}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "novni"},
			Spec:       danmtypes.DanmNetSpec{NetworkID: "internal", NetworkType: "ipvlan", Options: danmtypes.DanmNetOption{Device: "ens4"}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "corrupt"},
			Spec:       danmtypes.DanmNetSpec{NetworkID: "internal", NetworkType: "ipvlan", Options: danmtypes.DanmNetOption{Device: "corrupt", Vxlan: 700}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "conflict"},
			Spec:       danmtypes.DanmNetSpec{NetworkID: "internal", NetworkType: "ipvlan", Options: danmtypes.DanmNetOption{Device: "conflict", Vxlan: 705}},
		},
	}
)

var getTconfTcs = []struct {
	tcName          string
	tconfSets       TconfSets
	isErrorExpected bool
}{
	{"emptyTcs", testConfigs, true},
	{"errorTconfs", testConfigs, true},
	{"multipleConfigs", testConfigs, false},
}

var reserveTcs = []struct {
	tcName                    string
	tconfName                 string
	ifaceName                 string
	vniType                   string
	reserveVnis               []int
	isErrorExpected           bool
	expectedVni               int
	timesUpdateShouldBeCalled int
}{
	{"invalidVni", "tconf", "invalidVni", "", nil, true, 0, 0},
	{"reserveFirstFreeInEmptyIface", "tconf", "ens4", "vlan", nil, false, 200, 1},
	{"reserveLastFreeInIface", "tconf", "ens4", "vlan", []int{200, 509}, false, 510, 1},
	{"noFreeVniInIface", "tconf", "ens4", "vlan", []int{200, 510}, true, 0, 0},
	{"errorUpdating", "error", "ens4", "vxlan", nil, true, 0, 1},
	{"nonExistentProfile", "tconf", "hupak", "vlan", nil, true, 0, 0},
	{"corruptedVniAllocation", "corrupt", "corrupt", "vxlan", nil, true, 0, 0},
	{"conflictDuringFirstUpdate", "conflict", "conflict", "vxlan", []int{700, 708}, false, 710, 2},
	{"failsToRefreshAfterConflict", "conflicterror", "conflict", "vxlan", []int{700, 708}, true, 0, 1},
}

var freeTcs = []struct {
	tcName                    string
	tconfName                 string
	networkName               string
	ifaceNameToCheck          string
	ifaceTypeToCheck          string
	vniShouldBeSet            bool
	isErrorExpected           bool
	timesUpdateShouldBeCalled int
}{
	{"invalidIface", "tconf", "ens5", "", "", false, false, 0},
	{"invalidVniType", "tconf", "ens6", "ens6", "vxlan", true, false, 0},
	{"hostDeviceWithVlan", "tconf", "ipvlan_vlan", "ens4", "vlan", false, false, 1},
	{"hostDeviceWithVxlan", "tconf", "ipvlan_vxlan", "ens4", "vxlan", false, false, 1},
	{"devicePoolWithVlan", "tconf", "sriov_vlan", "nokia.k8s.io/sriov_ens1f0", "vlan", false, false, 1},
	{"devicePoolWithVxlan", "tconf", "sriov_vxlan", "nokia.k8s.io/sriov_ens1f0", "vxlan", false, false, 1},
	{"errorUpdating", "error", "ipvlan_vxlan", "ens4", "vxlan", false, true, 1},
	{"noVnis", "tconf", "novni", "", "", false, false, 0},
	{"corruptedVniAllocation", "corrupt", "corrupt", "", "", false, true, 0},
	{"conflictDuringFree", "conflict", "conflict", "conflict", "vxlan", false, false, 2},
}

func TestGetTenantConfig(t *testing.T) {
	for _, tc := range getTconfTcs {
		t.Run(tc.tcName, func(t *testing.T) {
			tconfSet := getTconfSet(tc.tcName, tc.tconfSets.sets)
			testArtifacts := utils.TestArtifacts{TestTconfs: tconfSet}
			tconfClientStub := stubs.NewClientSetStub(testArtifacts)
			tconf, err := confman.GetTenantConfig(tconfClientStub)
			if (err != nil && !tc.isErrorExpected) || (err == nil && tc.isErrorExpected) {
				t.Errorf("Received error:%v does not match with expectation", err)
				return
			}
			if tconf != nil && tconf.ObjectMeta.Name != tconfSet[0].ObjectMeta.Name {
				t.Errorf("The name of the returned TenantConfig:%s does not match with the expected:%s", tconf.ObjectMeta.Name, tconfSet[0].ObjectMeta.Name)
			}
		})
	}
}

func TestReserve(t *testing.T) {
	for _, tc := range reserveTcs {
		t.Run(tc.tcName, func(t *testing.T) {
			tconf := utils.GetTconf(tc.tconfName, reserveConfs)
			iface := getIface(tc.ifaceName, tc.vniType, reserveIfaces)
			var exhaustAllocs []int
			if tc.reserveVnis != nil {
				utils.ReserveVnis(&iface, tc.reserveVnis)
				index, _ := getIfaceFromTconf(tc.ifaceName, tc.vniType, tconf)
				tconf.HostDevices[index] = iface
				exhaustAllocs = make([]int, 2)
				exhaustAllocs[0] = tc.reserveVnis[0]
				exhaustAllocs[1] = tc.reserveVnis[1] + 1
			}
			testArtifacts := utils.TestArtifacts{TestTconfs: reserveConfs, ExhaustAllocs: exhaustAllocs}
			tconfClientStub := stubs.NewClientSetStub(testArtifacts)
			vni, err := confman.Reserve(tconfClientStub, tconf, iface)
			if (err != nil && !tc.isErrorExpected) || (err == nil && tc.isErrorExpected) {
				t.Errorf("Received error:%v does not match with expectation", err)
				return
			}
			if tc.expectedVni != 0 {
				if tc.expectedVni != vni {
					t.Errorf("Received reserved VNI:%d does not match with expected:%d", vni, tc.expectedVni)
					return
				}
				_, updatedIface := getIfaceFromTconf(tc.ifaceName, tc.vniType, tconf)
				if updatedIface.Alloc == iface.Alloc {
					t.Errorf("Alloc field in the selected inteface profile did not change even though a VNI was reserved!")
					return
				}
			}
			var timesUpdateWasCalled int
			if tconfClientStub.DanmClient.TconfClient != nil {
				timesUpdateWasCalled = tconfClientStub.DanmClient.TconfClient.TimesUpdateWasCalled
			}
			if tc.timesUpdateShouldBeCalled != timesUpdateWasCalled {
				t.Errorf("Tconf should have been updated:" + strconv.Itoa(tc.timesUpdateShouldBeCalled) + " times, but it happened:" + strconv.Itoa(timesUpdateWasCalled) + " times instead")
			}
		})
	}
}

func TestFree(t *testing.T) {
	for _, tc := range freeTcs {
		t.Run(tc.tcName, func(t *testing.T) {
			tconf := utils.GetTconf(tc.tconfName, reserveConfs)
			dnet := utils.GetTestNet(tc.networkName, testNets)
			var exhaustAllocs []int
			if tc.ifaceNameToCheck != "" {
				index, iface := getIfaceFromTconf(tc.ifaceNameToCheck, tc.ifaceTypeToCheck, tconf)
				utils.ReserveVnis(&iface, []int{0, 4999})
				exhaustAllocs = make([]int, 2)
				exhaustAllocs[0] = 0
				exhaustAllocs[1] = 4999
				tconf.HostDevices[index] = iface
			}
			testArtifacts := utils.TestArtifacts{TestTconfs: reserveConfs, TestNets: testNets, ExhaustAllocs: exhaustAllocs}
			tconfClientStub := stubs.NewClientSetStub(testArtifacts)
			err := confman.Free(tconfClientStub, tconf, dnet)
			if (err != nil && !tc.isErrorExpected) || (err == nil && tc.isErrorExpected) {
				t.Errorf("Received error:%v does not match with expectation", err)
				return
			}
			_, ifaceAfter := getIfaceFromTconf(tc.ifaceNameToCheck, tc.ifaceTypeToCheck, tconf)
			vniToCheck := dnet.Spec.Options.Vlan
			if dnet.Spec.Options.Vxlan != 0 {
				vniToCheck = dnet.Spec.Options.Vxlan
			}
			if tc.ifaceNameToCheck != "" && tc.vniShouldBeSet && !isVniSet(ifaceAfter, vniToCheck) {
				t.Errorf("VNI:%d in interface profile:%s should be set, but it's not!", vniToCheck, tc.ifaceNameToCheck)
				return
			} else if tc.ifaceNameToCheck != "" && !tc.vniShouldBeSet && isVniSet(ifaceAfter, vniToCheck) {
				t.Errorf("VNI:%d in interface profile:%s should not be set, but it is!", vniToCheck, tc.ifaceNameToCheck)
				return
			}
			var timesUpdateWasCalled int
			if tconfClientStub.DanmClient.TconfClient != nil {
				timesUpdateWasCalled = tconfClientStub.DanmClient.TconfClient.TimesUpdateWasCalled
			}
			if tc.timesUpdateShouldBeCalled != timesUpdateWasCalled {
				t.Errorf("Tconf should have been updated:" + strconv.Itoa(tc.timesUpdateShouldBeCalled) + " times, but it happened:" + strconv.Itoa(timesUpdateWasCalled) + " times instead")
			}
		})
	}
}
func getTconfSet(tconfName string, tconfSets []TconfSet) []danmtypes.TenantConfig {
	for _, tconfSet := range tconfSets {
		if tconfSet.name == tconfName {
			return tconfSet.tconfs
		}
	}
	return nil
}

func getIface(ifaceName string, vniType string, ifaceSet []danmtypes.IfaceProfile) danmtypes.IfaceProfile {
	for _, iface := range ifaceSet {
		if iface.Name == ifaceName && iface.VniType == vniType {
			return iface
		}
	}
	return danmtypes.IfaceProfile{}
}

func getIfaceFromTconf(ifaceName string, vniType string, tconf *danmtypes.TenantConfig) (int, danmtypes.IfaceProfile) {
	for index, iface := range tconf.HostDevices {
		if iface.Name == ifaceName && iface.VniType == vniType {
			return index, iface
		}
	}
	return -1, danmtypes.IfaceProfile{}
}

func isVniSet(iface danmtypes.IfaceProfile, vni int) bool {
	allocs := bitarray.NewBitArrayFromBase64(iface.Alloc)
	return allocs.Get(uint32(vni))
}
