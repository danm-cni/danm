package danmep

import (
	"testing"

	danmtypes "github.com/danm-cni/danm/crd/apis/danm/v1"
	"github.com/danm-cni/danm/pkg/datastructs"
	stubs "github.com/danm-cni/danm/test/stubs/danm"
	"github.com/danm-cni/danm/test/utils"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// The DanmEp under deletion is always part of the test set, because DeleteDanmEp only removes it from the API server
// after it decided whether its addresses can be freed. isAddressShared needs to discount this self-match
var sharedIpTestEps = []danmtypes.DanmEp{
	{
		ObjectMeta: meta_v1.ObjectMeta{Name: "epUnderDeletion", Namespace: "rip-test", Labels: datastructs.EpLabels(nil, danmtypes.RipReferences{V4: "sharedRip", V6: "sharedRip6"})},
		Spec:       danmtypes.DanmEpSpec{RipRefs: danmtypes.RipReferences{V4: "sharedRip", V6: "sharedRip6"}},
	},
	{
		ObjectMeta: meta_v1.ObjectMeta{Name: "epHoldingTheSameRips", Namespace: "rip-test", Labels: datastructs.EpLabels(nil, danmtypes.RipReferences{V4: "sharedRip", V6: "sharedRip6"})},
		Spec:       danmtypes.DanmEpSpec{RipRefs: danmtypes.RipReferences{V4: "sharedRip", V6: "sharedRip6"}},
	},
	{
		ObjectMeta: meta_v1.ObjectMeta{Name: "epHoldingAnotherRip", Namespace: "rip-test", Labels: datastructs.EpLabels(nil, danmtypes.RipReferences{V4: "otherRip", V6: "otherRip6"})},
		Spec:       danmtypes.DanmEpSpec{RipRefs: danmtypes.RipReferences{V4: "otherRip", V6: "otherRip6"}},
	},
	{
		ObjectMeta: meta_v1.ObjectMeta{Name: "epWithoutAnyRip", Namespace: "rip-test"},
		Spec:       danmtypes.DanmEpSpec{},
	},
	{
		ObjectMeta: meta_v1.ObjectMeta{Name: "epInAnotherNamespace", Namespace: "another-tenant", Labels: datastructs.EpLabels(nil, danmtypes.RipReferences{V4: "lonelyRip", V6: "lonelyRip6"})},
		Spec:       danmtypes.DanmEpSpec{RipRefs: danmtypes.RipReferences{V4: "lonelyRip", V6: "lonelyRip6"}},
	},
	{
		ObjectMeta: meta_v1.ObjectMeta{Name: "epHoldingTheLonelyRipInAnotherNamespace", Namespace: "yet-another-tenant", Labels: datastructs.EpLabels(nil, danmtypes.RipReferences{V4: "lonelyRip", V6: "lonelyRip6"})},
		Spec:       danmtypes.DanmEpSpec{RipRefs: danmtypes.RipReferences{V4: "lonelyRip", V6: "lonelyRip6"}},
	},
}

// A DanmEp named with the "error" prefix makes the stubbed List fail with a generic, non-API error
// isAddressShared discards every List error, so a failed query currently reads as "not shared", and the address is freed
var listErrorTestEps = []danmtypes.DanmEp{
	{
		ObjectMeta: meta_v1.ObjectMeta{Name: "errorEp", Namespace: "rip-test", Labels: datastructs.EpLabels(nil, danmtypes.RipReferences{V4: "sharedRip"})},
		Spec:       danmtypes.DanmEpSpec{RipRefs: danmtypes.RipReferences{V4: "sharedRip"}},
	},
}

var isAddressSharedTcs = []struct {
	tcName         string
	testEps        []danmtypes.DanmEp
	ripLabel       string
	ns             string
	ripName        string
	isSharedResult bool
}{
	{"onlyTheEpUnderDeletionHoldsTheRip", sharedIpTestEps, datastructs.V4RipLabel, "rip-test", "otherRip", false},
	{"anotherEpHoldsTheSameRip", sharedIpTestEps, datastructs.V4RipLabel, "rip-test", "sharedRip", true},
	{"onlyTheEpUnderDeletionHoldsTheRipV6", sharedIpTestEps, datastructs.V6RipLabel, "rip-test", "otherRip6", false},
	{"anotherEpHoldsTheSameRipV6", sharedIpTestEps, datastructs.V6RipLabel, "rip-test", "sharedRip6", true},
	{"ripIsOnlySharedInAnotherNamespace", sharedIpTestEps, datastructs.V4RipLabel, "another-tenant", "lonelyRip", false},
	{"ripIsOnlySharedInAnotherNamespaceV6", sharedIpTestEps, datastructs.V6RipLabel, "another-tenant", "lonelyRip6", false},
	{"noEpReferencesTheRip", sharedIpTestEps, datastructs.V4RipLabel, "rip-test", "unreferencedRip", false},
	{"clusterWithoutAnyDanmEp", nil, datastructs.V4RipLabel, "rip-test", "sharedRip", false},
	{"ripOfTheOtherIpFamily", sharedIpTestEps, datastructs.V6RipLabel, "rip-test", "sharedRip", false},
	{"listFailsWithANonApiError", listErrorTestEps, datastructs.V4RipLabel, "rip-test", "sharedRip", false},
}

func TestIsAddressShared(t *testing.T) {
	for _, tc := range isAddressSharedTcs {
		t.Run(tc.tcName, func(t *testing.T) {
			testArtifacts := utils.TestArtifacts{TestEps: tc.testEps}
			epClientStub := stubs.NewClientSetStub(testArtifacts)
			isShared := isAddressShared(epClientStub, tc.ripLabel, tc.ns, tc.ripName)
			if isShared != tc.isSharedResult {
				t.Errorf("Sharing verdict:%v does not match with expectation:%v", isShared, tc.isSharedResult)
			}
		})
	}
}
