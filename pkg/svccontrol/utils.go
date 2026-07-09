package svccontrol

import (
	"encoding/json"
	"reflect"

	danmv1 "github.com/danm-cni/danm/crd/apis/danm/v1"
	"github.com/danm-cni/danm/pkg/netcontrol"
	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
)

const (
	PodSelector        = "danm.io/selector"
	DanmNetSelector    = "danm.io/network"
	TenantNetSelector  = "danm.io/tenantNetwork"
	ClusterNetSelector = "danm.io/clusterNetwork"
	TolerateUnreadyEps = "service.alpha.kubernetes.io/tolerate-unready-endpoints"
)

func IsContain(ep, svc map[string]string) bool {
	epFit := true
	for k, v := range svc {
		if val, ok := ep[k]; ok {
			if val != v {
				epFit = false
				break
			}
		} else {
			epFit = false
			break
		}
	}
	return epFit
}

func GetDanmSvcAnnotations(annotations map[string]string) (map[string]string, map[string]string, error) {
	selectorMap := make(map[string]string)
	netSelectors := make(map[string]string)
	if danmSel, ok := annotations[PodSelector]; ok {
		if danmSel != "" {
			err := json.Unmarshal([]byte(danmSel), &selectorMap)
			if err != nil {
				glog.Errorf("utils: json error: %s", err)
				return selectorMap, netSelectors, err
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
	for i := range pod.Status.Conditions {
		if pod.Status.Conditions[i].Type == corev1.PodReady && pod.Status.Conditions[i].Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func SelectDesMatchLabels(des []*danmv1.DanmEp, selectorMap map[string]string, svcNets map[string]string, svcNs string) []*danmv1.DanmEp {
	var deList []*danmv1.DanmEp
	for _, de := range des {
		deFit := true
		if de.GetNamespace() != svcNs {
			deFit = false
		} else {
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

func FindEpsForSvc(eps []*corev1.Endpoints, svcName, svcNs string) bool {
	epFound := false
	for _, ep := range eps {
		epName := ep.GetName()
		if epName == svcName && ep.GetNamespace() == svcNs {
			epFound = true
			break
		}
	}
	return epFound
}

func SvcChanged(oldSvc, newSvc *corev1.Service) bool {
	// danm svc annotations and annotations.ealy change are relevant
	oldAnno := oldSvc.Annotations
	newAnno := newSvc.Annotations
	oldSelMap, oldNets, oldErr := GetDanmSvcAnnotations(oldAnno)
	newSelMap, newNets, newErr := GetDanmSvcAnnotations(newAnno)
	if oldErr != nil || newErr != nil {
		return true
	}
	if reflect.DeepEqual(oldSelMap, newSelMap) && reflect.DeepEqual(oldNets, newNets) && reflect.DeepEqual(oldSvc.Spec.Ports, newSvc.Spec.Ports) && (oldSvc.Annotations[TolerateUnreadyEps] == newSvc.Annotations[TolerateUnreadyEps]) {
		// no change
		return false
	}
	return true
}

func PodLabelChanged(oldPod, newPod *corev1.Pod) bool {
	if reflect.DeepEqual(oldPod.GetLabels(), newPod.GetLabels()) {
		// no change
		return false
	}
	return true
}

func MatchExistingSvc(de *danmv1.DanmEp, servicesList []*corev1.Service) []*corev1.Service {
	deNs := de.Namespace
	var svcList []*corev1.Service
	for _, svc := range servicesList {
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
		if danmNet == dep.Spec.NetworkName && (netcontrol.DanmNetKind == dep.Spec.ApiType || "" == dep.Spec.ApiType) {
			return true
		}
	}
	if tenantNet, ok := netSelectors[netcontrol.TenantNetworkKind]; ok {
		if tenantNet == dep.Spec.NetworkName && netcontrol.TenantNetworkKind == dep.Spec.ApiType {
			return true
		}
	}
	if clusterNet, ok := netSelectors[netcontrol.ClusterNetworkKind]; ok {
		if clusterNet == dep.Spec.NetworkName && netcontrol.ClusterNetworkKind == dep.Spec.ApiType {
			return true
		}
	}
	return false
}
