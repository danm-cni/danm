package mtu_test

import (
	"strings"
	"testing"

	danmtypes "github.com/danm-cni/danm/crd/apis/danm/v1"
	"github.com/danm-cni/danm/pkg/mtu"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var testNets = []danmtypes.DanmNet{
	{ObjectMeta: meta_v1.ObjectMeta{Name: "v4vxlan"}, Spec: danmtypes.DanmNetSpec{NetworkID: "v4vxlan", Options: danmtypes.DanmNetOption{Cidr: "192.168.1.64/26", Vxlan: 5001, Mtu: 9000}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "v4vlan"}, Spec: danmtypes.DanmNetSpec{NetworkID: "v4vlan", Options: danmtypes.DanmNetOption{Cidr: "192.168.1.64/26", Vlan: 4094, Mtu: 9000}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "net6vxlan"}, Spec: danmtypes.DanmNetSpec{NetworkID: "net6vxlan", Options: danmtypes.DanmNetOption{Net6: "2a00:8a00:a000:1193::/64", Vxlan: 5001, Mtu: 9000}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "net6vlan"}, Spec: danmtypes.DanmNetSpec{NetworkID: "net6vlan", Options: danmtypes.DanmNetOption{Net6: "2a00:8a00:a000:1193::/64", Vlan: 4094, Mtu: 9000}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "dualvxlan"}, Spec: danmtypes.DanmNetSpec{NetworkID: "dualvxlan", Options: danmtypes.DanmNetOption{Net6: "2a00:8a00:a000:1193::/64", Cidr: "192.168.1.64/26", Vxlan: 5001, Mtu: 9000}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "dualvlan"}, Spec: danmtypes.DanmNetSpec{NetworkID: "dualvlan", Options: danmtypes.DanmNetOption{Net6: "2a00:8a00:a000:1193::/64", Cidr: "192.168.1.64/26", Vlan: 4094, Mtu: 9000}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "dualnothing"}, Spec: danmtypes.DanmNetSpec{NetworkID: "dualnothing", Options: danmtypes.DanmNetOption{Net6: "2a00:8a00:a000:1193::/64", Cidr: "192.168.1.64/26", Mtu: 9000}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "noMtu"}, Spec: danmtypes.DanmNetSpec{NetworkID: "noMtu", Options: danmtypes.DanmNetOption{Net6: "2a00:8a00:a000:1193::/64", Cidr: "192.168.1.64/26", Vxlan: 5001}}},
}

func TestValidateForDev(t *testing.T) {
	cases := []struct {
		name    string
		net     *danmtypes.DanmNet
		devMTU  int
		wantErr bool
	}{
		{"unknown device mtu (0) → nil", &testNets[0], 0, false},
		{"unknown device mtu (negative) → nil", &testNets[0], -1, false},
		{"fits exactly V4 VxLAN → nil", &testNets[0], testNets[0].Spec.Options.Mtu + mtu.VxlanOverhead, false},
		{"fits exactly V6 VxLAN → nil", &testNets[2], testNets[2].Spec.Options.Mtu + mtu.VxlanOverheadV6, false},
		{"fits exactly dual stack VxLAN → nil", &testNets[4], testNets[4].Spec.Options.Mtu + mtu.VxlanOverheadV6, false},
		{"fits exactly VLAN → nil", &testNets[5], testNets[5].Spec.Options.Mtu, false},
		{"fits exactly no host management → nil", &testNets[6], testNets[6].Spec.Options.Mtu, false},
		{"exceeds V4 VxLAN → err", &testNets[0], testNets[0].Spec.Options.Mtu + mtu.VxlanOverhead - 1, true},
		{"exceeds V6 VxLAN → err", &testNets[2], testNets[2].Spec.Options.Mtu + mtu.VxlanOverheadV6 - 1, true},
		{"exceeds dual stack VxLAN → err", &testNets[4], testNets[4].Spec.Options.Mtu + mtu.VxlanOverheadV6 - 1, true},
		{"exceeds VLAN → err", &testNets[5], testNets[5].Spec.Options.Mtu - 1, true},
		{"exceeds no host management → err", &testNets[6], testNets[6].Spec.Options.Mtu - 1, true},
		{"defaultMTUVxLAN fits device → nil", &testNets[7], mtu.DefaultMtu + mtu.VxlanOverheadV6, false},
		{"defaultMTUVxLAN exceeds device → err", &testNets[7], mtu.DefaultMtu, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := mtu.ValidateForDev(tc.net, tc.devMTU, "dev0")
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateForDev(%d): err=%v, wantErr=%v", tc.devMTU, err, tc.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "dev0") {
				t.Fatalf("ValidateForDev error should name the device: %v", err)
			}
		})
	}
}

func TestGetMtuForNet(t *testing.T) {
	cases := []struct {
		name        string
		net         *danmtypes.DanmNet
		expectedMtu int
	}{
		{"no MTU", &testNets[7], mtu.DefaultMtu},
		{"set MTU", &testNets[0], testNets[0].Spec.Options.Mtu},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mtu := mtu.GetMtuForNet(tc.net)
			if mtu != tc.expectedMtu {
				t.Fatalf(" Expected MTU=%d, received MTU=%d", tc.expectedMtu, mtu)
			}
		})
	}
}
