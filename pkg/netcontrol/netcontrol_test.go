package netcontrol

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	danmtypes "github.com/danm-cni/danm/crd/apis/danm/v1"
	nadtypes "github.com/danm-cni/danm/crd/apis/k8s.cni.cncf.io/v1"
	danmfake "github.com/danm-cni/danm/crd/client/clientset/versioned/fake"
	danminformers "github.com/danm-cni/danm/crd/client/informers/externalversions"
	nadfake "github.com/danm-cni/danm/crd/client/nad/clientset/versioned/fake"
	nadinformers "github.com/danm-cni/danm/crd/client/nad/informers/externalversions"
	"github.com/vishvananda/netlink"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakediscovery "k8s.io/client-go/discovery/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

func TestDiscoverResources(t *testing.T) {
	servedResources := []*meta_v1.APIResourceList{
		{GroupVersion: "danm.io/v1", APIResources: []meta_v1.APIResource{{Name: "danmnets"}, {Name: "tenantnetworks"}}},
	}
	cases := []struct {
		name              string
		groupVersion      string
		discoveryErr      error
		expectedResources map[string]bool
		isErrorExpected   bool
	}{
		{"served group", "danm.io/v1", nil, map[string]bool{"danmnets": true, "tenantnetworks": true}, false},
		{"group not installed", "k8s.cni.cncf.io/v1", nil, map[string]bool{}, false},
		{"API server unreachable", "danm.io/v1", errors.New("connection timed out"), nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fakeDiscovery := &fakediscovery.FakeDiscovery{Fake: &k8stesting.Fake{Resources: servedResources}}
			if tc.discoveryErr != nil {
				fakeDiscovery.AddReactor("get", "resource", func(k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, tc.discoveryErr
				})
			}
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			resources, err := discoverResources(ctx, fakeDiscovery, tc.groupVersion)
			if tc.isErrorExpected {
				if err == nil || !errors.Is(err, tc.discoveryErr) {
					t.Fatalf("discoverResources(%s): expected an error wrapping %v, got:%v", tc.name, tc.discoveryErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("discoverResources(%s): unexpected error:%v", tc.name, err)
			}
			if len(resources) != len(tc.expectedResources) {
				t.Fatalf("discoverResources(%s): actual resources=%v, expected resources=%v", tc.name, resources, tc.expectedResources)
			}
			for resource := range tc.expectedResources {
				if !resources[resource] {
					t.Fatalf("discoverResources(%s): actual resources=%v, expected resources=%v", tc.name, resources, tc.expectedResources)
				}
			}
		})
	}
}

var plainTestNet = danmtypes.DanmNet{ObjectMeta: meta_v1.ObjectMeta{Name: "plain", Namespace: "default"}, Spec: danmtypes.DanmNetSpec{NetworkID: "plain", Options: danmtypes.DanmNetOption{Device: "dev1"}}}

var sharedTestNet = danmtypes.DanmNet{ObjectMeta: meta_v1.ObjectMeta{Name: "shared", Namespace: "default"}, Spec: danmtypes.DanmNetSpec{NetworkID: "shared", Options: danmtypes.DanmNetOption{Device: "dev1"}}}

var sharedTestNad = nadtypes.NetworkAttachmentDefinition{ObjectMeta: meta_v1.ObjectMeta{Name: "sharednad", Namespace: "default"},
	Spec: nadtypes.NetworkAttachmentDefinitionSpec{Config: `{"cniVersion":"0.4.0","name":"shared","type":"macvlan","master":"dev1"}`}}

var invalidTestNad = nadtypes.NetworkAttachmentDefinition{ObjectMeta: meta_v1.ObjectMeta{Name: "invalid", Namespace: "default"}, Spec: nadtypes.NetworkAttachmentDefinitionSpec{Config: "{"}}

var missingParentTestNad = nadtypes.NetworkAttachmentDefinition{ObjectMeta: meta_v1.ObjectMeta{Name: "missingparent", Namespace: "default"},
	Spec: nadtypes.NetworkAttachmentDefinitionSpec{Config: `{"cniVersion":"0.4.0","name":"missingparent","type":"macvlan","master":"nonexistentdev0","vlan":100}`}}

var hostTestLinks = []netlink.Link{
	&netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: "tenant-bond", Index: 2}},
	&netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: "eth1", Index: 3}},
	&netlink.Vlan{LinkAttrs: netlink.LinkAttrs{Name: "vlantest.500", Index: 10, ParentIndex: 2}, VlanId: 500},
	&netlink.Vlan{LinkAttrs: netlink.LinkAttrs{Name: "vlantest.600", Index: 11, ParentIndex: 3}, VlanId: 600},
	&netlink.Vlan{LinkAttrs: netlink.LinkAttrs{Name: "vlantest.700", Index: 12, ParentIndex: 2}, VlanId: 701},
	&netlink.Vlan{LinkAttrs: netlink.LinkAttrs{Name: "other.500", Index: 13, ParentIndex: 2}, VlanId: 500},
	&netlink.Vxlan{LinkAttrs: netlink.LinkAttrs{Name: "vx_vlantest", Index: 14}, VxlanId: 50, VtepDevIndex: 2},
	&netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: "vlantest.800", Index: 15}},
	&netlink.Vlan{LinkAttrs: netlink.LinkAttrs{Name: "renamed", Index: 16, ParentIndex: 2, Alias: managedAliasPrefix + "vlantest"}, VlanId: 900},
	&netlink.Vlan{LinkAttrs: netlink.LinkAttrs{Name: "foreign.300", Index: 17, ParentIndex: 2, Alias: managedAliasPrefix + "foreign"}, VlanId: 300},
	&netlink.Vlan{LinkAttrs: netlink.LinkAttrs{Name: "admin.400", Index: 18, ParentIndex: 2, Alias: "uplink to rack 4"}, VlanId: 400},
}

func newTestWatcher(t *testing.T) *NetWatcher {
	danmClient := danmfake.NewSimpleClientset()
	nadClient := nadfake.NewSimpleClientset()
	netWatcher := &NetWatcher{
		DanmClient:  danmClient,
		DanmFactory: danminformers.NewSharedInformerFactory(danmClient, 0),
		NadClient:   nadClient,
		NadFactory:  nadinformers.NewSharedInformerFactory(nadClient, 0),
		Informers:   make(map[string]cache.SharedIndexInformer),
		queue:       workqueue.NewTypedRateLimitingQueue(workqueue.NewTypedItemExponentialFailureRateLimiter[string](RetryBaseDelay, RetryMaxDelay)),
	}
	t.Cleanup(netWatcher.queue.ShutDown)
	err := netWatcher.registerInformer(DanmNetKind, netWatcher.DanmFactory.Danm().V1().DanmNets().Informer())
	if err != nil {
		t.Fatalf("registering DanmNet Informer failed:%v", err)
	}
	err = netWatcher.registerInformer(NadKind, netWatcher.NadFactory.K8sCniCncfIo().V1().NetworkAttachmentDefinitions().Informer())
	if err != nil {
		t.Fatalf("registering NAD Informer failed:%v", err)
	}
	return netWatcher
}

func linkNames(hostIfaces []hostInterface) []string {
	var names []string
	for _, hostIface := range hostIfaces {
		names = append(names, hostIface.link.Attrs().Name)
	}
	return names
}

func TestFindHostInterfaces(t *testing.T) {
	cases := []struct {
		name          string
		networkID     string
		expectedNames []string
	}{
		{"VLANs and VxLAN of the network are found by name or tag", "vlantest", []string{"vlantest.500", "vlantest.600", "vx_vlantest", "renamed"}},
		{"VLAN tagged with the network is found", "foreign", []string{"foreign.300"}},
		{"VLAN of another network is found", "other", []string{"other.500"}},
		{"nothing is found for unknown network", "unknown", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			names := linkNames(findHostInterfaces(tc.networkID, hostTestLinks))
			if !slices.Equal(names, tc.expectedNames) {
				t.Fatalf("findHostInterfaces(%s): actual interfaces=%v, expected interfaces=%v", tc.name, names, tc.expectedNames)
			}
		})
	}
}

func TestIsHostInterfaceDesired(t *testing.T) {
	hostIfaces := findHostInterfaces("vlantest", hostTestLinks)
	cases := []struct {
		name            string
		ifaceName       string
		desiredOptions  []danmtypes.DanmNetOption
		isDesiredResult bool
	}{
		{"VLAN with same VNI and parent is kept", "vlantest.500", []danmtypes.DanmNetOption{{Device: "tenant-bond", Vlan: 500}}, true},
		{"VLAN with changed VNI is deleted", "vlantest.500", []danmtypes.DanmNetOption{{Device: "tenant-bond", Vlan: 501}}, false},
		{"VLAN with changed parent is deleted", "vlantest.500", []danmtypes.DanmNetOption{{Device: "eth1", Vlan: 500}}, false},
		{"VLAN with missing parent is deleted", "vlantest.500", []danmtypes.DanmNetOption{{Device: "bond9", Vlan: 500}}, false},
		{"VLAN of deleted network is deleted", "vlantest.500", nil, false},
		{"VLAN required by any network is kept", "vlantest.600", []danmtypes.DanmNetOption{{Device: "tenant-bond", Vlan: 500}, {Device: "eth1", Vlan: 600}}, true},
		{"VxLAN with same VNI and parent is kept", "vx_vlantest", []danmtypes.DanmNetOption{{Device: "tenant-bond", Vxlan: 50}}, true},
		{"VxLAN with changed VNI is deleted", "vx_vlantest", []danmtypes.DanmNetOption{{Device: "tenant-bond", Vxlan: 51}}, false},
		{"VxLAN of a network changed to VLAN is deleted", "vx_vlantest", []danmtypes.DanmNetOption{{Device: "tenant-bond", Vlan: 50}}, false},
		{"tagged VLAN with unexpected name is deleted", "renamed", []danmtypes.DanmNetOption{{Device: "tenant-bond", Vlan: 900}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var networks []*danmtypes.DanmNet
			for _, options := range tc.desiredOptions {
				networks = append(networks, &danmtypes.DanmNet{TypeMeta: meta_v1.TypeMeta{Kind: DanmNetKind}, Spec: danmtypes.DanmNetSpec{NetworkID: "vlantest", Options: options}})
			}
			index := slices.IndexFunc(hostIfaces, func(hostIface hostInterface) bool { return hostIface.link.Attrs().Name == tc.ifaceName })
			isDesired := isHostInterfaceDesired(hostIfaces[index], networks, hostTestLinks)
			if isDesired != tc.isDesiredResult {
				t.Fatalf("isHostInterfaceDesired(%s): actual decision=%t, expected decision=%t", tc.name, isDesired, tc.isDesiredResult)
			}
		})
	}
}

func TestPlanHostInterfaces(t *testing.T) {
	cases := []struct {
		name             string
		networkID        string
		desiredOptions   []danmtypes.DanmNetOption
		expectedToTag    []string
		expectedToDelete []string
	}{
		{"required untagged interfaces are tagged, unrequired tagged ones are deleted", "vlantest",
			[]danmtypes.DanmNetOption{{Device: "tenant-bond", Vlan: 500}, {Device: "tenant-bond", Vxlan: 50}}, []string{"vlantest.500", "vx_vlantest"}, []string{"renamed"}},
		{"unrequired untagged interfaces are left alone", "vlantest", nil, nil, []string{"renamed"}},
		{"required tagged interface is left alone", "foreign", []danmtypes.DanmNetOption{{Device: "tenant-bond", Vlan: 300}}, nil, nil},
		{"unrequired tagged interface is deleted", "foreign", []danmtypes.DanmNetOption{{Device: "tenant-bond", Vlan: 301}}, nil, []string{"foreign.300"}},
		{"interface created by the admin with a matching name is left alone", "admin", nil, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var networks []*danmtypes.DanmNet
			for _, options := range tc.desiredOptions {
				networks = append(networks, &danmtypes.DanmNet{TypeMeta: meta_v1.TypeMeta{Kind: DanmNetKind}, Spec: danmtypes.DanmNetSpec{NetworkID: tc.networkID, Options: options}})
			}
			hostIfaces := findHostInterfaces(tc.networkID, hostTestLinks)
			toTag, toDelete := planHostInterfaces(networks, hostIfaces, hostTestLinks)
			if !slices.Equal(linkNames(toTag), tc.expectedToTag) {
				t.Fatalf("planHostInterfaces(%s): actual interfaces to tag=%v, expected interfaces to tag=%v", tc.name, linkNames(toTag), tc.expectedToTag)
			}
			if !slices.Equal(linkNames(toDelete), tc.expectedToDelete) {
				t.Fatalf("planHostInterfaces(%s): actual interfaces to delete=%v, expected interfaces to delete=%v", tc.name, linkNames(toDelete), tc.expectedToDelete)
			}
		})
	}
}

func TestTaggedNetworkIDs(t *testing.T) {
	links := append(hostTestLinks, &netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: "empty", Index: 19, Alias: managedAliasPrefix}})
	networkIDs := taggedNetworkIDs(links)
	expectedIDs := []string{"vlantest", "foreign"}
	if !slices.Equal(networkIDs, expectedIDs) {
		t.Fatalf("taggedNetworkIDs: actual NetworkIDs=%v, expected NetworkIDs=%v", networkIDs, expectedIDs)
	}
}

func TestResolveNadParentDevice(t *testing.T) {
	patchedAnnotations := map[string]string{NadParentAnnotation: "tenant-bond", NadPatchedMasterAnnotation: "vlantest.500"}
	cases := []struct {
		name           string
		device         string
		vlan           int
		annotations    map[string]string
		expectedDevice string
	}{
		{"parent is restored from annotations", "vlantest.500", 500, patchedAnnotations, "tenant-bond"},
		{"parent is restored from annotations when VNI changes", "vlantest.500", 600, patchedAnnotations, "tenant-bond"},
		{"parent is restored from annotations when VNI is removed", "vlantest.500", 0, patchedAnnotations, "tenant-bond"},
		{"parent changed by the user is kept despite annotations", "eth1", 500, patchedAnnotations, "eth1"},
		{"parent of legacy NAD is restored from the host", "vlantest.600", 600, nil, "eth1"},
		{"parent of legacy NAD is restored from the host when VNI changes", "vlantest.600", 700, nil, "eth1"},
		{"interface of another network is kept as parent", "other.500", 500, nil, "other.500"},
		{"parent of legacy NAD is kept when its interface is missing", "vlantest.900", 900, nil, "vlantest.900"},
		{"plain parent is kept", "tenant-bond", 500, nil, "tenant-bond"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dnet := &danmtypes.DanmNet{ObjectMeta: meta_v1.ObjectMeta{Annotations: tc.annotations}, Spec: danmtypes.DanmNetSpec{NetworkID: "vlantest", Options: danmtypes.DanmNetOption{Device: tc.device, Vlan: tc.vlan}}}
			resolveNadParentDevice(dnet, findHostInterfaces("vlantest", hostTestLinks), hostTestLinks)
			if dnet.Spec.Options.Device != tc.expectedDevice {
				t.Fatalf("resolveNadParentDevice(%s): actual device=%s, expected device=%s", tc.name, dnet.Spec.Options.Device, tc.expectedDevice)
			}
		})
	}
}

func TestEnqueue(t *testing.T) {
	noIDNet := plainTestNet
	noIDNet.Spec.NetworkID = ""
	cases := []struct {
		name          string
		kind          string
		obj           interface{}
		isValid       bool
		expectedQueue int
	}{
		{"valid network is queued", DanmNetKind, &plainTestNet, true, 1},
		{"valid NAD is queued", NadKind, &sharedTestNad, true, 1},
		{"deleted network in tombstone is queued", DanmNetKind, cache.DeletedFinalStateUnknown{Key: "default/plain", Obj: &plainTestNet}, true, 1},
		{"network without NetworkID is not queued", DanmNetKind, &noIDNet, true, 0},
		{"invalid NAD is not queued", NadKind, &invalidTestNad, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			netWatcher := newTestWatcher(t)
			isValid := netWatcher.enqueue(tc.kind, tc.obj)
			if isValid != tc.isValid || netWatcher.queue.Len() != tc.expectedQueue {
				t.Fatalf("enqueue(%s): actual validity=%t queue length=%d, expected validity=%t queue length=%d", tc.name, isValid, netWatcher.queue.Len(), tc.isValid, tc.expectedQueue)
			}
		})
	}
}

func TestNetworksWithID(t *testing.T) {
	netWatcher := newTestWatcher(t)
	for kind, obj := range map[string]interface{}{DanmNetKind: &sharedTestNet, NadKind: &sharedTestNad} {
		err := netWatcher.Informers[kind].GetIndexer().Add(obj)
		if err != nil {
			t.Fatalf("adding object to the cache failed:%v", err)
		}
	}
	err := netWatcher.Informers[DanmNetKind].GetIndexer().Add(&plainTestNet)
	if err != nil {
		t.Fatalf("adding object to the cache failed:%v", err)
	}
	networks, err := netWatcher.networksWithID("shared")
	if err != nil {
		t.Fatalf("networksWithID: unexpected error:%v", err)
	}
	var kinds []string
	for _, dnet := range networks {
		kinds = append(kinds, dnet.TypeMeta.Kind)
	}
	slices.Sort(kinds)
	if !slices.Equal(kinds, []string{DanmNetKind, NadKind}) {
		t.Fatalf("networksWithID: actual network kinds=%v, expected network kinds=%v", kinds, []string{DanmNetKind, NadKind})
	}
}

func TestProcessNextWorkItem(t *testing.T) {
	cases := []struct {
		name              string
		kind              string
		obj               interface{}
		networkID         string
		isRetriedExpected bool
	}{
		{"network without host interfaces succeeds", DanmNetKind, &plainTestNet, "plain", false},
		{"NAD with missing parent device is retried", NadKind, &missingParentTestNad, "missingparent", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			netWatcher := newTestWatcher(t)
			err := netWatcher.Informers[tc.kind].GetIndexer().Add(tc.obj)
			if err != nil {
				t.Fatalf("adding object to the cache failed:%v", err)
			}
			netWatcher.queue.Add(tc.networkID)
			netWatcher.processNextWorkItem(context.Background())
			if (netWatcher.queue.NumRequeues(tc.networkID) > 0) != tc.isRetriedExpected {
				t.Fatalf("processNextWorkItem(%s): actual requeues=%d, retry expected=%t", tc.name, netWatcher.queue.NumRequeues(tc.networkID), tc.isRetriedExpected)
			}
		})
	}
}

func TestPatchNadParentDevice(t *testing.T) {
	patchedAnnotations := map[string]string{NadParentAnnotation: "tenant-bond", NadPatchedMasterAnnotation: "vlantest.500"}
	cases := []struct {
		name                string
		config              string
		annotations         map[string]string
		isUpdateExpected    bool
		expectedMaster      string
		expectedAnnotations map[string]string
	}{
		{"new VLAN NAD gets patched", `{"name":"vlantest","type":"ipvlan","master":"tenant-bond","vlan":500}`, nil, true, "vlantest.500", patchedAnnotations},
		{"patched NAD is left alone", `{"name":"vlantest","type":"ipvlan","master":"vlantest.500","vlan":500}`, patchedAnnotations, false, "vlantest.500", patchedAnnotations},
		{"VLAN change is patched with the stored parent", `{"name":"vlantest","type":"ipvlan","master":"vlantest.500","vlan":600}`, patchedAnnotations, true, "vlantest.600",
			map[string]string{NadParentAnnotation: "tenant-bond", NadPatchedMasterAnnotation: "vlantest.600"}},
		{"VLAN removal restores the parent", `{"name":"vlantest","type":"ipvlan","master":"vlantest.500"}`, patchedAnnotations, true, "tenant-bond", map[string]string{}},
		{"legacy patched NAD gets annotated", `{"name":"vlantest","type":"ipvlan","master":"vlantest.500","vlan":500}`, nil, true, "vlantest.500", patchedAnnotations},
		{"NAD without VLAN is left alone", `{"name":"plain","type":"ipvlan","master":"tenant-bond"}`, nil, false, "tenant-bond", map[string]string{}},
		{"VLAN NAD without master is left alone", `{"name":"bridgetest","type":"bridge","bridge":"br0","vlan":100}`, nil, false, "", map[string]string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nad := &nadtypes.NetworkAttachmentDefinition{ObjectMeta: meta_v1.ObjectMeta{Name: "nad", Namespace: "default", Annotations: tc.annotations}, Spec: nadtypes.NetworkAttachmentDefinitionSpec{Config: tc.config}}
			netWatcher := newTestWatcher(t)
			nadClient := netWatcher.NadClient.(*nadfake.Clientset)
			_, err := nadClient.K8sCniCncfIoV1().NetworkAttachmentDefinitions("default").Create(context.Background(), nad, meta_v1.CreateOptions{})
			if err != nil {
				t.Fatalf("creating NAD failed:%v", err)
			}
			err = netWatcher.Informers[NadKind].GetIndexer().Add(nad)
			if err != nil {
				t.Fatalf("adding NAD to the cache failed:%v", err)
			}
			dnet, err := toDanmNet(nad)
			if err != nil {
				t.Fatalf("converting NAD failed:%v", err)
			}
			resolveNadParentDevice(dnet, findHostInterfaces(dnet.Spec.NetworkID, hostTestLinks), hostTestLinks)
			err = netWatcher.patchNadParentDevice(context.Background(), dnet)
			if err != nil {
				t.Fatalf("patchNadParentDevice(%s): unexpected error:%v", tc.name, err)
			}
			isUpdated := false
			for _, action := range nadClient.Actions() {
				isUpdated = isUpdated || action.GetVerb() == "update"
			}
			if isUpdated != tc.isUpdateExpected {
				t.Fatalf("patchNadParentDevice(%s): actual update=%t, expected update=%t", tc.name, isUpdated, tc.isUpdateExpected)
			}
			storedNad, err := nadClient.K8sCniCncfIoV1().NetworkAttachmentDefinitions("default").Get(context.Background(), "nad", meta_v1.GetOptions{})
			if err != nil {
				t.Fatalf("reading back NAD failed:%v", err)
			}
			storedDnet, err := toDanmNet(storedNad)
			if err != nil {
				t.Fatalf("converting stored NAD failed:%v", err)
			}
			if storedDnet.Spec.Options.Device != tc.expectedMaster {
				t.Fatalf("patchNadParentDevice(%s): actual master=%s, expected master=%s", tc.name, storedDnet.Spec.Options.Device, tc.expectedMaster)
			}
			if len(storedNad.ObjectMeta.Annotations) != len(tc.expectedAnnotations) {
				t.Fatalf("patchNadParentDevice(%s): actual annotations=%v, expected annotations=%v", tc.name, storedNad.ObjectMeta.Annotations, tc.expectedAnnotations)
			}
			for key, value := range tc.expectedAnnotations {
				if storedNad.ObjectMeta.Annotations[key] != value {
					t.Fatalf("patchNadParentDevice(%s): actual annotations=%v, expected annotations=%v", tc.name, storedNad.ObjectMeta.Annotations, tc.expectedAnnotations)
				}
			}
		})
	}
}
