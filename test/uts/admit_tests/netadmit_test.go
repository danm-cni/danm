package admit_tests

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	danmtypes "github.com/danm-cni/danm/crd/apis/danm/v1"
	"github.com/danm-cni/danm/pkg/admit"
	stubs "github.com/danm-cni/danm/test/stubs/danm"
	httpstub "github.com/danm-cni/danm/test/stubs/http"
	"github.com/danm-cni/danm/test/utils"
	"k8s.io/api/admission/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	DnetType = "DanmNet"
	TnetType = "TenantNetwork"
	CnetType = "ClusterNetwork"
)

var validateNetworkTcs = []struct {
	tcName                    string
	oldNetName                string
	newNetName                string
	neType                    string
	opType                    v1beta1.Operation
	tconf                     []danmtypes.TenantConfig
	eps                       []danmtypes.DanmEp
	isErrorExpected           bool
	expectedPatches           []admit.Patch
	timesUpdateShouldBeCalled int
}{
	{"EmptyRequest", "", "", "", "", nil, nil, true, nil, 0},
	{"MalformedOldObject", "malformed", "", "", "", nil, nil, true, nil, 0},
	{"MalformedNewObject", "", "malformed", "", "", nil, nil, true, nil, 0},
	{"ObjectWithInvalidType", "", "invalid-type", "", "", nil, nil, true, nil, 0},
	{"Ipv4RouteWithoutCidrDNet", "", "no-cidr", DnetType, "", nil, nil, true, nil, 0},
	{"Ipv4RouteWithoutCidrTNet", "", "no-cidr", TnetType, "", nil, nil, true, nil, 0},
	{"Ipv4RouteWithoutCidrCNet", "", "no-cidr", CnetType, "", nil, nil, true, nil, 0},
	{"Ipv4InvalidCidrDNet", "", "invalid-cidr", DnetType, "", nil, nil, true, nil, 0},
	{"Ipv4InvalidCidrTNet", "", "invalid-cidr", TnetType, "", nil, nil, true, nil, 0},
	{"Ipv4InvalidCidrCNet", "", "invalid-cidr", CnetType, "", nil, nil, true, nil, 0},
	{"Ipv4TooBigCidrDNet", "", "long-cidr", DnetType, "", nil, nil, true, nil, 0},
	{"Ipv4TooBigCidrTNet", "", "long-cidr", TnetType, "", nil, nil, true, nil, 0},
	{"Ipv4TooBigCidrCNet", "", "long-cidr", CnetType, "", nil, nil, true, nil, 0},
	{"Ipv4GwOutsideCidrDNet", "", "gw-outside-cidr", DnetType, "", nil, nil, true, nil, 0},
	{"Ipv4GwOutsideCidrTNet", "", "gw-outside-cidr", TnetType, "", nil, nil, true, nil, 0},
	{"Ipv4GwOutsideCidrCNet", "", "gw-outside-cidr", CnetType, "", nil, nil, true, nil, 0},
	{"Ipv6RouteWithoutCidrDNet", "", "no-net6", DnetType, "", nil, nil, true, nil, 0},
	{"Ipv6RouteWithoutCidrTNet", "", "no-net6", TnetType, "", nil, nil, true, nil, 0},
	{"Ipv6RouteWithoutCidrCNet", "", "no-net6", CnetType, "", nil, nil, true, nil, 0},
	{"Ipv6InvalidCidrDNet", "", "invalid-net6", DnetType, "", nil, nil, true, nil, 0},
	{"Ipv6InvalidCidrTNet", "", "invalid-net6", TnetType, "", nil, nil, true, nil, 0},
	{"Ipv6InvalidCidrCNet", "", "invalid-net6", CnetType, "", nil, nil, true, nil, 0},
	{"Ipv6GwOutsideCidrDNet", "", "gw-outside-net6", DnetType, "", nil, nil, true, nil, 0},
	{"Ipv6GwOutsideCidrTNet", "", "gw-outside-net6", TnetType, "", nil, nil, true, nil, 0},
	{"Ipv6GwOutsideCidrCNet", "", "gw-outside-net6", CnetType, "", nil, nil, true, nil, 0},
	{"InvalidVidsDNet", "", "invalid-vids", DnetType, "", nil, nil, true, nil, 0},
	{"InvalidVidsCNet", "", "invalid-vids", CnetType, "", nil, nil, true, nil, 0},
	{"MissingNidDNet", "", "missing-nid", DnetType, "", nil, nil, true, nil, 0},
	{"MissingNidCNet", "", "missing-nid", CnetType, "", nil, nil, true, nil, 0},
	{"TooLongNidWithDynamicNeTypeDNet", "", "long-nid", DnetType, "", nil, nil, true, nil, 0},
	{"TooLongNidWithDynamicNeTypeCNet", "", "long-nid", CnetType, "", nil, nil, true, nil, 0},
	{"WithAllowedTenantsDefinedDNet", "", "with-allowed-tenants", DnetType, "", nil, nil, true, nil, 0},
	{"WithAllowedTenantsDefinedTNet", "", "with-allowed-tenants", TnetType, "", nil, nil, true, nil, 0},
	{"SriovWithoutDevicePoolDNet", "", "sriov-without-dp", DnetType, "", nil, nil, true, nil, 0},
	{"SriovWithoutDevicePoolTNet", "", "sriov-without-dp", TnetType, "", nil, nil, true, nil, 0},
	{"SriovWithoutDevicePoolCNet", "", "sriov-without-dp", CnetType, "", nil, nil, true, nil, 0},
	{"SriovWithDeviceDNet", "", "sriov-with-device", DnetType, "", nil, nil, true, nil, 0},
	{"SriovWithDeviceTNet", "", "sriov-with-device", TnetType, "", nil, nil, true, nil, 0},
	{"SriovWithDeviceCNet", "", "sriov-with-device", CnetType, "", nil, nil, true, nil, 0},
	{"SriovWithDevicePlusDpDNet", "", "sriov-with-dp-and-device", DnetType, "", nil, nil, true, nil, 0},
	{"SriovWithDevicePlusDpTNet", "", "sriov-with-dp-and-device", TnetType, "", nil, nil, true, nil, 0},
	{"SriovWithDevicePlusDpCNet", "", "sriov-with-dp-and-device", CnetType, "", nil, nil, true, nil, 0},
	{"IpvlanWithDevicePlusDpDNet", "", "ipvlan-with-dp-and-device", DnetType, "", nil, nil, true, nil, 0},
	{"IpvlanWithDevicePlusDpTNet", "", "ipvlan-with-dp-and-device", TnetType, "", nil, nil, true, nil, 0},
	{"IpvlanWithDevicePlusDpCNet", "", "ipvlan-with-dp-and-device", CnetType, "", nil, nil, true, nil, 0},
	{"AllocDuringCreateDNet", "", "alloc-without-cidr", DnetType, v1beta1.Create, nil, nil, true, nil, 0},
	{"AllocDuringCreateTNet", "", "alloc-without-cidr", TnetType, v1beta1.Create, nil, nil, true, nil, 0},
	{"AllocDuringCreateCNet", "", "alloc-without-cidr", CnetType, v1beta1.Create, nil, nil, true, nil, 0},
	{"AllocationPoolWithoutCidrDNet", "", "alloc-without-cidr", DnetType, v1beta1.Update, nil, nil, true, nil, 0},
	{"AllocationPoolWithoutCidrTNet", "", "alloc-without-cidr", TnetType, v1beta1.Update, nil, nil, true, nil, 0},
	{"AllocationPoolWithoutCidrCNet", "", "alloc-without-cidr", CnetType, v1beta1.Update, nil, nil, true, nil, 0},
	{"AllocationPoolStartOutsideCidrDNet", "", "allocstart-outside-cidr", DnetType, "", nil, nil, true, nil, 0},
	{"AllocationPoolStartOutsideCidrTNet", "", "allocstart-outside-cidr", TnetType, "", nil, nil, true, nil, 0},
	{"AllocationPoolStartOutsideCidrCNet", "", "allocstart-outside-cidr", CnetType, "", nil, nil, true, nil, 0},
	{"AllocationPoolEndOutsideCidrDNet", "", "allocend-outside-cidr", DnetType, "", nil, nil, true, nil, 0},
	{"AllocationPoolEndOutsideCidrTNet", "", "allocend-outside-cidr", TnetType, "", nil, nil, true, nil, 0},
	{"AllocationPoolEndOutsideCidrCNet", "", "allocend-outside-cidr", CnetType, "", nil, nil, true, nil, 0},
	{"AllocationPoolWithoutAnyIpDNet", "", "no-free-ip", DnetType, "", nil, nil, true, nil, 0},
	{"AllocationPoolWithoutAnyIpTNet", "", "no-free-ip", TnetType, "", nil, nil, true, nil, 0},
	{"AllocationPoolWithoutAnyIpCNet", "", "no-free-ip", CnetType, "", nil, nil, true, nil, 0},
	{"CreateWithVlanTNet", "", "tnet-vlan", TnetType, v1beta1.Create, nil, nil, true, nil, 0},
	{"CreateWithVxlanTNet", "", "tnet-vxlan", TnetType, v1beta1.Create, nil, nil, true, nil, 0},
	{"UpdateWithVlanTNet", "", "tnet-vlan", TnetType, v1beta1.Update, nil, nil, true, nil, 0},
	{"UpdateWithVxlanTNet", "", "tnet-vxlan", TnetType, v1beta1.Update, nil, nil, true, nil, 0},
	{"UpdateWithDeviceTNet", "", "tnet-device", TnetType, v1beta1.Update, nil, nil, true, nil, 0},
	{"UpdateWithDevicePoolTNet", "", "tnet-dp", TnetType, v1beta1.Update, nil, nil, true, nil, 0},
	{"NoNeTypeCreateSuccess", "", "no-netype", DnetType, v1beta1.Create, nil, nil, false, neTypeAndAlloc, 0},
	{"NoNeTypeUpdateSuccess", "", "no-netype-update", CnetType, v1beta1.Update, nil, nil, false, onlyNeType, 0},
	{"L2NoPatchSuccess", "", "l2-with-allowedtenants", CnetType, v1beta1.Create, nil, nil, false, nil, 0},
	{"NoTConfForTNet", "", "l2", TnetType, v1beta1.Create, nil, nil, true, nil, 0},
	{"DeviceNotAllowedForTnet", "", "l2", TnetType, v1beta1.Create, oneDev, nil, true, nil, 0},
	{"DevicePoolNotAllowedForTnet", "", "tnet-dp", TnetType, v1beta1.Create, oneDev, nil, true, nil, 0},
	{"NoDevicesForRandomTnets", "", "no-netype", TnetType, v1beta1.Create, oneDevPool, nil, true, nil, 0},
	{"NoFreeVnisForTnet", "", "tnet-device", TnetType, v1beta1.Create, oneDev, nil, true, nil, 0},
	{"DeviceAndVlanTnetSuccess", "", "tnet-ens3", TnetType, v1beta1.Create, twoDevs, nil, false, allocAndVlan, 1},
	{"DeviceAndVxlanTnetSuccess", "", "tnet-ens4", TnetType, v1beta1.Create, twoDevs, nil, false, allocAndVxlan, 1},
	{"DevicePoolAndVlanTnetSuccess", "", "tnet-ens1f0", TnetType, v1beta1.Create, twoDevPools, nil, false, allocAndVlan, 1},
	{"DevicePoolAndVxlanTnetSuccess", "", "tnet-ens1f1", TnetType, v1beta1.Create, twoDevPools, nil, false, allocAndVxlan, 1},
	{"RandomDeviceAndVxlanTnetSuccess", "", "tnet-random", TnetType, v1beta1.Create, randomDev, nil, false, allocAndVxlanAndDevice, 1},
	{"FlannelWithNidOverwriteTnetSuccess", "", "flannel-with-name", TnetType, v1beta1.Create, nidMappings, nil, false, onlyNid, 0},
	{"FlannelWithNidSettingTnetSuccess", "", "flannel-without-name", TnetType, v1beta1.Create, nidMappings, nil, false, onlyNid, 0},
	{"IpvlanWithNidSettingTnetSuccess", "", "ipvlan-without-name", TnetType, v1beta1.Create, nidMappings, nil, false, deviceAndNidAndVxlan, 1},
	{"CalicoWithoutNidTnet", "", "calico-without-name", TnetType, v1beta1.Create, nidMappings, nil, true, nil, 0},
	{"CannotModifyDueToErrorDNet", "vniOld", "vniNew", DnetType, v1beta1.Update, nil, errEp, true, nil, 0},
	{"CannotModifyDueToErrorCNet", "vniOld", "vniNew", CnetType, v1beta1.Update, nil, errEp, true, nil, 0},
	{"OkayToModifyNoConnectionsDNet", "vniOld", "vniNew", DnetType, v1beta1.Update, nil, noMatchDnet, false, nil, 0},
	{"NotOkayToModifyVlanDNet", "vniOld", "vniNew", DnetType, v1beta1.Update, nil, matchDnet, true, nil, 0},
	{"NotOkayToModifyVlanCNet", "vniOld", "vniNew", CnetType, v1beta1.Update, nil, matchCnet, true, nil, 0},
	{"NotOkayToModifyVxlanDNet", "vxlanOld", "vxlanNew", DnetType, v1beta1.Update, nil, matchDnet2, true, nil, 0},
	{"NotOkayToModifyVxlanCNet", "vxlanOld", "vxlanNew", CnetType, v1beta1.Update, nil, matchCnet2, true, nil, 0},
	{"NotOkayToModifyDeviceDNet", "vniOld", "deviceNew", DnetType, v1beta1.Update, nil, matchDnet, true, nil, 0},
	{"NotOkayToModifyDeviceCNet", "vniOld", "deviceNew", CnetType, v1beta1.Update, nil, matchCnet, true, nil, 0},
	{"OkayToModifyRandomChangeCNet", "vniOld", "nidNew", CnetType, v1beta1.Update, nil, matchCnet, false, nil, 0},
	{"Ipv6ProvidedAsCidrDNet", "", "v6-as-cidr", DnetType, "", nil, nil, true, nil, 0},
	{"Ipv6ProvidedAsCidrTNet", "", "v6-as-cidr", TnetType, "", nil, nil, true, nil, 0},
	{"Ipv6ProvidedAsCidrCNet", "", "v6-as-cidr", CnetType, "", nil, nil, true, nil, 0},
	{"Ipv4ProvidedAsNet6DNet", "", "v4-as-net6", DnetType, "", nil, nil, true, nil, 0},
	{"Ipv4ProvidedAsNet6TNet", "", "v4-as-net6", TnetType, "", nil, nil, true, nil, 0},
	{"Ipv4ProvidedAsNet6CNet", "", "v4-as-net6", CnetType, "", nil, nil, true, nil, 0},
	{"Ipv4ProvidedAsPool6CidrDNet", "", "v4-as-pool6", DnetType, "", nil, nil, true, nil, 0},
	{"Ipv4ProvidedAsPool6CidrTNet", "", "v4-as-pool6", TnetType, "", nil, nil, true, nil, 0},
	{"Ipv4ProvidedAsPool6CidrCNet", "", "v4-as-pool6", CnetType, "", nil, nil, true, nil, 0},
	{"InvalidPool6CidrDNet", "", "invalid-pool6", DnetType, "", nil, nil, true, nil, 0},
	{"InvalidPool6CidrTNet", "", "invalid-pool6", TnetType, "", nil, nil, true, nil, 0},
	{"InvalidPool6CidrCNet", "", "invalid-pool6", CnetType, "", nil, nil, true, nil, 0},
	{"Pool6CidrWithoutNet6DNet", "", "pool6-wo-net6", DnetType, "", nil, nil, true, nil, 0},
	{"Pool6CidrWithoutNet6TNet", "", "pool6-wo-net6", TnetType, "", nil, nil, true, nil, 0},
	{"Pool6CidrWithoutNet6CNet", "", "pool6-wo-net6", CnetType, "", nil, nil, true, nil, 0},
	{"CreateBigV6NetworkWithoutPool6DNet", "", "big-net6-without-pool6", DnetType, v1beta1.Create, nil, nil, false, v6Allocs, 0},
	{"CreateBigV6NetworkWithoutPool6TNet", "", "big-net6-without-pool6", TnetType, v1beta1.Create, randomDev, nil, false, v6AllocsForTnet, 1},
	{"CreateBigV6NetworkWithoutPool6CNet", "", "big-net6-without-pool6", CnetType, v1beta1.Create, nil, nil, false, v6Allocs, 0},
	{"CreateSmallV6NetworkWithoutPool6DNet", "", "small-net6-without-pool6", DnetType, v1beta1.Create, nil, nil, false, v6Allocs, 0},
	{"CreateSmallV6NetworkWithoutPool6TNet", "", "small-net6-without-pool6", TnetType, v1beta1.Create, randomDev, nil, false, v6AllocsForTnet, 1},
	{"CreateSmallV6NetworkWithoutPool6CNet", "", "small-net6-without-pool6", CnetType, v1beta1.Create, nil, nil, false, v6Allocs, 0},
	{"V4PlusV6IsOverCapacity", "", "no-space-for-v6-alloc", DnetType, "", nil, nil, true, nil, 0},
	{"Pool6CidrBiggerThanNet6", "", "pool6-cidr-outside-net6", DnetType, "", nil, nil, true, nil, 0},
	{"InvalidPool6StartAddress", "", "invalid-pool6-start", DnetType, "", nil, nil, true, nil, 0},
	{"Pool6StartAddressMatchesEnd", "", "pool6-end-equals-start", DnetType, "", nil, nil, true, nil, 0},
}

var (
	valNets = []danmtypes.DanmNet{
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "malformed"},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "invalid-type"},
			TypeMeta:   meta_v1.TypeMeta{Kind: "DanmEp"},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "no-cidr"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Routes: map[string]string{"10.20.0.0/24": "10.0.0.1"}}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "invalid-cidr"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Cidr: "192.168.1.0/a4"}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "long-cidr"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Cidr: "10.0.0.0/7"}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "gw-outside-cidr"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Cidr: "10.20.1.0/24", Routes: map[string]string{"10.20.20.0/24": "10.20.1.1", "10.20.30.0/24": "10.20.0.1"}}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "no-net6"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Routes6: map[string]string{"2a00:8a00:a000:1193::/64": "2a00:8a00:a000:1192::"}}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "invalid-net6"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Net6: "2g00:8a00:a000:1193::/64"}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "gw-outside-net6"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Net6: "2a00:8a00:a000:1193::/64", Routes6: map[string]string{"3a00:8a00:a000:1193::/64": "4a00:8a00:a000:1192::"}}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "invalid-vids"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Vlan: 50, Vxlan: 60}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "missing-nid"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "flannel"},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "long-nid"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "abcdeftgasdf", Options: danmtypes.DanmNetOption{Vlan: 50}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "with-allowed-tenants"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", AllowedTenants: []string{"tenant1", "tenant2"}, Options: danmtypes.DanmNetOption{Vlan: 50}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "sriov-without-dp"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "sriov", NetworkID: "e2"},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "sriov-with-device"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "sriov", NetworkID: "e2", Options: danmtypes.DanmNetOption{Device: "ens1f1"}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "sriov-with-dp-and-device"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "sriov", NetworkID: "e2", Options: danmtypes.DanmNetOption{DevicePool: "nokia.k8s.io/sriov_ens1f1", Device: "ens1f1"}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "ipvlan-with-dp-and-device"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{DevicePool: "nokia.k8s.io/sriov_ens1f1", Device: "ens1f1"}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "alloc-without-cidr"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Alloc: "gAAAAAAAAAAAAAAE", Pool: danmtypes.IpPool{Start: "192.168.1.1"}}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "allocstart-outside-cidr"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Pool: danmtypes.IpPool{Start: "192.168.1.63"}, Cidr: "192.168.1.64/26"}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "allocend-outside-cidr"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Pool: danmtypes.IpPool{End: "192.168.1.128"}, Cidr: "192.168.1.64/26"}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "no-free-ip"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Pool: danmtypes.IpPool{Start: "192.168.1.127", End: "192.168.1.127"}, Cidr: "192.168.1.64/26"}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "tnet-vlan"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Vlan: 50}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "tnet-vxlan"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Vxlan: 50}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "tnet-device"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Device: "ens4"}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "tnet-dp"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{DevicePool: "nokia.k8s.io/sriov_ens1f0"}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "no-netype"},
			Spec:       danmtypes.DanmNetSpec{NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Cidr: "192.168.1.64/26", Routes: map[string]string{"10.20.0.0/24": "192.168.1.64"}}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "no-netype-update"},
			Spec:       danmtypes.DanmNetSpec{NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Alloc: "gAAAAAE=", Pool: danmtypes.IpPool{Start: "192.168.1.65", End: "192.168.1.126"}, Cidr: "192.168.1.64/26", Routes: map[string]string{"10.20.0.0/24": "192.168.1.64"}}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "l2"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Device: "ens3"}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "l2-with-allowedtenants"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", AllowedTenants: []string{"tenant1", "tenant2"}, Options: danmtypes.DanmNetOption{Device: "ens3"}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "tnet-ens3"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Device: "ens3", Pool: danmtypes.IpPool{Start: "192.168.1.65", End: "192.168.1.126"}, Cidr: "192.168.1.64/26"}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "tnet-ens4"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Device: "ens4", Pool: danmtypes.IpPool{Start: "192.168.1.65", End: "192.168.1.126"}, Cidr: "192.168.1.64/26"}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "tnet-ens1f0"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "sriov", NetworkID: "e2", Options: danmtypes.DanmNetOption{DevicePool: "nokia.k8s.io/sriov_ens1f0", Pool: danmtypes.IpPool{Start: "192.168.1.65", End: "192.168.1.126"}, Cidr: "192.168.1.64/26"}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "tnet-ens1f1"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "sriov", NetworkID: "e2", Options: danmtypes.DanmNetOption{DevicePool: "nokia.k8s.io/sriov_ens1f1", Pool: danmtypes.IpPool{Start: "192.168.1.65", End: "192.168.1.126"}, Cidr: "192.168.1.64/26"}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "tnet-random"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "macvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Pool: danmtypes.IpPool{Start: "192.168.1.65", End: "192.168.1.126"}, Cidr: "192.168.1.64/26"}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "flannel-with-name"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "flannel", NetworkID: "hupak"},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "flannel-without-name"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "flannel"},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "ipvlan-without-name"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan"},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "calico-without-name"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "calico"},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "vniOld", Namespace: "vni-test"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Device: "ens4", Vlan: 50}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "vniNew", Namespace: "vni-test"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Device: "ens4", Vlan: 51}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "vxlanOld", Namespace: "vni-test"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Device: "ens4", Vxlan: 50}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "vxlanNew", Namespace: "vni-test"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Device: "ens4", Vxlan: 51}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "deviceNew", Namespace: "vni-test"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Device: "ens5", Vlan: 50}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "nidNew", Namespace: "vni-test"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "e2", Options: danmtypes.DanmNetOption{Device: "ens4", Vlan: 50}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "v6-as-cidr"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Cidr: "2a00:8a00:a000:1193::/64"}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "v4-as-net6"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Net6: "192.168.1.0/24"}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "v4-as-pool6"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Net6: "2a00:8a00:a000:1193::/64", Pool6: danmtypes.IpPoolV6{Cidr: "192.168.1.0/24"}}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "invalid-pool6"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Net6: "2a00:8a00:a000:1193::/64", Pool6: danmtypes.IpPoolV6{Cidr: "2a00:8a00:a000:1193::/129"}}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "pool6-wo-net6"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Pool6: danmtypes.IpPoolV6{Cidr: "2a00:8a00:a000:1193::/64"}}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "big-net6-without-pool6"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Net6: "2a00:8a00:a000:1193::/64"}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "small-net6-without-pool6"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Net6: "2001:db8:85a3::8a2e:370:7334/120"}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "no-space-for-v6-alloc"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Cidr: "37.0.0.0/9", Net6: "2001:db8:85a3::8a2e:370:7334/105"}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "pool6-cidr-outside-net6"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Net6: "2001:db8:85a3::8a2e:370:7334/110", Pool6: danmtypes.IpPoolV6{Cidr: "2001:db8:85a3::8a2e:370:7334/109"}}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "invalid-pool6-start"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Net6: "2001:db8:85a3::8a2e:370:7334/108", Pool6: danmtypes.IpPoolV6{Cidr: "2001:db8:85a3::8a2e:370:7334/109", IpPool: danmtypes.IpPool{Start: "2001:db8:85a3::8a2e:370:734g"}}}},
		},
		danmtypes.DanmNet{
			ObjectMeta: meta_v1.ObjectMeta{Name: "pool6-end-equals-start"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "nanomsg", Options: danmtypes.DanmNetOption{Net6: "2001:db8:85a3::8a2e:370:7334/108", Pool6: danmtypes.IpPoolV6{Cidr: "2001:db8:85a3::8a2e:370:7334/109", IpPool: danmtypes.IpPool{Start: "2001:db8:85a3::8a2e:370:7340", End: "2001:db8:85a3::8a2e:370:7340"}}}},
		},
	}
)

var (
	neTypeAndAlloc = []admit.Patch{
		admit.Patch{Path: "/spec/NetworkType"},
		admit.Patch{Path: "/spec/Options/alloc"},
		admit.Patch{Path: "/spec/Options/allocation_pool"},
	}
	onlyNeType = []admit.Patch{
		admit.Patch{Path: "/spec/NetworkType"},
	}
	allocAndVlan = []admit.Patch{
		admit.Patch{Path: "/spec/Options/alloc"},
		admit.Patch{Path: "/spec/Options/vlan"},
	}
	allocAndVxlan = []admit.Patch{
		admit.Patch{Path: "/spec/Options/alloc"},
		admit.Patch{Path: "/spec/Options/vxlan"},
	}
	allocAndVxlanAndDevice = []admit.Patch{
		admit.Patch{Path: "/spec/Options/alloc"},
		admit.Patch{Path: "/spec/Options/vxlan"},
		admit.Patch{Path: "/spec/Options/host_device"},
	}
	onlyNid = []admit.Patch{
		admit.Patch{Path: "/spec/NetworkID"},
	}
	deviceAndNidAndVxlan = []admit.Patch{
		admit.Patch{Path: "/spec/NetworkID"},
		admit.Patch{Path: "/spec/Options/host_device"},
		admit.Patch{Path: "/spec/Options/vxlan"},
	}
	v6Allocs = []admit.Patch{
		admit.Patch{Path: "/spec/Options/alloc6"},
		admit.Patch{Path: "/spec/Options/allocation_pool_v6"},
	}
	v6AllocsForTnet = []admit.Patch{
		admit.Patch{Path: "/spec/Options/host_device"},
		admit.Patch{Path: "/spec/Options/vxlan"},
		admit.Patch{Path: "/spec/Options/alloc6"},
		admit.Patch{Path: "/spec/Options/allocation_pool_v6"},
	}
)

var (
	oneDev = []danmtypes.TenantConfig{
		danmtypes.TenantConfig{
			ObjectMeta: meta_v1.ObjectMeta{Name: "tconf"}, TypeMeta: meta_v1.TypeMeta{Kind: "TenantConfig"},
			HostDevices: []danmtypes.IfaceProfile{
				danmtypes.IfaceProfile{Name: "ens4", VniType: "vxlan", VniRange: "900-4999,5000", Alloc: utils.ExhaustedAllocFor5k},
			},
		},
	}
	oneDevPool = []danmtypes.TenantConfig{
		danmtypes.TenantConfig{
			ObjectMeta: meta_v1.ObjectMeta{Name: "tconf"}, TypeMeta: meta_v1.TypeMeta{Kind: "TenantConfig"},
			HostDevices: []danmtypes.IfaceProfile{
				danmtypes.IfaceProfile{Name: "nokia.k8s.io/sriov_ens1f1", VniType: "vlan", VniRange: "900-4999,5000", Alloc: utils.ExhaustedAllocFor5k},
			},
		},
	}
	twoDevs = []danmtypes.TenantConfig{
		danmtypes.TenantConfig{
			ObjectMeta: meta_v1.ObjectMeta{Name: "tconf"}, TypeMeta: meta_v1.TypeMeta{Kind: "TenantConfig"},
			HostDevices: []danmtypes.IfaceProfile{
				danmtypes.IfaceProfile{Name: "ens3", VniType: "vlan", VniRange: "900-4999,5000", Alloc: utils.AllocFor5k},
				danmtypes.IfaceProfile{Name: "ens4", VniType: "vxlan", VniRange: "1000-4999,5000", Alloc: utils.AllocFor5k},
			},
		},
	}
	twoDevPools = []danmtypes.TenantConfig{
		danmtypes.TenantConfig{
			ObjectMeta: meta_v1.ObjectMeta{Name: "tconf"}, TypeMeta: meta_v1.TypeMeta{Kind: "TenantConfig"},
			HostDevices: []danmtypes.IfaceProfile{
				danmtypes.IfaceProfile{Name: "nokia.k8s.io/sriov_ens1f0", VniType: "vlan", VniRange: "900-4999,5000", Alloc: utils.AllocFor5k},
				danmtypes.IfaceProfile{Name: "nokia.k8s.io/sriov_ens1f1", VniType: "vxlan", VniRange: "1000-4999,5000", Alloc: utils.AllocFor5k},
			},
		},
	}
	randomDev = []danmtypes.TenantConfig{
		danmtypes.TenantConfig{
			ObjectMeta: meta_v1.ObjectMeta{Name: "tconf"}, TypeMeta: meta_v1.TypeMeta{Kind: "TenantConfig"},
			HostDevices: []danmtypes.IfaceProfile{
				danmtypes.IfaceProfile{Name: "ens4", VniType: "vxlan", VniRange: "900-4999,5000", Alloc: utils.AllocFor5k},
			},
		},
	}
	nidMappings = []danmtypes.TenantConfig{
		danmtypes.TenantConfig{
			ObjectMeta: meta_v1.ObjectMeta{Name: "tconf"}, TypeMeta: meta_v1.TypeMeta{Kind: "TenantConfig"},
			HostDevices: []danmtypes.IfaceProfile{
				danmtypes.IfaceProfile{Name: "ens4", VniType: "vxlan", VniRange: "900-4999,5000", Alloc: utils.AllocFor5k},
			},
			NetworkIds: map[string]string{
				"flannel": "flannel1234567",
				"ipvlan":  "ipvlan",
			},
		},
	}
)

var (
	errEp = []danmtypes.DanmEp{
		danmtypes.DanmEp{
			ObjectMeta: meta_v1.ObjectMeta{Name: "error"},
		},
	}
	noMatchDnet = []danmtypes.DanmEp{
		danmtypes.DanmEp{
			ObjectMeta: meta_v1.ObjectMeta{Name: "random1", Namespace: "vni-test"},
			Spec:       danmtypes.DanmEpSpec{ApiType: "TenantNetwork", NetworkName: "vniOld"},
		},
		danmtypes.DanmEp{
			ObjectMeta: meta_v1.ObjectMeta{Name: "random2", Namespace: "vni-test"},
			Spec:       danmtypes.DanmEpSpec{ApiType: "DanmNet", NetworkName: "vniOl"},
		},
		danmtypes.DanmEp{
			ObjectMeta: meta_v1.ObjectMeta{Name: "random3", Namespace: "vni-test"},
			Spec:       danmtypes.DanmEpSpec{ApiType: "DanmNet", NetworkName: "niOld"},
		},
		danmtypes.DanmEp{
			ObjectMeta: meta_v1.ObjectMeta{Name: "random4", Namespace: "vni-test"},
			Spec:       danmtypes.DanmEpSpec{ApiType: "DanmNet", NetworkName: "vniold"},
		},
		danmtypes.DanmEp{
			ObjectMeta: meta_v1.ObjectMeta{Name: "random5", Namespace: "sdm"},
			Spec:       danmtypes.DanmEpSpec{ApiType: "DanmNet", NetworkName: "vniOld"},
		},
	}
	matchDnet = []danmtypes.DanmEp{
		danmtypes.DanmEp{
			ObjectMeta: meta_v1.ObjectMeta{Name: "random1", Namespace: "vni-test"},
			Spec:       danmtypes.DanmEpSpec{ApiType: "DanmNet", NetworkName: "vniOld", Pod: "blurp"},
		},
	}
	matchCnet = []danmtypes.DanmEp{
		danmtypes.DanmEp{
			ObjectMeta: meta_v1.ObjectMeta{Name: "random1"},
			Spec:       danmtypes.DanmEpSpec{ApiType: "ClusterNetwork", NetworkName: "vniOld", Pod: "blurp"},
		},
	}
	matchDnet2 = []danmtypes.DanmEp{
		danmtypes.DanmEp{
			ObjectMeta: meta_v1.ObjectMeta{Name: "random1", Namespace: "vni-test"},
			Spec:       danmtypes.DanmEpSpec{ApiType: "DanmNet", NetworkName: "vxlanOld", Pod: "blurp"},
		},
	}
	matchCnet2 = []danmtypes.DanmEp{
		danmtypes.DanmEp{
			ObjectMeta: meta_v1.ObjectMeta{Name: "random1"},
			Spec:       danmtypes.DanmEpSpec{ApiType: "ClusterNetwork", NetworkName: "vxlanOld", Pod: "blurp"},
		},
	}
)

func TestValidateNetwork(t *testing.T) {
	validator := admit.Validator{}
	for _, tc := range validateNetworkTcs {
		t.Run(tc.tcName, func(t *testing.T) {
			writerStub := httpstub.NewWriterStub()
			oldNet, _, shouldOldMalform := getNetForValidate(tc.oldNetName, valNets, tc.neType)
			newNet, _, shouldNewMalform := getNetForValidate(tc.newNetName, valNets, tc.neType)
			request, err := utils.CreateHttpRequest(oldNet, newNet, shouldOldMalform, shouldNewMalform, tc.opType)
			if err != nil {
				t.Errorf("Could not create test HTTP Request object, because:%v", err)
				return
			}
			testArtifacts := utils.TestArtifacts{TestNets: valNets, TestEps: tc.eps}
			if tc.tconf != nil {
				testArtifacts.TestTconfs = tc.tconf
			}
			testClient := stubs.NewClientSetStub(testArtifacts)
			validator.Client = testClient
			validator.ValidateNetwork(writerStub, request)
			err = utils.ValidateHttpResponse(writerStub, tc.isErrorExpected, tc.expectedPatches)
			if err != nil {
				t.Errorf("Received HTTP Response did not match expectation, because:%v", err)
				return
			}
			var timesUpdateWasCalled int
			if testClient.DanmClient.TconfClient != nil {
				timesUpdateWasCalled = testClient.DanmClient.TconfClient.TimesUpdateWasCalled
			}
			if tc.timesUpdateShouldBeCalled != timesUpdateWasCalled {
				t.Errorf("TenantConfig should have been updated:" + strconv.Itoa(tc.timesUpdateShouldBeCalled) + " times, but it happened:" + strconv.Itoa(timesUpdateWasCalled) + " times instead")
			}
		})
	}
}

func getNetForValidate(name string, nets []danmtypes.DanmNet, neType string) ([]byte, *danmtypes.DanmNet, bool) {
	dnet := utils.GetTestNet(name, nets)
	if dnet == nil {
		return nil, nil, false
	}
	var shouldItMalform bool
	if strings.HasPrefix(dnet.ObjectMeta.Name, "malform") {
		shouldItMalform = true
	}
	dnet.TypeMeta.Kind = neType
	dnetBinary, _ := json.Marshal(dnet)
	return dnetBinary, dnet, shouldItMalform
}
