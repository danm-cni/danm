package svccontrol

import (
	"context"
	"testing"
	"time"

	danmv1 "github.com/danm-cni/danm/crd/apis/danm/v1"
	danmfake "github.com/danm-cni/danm/crd/client/clientset/versioned/fake"
	danminformers "github.com/danm-cni/danm/crd/client/informers/externalversions"
	"github.com/danm-cni/danm/pkg/datastructs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	coreinformers "k8s.io/client-go/informers"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
)

func TestPodFilterFunc(t *testing.T) {
	relevantPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "should", Annotations: map[string]string{danmIfDefinitionSyntax: "someif"},
	}}
	irrelevantPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "shouldnot", Annotations: map[string]string{"other.anno": "abc"},
	}}

	assert.True(t, isPodRelevant(relevantPod))
	assert.False(t, isPodRelevant(irrelevantPod))
	assert.False(t, isPodRelevant(&corev1.Service{}))
}

func TestSvcFilterFunc(t *testing.T) {
	relevantSvc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{
		Name: "should", Annotations: map[string]string{PodSelector: "{\"app\": \"x\"}"},
	}}
	irrelevantSvc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{
		Name: "shouldnot", Annotations: map[string]string{"other.anno": "abc"},
	}}

	assert.True(t, isSvcRelevant(relevantSvc))
	assert.False(t, isSvcRelevant(irrelevantSvc))
	assert.False(t, isSvcRelevant(&corev1.Pod{}))
}

func TestDanmEpFilterFunc(t *testing.T) {
	relevantDep := &danmv1.DanmEp{
		ObjectMeta: metav1.ObjectMeta{Name: "should"},
		Spec:       danmv1.DanmEpSpec{NetworkName: "csipate"},
	}
	irrelevantDep := &danmv1.DanmEp{
		ObjectMeta: metav1.ObjectMeta{Name: "shouldnot"},
		Spec:       danmv1.DanmEpSpec{NetworkName: "default"},
	}

	assert.True(t, isDanmEpRelevant(relevantDep))
	assert.False(t, isDanmEpRelevant(irrelevantDep))
	assert.False(t, isDanmEpRelevant(&corev1.Pod{}))
}

// newTestController builds a leading Controller on top of fake API clients
// Its informers are never started: tests populate the caches via addObjects, and invoke the sync functions themselves
func newTestController(t *testing.T) (*Controller, *k8sfake.Clientset, *danmfake.Clientset) {
	t.Helper()
	kubeClient := k8sfake.NewSimpleClientset()
	kubeInformerFactory := coreinformers.NewSharedInformerFactory(kubeClient, 0)
	danmClient := danmfake.NewSimpleClientset()
	danmInformerFactory := danminformers.NewSharedInformerFactory(danmClient, 0)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ctrl, err := NewController(ctx.Done(), kubeClient, danmClient,
		kubeInformerFactory.Core().V1().Pods(),
		kubeInformerFactory.Core().V1().Services(),
		kubeInformerFactory.Core().V1().Endpoints(),
		danmInformerFactory.Danm().V1().DanmEps())
	require.NoErrorf(t, err, "NewController returned error: %v", err)
	require.NotNilf(t, ctrl, "NewController returned nil")
	ctrl.isLeader.Store(true)
	return ctrl, kubeClient, danmClient
}

// addObjects creates the objects in the fake API servers, then adds them to the matching informer cache, as a synced informer would
//
//nolint:staticcheck // Endpoints is deprecated but still supported, refactor it at some point though
func addObjects(t *testing.T, ctrl *Controller, objs ...metav1.Object) {
	t.Helper()
	for _, obj := range objs {
		var err error
		var store cache.Store
		switch o := obj.(type) {
		case *corev1.Pod:
			_, err = ctrl.kubeclient.CoreV1().Pods(o.Namespace).Create(context.TODO(), o, metav1.CreateOptions{})
			store = ctrl.podInformer.GetStore()
		case *corev1.Service:
			_, err = ctrl.kubeclient.CoreV1().Services(o.Namespace).Create(context.TODO(), o, metav1.CreateOptions{})
			store = ctrl.serviceInformer.GetStore()
		case *corev1.Endpoints:
			_, err = ctrl.kubeclient.CoreV1().Endpoints(o.Namespace).Create(context.TODO(), o, metav1.CreateOptions{})
			store = ctrl.epsInformer.GetStore()
		case *danmv1.DanmEp:
			_, err = ctrl.danmclient.DanmV1().DanmEps(o.Namespace).Create(context.TODO(), o, metav1.CreateOptions{})
			store = ctrl.danmepInformer.GetStore()
		default:
			t.Fatalf("addObjects does not support objects of type %T", obj)
		}
		require.NoErrorf(t, err, "failed creating %T %s/%s in the fake API: %v", obj, obj.GetNamespace(), obj.GetName(), err)
		err = store.Add(obj)
		require.NoErrorf(t, err, "failed adding %T %s/%s to the informer cache: %v", obj, obj.GetNamespace(), obj.GetName(), err)
	}
}

func TestDanmEpIndexer(t *testing.T) {
	ctrl, _, _ := newTestController(t)

	dep1 := &danmv1.DanmEp{ObjectMeta: metav1.ObjectMeta{
		Name:      "dep1",
		Namespace: "default",
	},
		Spec: danmv1.DanmEpSpec{
			NetworkName: "ctrl",
			ApiType:     "ClusterNetwork",
			Pod:         "pod1",
		}}

	dep2 := &danmv1.DanmEp{ObjectMeta: metav1.ObjectMeta{
		Name:      "dep2",
		Namespace: "default",
	},
		Spec: danmv1.DanmEpSpec{
			NetworkName: "default",
			ApiType:     "ClusterNetwork",
			Pod:         "pod2",
		}}

	dep3 := &danmv1.DanmEp{ObjectMeta: metav1.ObjectMeta{
		Name:      "dep3",
		Namespace: "other",
	},
		Spec: danmv1.DanmEpSpec{
			NetworkName: "ctrl",
			ApiType:     "ClusterNetwork",
			Pod:         "pod3",
		}}

	addObjects(t, ctrl, dep1, dep2, dep3)

	// pod2 should not be in any index
	indexed, err := ctrl.danmepInformer.GetIndexer().ByIndex(ctrl.idx.Pod, "default/pod2")
	assert.Equal(t, 0, len(indexed))
	assert.NoErrorf(t, err, "error: %v", err)
	indexed, err = ctrl.danmepInformer.GetIndexer().ByIndex(ctrl.idx.Network, "default/ClusterNetwork/default")
	assert.Equal(t, 0, len(indexed))
	assert.NoErrorf(t, err, "error: %v", err)
	foundEps := false
	for _, o := range indexed {
		e := o.(*danmv1.DanmEp)
		if e.Spec.Pod == "pod2" {
			foundEps = true
			break
		}
	}
	assert.False(t, foundEps)

	// pod1 should be present in both indexes
	indexed, err = ctrl.danmepInformer.GetIndexer().ByIndex(ctrl.idx.Pod, "default/pod1")
	assert.Equal(t, 1, len(indexed))
	assert.NoErrorf(t, err, "error: %v", err)
	indexed, err = ctrl.danmepInformer.GetIndexer().ByIndex(ctrl.idx.Network, "default/ClusterNetwork/ctrl")
	assert.Equal(t, 1, len(indexed))
	assert.NoErrorf(t, err, "error: %v", err)
	foundEps = false
	for _, o := range indexed {
		e := o.(*danmv1.DanmEp)
		if e.Spec.Pod == "pod1" {
			foundEps = true
			break
		}
	}
	assert.True(t, foundEps)

	// index network should contain pod1 in default and pod3 in other
	indexed, err = ctrl.danmepInformer.GetIndexer().ByIndex(ctrl.idx.Network, "default/ClusterNetwork/ctrl")
	assert.Equal(t, 1, len(indexed))
	assert.NoErrorf(t, err, "error: %v", err)
	foundEps = false
	for _, o := range indexed {
		e := o.(*danmv1.DanmEp)
		if e.Spec.Pod == "pod1" {
			foundEps = true
			break
		}
	}
	assert.True(t, foundEps)
	indexed, err = ctrl.danmepInformer.GetIndexer().ByIndex(ctrl.idx.Network, "other/ClusterNetwork/ctrl")
	assert.Equal(t, 1, len(indexed))
	assert.NoErrorf(t, err, "error: %v", err)
	foundEps = false
	for _, o := range indexed {
		e := o.(*danmv1.DanmEp)
		if e.Spec.Pod == "pod3" {
			foundEps = true
			break
		}
	}
	assert.True(t, foundEps)

	// index pod contains pod3 for key other/pod3
	indexed, err = ctrl.danmepInformer.GetIndexer().ByIndex(ctrl.idx.Pod, "other/pod3")
	assert.Equal(t, 1, len(indexed))
	assert.NoErrorf(t, err, "error: %v", err)
}

func TestSyncPod_UpdatesDanmEpLabels(t *testing.T) {
	ctrl, _, danmClient := newTestController(t)

	ns := "default"
	podName := "mypod"
	podUID := types.UID("pod-uid-123")
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            podName,
			Namespace:       ns,
			UID:             podUID,
			ResourceVersion: "2",
			Labels:          map[string]string{"app": "x"},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
		Spec: corev1.PodSpec{
			Hostname: "host1",
			NodeName: "node1",
		},
	}

	// DanmEp that references the pod but has different labels (so syncPod should update it)
	depName := "dep1"
	dep := &danmv1.DanmEp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      depName,
			Namespace: ns,
			Labels:    map[string]string{"app": "old"},
		},
		Spec: danmv1.DanmEpSpec{
			Pod:         podName,
			PodUID:      podUID,
			ApiType:     "V4",
			NetworkName: "net1",
			Iface: danmv1.DanmEpIface{
				Address: "10.1.1.5/24",
			},
		},
	}

	addObjects(t, ctrl, pod, dep)

	err := ctrl.syncPod(ns, podName)
	require.NoErrorf(t, err, "syncPod returned error: %v", err)
	err = ctrl.syncPod(ns, podName)
	require.NoErrorf(t, err, "syncPod returned error: %v", err)

	// Validate that danmClient recorded an update action for danmeps
	actions := danmClient.Actions()
	assert.Truef(t, hasAction(actions, "update", "danmeps"), "expected danmclient Update action on DanmEp, got actions: %#v", actions)

	newDep, err := danmClient.DanmV1().DanmEps(ns).Get(context.TODO(), depName, metav1.GetOptions{})
	require.NoError(t, err, "DanmEp object could not be GET")
	assert.Equal(t, pod.Labels, newDep.Labels)
}

// ReservedIP labels of a DanmEp are derived from its Spec, so syncing Pod labels must neither drop them, nor let the Pod spoof them
func TestSyncPod_KeepsDanmEpRipLabels(t *testing.T) {
	ctrl, _, danmClient := newTestController(t)

	ns := "default"
	podName := "mypod"
	podUID := types.UID("pod-uid-123")
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            podName,
			Namespace:       ns,
			UID:             podUID,
			ResourceVersion: "2",
			Labels:          map[string]string{"app": "x", datastructs.V6RipLabel: "spoofedRip"},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
		Spec: corev1.PodSpec{
			Hostname: "host1",
			NodeName: "node1",
		},
	}

	// DanmEp that references the pod but has different labels (so syncPod should update it)
	depName := "dep1"
	dep := &danmv1.DanmEp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      depName,
			Namespace: ns,
			Labels:    map[string]string{"app": "old", datastructs.V4RipLabel: "sharedRip"},
		},
		Spec: danmv1.DanmEpSpec{
			Pod:         podName,
			PodUID:      podUID,
			ApiType:     "V4",
			NetworkName: "net1",
			RipRefs:     danmv1.RipReferences{V4: "sharedRip"},
			Iface: danmv1.DanmEpIface{
				Address: "10.1.1.5/24",
			},
		},
	}

	addObjects(t, ctrl, pod, dep)
	err := ctrl.syncPod(ns, podName)
	require.NoErrorf(t, err, "syncPod returned error: %v", err)
	err = ctrl.syncPod(ns, podName)
	require.NoErrorf(t, err, "syncPod returned error: %v", err)

	// Validate that danmClient recorded an update action for danmeps
	actions := danmClient.Actions()
	assert.Truef(t, hasAction(actions, "update", "danmeps"), "expected danmclient Update action on DanmEp, got actions: %#v", actions)

	// Check result in kubernetes
	newDep, err := danmClient.DanmV1().DanmEps(ns).Get(context.TODO(), depName, metav1.GetOptions{})
	require.NoError(t, err, "DanmEp object could not be GET")
	assert.Equal(t, map[string]string{"app": "x", datastructs.V4RipLabel: "sharedRip"}, newDep.Labels)
}

//nolint:staticcheck // Endpoints and EndpointSubset are deprecated but still supported, refactor it at some point though
func TestSyncPod_UpdatesEps(t *testing.T) {
	ctrl, kubeClient, _ := newTestController(t)

	ns := "default"
	podName := "mypod"
	podUID := types.UID("pod-uid-123")
	podName2 := "mypod2"
	podUID2 := types.UID("pod2-uid-123")
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            podName,
			Namespace:       ns,
			UID:             podUID,
			ResourceVersion: "2",
			Labels:          map[string]string{"app": "x"},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
		Spec: corev1.PodSpec{
			Hostname: "host1",
			NodeName: "node1",
		},
	}

	svcName := "mysvc"
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            svcName,
			Namespace:       ns,
			ResourceVersion: "2",
		},
	}

	eps := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:            svcName,
			Namespace:       ns,
			ResourceVersion: "2",
			Annotations:     map[string]string{"danm.io/clusterNetwork": "ctrl", "danm.io/selector": "{\"app\": \"x\"}"},
		},
		Subsets: []corev1.EndpointSubset{
			corev1.EndpointSubset{
				NotReadyAddresses: []corev1.EndpointAddress{
					corev1.EndpointAddress{
						IP: "10.1.1.5",
						TargetRef: &corev1.ObjectReference{
							Namespace:       ns,
							Name:            podName,
							Kind:            "Pod",
							UID:             podUID,
							ResourceVersion: "2",
						},
					},
				},
				Addresses: []corev1.EndpointAddress{
					corev1.EndpointAddress{
						IP: "10.1.1.15",
						TargetRef: &corev1.ObjectReference{
							Namespace:       ns,
							Name:            podName2,
							Kind:            "Pod",
							UID:             podUID2,
							ResourceVersion: "2",
						},
					},
				},
			},
		},
	}

	// DanmEp in sync with pod
	depName := "dep1"
	dep := &danmv1.DanmEp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      depName,
			Namespace: ns,
			Labels:    map[string]string{"app": "x"},
		},
		Spec: danmv1.DanmEpSpec{
			Pod:         podName,
			PodUID:      podUID,
			ApiType:     "ClusterNetwork",
			NetworkName: "ctrl",
			Iface: danmv1.DanmEpIface{
				Address: "10.1.1.5/24",
			},
		},
	}

	addObjects(t, ctrl, pod, svc, eps, dep)
	err := ctrl.syncPod(ns, podName)
	require.NoErrorf(t, err, "syncPod returned error: %v", err)

	// Validate that kubeClient recorded an update action for endpoints
	actions := kubeClient.Actions()
	assert.Truef(t, hasAction(actions, "update", "endpoints"), "expected kubeclient Update action on Endpoints, got actions: %#v", actions)

	newEps, err := kubeClient.CoreV1().Endpoints(ns).Get(context.TODO(), svcName, metav1.GetOptions{})
	require.NoErrorf(t, err, "Eps object could not be GET: %v", err)
	foundIP := false
	for _, a := range newEps.Subsets[0].Addresses {
		if a.IP == "10.1.1.5" {
			foundIP = true
			break
		}
	}
	assert.Truef(t, foundIP, "Addresses does not contain the requested IP address")
}

// Test that reconcileAllEndpoints enqueues all services
func TestReconcileAllEndpoints_EnqueuesOnlyRelevantServices(t *testing.T) {
	ctrl, _, _ := newTestController(t)

	svc1 := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "svc1",
			Namespace:   "ns1",
			Annotations: map[string]string{PodSelector: `{"app":"test"}`},
		},
	}
	svc2 := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "svc2",
			Namespace:   "ns2",
			Annotations: map[string]string{PodSelector: `{"app":"test"}`},
		},
	}
	vanillaSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vanilla-svc",
			Namespace: "ns1",
		},
	}

	addObjects(t, ctrl, svc1, svc2, vanillaSvc)

	for ctrl.workqueue.Len() > 0 {
		obj, _ := ctrl.workqueue.Get()
		ctrl.workqueue.Done(obj)
	}

	ctrl.reconcileAllEndpoints()

	// both DANM Services are enqueued, the vanilla one is not
	expected := 2
	require.Equal(t, expected, ctrl.workqueue.Len())
	var enqueued []string
	for ctrl.workqueue.Len() > 0 {
		obj, _ := ctrl.workqueue.Get()
		ctrl.workqueue.Done(obj)
		key, ok := obj.(string)
		require.Truef(t, ok, "expected a string in the workqueue, but got %#v", obj)
		enqueued = append(enqueued, key)
	}
	assert.ElementsMatch(t, []string{"svc/ns1/svc1", "svc/ns2/svc2"}, enqueued)
}

func countActions(actions []k8stesting.Action, verb, resource string) int {
	count := 0
	for _, a := range actions {
		if a.GetVerb() == verb && a.GetResource().Resource == resource {
			count++
		}
	}
	return count
}

func hasAction(actions []k8stesting.Action, verb, resource string) bool {
	return countActions(actions, verb, resource) > 0
}

//nolint:staticcheck // Endpoints is deprecated but still supported, refactor it at some point though
func epIPs(addrs []corev1.EndpointAddress) []string {
	ips := make([]string, 0, len(addrs))
	for _, a := range addrs {
		ips = append(ips, a.IP)
	}
	return ips
}

func TestSyncPod_FromPodCreationTillRemoval(t *testing.T) {
	// the DanmEp of the Pod is dual-stack, so every transition below has to carry both families
	const v4Addr = "10.1.1.5"
	const v6Addr = "2001:db8::5"

	ctrl, kubeClient, _ := newTestController(t)

	ns := "default"
	podName := "mypod"
	podUID := types.UID("pod-uid-123")
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            podName,
			Namespace:       ns,
			UID:             podUID,
			ResourceVersion: "2",
			Labels:          map[string]string{"app": "x"},
			Annotations:     map[string]string{"danm.io/interfaces": "someif"},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionFalse},
			},
		},
		Spec: corev1.PodSpec{
			Hostname: "host1",
			NodeName: "node1",
		},
	}

	svcName := "mysvc"
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            svcName,
			Namespace:       ns,
			ResourceVersion: "2",
			Annotations:     map[string]string{"danm.io/clusterNetwork": "ctrl", "danm.io/selector": "{\"app\": \"x\"}"},
		},
	}

	// DanmEp in sync with pod
	depName := "dep1"
	dep := &danmv1.DanmEp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      depName,
			Namespace: ns,
			Labels:    map[string]string{"app": "x"},
		},
		Spec: danmv1.DanmEpSpec{
			Pod:         podName,
			PodUID:      podUID,
			ApiType:     "ClusterNetwork",
			NetworkName: "ctrl",
			Iface: danmv1.DanmEpIface{
				Address:     v4Addr + "/24",
				AddressIPv6: v6Addr + "/64",
			},
		},
	}

	podStore := ctrl.podInformer.GetStore()
	epsStore := ctrl.epsInformer.GetStore()
	danmepStore := ctrl.danmepInformer.GetStore()
	addObjects(t, ctrl, pod, dep)

	err := ctrl.syncDanmEp(ns, depName)
	require.NoErrorf(t, err, "syncDanmEp returned error: %v", err)

	// as there is no related svc in kubernetes: no eps created/updated
	epsList, err := kubeClient.CoreV1().Endpoints(ns).List(context.TODO(), metav1.ListOptions{})
	require.NoErrorf(t, err, "failed listing eps: %v", err)
	assert.Equal(t, 0, len(epsList.Items))

	// ============================
	// many times pod update is happening: trigger pod reconcile
	err = ctrl.syncPod(ns, podName)
	require.NoErrorf(t, err, "syncPod returned error: %v", err)

	// as there is no related svc in kubernetes: no eps created/updated
	epsList, err = kubeClient.CoreV1().Endpoints(ns).List(context.TODO(), metav1.ListOptions{})
	require.NoErrorf(t, err, "failed listing eps: %v", err)
	assert.Equal(t, 0, len(epsList.Items))

	// ============================
	// add svc to kubernetes and to the informer cache
	addObjects(t, ctrl, svc)

	// trigger svc reconcile
	err = ctrl.syncSvc(ns, svcName)
	require.NoErrorf(t, err, "syncSvc returned error: %v", err)

	// eps should be created, carrying both addresses of the not yet ready Pod
	epsList, err = kubeClient.CoreV1().Endpoints(ns).List(context.TODO(), metav1.ListOptions{})
	require.NoErrorf(t, err, "failed listing eps: %v", err)
	require.Equal(t, 1, len(epsList.Items))
	require.Empty(t, epsList.Items[0].Subsets[0].Addresses)
	require.ElementsMatch(t, []string{v4Addr, v6Addr}, epIPs(epsList.Items[0].Subsets[0].NotReadyAddresses))

	// add epsList to to informer
	eps := epsList.Items[0].DeepCopy()
	err = epsStore.Update(eps)
	require.NoErrorf(t, err, "failed to add eps to store: %v", err)

	// ============================
	// the DanmEp is reconciled again while the Endpoints already holds both of its addresses:
	// epCheckUpdate has to recognise them in NotReadyAddresses and leave the Endpoints alone
	updatesBefore := countActions(kubeClient.Actions(), "update", "endpoints")
	err = ctrl.syncDanmEp(ns, depName)
	require.NoErrorf(t, err, "syncDanmEp returned error: %v", err)
	assert.Equal(t, updatesBefore, countActions(kubeClient.Actions(), "update", "endpoints"), "an unchanged Endpoints shall not be rewritten")

	epsList, err = kubeClient.CoreV1().Endpoints(ns).List(context.TODO(), metav1.ListOptions{})
	require.NoErrorf(t, err, "failed listing eps: %v", err)
	require.ElementsMatch(t, []string{v4Addr, v6Addr}, epIPs(epsList.Items[0].Subsets[0].NotReadyAddresses))

	// ============================
	// the DanmEp is reconciled while the Endpoints has a subset, but holds neither of its addresses:
	// this is the ordinary state when another Pod of the same Service got there first.
	// Both families have to be added, not just one, and not neither
	foreign := corev1.EndpointAddress{
		IP: "10.1.1.99",
		TargetRef: &corev1.ObjectReference{
			Namespace: ns, Name: "otherpod", Kind: "Pod", UID: types.UID("otherpod-uid"), ResourceVersion: "2",
		},
	}
	doctored := eps.DeepCopy()
	doctored.Subsets[0].Addresses = nil
	doctored.Subsets[0].NotReadyAddresses = []corev1.EndpointAddress{foreign}
	err = epsStore.Update(doctored)
	require.NoErrorf(t, err, "failed to add eps to store: %v", err)

	err = ctrl.syncDanmEp(ns, depName)
	require.NoErrorf(t, err, "syncDanmEp returned error: %v", err)

	epsList, err = kubeClient.CoreV1().Endpoints(ns).List(context.TODO(), metav1.ListOptions{})
	require.NoErrorf(t, err, "failed listing eps: %v", err)
	require.Empty(t, epsList.Items[0].Subsets[0].Addresses)
	require.ElementsMatch(t, []string{foreign.IP, v4Addr, v6Addr}, epIPs(epsList.Items[0].Subsets[0].NotReadyAddresses))

	// let the Service reconcile rebuild the Endpoints from the DanmEps, dropping the foreign address
	err = ctrl.syncSvc(ns, svcName)
	require.NoErrorf(t, err, "syncSvc returned error: %v", err)
	epsList, err = kubeClient.CoreV1().Endpoints(ns).List(context.TODO(), metav1.ListOptions{})
	require.NoErrorf(t, err, "failed listing eps: %v", err)
	require.ElementsMatch(t, []string{v4Addr, v6Addr}, epIPs(epsList.Items[0].Subsets[0].NotReadyAddresses))
	eps = epsList.Items[0].DeepCopy()
	err = epsStore.Update(eps)
	require.NoErrorf(t, err, "failed to add eps to store: %v", err)

	pod, err = kubeClient.CoreV1().Pods(ns).Get(context.TODO(), podName, metav1.GetOptions{})
	require.NoErrorf(t, err, "failed getting Pod: %v", err)
	updated := pod.DeepCopy()
	updated.Status.Conditions[0].Status = corev1.ConditionTrue
	updated.ResourceVersion = "3"
	_, err = kubeClient.CoreV1().Pods(ns).Update(context.TODO(), updated, metav1.UpdateOptions{})
	require.NoErrorf(t, err, "failed creating pod in kube api: %v", err)
	err = podStore.Update(updated)
	require.NoErrorf(t, err, "failed to add pod to store: %v", err)
	err = ctrl.syncPod(ns, podName)
	require.NoErrorf(t, err, "syncPod returned error: %v", err)

	// eps should be updated notreadyaddress to address: a dual-stack Pod must never be left
	// half-ready, so a single sync has to move both of its addresses
	epsList, err = kubeClient.CoreV1().Endpoints(ns).List(context.TODO(), metav1.ListOptions{})
	require.NoErrorf(t, err, "failed listing eps: %v", err)
	require.Equal(t, 1, len(epsList.Items))
	require.Empty(t, epsList.Items[0].Subsets[0].NotReadyAddresses)
	require.ElementsMatch(t, []string{v4Addr, v6Addr}, epIPs(epsList.Items[0].Subsets[0].Addresses))
	eps = epsList.Items[0].DeepCopy()
	err = epsStore.Update(eps)
	require.NoErrorf(t, err, "failed to add eps to store: %v", err)

	pod, err = kubeClient.CoreV1().Pods(ns).Get(context.TODO(), podName, metav1.GetOptions{})
	require.NoErrorf(t, err, "failed to list pods: %v", err)
	updated = pod.DeepCopy()
	updated.ObjectMeta.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	updated.ResourceVersion = "4"
	_, err = kubeClient.CoreV1().Pods(ns).Update(context.TODO(), updated, metav1.UpdateOptions{})
	require.NoErrorf(t, err, "failed creating pod in kube api: %v", err)
	err = podStore.Update(updated)
	require.NoErrorf(t, err, "failed to add pod to store: %v", err)
	err = ctrl.syncPod(ns, podName)
	require.NoErrorf(t, err, "syncPod returned error: %v", err)

	// eps should be updated address to notreadyaddress, again for both families at once
	epsList, err = kubeClient.CoreV1().Endpoints(ns).List(context.TODO(), metav1.ListOptions{})
	require.NoErrorf(t, err, "failed listing eps: %v", err)
	require.Equal(t, 1, len(epsList.Items))
	require.Empty(t, epsList.Items[0].Subsets[0].Addresses)
	require.ElementsMatch(t, []string{v4Addr, v6Addr}, epIPs(epsList.Items[0].Subsets[0].NotReadyAddresses))
	eps = epsList.Items[0].DeepCopy()
	err = epsStore.Update(eps)
	require.NoErrorf(t, err, "failed to add eps to store: %v", err)

	// ============================
	// trigger syncDanmEpDelete (pod deletion)
	dk := ns + "/" + depName
	o, _, err := danmepStore.GetByKey(dk)
	dep = o.(*danmv1.DanmEp)
	require.NoErrorf(t, err, "failed to get danmep from store: %v", err)
	ctrl.deletedDepStore[dk] = dep.DeepCopy()
	err = ctrl.syncDanmEpDelete(ns, depName)
	require.NoErrorf(t, err, "syncDanmEpDelete returned error: %v", err)

	// eps should be updated no address and notreadyaddress => no subsets
	epsList, err = kubeClient.CoreV1().Endpoints(ns).List(context.TODO(), metav1.ListOptions{})
	require.NoErrorf(t, err, "failed listing eps: %v", err)
	require.Equal(t, 1, len(epsList.Items))
	require.Equal(t, 0, len(epsList.Items[0].Subsets))
	eps = epsList.Items[0].DeepCopy()
	err = epsStore.Update(eps)
	require.NoErrorf(t, err, "failed to add eps to store: %v", err)
}

//nolint:staticcheck // Endpoints and EndpointSubset are deprecated but still supported, refactor it at some point though
func TestSubsetsOrder(t *testing.T) {
	ctrl, kubeClient, _ := newTestController(t)
	ns := "default"
	pod1Name := "pod1"
	pod1UID := types.UID("pod-uid-123")
	pod1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            pod1Name,
			Namespace:       ns,
			UID:             pod1UID,
			ResourceVersion: "3",
			Labels:          map[string]string{"app": "x"},
			Annotations:     map[string]string{"danm.io/interfaces": "someif"},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
		Spec: corev1.PodSpec{
			Hostname: "host1",
			NodeName: "node1",
		},
	}

	pod2Name := "pod2"
	pod2UID := types.UID("pod-uid-456")
	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            pod2Name,
			Namespace:       ns,
			UID:             pod2UID,
			ResourceVersion: "2",
			Labels:          map[string]string{"app": "x"},
			Annotations:     map[string]string{"danm.io/interfaces": "someif"},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
		Spec: corev1.PodSpec{
			Hostname: "host2",
			NodeName: "node2",
		},
	}

	svcName := "mysvc"
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            svcName,
			Namespace:       ns,
			ResourceVersion: "2",
			Annotations:     map[string]string{"danm.io/clusterNetwork": "ctrl", "danm.io/selector": "{\"app\": \"x\"}"},
		},
	}

	eps := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:            svcName,
			Namespace:       ns,
			ResourceVersion: "2",
			Annotations:     map[string]string{"danm.io/clusterNetwork": "ctrl", "danm.io/selector": "{\"app\": \"x\"}"},
		},
		Subsets: []corev1.EndpointSubset{
			corev1.EndpointSubset{
				Addresses: []corev1.EndpointAddress{
					corev1.EndpointAddress{
						IP:       "10.1.1.6",
						Hostname: pod2.Spec.Hostname,
						NodeName: &pod2.Spec.NodeName,
						TargetRef: &corev1.ObjectReference{
							Namespace:       ns,
							Name:            pod2Name,
							Kind:            "Pod",
							UID:             pod2UID,
							ResourceVersion: "2",
						},
					},
					corev1.EndpointAddress{
						IP:       "10.1.1.5",
						Hostname: pod1.Spec.Hostname,
						NodeName: &pod1.Spec.NodeName,
						TargetRef: &corev1.ObjectReference{
							Namespace:       ns,
							Name:            pod1Name,
							Kind:            "Pod",
							UID:             pod1UID,
							ResourceVersion: "2",
						},
					},
				},
			},
		},
	}

	dep1Name := "dep1"
	dep1 := &danmv1.DanmEp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dep1Name,
			Namespace: ns,
			Labels:    map[string]string{"app": "x"},
		},
		Spec: danmv1.DanmEpSpec{
			Pod:         pod1Name,
			PodUID:      pod1UID,
			ApiType:     "ClusterNetwork",
			NetworkName: "ctrl",
			Iface: danmv1.DanmEpIface{
				Address: "10.1.1.5/24",
			},
		},
	}

	dep2Name := "dep2"
	dep2 := &danmv1.DanmEp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dep2Name,
			Namespace: ns,
			Labels:    map[string]string{"app": "x"},
		},
		Spec: danmv1.DanmEpSpec{
			Pod:         pod2Name,
			PodUID:      pod2UID,
			ApiType:     "ClusterNetwork",
			NetworkName: "ctrl",
			Iface: danmv1.DanmEpIface{
				Address: "10.1.1.6/24",
			},
		},
	}

	addObjects(t, ctrl, pod1, pod2)

	// CASE1: old and new eps has same content
	// createModifyEndpoints
	des := []*danmv1.DanmEp{dep1, dep2}
	err := ctrl.createModifyEndpoints(svc, eps, des)
	require.NoErrorf(t, err, "error occured: %v", err)

	actions := kubeClient.Actions()
	assert.Falsef(t, hasAction(actions, "update", "endpoints"), "not expected kubeclient Update action on Endpoints, got actions: %#v", actions)

	// CASE2: old eps has no subset
	epsEmpty := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:            svcName,
			Namespace:       ns,
			ResourceVersion: "2",
			Annotations:     map[string]string{"danm.io/clusterNetwork": "ctrl", "danm.io/selector": "{\"app\": \"x\"}"},
		},
	}

	_, err = kubeClient.CoreV1().Endpoints(ns).Create(context.TODO(), eps, metav1.CreateOptions{})
	require.NoErrorf(t, err, "failed creating eps in kube api: %v", err)
	err = ctrl.createModifyEndpoints(svc, epsEmpty, des)
	require.NoErrorf(t, err, "error occured: %v", err)
	actions = kubeClient.Actions()
	assert.Truef(t, hasAction(actions, "update", "endpoints"), "expected kubeclient Update action on Endpoints, got actions: %#v", actions)

	// CASE3: old eps is nil
	svc.Name = "somethingelse"
	err = ctrl.createModifyEndpoints(svc, nil, des)
	require.NoErrorf(t, err, "error occured: %v", err)
	actions = kubeClient.Actions()
	assert.Truef(t, hasAction(actions, "create", "endpoints"), "expected kubeclient Create action on Endpoints, got actions: %#v", actions)

}

func TestSyncDanmEpDelete_UnknownDanmEp(t *testing.T) {
	ctrl, kubeClient, _ := newTestController(t)
	err := ctrl.syncDanmEpDelete("default", "unknown-dep")
	require.NoErrorf(t, err, "syncDanmEpDelete returned error: %v", err)
	assert.Empty(t, kubeClient.Actions(), "no Endpoints shall be touched for an unknown DanmEp")
}
