package svccontrol

import (
	"encoding/json"
	"fmt"
	"sort"

	danmv1 "github.com/danm-cni/danm/crd/apis/danm/v1"
	"github.com/danm-cni/danm/pkg/netcontrol"
	corev1 "k8s.io/api/core/v1"
)

const (
	//Keep in sync with metacni.danmIfDefinitionSyntax
	danmIfDefinitionSyntax = "danm.io/interfaces"
	PodSelector            = "danm.io/selector"
	DanmNetSelector        = "danm.io/network"
	TenantNetSelector      = "danm.io/tenantNetwork"
	ClusterNetSelector     = "danm.io/clusterNetwork"
	TolerateUnreadyEps     = "service.alpha.kubernetes.io/tolerate-unready-endpoints"
)

func IsContain(labels, selector map[string]string) bool {
	fit := true
	for k, v := range selector {
		if val, ok := labels[k]; ok {
			if val != v {
				fit = false
				break
			}
		} else {
			fit = false
			break
		}
	}
	return fit
}

func GetDanmSvcAnnotations(annotations map[string]string) (map[string]string, map[string]string, error) {
	selectorMap := make(map[string]string)
	netSelectors := make(map[string]string)
	if danmSel, ok := annotations[PodSelector]; ok {
		if danmSel != "" {
			err := json.Unmarshal([]byte(danmSel), &selectorMap)
			if err != nil {
				return selectorMap, netSelectors, fmt.Errorf("Pod selector annotation %s could not be unmarshalled because: %w", PodSelector, err)
			}
		}
	}
	//TODO: instead of this we might need to iterate over the whole annotation and do strings.EqualFold for a case-insensitive key comparison
	if danmNet, ok := annotations[DanmNetSelector]; ok {
		if danmNet != "" {
			netSelectors[netcontrol.DanmNetKind] = danmNet
		}
	}
	if tenantNet, ok := annotations[TenantNetSelector]; ok {
		if tenantNet != "" {
			netSelectors[netcontrol.TenantNetworkKind] = tenantNet
		}
	}
	if clusterNet, ok := annotations[ClusterNetSelector]; ok {
		if clusterNet != "" {
			netSelectors[netcontrol.ClusterNetworkKind] = clusterNet
		}
	}
	return selectorMap, netSelectors, nil
}

func PodReady(pod *corev1.Pod) bool {
	if pod.DeletionTimestamp != nil {
		return false
	}
	for i := range pod.Status.Conditions {
		if pod.Status.Conditions[i].Type == corev1.PodReady && pod.Status.Conditions[i].Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func SelectDesMatchLabels(objs []interface{}, selectorMap map[string]string, svcNets map[string]string, svcNs string) []*danmv1.DanmEp {
	var deList []*danmv1.DanmEp
	for _, o := range objs {
		de := o.(*danmv1.DanmEp)
		var deFit bool
		if de.GetNamespace() == svcNs {
			deMap := de.GetLabels()
			deFit = IsContain(deMap, selectorMap)
			if deFit && !isDepSelectedBySvc(de, svcNets) {
				deFit = false
			}
		}
		if deFit {
			deList = append(deList, de.DeepCopy())
		}
	}
	return deList
}

func MatchExistingSvc(de *danmv1.DanmEp, objs []interface{}) []*corev1.Service {
	deNs := de.Namespace
	var svcList []*corev1.Service
	for _, obj := range objs {
		svc := obj.(*corev1.Service)
		annotations := svc.GetAnnotations()
		selectorMap, svcNets, err := GetDanmSvcAnnotations(annotations)
		if err != nil {
			return svcList
		}
		if len(selectorMap) == 0 || !isDepSelectedBySvc(de, svcNets) || svc.GetNamespace() != deNs {
			continue
		}
		deMap := de.GetLabels()
		deFit := IsContain(deMap, selectorMap)
		if !deFit {
			continue
		}
		svcList = append(svcList, svc.DeepCopy())
	}
	return svcList
}

func isDepSelectedBySvc(dep *danmv1.DanmEp, netSelectors map[string]string) bool {
	if len(netSelectors) == 0 {
		return false
	}
	if danmNet, ok := netSelectors[netcontrol.DanmNetKind]; ok {
		if danmNet == dep.Spec.NetworkName && (netcontrol.DanmNetKind == dep.Spec.ApiType || dep.Spec.ApiType == "") {
			return true
		}
	}
	if tenantNet, ok := netSelectors[netcontrol.TenantNetworkKind]; ok {
		if tenantNet == dep.Spec.NetworkName && dep.Spec.ApiType == netcontrol.TenantNetworkKind {
			return true
		}
	}
	if clusterNet, ok := netSelectors[netcontrol.ClusterNetworkKind]; ok {
		if clusterNet == dep.Spec.NetworkName && dep.Spec.ApiType == netcontrol.ClusterNetworkKind {
			return true
		}
	}
	return false
}

// normalizeSubsets returns a comparison-only canonical copy of the subsets: addresses and ports are put
// into a deterministic order, and the Pod ResourceVersions are cleared from the target references.
// The input is never modified, and the result is never written back into the cluster
//
//nolint:staticcheck // Endpoints is deprecated but still supported, refactor it at some point though
func normalizeSubsets(subsets []corev1.EndpointSubset) []corev1.EndpointSubset {
	ordered := make([]corev1.EndpointSubset, len(subsets))
	for i, s := range subsets {
		s.DeepCopyInto(&ordered[i])
		sort.Slice(ordered[i].Addresses, func(a, b int) bool {
			return ordered[i].Addresses[a].IP < ordered[i].Addresses[b].IP
		})
		sort.Slice(ordered[i].NotReadyAddresses, func(a, b int) bool {
			return ordered[i].NotReadyAddresses[a].IP < ordered[i].NotReadyAddresses[b].IP
		})

		sort.Slice(ordered[i].Ports, func(a, b int) bool {
			return ordered[i].Ports[a].Port < ordered[i].Ports[b].Port
		})
		clearPodResourceVersions(ordered[i].Addresses)
		clearPodResourceVersions(ordered[i].NotReadyAddresses)
	}
	return ordered
}

// clearPodResourceVersions blanks the Pod ResourceVersion in the target reference of every address.
// It is bumped by every single Pod update, so Endpoints which are otherwise identical would never
// compare equal, and svcwatcher would rewrite them on each periodic reconciliation
//
//nolint:staticcheck // Endpoints is deprecated but still supported, refactor it at some point though
func clearPodResourceVersions(addrs []corev1.EndpointAddress) {
	for i := range addrs {
		if addrs[i].TargetRef != nil {
			addrs[i].TargetRef.ResourceVersion = ""
		}
	}
}

//nolint:staticcheck // Endpoints and EndpointSubset are deprecated but still supported, refactor it at some point though
func shrinkSubsets(eps *corev1.Endpoints) {
	if len(eps.Subsets) > 0 &&
		len(eps.Subsets[0].Addresses) == 0 &&
		len(eps.Subsets[0].NotReadyAddresses) == 0 {
		eps.Subsets = nil
	}
}
