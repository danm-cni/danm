package admit_tests

import (
	"encoding/json"
	"fmt"
	"slices"
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

var (
	validateRips = []danmtypes.ReservedIP{
		{
			TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "malformed"},
			Spec:       danmtypes.ReservedIPSpec{},
		},
		{
			ObjectMeta: meta_v1.ObjectMeta{Name: "empty-rip"}, TypeMeta: meta_v1.TypeMeta{Kind: "ReservedIP"},
		},
		{
			TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "cnet"},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "default", Type: "ClusterNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "", Shareable: false}},
			},
		},
		{
			TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "nonet"},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "bogus", Type: "ClusterNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.102", Shareable: false}},
			},
		},
		{
			TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "wrongIp"},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "calico", Type: "TenantNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.1a2", Shareable: false}},
			},
		},
		{
			TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "v4NoCidr"},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "calico", Type: "TenantNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.102", Shareable: false}},
			},
		},
		{
			TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "v4OutsideCidr"},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "superDualStack", Type: "TenantNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.63", Shareable: false}},
			},
		},
		{
			TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "v6NoCidr"},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "calico", Type: "TenantNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "2001:db8:85a3::8a2e:370:8000", Shareable: false}},
			},
		},
		{
			TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "v6OutsideCidr"},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "superDualStack", Type: "TenantNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "2001:db8:85a3::8a2e:380:8000", Shareable: false}},
			},
		},
		{
			TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "doubleV4"},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "superDualStack", Type: "TenantNetwork"},
				Ips: []danmtypes.RipIP{
					{Address: "192.168.1.70", Shareable: false},
					{Address: "192.168.1.71", Shareable: false},
				},
			},
		},
		{
			TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "doubleV6"},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "superDualStack", Type: "TenantNetwork"},
				Ips: []danmtypes.RipIP{
					{Address: "2001:db8:85a3::8a2e:370:7350", Shareable: false},
					{Address: "2001:db8:85a3::8a2e:370:7380", Shareable: false},
				},
			},
		},
		{
			TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "dsTnetSuccess", Namespace: "rip-test"},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "superDualStack", Type: "TenantNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.70", Shareable: false}, {Address: "2001:db8:85a3::8a2e:370:7350", Shareable: false}},
			},
		},
		{
			TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "dsCnetSuccess", Namespace: "rip-test"},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "superDualStackWithStatus", Type: "ClusterNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.70", Shareable: false}, {Address: "2001:db8:85a3::8a2e:370:7350", Shareable: false}},
			},
		},
		{
			TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "dsCnetSuccessDupe", Namespace: "rip-test"},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "superDualStackWithStatusDupe", Type: "ClusterNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.70", Shareable: false}, {Address: "2001:db8:85a3::8a2e:370:7350", Shareable: false}},
			},
		},
		{
			TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "dsCnetSuccessUpdate", Namespace: "rip-test"},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "superDualStackWithStatus", Type: "ClusterNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.69", Shareable: true}, {Address: "2001:db8:85a3::8a2e:370:7350", Shareable: false}},
			},
		},
		{
			TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "dsCnetSuccessDupeUpdate", Namespace: "rip-test"},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "superDualStackWithStatusDupe", Type: "ClusterNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.69", Shareable: true}, {Address: "2001:db8:85a3::8a2e:370:7350", Shareable: false}},
			},
		},
		{
			TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "deleteNoNetwork", Namespace: "rip-test"},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "alreadyDeleted", Type: "TenantNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.69", Shareable: true}, {Address: "2001:db8:85a3::8a2e:370:7350", Shareable: false}},
			},
		},
		{
			TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "alreadyLabelled", Namespace: "rip-test", Labels: map[string]string{"unrelated": "label"}},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "superDualStackWithStatus", Type: "ClusterNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.70", Shareable: false}},
			},
		},
		{
			TypeMeta: meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "correctlyLabelled", Namespace: "rip-test",
				Labels: map[string]string{admit.NetworkNameLabel: "superDualStack", admit.NetworkTypeLabel: "TenantNetwork"}},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "superDualStack", Type: "TenantNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.70", Shareable: false}},
			},
		},
		{
			TypeMeta: meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "staleTypeLabel", Namespace: "rip-test",
				Labels: map[string]string{admit.NetworkNameLabel: "superDualStack", admit.NetworkTypeLabel: "ClusterNetwork"}},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "superDualStack", Type: "TenantNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.70", Shareable: false}},
			},
		},
		{
			TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "defaultedType", Namespace: "rip-test"},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "superDualStack"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.70", Shareable: false}},
			},
		},
		{
			TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "deleteLastRipInStatus", Namespace: "rip-test"},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "deleteLastRip", Type: "ClusterNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.69", Shareable: true}, {Address: "2001:db8:85a3::8a2e:370:7350", Shareable: false}},
			},
		},
	}
)

var (
	tconfs = []danmtypes.TenantConfig{
		{
			ObjectMeta: meta_v1.ObjectMeta{Name: "vlan"}, TypeMeta: meta_v1.TypeMeta{Kind: "TenantConfig"},
			HostDevices: []danmtypes.IfaceProfile{
				{Name: "ens4", VniType: "vlan", VniRange: "900-4094"},
			},
		},
		{
			ObjectMeta: meta_v1.ObjectMeta{Name: "wrongPriv"}, TypeMeta: meta_v1.TypeMeta{Kind: "TenantConfig"},
			AllowedCNets: []danmtypes.AllowedCNet{
				{
					Name:       "default",
					Privileges: []string{"notReserved"},
				},
			},
		},
		{
			ObjectMeta: meta_v1.ObjectMeta{Name: "bueno"}, TypeMeta: meta_v1.TypeMeta{Kind: "TenantConfig"},
			AllowedCNets: []danmtypes.AllowedCNet{
				{
					Name:       "superDualStackWithStatus",
					Privileges: []string{"reservedIp"},
				},
				{
					Name:       "superDualStackWithStatusDupe",
					Privileges: []string{"reservedIp"},
				},
			},
		},
	}
	tnets = []danmtypes.TenantNetwork{
		{
			ObjectMeta: meta_v1.ObjectMeta{Name: "calico", Namespace: "rip-test"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "calico"},
		},
		{
			ObjectMeta: meta_v1.ObjectMeta{Name: "superDualStack", Namespace: "rip-test"},
			Spec: danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "superDualStack",
				Options: danmtypes.DanmNetOption{
					Alloc:  "gAAAAAAAAAAAAAAE",
					Alloc6: "gAAAAAAAAAAAAAAE",
					Cidr:   "192.168.1.64/26", Pool: danmtypes.IpPool{Start: "192.168.1.70", End: "192.168.1.80", LastIp: "192.168.1.72/26"},
					Net6: "2001:db8:85a3::8a2e:370:7334/108", Pool6: danmtypes.IpPoolV6{Cidr: "2001:db8:85a3::8a2e:370:7334/109", IpPool: danmtypes.IpPool{Start: "2001:db8:85a3::8a2e:370:7340", End: "2001:db8:85a3::8a2e:370:7350"}}}},
		},
	}
	cnets = []danmtypes.ClusterNetwork{
		{
			ObjectMeta: meta_v1.ObjectMeta{Name: "default"},
			Spec:       danmtypes.DanmNetSpec{NetworkType: "calico"},
		},
		{
			ObjectMeta: meta_v1.ObjectMeta{Name: "superDualStackWithStatus"},
			Spec: danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "superDualStack",
				Options: danmtypes.DanmNetOption{
					Alloc:  "gAAAAAAAAAAAAAAE",
					Alloc6: "gAAAAAAAAAAAAAAE",
					Cidr:   "192.168.1.64/26", Pool: danmtypes.IpPool{Start: "192.168.1.70", End: "192.168.1.80", LastIp: "192.168.1.72/26"},
					Net6: "2001:db8:85a3::8a2e:370:7334/108", Pool6: danmtypes.IpPoolV6{Cidr: "2001:db8:85a3::8a2e:370:7334/109", IpPool: danmtypes.IpPool{Start: "2001:db8:85a3::8a2e:370:7340", End: "2001:db8:85a3::8a2e:370:7350"}}}},
			Status: danmtypes.DanmNetStatus{
				ReservedIPs: []danmtypes.NetRipStatus{
					{Namespace: "rip-test", Objects: []string{"oldRip"}},
					{Namespace: "", Objects: []string{"defaultRip"}},
				},
			},
		},
		{
			ObjectMeta: meta_v1.ObjectMeta{Name: "superDualStackWithStatusDupe"},
			Spec: danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "superDualStack",
				Options: danmtypes.DanmNetOption{
					Alloc:  "gAAAAAAAAAAAAAAE",
					Alloc6: "gAAAAAAAAAAAAAAE",
					Cidr:   "192.168.1.64/26", Pool: danmtypes.IpPool{Start: "192.168.1.70", End: "192.168.1.80", LastIp: "192.168.1.72/26"},
					Net6: "2001:db8:85a3::8a2e:370:7334/108", Pool6: danmtypes.IpPoolV6{Cidr: "2001:db8:85a3::8a2e:370:7334/109", IpPool: danmtypes.IpPool{Start: "2001:db8:85a3::8a2e:370:7340", End: "2001:db8:85a3::8a2e:370:7350"}}}},
			Status: danmtypes.DanmNetStatus{
				ReservedIPs: []danmtypes.NetRipStatus{
					{Namespace: "rip-test", Objects: []string{"oldRip", "dsCnetSuccessDupe"}},
					{Namespace: "", Objects: []string{"defaultRip"}},
				},
			},
		},
		{
			ObjectMeta: meta_v1.ObjectMeta{Name: "deleteLastRip"},
			Spec: danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "deleteLastRip",
				Options: danmtypes.DanmNetOption{
					Alloc:  "gAAAAAAAAAAAAAAE",
					Alloc6: "gAAAAAAAAAAAAAAE",
					Cidr:   "192.168.1.64/26", Pool: danmtypes.IpPool{Start: "192.168.1.70", End: "192.168.1.80", LastIp: "192.168.1.72/26"},
					Net6: "2001:db8:85a3::8a2e:370:7334/108", Pool6: danmtypes.IpPoolV6{Cidr: "2001:db8:85a3::8a2e:370:7334/109", IpPool: danmtypes.IpPool{Start: "2001:db8:85a3::8a2e:370:7340", End: "2001:db8:85a3::8a2e:370:7350"}}}},
			Status: danmtypes.DanmNetStatus{
				ReservedIPs: []danmtypes.NetRipStatus{
					{Namespace: "rip-test", Objects: []string{"deleteLastRipInStatus"}},
					{Namespace: "", Objects: []string{"defaultRip"}},
				},
			},
		},
	}
)

var (
	ripTestStatus = danmtypes.DanmNetStatus{
		ReservedIPs: []danmtypes.NetRipStatus{
			{Namespace: "rip-test", Objects: []string{"dsTnetSuccess"}},
		},
	}
	ripTestStatusCnet = danmtypes.DanmNetStatus{
		ReservedIPs: []danmtypes.NetRipStatus{
			{Namespace: "rip-test", Objects: []string{"oldRip", "dsCnetSuccess"}},
			{Namespace: "", Objects: []string{"defaultRip"}},
		},
	}
	ripTestStatusCnetDupe = danmtypes.DanmNetStatus{
		ReservedIPs: []danmtypes.NetRipStatus{
			{Namespace: "rip-test", Objects: []string{"oldRip", "dsCnetSuccessDupe"}},
			{Namespace: "", Objects: []string{"defaultRip"}},
		},
	}
	ripUpdateNetRef = danmtypes.DanmNetStatus{
		ReservedIPs: []danmtypes.NetRipStatus{
			{Namespace: "rip-test", Objects: []string{"oldRip", "dsCnetSuccessUpdate"}},
			{Namespace: "", Objects: []string{"defaultRip"}},
		},
	}
	ripTestStatusCnetOld = danmtypes.DanmNetStatus{
		ReservedIPs: []danmtypes.NetRipStatus{
			{Namespace: "rip-test", Objects: []string{"oldRip"}},
			{Namespace: "", Objects: []string{"defaultRip"}},
		},
	}
	ripDeleteLastObject = danmtypes.DanmNetStatus{
		ReservedIPs: []danmtypes.NetRipStatus{
			{Namespace: "", Objects: []string{"defaultRip"}},
		},
	}
)

var (
	tnetDsLabels = []admit.Patch{
		{Op: "add", Path: "/metadata/labels", Value: map[string]string{
			admit.NetworkNameLabel: "superDualStack",
			admit.NetworkTypeLabel: "TenantNetwork"}},
	}
	cnetDsLabels = []admit.Patch{
		{Op: "add", Path: "/metadata/labels", Value: map[string]string{
			admit.NetworkNameLabel: "superDualStackWithStatus",
			admit.NetworkTypeLabel: "ClusterNetwork"}},
	}
	cnetDupeLabels = []admit.Patch{
		{Op: "add", Path: "/metadata/labels", Value: map[string]string{
			admit.NetworkNameLabel: "superDualStackWithStatusDupe",
			admit.NetworkTypeLabel: "ClusterNetwork"}},
	}
	labelsMergedIntoExistingSet = []admit.Patch{
		{Op: "add", Path: "/metadata/labels/danm.k8s.io~1network-name", Value: "superDualStackWithStatus"},
		{Op: "add", Path: "/metadata/labels/danm.k8s.io~1network-type", Value: "ClusterNetwork"},
	}
	onlyTypeLabelRefreshed = []admit.Patch{
		{Op: "add", Path: "/metadata/labels/danm.k8s.io~1network-type", Value: "TenantNetwork"},
	}
)

var validateRipTcs = []struct {
	tcName                string
	oldRipName            string
	newRipName            string
	tconfName             string
	opType                v1beta1.Operation
	isErrorExpected       bool
	expectedOldTnetStatus *danmtypes.DanmNetStatus
	expectedOldCnetStatus *danmtypes.DanmNetStatus
	expectedTnetStatus    *danmtypes.DanmNetStatus
	expectedCnetStatus    *danmtypes.DanmNetStatus
	expectedPatches       []admit.Patch
}{
	{"emptyRequest", "", "", "", v1beta1.Create, true, nil, nil, nil, nil, nil},
	{"malformedOldObject", "malformed", "", "", v1beta1.Create, true, nil, nil, nil, nil, nil},
	{"malformedNewObject", "", "malformed", "", v1beta1.Create, true, nil, nil, nil, nil, nil},
	{"emptyRip", "", "empty-rip", "", v1beta1.Create, true, nil, nil, nil, nil, nil},
	{"createNoNetwork", "", "nonet", "bueno", v1beta1.Create, true, nil, nil, nil, nil, nil},
	{"cnetWoTconf", "", "cnet", "", v1beta1.Create, true, nil, nil, nil, nil, nil},
	{"cnetNotAllowed", "", "cnet", "vlan", v1beta1.Create, true, nil, nil, nil, nil, nil},
	{"cnetWrongPrivilege", "", "cnet", "wrongPriv", v1beta1.Create, true, nil, nil, nil, nil, nil},
	{"malformedIp", "", "wrongIp", "", v1beta1.Create, true, nil, nil, nil, nil, nil},
	{"emptyCidr", "", "v4NoCidr", "", v1beta1.Create, true, nil, nil, nil, nil, nil},
	{"v4OutsideCidr", "", "v4OutsideCidr", "", v1beta1.Create, true, nil, nil, nil, nil, nil},
	{"emptyNet6", "", "v6NoCidr", "", v1beta1.Create, true, nil, nil, nil, nil, nil},
	{"v6OutsideCidr", "", "v6OutsideCidr", "", v1beta1.Create, true, nil, nil, nil, nil, nil},
	{"doubleV4", "", "doubleV4", "", v1beta1.Create, true, nil, nil, nil, nil, nil},
	{"doubleV6", "", "doubleV6", "", v1beta1.Create, true, nil, nil, nil, nil, nil},
	{"dsTnetSuccessNewStatus", "", "dsTnetSuccess", "", v1beta1.Create, false, nil, nil, &ripTestStatus, nil, tnetDsLabels},
	{"dsCnetSuccessStatusAppend", "", "dsCnetSuccess", "bueno", v1beta1.Create, false, nil, nil, nil, &ripTestStatusCnet, cnetDsLabels},
	{"dsCnetSuccessStatusNoAppendDueToDupe", "", "dsCnetSuccessDupe", "bueno", v1beta1.Create, false, nil, nil, nil, &ripTestStatusCnetDupe, cnetDupeLabels},
	{"dsCnetUpdateSuccessStatusAppend", "dsCnetSuccessUpdate", "dsCnetSuccess", "bueno", v1beta1.Update, false, nil, nil, nil, &ripTestStatusCnet, cnetDsLabels},
	{"dsCnetUpdateSuccessStatusNoAppendDueToDupe", "dsCnetSuccessDupeUpdate", "dsCnetSuccessDupe", "bueno", v1beta1.Update, false, nil, nil, nil, &ripTestStatusCnetDupe, cnetDupeLabels},
	{"dryDsCnetSuccessStatusAppend", "", "dsCnetSuccess", "bueno", v1beta1.Create, false, nil, nil, nil, nil, cnetDsLabels},
	{"deleteNoNetworkSuccess", "deleteNoNetwork", "", "bueno", v1beta1.Delete, false, nil, nil, nil, nil, nil},
	{"dsCnetChangeNetworkRefSuccess", "dsCnetSuccessDupe", "dsCnetSuccessUpdate", "bueno", v1beta1.Update, false, nil, &ripTestStatusCnetOld, nil, &ripUpdateNetRef, cnetDsLabels},
	{"deleteLastRipInNs", "deleteLastRipInStatus", "", "", v1beta1.Delete, false, nil, &ripDeleteLastObject, nil, nil, nil},
	{"labelsMergedIntoAlreadyLabelledRip", "", "alreadyLabelled", "bueno", v1beta1.Create, false, nil, nil, nil, nil, labelsMergedIntoExistingSet},
	{"noPatchWhenLabelsAreAlreadyCorrect", "", "correctlyLabelled", "", v1beta1.Create, false, nil, nil, nil, nil, nil},
	{"staleLabelRefreshedOnUpdate", "", "staleTypeLabel", "", v1beta1.Update, false, nil, nil, nil, nil, onlyTypeLabelRefreshed},
	{"emptyNetworkTypeDefaultedInLabel", "", "defaultedType", "", v1beta1.Create, false, nil, nil, nil, nil, tnetDsLabels},
}

func TestValidateReservedIp(t *testing.T) {
	validator := admit.Validator{}
	for _, tc := range validateRipTcs {
		t.Run(tc.tcName, func(t *testing.T) {
			writerStub := httpstub.NewWriterStub()
			oldRip, oldRipNetName, shouldOldMalform := getTestRip(tc.oldRipName)
			newRip, newRipNetName, shouldNewMalform := getTestRip(tc.newRipName)
			var isDry bool
			if strings.HasPrefix(tc.tcName, "dry") {
				isDry = true
			}
			request, err := utils.CreateHttpRequest(oldRip, newRip, shouldOldMalform, shouldNewMalform, tc.opType, isDry)
			if err != nil {
				t.Errorf("Could not create test HTTP Request object, because:%v", err)
				return
			}
			var testConf []danmtypes.TenantConfig
			if tc.tconfName != "" {
				for _, tconf := range tconfs {
					if tconf.Name == tc.tconfName {
						testConf = append(testConf, tconf)
					}
				}
			}
			testArtifacts := utils.TestArtifacts{TestTnets: tnets, TestCnets: cnets, TestTconfs: testConf}
			testClient := stubs.NewClientSetStub(testArtifacts)
			validator.Client = testClient
			validator.ValidateReservedIp(writerStub, request)
			err = utils.ValidateHttpResponse(writerStub, tc.isErrorExpected, tc.expectedPatches)
			if err != nil {
				t.Errorf("Received HTTP Response did not match expectation, because:%v", err)
				return
			}
			if tc.expectedTnetStatus != nil {
				if err = testStatus(*tc.expectedTnetStatus, testClient.DanmClient.TnetClient.Statuses[newRipNetName]); err != nil {
					t.Errorf("%v", err)
					return
				}
				if tc.expectedOldTnetStatus == nil && testClient.DanmClient.TnetClient.UpdateStatusCalled > 1 {
					t.Errorf("TNet status was updated more than once even though we did not expect old status change")
					return
				}
			}
			if tc.expectedCnetStatus != nil {
				if err = testStatus(*tc.expectedCnetStatus, testClient.DanmClient.CnetClient.Statuses[newRipNetName]); err != nil {
					t.Errorf("%v", err)
					return
				}
				if tc.expectedOldCnetStatus == nil && testClient.DanmClient.CnetClient.UpdateStatusCalled > 1 {
					t.Errorf("CNet status was updated more than once even though we did not expect old status change")
					return
				}
			}
			if tc.expectedOldTnetStatus != nil {
				if err = testStatus(*tc.expectedOldTnetStatus, testClient.DanmClient.TnetClient.Statuses[oldRipNetName]); err != nil {
					t.Errorf("%v", err)
					return
				}
			}
			if tc.expectedOldCnetStatus != nil {
				if err = testStatus(*tc.expectedOldCnetStatus, testClient.DanmClient.CnetClient.Statuses[oldRipNetName]); err != nil {
					t.Errorf("%v", err)
					return
				}
			}
			if isDry && testClient.DanmClient.CnetClient.UpdateStatusCalled > 0 {
				t.Errorf("UpdateStatus was called in DryRun!")
			}
		})
	}
}

func getTestRip(name string) ([]byte, string, bool) {
	var testRip *danmtypes.ReservedIP
	for _, rip := range validateRips {
		if rip.Name == name {
			testRip = &rip
		}
	}
	if testRip == nil {
		return nil, "", false
	}
	var shouldItMalform bool
	if strings.HasPrefix(testRip.Name, "malform") {
		shouldItMalform = true
	}
	testRipBinary, _ := json.Marshal(testRip)
	return testRipBinary, testRip.Spec.Network.Name, shouldItMalform
}

func testStatus(expStatus, actualStatus danmtypes.DanmNetStatus) error {
	if len(expStatus.ReservedIPs) != len(actualStatus.ReservedIPs) {
		return fmt.Errorf("Received Network Status:%v does not match with expected: %v", actualStatus, expStatus)
	}
	for _, rip := range expStatus.ReservedIPs {
		var inspectedRip *danmtypes.NetRipStatus
		for _, actRip := range actualStatus.ReservedIPs {
			if actRip.Namespace == rip.Namespace {
				inspectedRip = &actRip
				break
			}
		}
		if inspectedRip == nil {
			return fmt.Errorf("Received Network Status:%v does not match with expected: %v", actualStatus, expStatus)
		}
		if slices.Compare(inspectedRip.Objects, rip.Objects) != 0 {
			return fmt.Errorf("Received Network Status:%v does not match with expected: %v", actualStatus, expStatus)
		}
	}
	return nil
}
