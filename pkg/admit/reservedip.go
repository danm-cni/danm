package admit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"

	danmtypes "github.com/danm-cni/danm/crd/apis/danm/v1"
	danmclientset "github.com/danm-cni/danm/crd/client/clientset/versioned"
	"github.com/danm-cni/danm/pkg/confman"
	"github.com/danm-cni/danm/pkg/datastructs"
	"github.com/danm-cni/danm/pkg/ipam"
	"github.com/danm-cni/danm/pkg/netcontrol"
	admv1beta1 "k8s.io/api/admission/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	RipLabelsPath = "/metadata/labels"
)

func (validator *Validator) ValidateReservedIp(responseWriter http.ResponseWriter, request *http.Request) {
	admissionReview, err := DecodeAdmissionReview(request)
	if err != nil {
		SendErroneousAdmissionResponse(responseWriter, admissionReview.Request, err)
		return
	}
	oldManifest, err := decodeReservedIp(admissionReview.Request.OldObject.Raw)
	if err != nil {
		SendErroneousAdmissionResponse(responseWriter, admissionReview.Request, err)
		return
	}
	newManifest, err := decodeReservedIp(admissionReview.Request.Object.Raw)
	if err != nil {
		SendErroneousAdmissionResponse(responseWriter, admissionReview.Request, err)
		return
	}
	oldNet, newNet, err := validator.getNetworks(oldManifest, newManifest, admissionReview.Request.Operation)
	if err != nil {
		SendErroneousAdmissionResponse(responseWriter, admissionReview.Request, err)
		return
	}
	err = validator.validateReserveIp(newNet, oldManifest, newManifest, admissionReview.Request.Operation)
	if err != nil {
		SendErroneousAdmissionResponse(responseWriter, admissionReview.Request, err)
		return
	}
	if admissionReview.Request.DryRun == nil || !*admissionReview.Request.DryRun {
		err = validator.updateNetworkStatuses(oldNet, newNet, oldManifest, newManifest)
		if err != nil {
			SendErroneousAdmissionResponse(responseWriter, admissionReview.Request, err)
			return
		}
	}
	responseAdmissionReview := admv1beta1.AdmissionReview{
		Response: CreateReviewResponseFromPatches(createPatchListFromRipLabels(newManifest)),
	}
	responseAdmissionReview.Response.UID = admissionReview.Request.UID
	SendAdmissionResponse(responseWriter, responseAdmissionReview)
}

func decodeReservedIp(objectToReview []byte) (*danmtypes.ReservedIP, error) {
	if len(objectToReview) == 0 {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(objectToReview))
	//We are using Decoder interface, because it can notify us if any unknown fields were put into the object
	decoder.DisallowUnknownFields()
	reservedIp := danmtypes.ReservedIP{}
	err := decoder.Decode(&reservedIp)
	if err != nil || reservedIp.Spec.Network.Name == "" || len(reservedIp.Spec.Ips) == 0 {
		return nil, fmt.Errorf("received reservedIp object is malformed: %w", err)
	}
	return &reservedIp, nil
}

func (validator *Validator) getNetworks(oldIp, newIp *danmtypes.ReservedIP, opType admv1beta1.Operation) (*danmtypes.DanmNet, *danmtypes.DanmNet, error) {
	var oldNet, newNet *danmtypes.DanmNet
	var err error
	if opType == admv1beta1.Delete ||
		(oldIp != nil && newIp != nil && (oldIp.Spec.Network.Name != newIp.Spec.Network.Name || oldIp.Spec.Network.Type != newIp.Spec.Network.Type)) {
		oldNet, err = netcontrol.GetNetworkFromReference(validator.Client, oldIp.Spec.Network.Name, oldIp.Namespace, oldIp.Spec.Network.Type)
		//The only reason we are interested in oldNetwork is to clean it up, so if it was already deleted we can consider it done and allow the RIP DELETE operation through
		if err != nil && !apierrors.IsNotFound(err) {
			return nil, nil, fmt.Errorf("Old referenced network:%v in namespace:%v could not be read because:%w", oldIp.Spec.Network.Name, oldIp.Namespace, err)
		}
	}
	if newIp != nil {
		newNet, err = netcontrol.GetNetworkFromReference(validator.Client, newIp.Spec.Network.Name, newIp.Namespace, newIp.Spec.Network.Type)
		if err != nil {
			return nil, nil, fmt.Errorf("New referenced network:%v in namespace:%v could not be read because:%w", newIp.Spec.Network.Name, newIp.Namespace, err)
		}
	}
	return oldNet, newNet, nil
}

func (validator *Validator) validateReserveIp(newNet *danmtypes.DanmNet, oldIp, newIp *danmtypes.ReservedIP, opType admv1beta1.Operation) error {
	if (newIp == nil && (opType == admv1beta1.Create || opType == admv1beta1.Update)) ||
		(oldIp == nil && opType == admv1beta1.Delete) {
		return fmt.Errorf("invalid operation possibly due to an intermittent network error, please try again!")
	}
	isOldIpReferenced, err := validator.isOldIpReferenced(oldIp)
	if err != nil {
		return fmt.Errorf("cannot validate change request because K8s API query failed with error: %w", err)
	}
	if isOldIpReferenced {
		return errors.New("shareable ReservedIP objects cannot be modified or deleted as long as they have a live Pod referencing them, please remove all affected Pods then try again")
	}
	if newNet == nil {
		return nil
	}
	if newIp.Name == ipam.DynamicAllocType {
		return errors.New("name:" + ipam.DynamicAllocType + " is reserved for DANM IPAM internal use, please try again with a different name")
	}
	if errs := validation.IsValidLabelValue(newIp.Name); len(errs) > 0 {
		return fmt.Errorf("name:%v cannot be used because DanmEps reference ReservedIPs via labels and label validation fails with: %v", newIp.Name, strings.Join(errs, ", "))
	}
	if errs := validation.IsValidLabelValue(newIp.Spec.Network.Name); len(errs) > 0 {
		return fmt.Errorf("network:%v cannot be referenced because ReservedIPs are looked up via network name labels and label validation fails with: %v", newIp.Spec.Network.Name, strings.Join(errs, ", "))
	}
	err = validateNetworkRef(newIp, validator.Client)
	if err != nil {
		return fmt.Errorf("network reference validation failed with error: %w", err)
	}
	err = validator.validateIps(newNet, newIp)
	if err != nil {
		return fmt.Errorf("IP reservation validation failed with error: %w", err)
	}
	return nil
}

func (validator *Validator) isOldIpReferenced(oldIp *danmtypes.ReservedIP) (bool, error) {
	if oldIp == nil {
		return false, nil
	}
	for _, ip := range oldIp.Spec.Ips {
		if ip.Shareable {
			addr := net.ParseIP(ip.Address)
			ripLabel := datastructs.V4RipLabel
			if addr.To4() == nil {
				ripLabel = datastructs.V6RipLabel
			}
			epList, err := validator.Client.DanmV1().DanmEps(oldIp.Namespace).List(context.TODO(), meta_v1.ListOptions{
				LabelSelector: labels.Set{ripLabel: oldIp.Name}.String(),
			})
			if err != nil {
				return false, err
			}
			if epList != nil && len(epList.Items) > 0 {
				return true, nil
			}
		}
	}
	return false, nil
}

func validateNetworkRef(newIp *danmtypes.ReservedIP, client danmclientset.Interface) error {
	if newIp.Spec.Network.Type == netcontrol.ClusterNetworkKind {
		isCnetAllowed, err := confman.CheckCnetPrivilege(client, newIp.Spec.Network.Name, confman.ReservedIpPrivilege)
		if err != nil {
			return err
		}
		if !isCnetAllowed {
			return fmt.Errorf("referenced ClusterNetwork:%v is not configured with reservedIp privilege in TenantConfig", newIp.Spec.Network.Name)
		}
	}
	return nil
}

func (validator *Validator) validateIps(dnet *danmtypes.DanmNet, newIp *danmtypes.ReservedIP) error {
	_, v4Cidr, _ := net.ParseCIDR(dnet.Spec.Options.Cidr)
	_, v6Cidr, _ := net.ParseCIDR(dnet.Spec.Options.Net6)
	var v4AddrCounter, v6AddrCounter int
	var v4Ip, v6Ip net.IP
	for _, rip := range newIp.Spec.Ips {
		ip := net.ParseIP(rip.Address)
		if ip == nil {
			return fmt.Errorf("IP:%v requested to be reserved is not a valid IP address", rip.Address)
		}
		//Static IPs can fall outside the defined allocation pool but they still need to come from a 1: L3 network's 2: network CIDR
		tempIp := ip.To4()
		if tempIp != nil {
			v4AddrCounter++
			v4Ip = tempIp
			if dnet.Spec.Options.Alloc == "" || !v4Cidr.Contains(ip) {
				return fmt.Errorf("V4 IP:%v requested to be reserved is not part of the referenced network's V4 Allocation CIDR:%v", ip, dnet.Spec.Options.Cidr)
			}
		} else {
			v6AddrCounter++
			v6Ip = ip
			if dnet.Spec.Options.Alloc6 == "" || !v6Cidr.Contains(ip) {
				return fmt.Errorf("V6 IP:%v requested to be reserved is not part of the referenced network's V6 Allocation CIDR:%v", ip, dnet.Spec.Options.Net6)
			}
		}
	}
	if v4AddrCounter > 1 || v6AddrCounter > 1 {
		return fmt.Errorf("a ReservedIP object can have maximum one IPv4 and one IPv6 address defined")
	}
	conflictingAddress, err := validator.checkReservationConflicts(dnet, v4Ip, v6Ip, newIp)
	if err != nil {
		return err
	}
	if conflictingAddress != "" {
		return fmt.Errorf("desired IP address:%v is already reserved in the network, please select a different address", conflictingAddress)
	}
	return nil
}

func (validator *Validator) checkReservationConflicts(dnet *danmtypes.DanmNet, v4Ip, v6Ip net.IP, newIp *danmtypes.ReservedIP) (string, error) {
	labelSelectorListOpt := meta_v1.ListOptions{LabelSelector: labels.Set{
		datastructs.NetworkNameLabel: dnet.Name,
		datastructs.NetworkTypeLabel: dnet.Kind,
	}.String()}
	_, v4AllocSubnet, _ := net.ParseCIDR(dnet.Spec.Options.Cidr)
	_, v6AllocSubnet, _ := net.ParseCIDR(dnet.Spec.Options.Pool6.Cidr)
	if v4Ip != nil && v4AllocSubnet != nil && v4AllocSubnet.Contains(v4Ip) && ipam.WasIpAlreadyAllocated(v4Ip, v4AllocSubnet, dnet.Spec.Options.Alloc) {
		return v4Ip.String(), nil
	}
	if v6Ip != nil && v6AllocSubnet != nil && v6AllocSubnet.Contains(v6Ip) && ipam.WasIpAlreadyAllocated(v6Ip, v6AllocSubnet, dnet.Spec.Options.Alloc6) {
		return v6Ip.String(), nil
	}
	rips, err := validator.Client.DanmV1().ReservedIPs(dnet.Namespace).List(context.TODO(), labelSelectorListOpt)
	if err != nil {
		return "", err
	}
	for _, rip := range rips.Items {
		if rip.Name == newIp.Name && rip.Namespace == newIp.Namespace {
			continue
		}
		for _, existingAddress := range rip.Spec.Ips {
			for _, desiredAddress := range newIp.Spec.Ips {
				if net.ParseIP(desiredAddress.Address).Equal(net.ParseIP(existingAddress.Address)) {
					return desiredAddress.Address, nil
				}
			}
		}
	}
	return "", nil
}

func (validator *Validator) updateNetworkStatuses(oldNet, newNet *danmtypes.DanmNet, oldRip, newRip *danmtypes.ReservedIP) error {
	if newNet != nil {
		err := validator.updateNewNetwork(newNet, newRip)
		if err != nil {
			return err
		}
	}
	if oldNet != nil {
		err := validator.updateOldNetwork(oldNet, oldRip)
		if err != nil {
			return err
		}
	}
	return nil
}

func (validator *Validator) updateNewNetwork(newNet *danmtypes.DanmNet, newRip *danmtypes.ReservedIP) error {
	var found bool
	for id, rips := range newNet.Status.ReservedIPs {
		if rips.Namespace == newRip.Namespace {
			found = true
			if !slices.Contains(rips.Objects, newRip.Name) {
				rips.Objects = append(rips.Objects, newRip.Name)
				newNet.Status.ReservedIPs[id] = rips
			}
			break
		}
	}
	if !found {
		netRipStatus := danmtypes.NetRipStatus{
			Namespace: newRip.Namespace,
			Objects:   []string{newRip.Name},
		}
		newNet.Status.ReservedIPs = append(newNet.Status.ReservedIPs, netRipStatus)
	}
	// TODO: gather some experience from the field first but first glance I don't think we need to pro-actively implement retry on this non-critical provisioning path
	hadConflict, err := netcontrol.UpdateNetStatus(validator.Client, newNet)
	if hadConflict {
		return fmt.Errorf("network status update failed due to resource version mismatch, please retry the operation!")
	}
	return err
}

func (validator *Validator) updateOldNetwork(oldNet *danmtypes.DanmNet, oldRip *danmtypes.ReservedIP) error {
	for id, rips := range oldNet.Status.ReservedIPs {
		if rips.Namespace == oldRip.Namespace {
			slimmedObjects := slices.DeleteFunc(rips.Objects, func(s string) bool {
				return s == oldRip.Name
			})
			if len(slimmedObjects) == 0 {
				oldNet.Status.ReservedIPs = slices.Delete(oldNet.Status.ReservedIPs, id, id+1)
			} else {
				oldNet.Status.ReservedIPs[id].Objects = slimmedObjects
			}
			break
		}
	}
	// TODO: gather some experience from the field first but first glance I don't think we need to pro-actively implement retry on this non-critical provisioning path
	hadConflict, err := netcontrol.UpdateNetStatus(validator.Client, oldNet)
	if hadConflict {
		return fmt.Errorf("network status update failed due to resource version mismatch, please retry the operation!")
	}
	return err
}

func createPatchListFromRipLabels(newIp *danmtypes.ReservedIP) []Patch {
	if newIp == nil {
		return nil
	}
	netType := newIp.Spec.Network.Type
	if netType == "" {
		netType = netcontrol.TenantNetworkKind
	}
	desiredLabels := []struct{ key, value string }{
		{datastructs.NetworkNameLabel, newIp.Spec.Network.Name},
		{datastructs.NetworkTypeLabel, netType},
	}
	//JSON Patch "add" fails if the parent of the modified path does not exist, so the whole label set needs to be added at once to an unlabelled object
	if len(newIp.ObjectMeta.Labels) == 0 {
		labels := make(map[string]string, len(desiredLabels))
		for _, label := range desiredLabels {
			labels[label.key] = label.value
		}
		return []Patch{{Op: "add", Path: RipLabelsPath, Value: labels}}
	}
	patchList := make([]Patch, 0)
	for _, label := range desiredLabels {
		if newIp.ObjectMeta.Labels[label.key] != label.value {
			patchList = append(patchList, Patch{Op: "add", Path: RipLabelsPath + "/" + escapeJsonPointer(label.key), Value: label.value})
		}
	}
	return patchList
}

// JSON Pointer reserves "~" and "/", so prefixed label keys need to be escaped as per RFC 6901
func escapeJsonPointer(key string) string {
	return strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}
