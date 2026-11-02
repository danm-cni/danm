package admit_tests

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	danmtypes "github.com/danm-cni/danm/crd/apis/danm/v1"
	"github.com/danm-cni/danm/pkg/admit"
	"github.com/danm-cni/danm/pkg/datastructs"
	stubs "github.com/danm-cni/danm/test/stubs/danm"
	httpstub "github.com/danm-cni/danm/test/stubs/http"
	"github.com/danm-cni/danm/test/utils"
	"github.com/danm-cni/danm/test/utils/webhook"
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
			ObjectMeta: meta_v1.ObjectMeta{Name: "dynamic", Namespace: "rip-test"},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "superDualStack", Type: "TenantNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.70", Shareable: false}, {Address: "2001:db8:85a3::8a2e:370:7350", Shareable: false}},
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
				Labels: map[string]string{datastructs.NetworkNameLabel: "superDualStack", datastructs.NetworkTypeLabel: "TenantNetwork"}},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "superDualStack", Type: "TenantNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.70", Shareable: false}},
			},
		},
		{
			TypeMeta: meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "staleTypeLabel", Namespace: "rip-test",
				Labels: map[string]string{datastructs.NetworkNameLabel: "superDualStack", datastructs.NetworkTypeLabel: "ClusterNetwork"}},
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
		{
			TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "v4AddressTaken", Namespace: "rip-test", Labels: tnetDsLabelSet},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "superDualStack", Type: "TenantNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.68", Shareable: false}},
			},
		},
		{
			TypeMeta: meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "v6AddressTakenInAnotherNotation", Namespace: "rip-test",
				Labels: tnetDsLabelSet},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "superDualStack", Type: "TenantNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "2001:0db8:85a3:0000:0000:8a2e:0370:7340", Shareable: false}},
			},
		},
		{
			TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "cnetAddressTakenInAnotherNs", Namespace: "rip-test", Labels: cnetDsLabelSet},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "superDualStackWithStatus", Type: "ClusterNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.67", Shareable: false}},
			},
		},
		{
			TypeMeta: meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "addressOnlyTakenOnAnotherNetwork", Namespace: "rip-test",
				Labels: tnetDsLabelSet},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "superDualStack", Type: "TenantNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.65", Shareable: false}},
			},
		},
		{
			TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "addressOnlyTakenInAnotherNs", Namespace: "rip-test", Labels: tnetDsLabelSet},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "superDualStack", Type: "TenantNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.66", Shareable: false}},
			},
		},
		{
			TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "selfUpdatedRip", Namespace: "rip-test", Labels: tnetDsLabelSet},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "superDualStack", Type: "TenantNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.69", Shareable: false}},
			},
		},
		{
			TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "sharedV4Rip", Namespace: "rip-test", Labels: tnetDsLabelSet},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "superDualStack", Type: "TenantNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.71", Shareable: true}},
			},
		},
		{
			TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "sharedV6Rip", Namespace: "rip-test", Labels: tnetDsLabelSet},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "superDualStack", Type: "TenantNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "2001:db8:85a3::8a2e:370:7341", Shareable: true}},
			},
		},
		{
			TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "exclusiveRip", Namespace: "rip-test", Labels: tnetDsLabelSet},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "superDualStack", Type: "TenantNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.72", Shareable: false}},
			},
		},
		{
			TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "sharedRipOfAnotherNs", Namespace: "rip-test", Labels: tnetDsLabelSet},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "superDualStack", Type: "TenantNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.73", Shareable: true}},
			},
		},
		{
			TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "unreferencedSharedRip", Namespace: "rip-test", Labels: tnetDsLabelSet},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "superDualStack", Type: "TenantNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.74", Shareable: true}},
			},
		},
		{
			TypeMeta: meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "v4DynamicallyAllocated", Namespace: "rip-test",
				Labels: allocatedDsLabelSet},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "allocatedDualStack", Type: "TenantNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.75", Shareable: false}},
			},
		},
		{
			TypeMeta: meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "v4NotDynamicallyAllocated", Namespace: "rip-test",
				Labels: allocatedDsLabelSet},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "allocatedDualStack", Type: "TenantNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "192.168.1.76", Shareable: false}},
			},
		},
		{
			TypeMeta: meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "v6DynamicallyAllocated", Namespace: "rip-test",
				Labels: allocatedDsLabelSet},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "allocatedDualStack", Type: "TenantNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "2001:db8:85a3::8a2e:370:7345", Shareable: false}},
			},
		},
		{
			TypeMeta: meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "dsPartiallyAllocated", Namespace: "rip-test",
				Labels: allocatedDsLabelSet},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "allocatedDualStack", Type: "TenantNetwork"},
				Ips: []danmtypes.RipIP{
					{Address: "192.168.1.76", Shareable: false},
					{Address: "2001:db8:85a3::8a2e:370:7345", Shareable: false},
				},
			},
		},
		{
			TypeMeta: meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "v6OutsideAllocCidr", Namespace: "rip-test",
				Labels: allocatedDsLabelSet},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "allocatedDualStack", Type: "TenantNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "2001:db8:85a3::8a2e:370:7365", Shareable: false}},
			},
		},
		{
			TypeMeta: meta_v1.TypeMeta{Kind: "ReservedIP"},
			ObjectMeta: meta_v1.ObjectMeta{Name: "v6WithoutAllocCidr", Namespace: "rip-test",
				Labels: noV6AllocLabelSet},
			Spec: danmtypes.ReservedIPSpec{
				Network: danmtypes.RipNetworkSelector{Name: "noV6AllocCidr", Type: "TenantNetwork"},
				Ips:     []danmtypes.RipIP{{Address: "2001:db8:85a3::8a2e:370:7345", Shareable: false}},
			},
		},
	}
)

var existingRips = []danmtypes.ReservedIP{
	{
		TypeMeta: meta_v1.TypeMeta{Kind: "ReservedIP"},
		ObjectMeta: meta_v1.ObjectMeta{Name: "ripOfAnotherNetwork", Namespace: "rip-test",
			Labels: map[string]string{datastructs.NetworkNameLabel: "otherDualStack", datastructs.NetworkTypeLabel: "TenantNetwork"}},
		Spec: danmtypes.ReservedIPSpec{
			Network: danmtypes.RipNetworkSelector{Name: "otherDualStack", Type: "TenantNetwork"},
			Ips:     []danmtypes.RipIP{{Address: "192.168.1.65", Shareable: false}},
		},
	},
	{
		TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
		ObjectMeta: meta_v1.ObjectMeta{Name: "tnetRipOfAnotherNs", Namespace: "another-tenant", Labels: tnetDsLabelSet},
		Spec: danmtypes.ReservedIPSpec{
			Network: danmtypes.RipNetworkSelector{Name: "superDualStack", Type: "TenantNetwork"},
			Ips:     []danmtypes.RipIP{{Address: "192.168.1.66", Shareable: false}},
		},
	},
	{
		TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
		ObjectMeta: meta_v1.ObjectMeta{Name: "cnetRipOfAnotherNs", Namespace: "another-tenant", Labels: cnetDsLabelSet},
		Spec: danmtypes.ReservedIPSpec{
			Network: danmtypes.RipNetworkSelector{Name: "superDualStackWithStatus", Type: "ClusterNetwork"},
			Ips:     []danmtypes.RipIP{{Address: "192.168.1.67", Shareable: false}},
		},
	},
	{
		TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
		ObjectMeta: meta_v1.ObjectMeta{Name: "ripHoldingAV4Address", Namespace: "rip-test", Labels: tnetDsLabelSet},
		Spec: danmtypes.ReservedIPSpec{
			Network: danmtypes.RipNetworkSelector{Name: "superDualStack", Type: "TenantNetwork"},
			Ips:     []danmtypes.RipIP{{Address: "192.168.1.68", Shareable: false}},
		},
	},
	{
		TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
		ObjectMeta: meta_v1.ObjectMeta{Name: "ripHoldingAV6Address", Namespace: "rip-test", Labels: tnetDsLabelSet},
		Spec: danmtypes.ReservedIPSpec{
			Network: danmtypes.RipNetworkSelector{Name: "superDualStack", Type: "TenantNetwork"},
			Ips:     []danmtypes.RipIP{{Address: "2001:db8:85a3::8a2e:370:7340", Shareable: false}},
		},
	},
	{
		TypeMeta:   meta_v1.TypeMeta{Kind: "ReservedIP"},
		ObjectMeta: meta_v1.ObjectMeta{Name: "selfUpdatedRip", Namespace: "rip-test", Labels: tnetDsLabelSet},
		Spec: danmtypes.ReservedIPSpec{
			Network: danmtypes.RipNetworkSelector{Name: "superDualStack", Type: "TenantNetwork"},
			Ips:     []danmtypes.RipIP{{Address: "192.168.1.69", Shareable: false}},
		},
	},
}

var ripReferencingEps = []danmtypes.DanmEp{
	{
		ObjectMeta: meta_v1.ObjectMeta{Name: "epHoldingTheV4Rip", Namespace: "rip-test"},
		Spec:       danmtypes.DanmEpSpec{RipRefs: danmtypes.RipReferences{V4: "sharedV4Rip"}},
	},
	{
		ObjectMeta: meta_v1.ObjectMeta{Name: "epHoldingTheV6Rip", Namespace: "rip-test"},
		Spec:       danmtypes.DanmEpSpec{RipRefs: danmtypes.RipReferences{V6: "sharedV6Rip"}},
	},
	{
		ObjectMeta: meta_v1.ObjectMeta{Name: "epHoldingTheExclusiveRip", Namespace: "rip-test"},
		Spec:       danmtypes.DanmEpSpec{RipRefs: danmtypes.RipReferences{V4: "exclusiveRip"}},
	},
	{
		ObjectMeta: meta_v1.ObjectMeta{Name: "epOfAnotherNamespace", Namespace: "another-tenant"},
		Spec:       danmtypes.DanmEpSpec{RipRefs: danmtypes.RipReferences{V4: "sharedRipOfAnotherNs"}},
	},
}

var epListErrorEps = []danmtypes.DanmEp{
	{
		ObjectMeta: meta_v1.ObjectMeta{Name: "errorEp", Namespace: "rip-test"},
		Spec:       danmtypes.DanmEpSpec{RipRefs: danmtypes.RipReferences{V4: "sharedV4Rip"}},
	},
}

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
					Alloc6: "gAAAAAAAAAAAAAAAAAAAAQ==",
					Cidr:   "192.168.1.64/26", Pool: danmtypes.IpPool{Start: "192.168.1.70", End: "192.168.1.80", LastIp: "192.168.1.72/26"},
					Net6: "2001:db8:85a3::8a2e:370:7334/108", Pool6: danmtypes.IpPoolV6{Cidr: "2001:db8:85a3::8a2e:370:7300/121", IpPool: danmtypes.IpPool{Start: "2001:db8:85a3::8a2e:370:7340", End: "2001:db8:85a3::8a2e:370:7350"}}}},
		},
		{
			ObjectMeta: meta_v1.ObjectMeta{Name: "allocatedDualStack", Namespace: "rip-test"},
			Spec: danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "allocatedDualStack",
				Options: danmtypes.DanmNetOption{
					Alloc:  "gBAAAAAAAAE=",
					Alloc6: "hAAAAQ==",
					Cidr:   "192.168.1.64/26", Pool: danmtypes.IpPool{Start: "192.168.1.70", End: "192.168.1.80", LastIp: "192.168.1.72/26"},
					Net6: "2001:db8:85a3::8a2e:370:7300/120", Pool6: danmtypes.IpPoolV6{Cidr: "2001:db8:85a3::8a2e:370:7340/123", IpPool: danmtypes.IpPool{Start: "2001:db8:85a3::8a2e:370:7340", End: "2001:db8:85a3::8a2e:370:7350"}}}},
		},
		{
			ObjectMeta: meta_v1.ObjectMeta{Name: "noV6AllocCidr", Namespace: "rip-test"},
			Spec: danmtypes.DanmNetSpec{NetworkType: "ipvlan", NetworkID: "noV6AllocCidr",
				Options: danmtypes.DanmNetOption{
					Alloc:  "gBAAAAAAAAE=",
					Alloc6: "hAAAAQ==",
					Cidr:   "192.168.1.64/26", Pool: danmtypes.IpPool{Start: "192.168.1.70", End: "192.168.1.80", LastIp: "192.168.1.72/26"},
					Net6: "2001:db8:85a3::8a2e:370:7300/120"}},
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
					Alloc6: "gAAAAAAAAAAAAAAAAAAAAQ==",
					Cidr:   "192.168.1.64/26", Pool: danmtypes.IpPool{Start: "192.168.1.70", End: "192.168.1.80", LastIp: "192.168.1.72/26"},
					Net6: "2001:db8:85a3::8a2e:370:7334/108", Pool6: danmtypes.IpPoolV6{Cidr: "2001:db8:85a3::8a2e:370:7300/121", IpPool: danmtypes.IpPool{Start: "2001:db8:85a3::8a2e:370:7340", End: "2001:db8:85a3::8a2e:370:7350"}}}},
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
					Alloc6: "gAAAAAAAAAAAAAAAAAAAAQ==",
					Cidr:   "192.168.1.64/26", Pool: danmtypes.IpPool{Start: "192.168.1.70", End: "192.168.1.80", LastIp: "192.168.1.72/26"},
					Net6: "2001:db8:85a3::8a2e:370:7334/108", Pool6: danmtypes.IpPoolV6{Cidr: "2001:db8:85a3::8a2e:370:7300/121", IpPool: danmtypes.IpPool{Start: "2001:db8:85a3::8a2e:370:7340", End: "2001:db8:85a3::8a2e:370:7350"}}}},
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
					Alloc6: "gAAAAAAAAAAAAAAAAAAAAQ==",
					Cidr:   "192.168.1.64/26", Pool: danmtypes.IpPool{Start: "192.168.1.70", End: "192.168.1.80", LastIp: "192.168.1.72/26"},
					Net6: "2001:db8:85a3::8a2e:370:7334/108", Pool6: danmtypes.IpPoolV6{Cidr: "2001:db8:85a3::8a2e:370:7300/121", IpPool: danmtypes.IpPool{Start: "2001:db8:85a3::8a2e:370:7340", End: "2001:db8:85a3::8a2e:370:7350"}}}},
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
	tnetDsLabelSet = map[string]string{
		datastructs.NetworkNameLabel: "superDualStack",
		datastructs.NetworkTypeLabel: "TenantNetwork"}
	cnetDsLabelSet = map[string]string{
		datastructs.NetworkNameLabel: "superDualStackWithStatus",
		datastructs.NetworkTypeLabel: "ClusterNetwork"}
	allocatedDsLabelSet = map[string]string{
		datastructs.NetworkNameLabel: "allocatedDualStack",
		datastructs.NetworkTypeLabel: "TenantNetwork"}
	noV6AllocLabelSet = map[string]string{
		datastructs.NetworkNameLabel: "noV6AllocCidr",
		datastructs.NetworkTypeLabel: "TenantNetwork"}
	tnetDsLabels = []admit.Patch{
		{Op: "add", Path: "/metadata/labels", Value: map[string]string{
			datastructs.NetworkNameLabel: "superDualStack",
			datastructs.NetworkTypeLabel: "TenantNetwork"}},
	}
	cnetDsLabels = []admit.Patch{
		{Op: "add", Path: "/metadata/labels", Value: map[string]string{
			datastructs.NetworkNameLabel: "superDualStackWithStatus",
			datastructs.NetworkTypeLabel: "ClusterNetwork"}},
	}
	cnetDupeLabels = []admit.Patch{
		{Op: "add", Path: "/metadata/labels", Value: map[string]string{
			datastructs.NetworkNameLabel: "superDualStackWithStatusDupe",
			datastructs.NetworkTypeLabel: "ClusterNetwork"}},
	}
	labelsMergedIntoExistingSet = []admit.Patch{
		{Op: "add", Path: "/metadata/labels/" + datastructs.DanmApiPath + "~1network-name", Value: "superDualStackWithStatus"},
		{Op: "add", Path: "/metadata/labels/" + datastructs.DanmApiPath + "~1network-type", Value: "ClusterNetwork"},
	}
	onlyTypeLabelRefreshed = []admit.Patch{
		{Op: "add", Path: "/metadata/labels/" + datastructs.DanmApiPath + "~1network-type", Value: "TenantNetwork"},
	}
)

var validateRipTcs = []struct {
	tcName                 string
	oldRipName             string
	newRipName             string
	tconfName              string
	opType                 v1beta1.Operation
	isErrorExpected        bool
	expectedOldTnetStatus  *danmtypes.DanmNetStatus
	expectedOldCnetStatus  *danmtypes.DanmNetStatus
	expectedTnetStatus     *danmtypes.DanmNetStatus
	expectedCnetStatus     *danmtypes.DanmNetStatus
	expectedPatches        []admit.Patch
	testEps                []danmtypes.DanmEp
	isRipListErrorExpected bool
}{
	{"emptyRequest", "", "", "", v1beta1.Create, true, nil, nil, nil, nil, nil, nil, false},
	{"malformedOldObject", "malformed", "", "", v1beta1.Create, true, nil, nil, nil, nil, nil, nil, false},
	{"malformedNewObject", "", "malformed", "", v1beta1.Create, true, nil, nil, nil, nil, nil, nil, false},
	{"emptyRip", "", "empty-rip", "", v1beta1.Create, true, nil, nil, nil, nil, nil, nil, false},
	{"createNoNetwork", "", "nonet", "bueno", v1beta1.Create, true, nil, nil, nil, nil, nil, nil, false},
	{"cnetWoTconf", "", "cnet", "", v1beta1.Create, true, nil, nil, nil, nil, nil, nil, false},
	{"cnetNotAllowed", "", "cnet", "vlan", v1beta1.Create, true, nil, nil, nil, nil, nil, nil, false},
	{"cnetWrongPrivilege", "", "cnet", "wrongPriv", v1beta1.Create, true, nil, nil, nil, nil, nil, nil, false},
	{"malformedIp", "", "wrongIp", "", v1beta1.Create, true, nil, nil, nil, nil, nil, nil, false},
	{"emptyCidr", "", "v4NoCidr", "", v1beta1.Create, true, nil, nil, nil, nil, nil, nil, false},
	{"v4OutsideCidr", "", "v4OutsideCidr", "", v1beta1.Create, true, nil, nil, nil, nil, nil, nil, false},
	{"emptyNet6", "", "v6NoCidr", "", v1beta1.Create, true, nil, nil, nil, nil, nil, nil, false},
	{"v6OutsideCidr", "", "v6OutsideCidr", "", v1beta1.Create, true, nil, nil, nil, nil, nil, nil, false},
	{"doubleV4", "", "doubleV4", "", v1beta1.Create, true, nil, nil, nil, nil, nil, nil, false},
	{"doubleV6", "", "doubleV6", "", v1beta1.Create, true, nil, nil, nil, nil, nil, nil, false},
	{"reservedName", "", "dynamic", "", v1beta1.Create, true, nil, nil, nil, nil, nil, nil, false},
	{"dsTnetSuccessNewStatus", "", "dsTnetSuccess", "", v1beta1.Create, false, nil, nil, &ripTestStatus, nil, tnetDsLabels, nil, false},
	{"dsCnetSuccessStatusAppend", "", "dsCnetSuccess", "bueno", v1beta1.Create, false, nil, nil, nil, &ripTestStatusCnet, cnetDsLabels, nil, false},
	{"dsCnetSuccessStatusNoAppendDueToDupe", "", "dsCnetSuccessDupe", "bueno", v1beta1.Create, false, nil, nil, nil, &ripTestStatusCnetDupe, cnetDupeLabels, nil, false},
	{"dsCnetUpdateSuccessStatusAppend", "dsCnetSuccessUpdate", "dsCnetSuccess", "bueno", v1beta1.Update, false, nil, nil, nil, &ripTestStatusCnet, cnetDsLabels, nil, false},
	{"dsCnetUpdateSuccessStatusNoAppendDueToDupe", "dsCnetSuccessDupeUpdate", "dsCnetSuccessDupe", "bueno", v1beta1.Update, false, nil, nil, nil, &ripTestStatusCnetDupe, cnetDupeLabels, nil, false},
	{"dryDsCnetSuccessStatusAppend", "", "dsCnetSuccess", "bueno", v1beta1.Create, false, nil, nil, nil, nil, cnetDsLabels, nil, false},
	{"deleteNoNetworkSuccess", "deleteNoNetwork", "", "bueno", v1beta1.Delete, false, nil, nil, nil, nil, nil, nil, false},
	{"dsCnetChangeNetworkRefSuccess", "dsCnetSuccessDupe", "dsCnetSuccessUpdate", "bueno", v1beta1.Update, false, nil, &ripTestStatusCnetOld, nil, &ripUpdateNetRef, cnetDsLabels, nil, false},
	{"deleteLastRipInNs", "deleteLastRipInStatus", "", "", v1beta1.Delete, false, nil, &ripDeleteLastObject, nil, nil, nil, nil, false},
	{"labelsMergedIntoAlreadyLabelledRip", "", "alreadyLabelled", "bueno", v1beta1.Create, false, nil, nil, nil, nil, labelsMergedIntoExistingSet, nil, false},
	{"noPatchWhenLabelsAreAlreadyCorrect", "", "correctlyLabelled", "", v1beta1.Create, false, nil, nil, nil, nil, nil, nil, false},
	{"staleLabelRefreshedOnUpdate", "", "staleTypeLabel", "", v1beta1.Update, false, nil, nil, nil, nil, onlyTypeLabelRefreshed, nil, false},
	{"emptyNetworkTypeDefaultedInLabel", "", "defaultedType", "", v1beta1.Create, false, nil, nil, nil, nil, tnetDsLabels, nil, false},
	{"v4AddressAlreadyReserved", "", "v4AddressTaken", "", v1beta1.Create, true, nil, nil, nil, nil, nil, nil, false},
	{"v6AddressAlreadyReservedInAnotherNotation", "", "v6AddressTakenInAnotherNotation", "", v1beta1.Create, true, nil, nil, nil, nil, nil, nil, false},
	{"cnetAddressAlreadyReservedInAnotherNs", "", "cnetAddressTakenInAnotherNs", "bueno", v1beta1.Create, true, nil, nil, nil, nil, nil, nil, false},
	{"sameAddressReservedOnAnotherNetwork", "", "addressOnlyTakenOnAnotherNetwork", "", v1beta1.Create, false, nil, nil, nil, nil, nil, nil, false},
	{"sameTnetAddressReservedInAnotherNs", "", "addressOnlyTakenInAnotherNs", "", v1beta1.Create, false, nil, nil, nil, nil, nil, nil, false},
	{"ripDoesNotConflictWithItsOwnStoredCopy", "selfUpdatedRip", "selfUpdatedRip", "", v1beta1.Update, false, nil, nil, nil, nil, nil, nil, false},
	{"ripListFailure", "", "addressOnlyTakenOnAnotherNetwork", "", v1beta1.Create, true, nil, nil, nil, nil, nil, nil, true},
	{"deleteSharedRipHeldByAPod", "sharedV4Rip", "", "", v1beta1.Delete, true, nil, nil, nil, nil, nil, ripReferencingEps, false},
	{"updateSharedV6RipHeldByAPod", "sharedV6Rip", "sharedV6Rip", "", v1beta1.Update, true, nil, nil, nil, nil, nil, ripReferencingEps, false},
	{"deleteExclusiveRipHeldByAPod", "exclusiveRip", "", "", v1beta1.Delete, false, nil, nil, nil, nil, nil, ripReferencingEps, false},
	{"deleteSharedRipHeldByAPodOfAnotherNs", "sharedRipOfAnotherNs", "", "", v1beta1.Delete, false, nil, nil, nil, nil, nil, ripReferencingEps, false},
	{"deleteSharedRipWithoutAnyPod", "unreferencedSharedRip", "", "", v1beta1.Delete, false, nil, nil, nil, nil, nil, ripReferencingEps, false},
	{"epListFailure", "sharedV4Rip", "", "", v1beta1.Delete, true, nil, nil, nil, nil, nil, epListErrorEps, false},
	{"v4AddressAlreadyDynamicallyAllocated", "", "v4DynamicallyAllocated", "", v1beta1.Create, true, nil, nil, nil, nil, nil, nil, false},
	{"v4AddressFreeInAllocBitmask", "", "v4NotDynamicallyAllocated", "", v1beta1.Create, false, nil, nil, nil, nil, nil, nil, false},
	{"v6AddressAlreadyDynamicallyAllocated", "", "v6DynamicallyAllocated", "", v1beta1.Create, true, nil, nil, nil, nil, nil, nil, false},
	{"onlyV6AddressDynamicallyAllocatedInDualStackRip", "", "dsPartiallyAllocated", "", v1beta1.Create, true, nil, nil, nil, nil, nil, nil, false},
	{"v6AddressOutsideOfAllocCidrIsNotCheckedInBitmask", "", "v6OutsideAllocCidr", "", v1beta1.Create, false, nil, nil, nil, nil, nil, nil, false},
	{"v6AllocBitmaskIsNotCheckedWithoutAllocCidr", "", "v6WithoutAllocCidr", "", v1beta1.Create, false, nil, nil, nil, nil, nil, nil, false},
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
			testArtifacts := utils.TestArtifacts{TestTnets: tnets, TestCnets: cnets, TestTconfs: testConf,
				TestRips: existingRips, TestEps: tc.testEps, IsRipListErrorExpected: tc.isRipListErrorExpected}
			testClient := stubs.NewClientSetStub(testArtifacts)
			validator.Client = testClient
			validator.ValidateReservedIp(writerStub, request)
			err = webhook.ValidateHttpResponse(writerStub, tc.isErrorExpected, tc.expectedPatches)
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
