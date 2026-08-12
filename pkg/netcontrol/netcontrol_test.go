package netcontrol

import (
	"testing"

	danmtypes "github.com/danm-cni/danm/crd/apis/danm/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var cleanupNeededOldTestNets = []danmtypes.DanmNet{
	{ObjectMeta: meta_v1.ObjectMeta{Name: "vxlan5000dev1"}, Spec: danmtypes.DanmNetSpec{NetworkID: "vxlan5000dev1", Options: danmtypes.DanmNetOption{Cidr: "192.168.1.64/26", Vxlan: 5000, Device: "dev1", Mtu: 1500}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "vlan4000dev1"}, Spec: danmtypes.DanmNetSpec{NetworkID: "vlan4000dev1", Options: danmtypes.DanmNetOption{Cidr: "192.168.1.64/26", Vlan: 4000, Device: "dev1", Mtu: 1500}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "nothingdev1"}, Spec: danmtypes.DanmNetSpec{NetworkID: "nothingdev1", Options: danmtypes.DanmNetOption{Cidr: "192.168.1.64/26", Device: "dev1", Mtu: 1500}}},
}

var cleanupNeededNewTestNets = []danmtypes.DanmNet{
	{ObjectMeta: meta_v1.ObjectMeta{Name: "vxlan5000dev1"}, Spec: danmtypes.DanmNetSpec{NetworkID: "vxlan5000dev1", Options: danmtypes.DanmNetOption{Cidr: "192.168.1.64/26", Vxlan: 5000, Device: "dev1", Mtu: 9000}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "vlan4000dev1"}, Spec: danmtypes.DanmNetSpec{NetworkID: "vlan4000dev1", Options: danmtypes.DanmNetOption{Cidr: "192.168.1.64/26", Vlan: 4000, Device: "dev1", Mtu: 9000}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "vxlan5001dev1"}, Spec: danmtypes.DanmNetSpec{NetworkID: "vxlan5001dev1", Options: danmtypes.DanmNetOption{Cidr: "192.168.1.64/26", Vxlan: 5001, Device: "dev1", Mtu: 1500}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "vlan4001dev1"}, Spec: danmtypes.DanmNetSpec{NetworkID: "vlan4001dev1", Options: danmtypes.DanmNetOption{Cidr: "192.168.1.64/26", Vlan: 4001, Device: "dev1", Mtu: 1500}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "vxlan5000dev2"}, Spec: danmtypes.DanmNetSpec{NetworkID: "vxlan5000dev2", Options: danmtypes.DanmNetOption{Cidr: "192.168.1.64/26", Vxlan: 5000, Device: "dev2", Mtu: 1500}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "vlan4000dev2"}, Spec: danmtypes.DanmNetSpec{NetworkID: "vlan4000dev2", Options: danmtypes.DanmNetOption{Cidr: "192.168.1.64/26", Vlan: 4000, Device: "dev2", Mtu: 1500}}},
	{ObjectMeta: meta_v1.ObjectMeta{Name: "nothingdev2"}, Spec: danmtypes.DanmNetSpec{NetworkID: "nothingdev2", Options: danmtypes.DanmNetOption{Cidr: "192.168.1.64/26", Device: "dev2", Mtu: 1500}}},
}

func TestIsIfaceCleanupNeeded(t *testing.T) {
	cases := []struct {
		name          string
		oldNet        danmtypes.DanmNet
		newNet        danmtypes.DanmNet
		cleanupNeeded bool
	}{
		{"vxlan only mtu change", cleanupNeededOldTestNets[0], cleanupNeededNewTestNets[0], false},
		{"vlan only mtu change", cleanupNeededOldTestNets[1], cleanupNeededNewTestNets[1], false},
		{"vxlan vni change", cleanupNeededOldTestNets[0], cleanupNeededNewTestNets[2], true},
		{"vlan vni change", cleanupNeededOldTestNets[1], cleanupNeededNewTestNets[3], true},
		{"vxlan device change", cleanupNeededOldTestNets[0], cleanupNeededNewTestNets[4], true},
		{"vlan device change", cleanupNeededOldTestNets[1], cleanupNeededNewTestNets[5], true},
		{"No interface management only device change", cleanupNeededOldTestNets[2], cleanupNeededNewTestNets[6], false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cleanupNeeded := isIfaceCleanupNeeded(&tc.oldNet, &tc.newNet)
			if tc.cleanupNeeded != cleanupNeeded {
				t.Fatalf("IsIfaceCleanupNeeded(%s): actual cleanup decision=%t, expected cleanup decision=%t", tc.name, cleanupNeeded, tc.cleanupNeeded)
			}
		})
	}
}
