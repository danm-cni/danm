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
			ObjectMeta: meta_v1.ObjectMeta{Name: "malformed"},
			Spec:       danmtypes.ReservedIPSpec{},
		},
		{
			ObjectMeta: meta_v1.ObjectMeta{Name: "invalid-type"},
			TypeMeta:   meta_v1.TypeMeta{Kind: "invalid"},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{},
				Ips:     []danmtypes.RipIP{{Address: "", Shareable: false}},
			},
		},
		{
			ObjectMeta: meta_v1.ObjectMeta{Name: "empty-rip"}, TypeMeta: meta_v1.TypeMeta{Kind: "ReservedIP"},
		},
		{
			ObjectMeta: meta_v1.ObjectMeta{Name: "cnet"},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "default", Type: "ClusterNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "", Shareable: false}},
			},
		},
		{
			ObjectMeta: meta_v1.ObjectMeta{Name: "wrongIp"},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "calico", Type: "TenantNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.1a2", Shareable: false}},
			},
		},
		{
			ObjectMeta: meta_v1.ObjectMeta{Name: "v4NoCidr"},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "calico", Type: "TenantNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.102", Shareable: false}},
			},
		},
		{
			ObjectMeta: meta_v1.ObjectMeta{Name: "v4OutsideCidr"},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "superDualStack", Type: "TenantNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.63", Shareable: false}},
			},
		},
		{
			ObjectMeta: meta_v1.ObjectMeta{Name: "v6NoCidr"},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "calico", Type: "TenantNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "2001:db8:85a3::8a2e:370:8000", Shareable: false}},
			},
		},
		{
			ObjectMeta: meta_v1.ObjectMeta{Name: "v6OutsideCidr"},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "superDualStack", Type: "TenantNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "2001:db8:85a3::8a2e:380:8000", Shareable: false}},
			},
		},
		{
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
			ObjectMeta: meta_v1.ObjectMeta{Name: "dsTnetSuccess", Namespace: "rip-test"},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "superDualStack", Type: "TenantNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.70", Shareable: false}, {Address: "2001:db8:85a3::8a2e:370:7350", Shareable: false}},
			},
		},
		{
			ObjectMeta: meta_v1.ObjectMeta{Name: "dsCnetSuccess", Namespace: "rip-test"},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "superDualStackWithStatus", Type: "ClusterNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.70", Shareable: false}, {Address: "2001:db8:85a3::8a2e:370:7350", Shareable: false}},
			},
		},
		{
			ObjectMeta: meta_v1.ObjectMeta{Name: "dsCnetSuccessDupe", Namespace: "rip-test"},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "superDualStackWithStatusDupe", Type: "ClusterNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.70", Shareable: false}, {Address: "2001:db8:85a3::8a2e:370:7350", Shareable: false}},
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
)

var validateRipTcs = []struct {
	tcName             string
	oldRipName         string
	newRipName         string
	tconfName          string
	opType             v1beta1.Operation
	isErrorExpected    bool
	expectedTnetStatus *danmtypes.DanmNetStatus
	expectedCnetStatus *danmtypes.DanmNetStatus
}{
	{"emptyRequest", "", "", "", v1beta1.Create, true, nil, nil},
	{"malformedOldObject", "malformed", "", "", v1beta1.Delete, true, nil, nil},
	{"malformedNewObject", "", "malformed", "", v1beta1.Create, true, nil, nil},
	{"objectWithInvalidType", "", "invalid-type", "", v1beta1.Create, true, nil, nil},
	{"emptyRip", "", "empty-rip", "", v1beta1.Create, true, nil, nil},
	{"cnetWoTconf", "", "cnet", "", v1beta1.Create, true, nil, nil},
	{"cnetNotAllowed", "", "cnet", "vlan", v1beta1.Create, true, nil, nil},
	{"cnetWrongPrivilege", "", "cnet", "wrongPriv", v1beta1.Create, true, nil, nil},
	{"malformedIp", "", "wrongIp", "", v1beta1.Create, true, nil, nil},
	{"emptyCidr", "", "v4NoCidr", "", v1beta1.Create, true, nil, nil},
	{"v4OutsideCidr", "", "v4OutsideCidr", "", v1beta1.Create, true, nil, nil},
	{"emptyNet6", "", "v6NoCidr", "", v1beta1.Create, true, nil, nil},
	{"v6OutsideCidr", "", "v6OutsideCidr", "", v1beta1.Create, true, nil, nil},
	{"doubleV4", "", "doubleV4", "", v1beta1.Create, true, nil, nil},
	{"doubleV6", "", "doubleV6", "", v1beta1.Create, true, nil, nil},
	{"dsTnetSuccessNewStatus", "", "dsTnetSuccess", "", v1beta1.Create, false, &ripTestStatus, nil},
	{"dsCnetSuccessStatusAppend", "", "dsCnetSuccess", "bueno", v1beta1.Create, false, nil, &ripTestStatusCnet},
	{"dsCnetSuccessStatusNoAppendDueToDupe", "", "dsCnetSuccessDupe", "bueno", v1beta1.Create, false, nil, &ripTestStatusCnetDupe},
	{"dryDsCnetSuccessStatusAppend", "", "dsCnetSuccess", "bueno", v1beta1.Create, false, nil, nil},
}

func TestValidateReservedIp(t *testing.T) {
	validator := admit.Validator{}
	for _, tc := range validateRipTcs {
		t.Run(tc.tcName, func(t *testing.T) {
			writerStub := httpstub.NewWriterStub()
			oldRip, shouldOldMalform := getTestRip(tc.oldRipName)
			newRip, shouldNewMalform := getTestRip(tc.newRipName)
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
			err = utils.ValidateHttpResponse(writerStub, tc.isErrorExpected, nil)
			if err != nil {
				t.Errorf("Received HTTP Response did not match expectation, because:%v", err)
				return
			}
			if tc.expectedTnetStatus != nil {
				if err = testStatus(*tc.expectedTnetStatus, testClient.DanmClient.TnetClient.Status); err != nil {
					t.Errorf("%v", err)
					return
				}
			}
			if tc.expectedCnetStatus != nil {
				if err = testStatus(*tc.expectedCnetStatus, testClient.DanmClient.CnetClient.Status); err != nil {
					t.Errorf("%v", err)
					return
				}
			}
			if isDry && testClient.DanmClient.CnetClient.UpdateStatusCalled != false {
				t.Errorf("UpdateStatus was called in DryRun!")
			}
		})
	}
}

func getTestRip(name string) ([]byte, bool) {
	var testRip *danmtypes.ReservedIP
	for _, rip := range validateRips {
		if rip.Name == name {
			testRip = &rip
		}
	}
	if testRip == nil {
		return nil, false
	}
	var shouldItMalform bool
	if strings.HasPrefix(testRip.Name, "malform") {
		shouldItMalform = true
	}
	testRipBinary, _ := json.Marshal(testRip)
	return testRipBinary, shouldItMalform
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
