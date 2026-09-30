package admit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"slices"

	danmtypes "github.com/danm-cni/danm/crd/apis/danm/v1"
	danmclientset "github.com/danm-cni/danm/crd/client/clientset/versioned"
	"github.com/danm-cni/danm/pkg/confman"
	"github.com/danm-cni/danm/pkg/netcontrol"
	admv1beta1 "k8s.io/api/admission/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
		Response: &admv1beta1.AdmissionResponse{UID: admissionReview.Request.UID, Allowed: true},
	}
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
	if newNet == nil {
		return nil
	}
	err := validateNetworkRef(newIp, validator.Client)
	if err != nil {
		return fmt.Errorf("network reference validation failed with error: %w", err)
	}
	err = validateIps(newNet, newIp)
	if err != nil {
		return fmt.Errorf("IP reservation validation failed with error: %w", err)
	}
	return nil
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

func validateIps(dnet *danmtypes.DanmNet, newIp *danmtypes.ReservedIP) error {
	_, v4Cidr, _ := net.ParseCIDR(dnet.Spec.Options.Cidr)
	_, v6Cidr, _ := net.ParseCIDR(dnet.Spec.Options.Net6)
	var v4AddrCounter, v6AddrCounter int
	for _, rip := range newIp.Spec.Ips {
		ip := net.ParseIP(rip.Address)
		if ip == nil {
			return fmt.Errorf("IP:%v requested to be reserved is not a valid IP address", rip.Address)
		}
		//Static IPs can fall outside the defined allocation pool but they still need to come from a 1: L3 network's 2: network CIDR
		v4Ip := ip.To4()
		if v4Ip != nil {
			v4AddrCounter++
			if dnet.Spec.Options.Alloc == "" || !v4Cidr.Contains(ip) {
				return fmt.Errorf("V4 IP:%v requested to be reserved is not part of the referenced network's V4 Allocation CIDR:%v", ip, dnet.Spec.Options.Cidr)
			}
		} else {
			v6AddrCounter++
			if dnet.Spec.Options.Alloc6 == "" || !v6Cidr.Contains(ip) {
				return fmt.Errorf("V6 IP:%v requested to be reserved is not part of the referenced network's V6 Allocation CIDR:%v", ip, dnet.Spec.Options.Net6)
			}
		}
	}
	if v4AddrCounter > 1 || v6AddrCounter > 1 {
		return fmt.Errorf("a ReservedIP object can have maximum one IPv4 and one IPv6 address defined")
	}
	return nil
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
