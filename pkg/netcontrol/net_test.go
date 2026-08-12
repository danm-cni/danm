package netcontrol

import (
	"testing"

	danmtypes "github.com/danm-cni/danm/crd/apis/danm/v1"
	"github.com/vishvananda/netlink"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var resizeTestNets = []danmtypes.DanmNet{
	{ObjectMeta: meta_v1.ObjectMeta{Name: "smallV4Vlan"}, Spec: danmtypes.DanmNetSpec{NetworkID: "smallV4Vlan", Options: danmtypes.DanmNetOption{Cidr: "192.168.1.64/26", Vlan: 4000, Mtu: 1500}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "bigV4Vxlan"}, Spec: danmtypes.DanmNetSpec{NetworkID: "bigV4Vxlan", Options: danmtypes.DanmNetOption{Cidr: "192.168.1.64/26", Vxlan: 5001, Mtu: 8950}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "hugeV4Vxlan"}, Spec: danmtypes.DanmNetSpec{NetworkID: "hugeV4Vxlan", Options: danmtypes.DanmNetOption{Cidr: "192.168.1.64/26", Vxlan: 5001, Mtu: 8951}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "bigV6Vxlan"}, Spec: danmtypes.DanmNetSpec{NetworkID: "bigV6Vxlan", Options: danmtypes.DanmNetOption{Net6: "2a00:8a00:a000:1193::/64", Vxlan: 5001, Mtu: 8930}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "hugeV6Vxlan"}, Spec: danmtypes.DanmNetSpec{NetworkID: "hugeV6Vxlan", Options: danmtypes.DanmNetOption{Net6: "2a00:8a00:a000:1193::/64", Vxlan: 5001, Mtu: 8931}}},
}

var testLinks = []netlink.Dummy{
	{LinkAttrs: netlink.LinkAttrs{Name: "1500", MTU: 1500}},
	{LinkAttrs: netlink.LinkAttrs{Name: "9000", MTU: 9000}},
}

func TestShouldWeResizeExistingLink(t *testing.T) {
	cases := []struct {
		name        string
		link        netlink.Link
		parent      netlink.Link
		net         *danmtypes.DanmNet
		linkResized bool
	}{
		{"desired == current = parent", &testLinks[0], &testLinks[0], &resizeTestNets[0], false},
		{"desired < current = parent", &testLinks[1], &testLinks[1], &resizeTestNets[0], true},
		{"desired > current, parent can fit it (V4)", &testLinks[0], &testLinks[1], &resizeTestNets[1], true},
		{"desired > current, parent can't fit it (V4)", &testLinks[0], &testLinks[1], &resizeTestNets[2], false},
		{"desired > current, parent can fit it (V6)", &testLinks[0], &testLinks[1], &resizeTestNets[3], true},
		{"desired > current, parent can't fit it (V6)", &testLinks[0], &testLinks[1], &resizeTestNets[4], false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			linkResized := shouldWeResizeExistingLink(tc.link, tc.parent, tc.net)
			if tc.linkResized != linkResized {
				t.Fatalf("ShouldWeResizeExistingLink(%s): actual resize decision=%t, expected resize decision=%t", tc.name, linkResized, tc.linkResized)
			}
		})
	}
}
