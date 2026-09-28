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
	dnet, err := validator.validateReserveIp(oldManifest, newManifest, admissionReview.Request.Operation)
	if err != nil {
		SendErroneousAdmissionResponse(responseWriter, admissionReview.Request, err)
		return
	}
	if admissionReview.Request.DryRun == nil || !*admissionReview.Request.DryRun {
		err = validator.updateNetworkStatus(dnet, newManifest)
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
	reservedIp := danmtypes.ReservedIP{}
	if objectToReview == nil {
		return &reservedIp, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(objectToReview))
	//We are using Decoder interface, because it can notify us if any unknown fields were put into the object
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&reservedIp)
	if err != nil {
		return nil, fmt.Errorf("unknown fields are not allowed: %w", err)
	}
	return &reservedIp, nil
}

func (validator *Validator) validateReserveIp(oldIp, newIp *danmtypes.ReservedIP, opType admv1beta1.Operation) (*danmtypes.DanmNet, error) {
	net, err := netcontrol.GetNetworkFromReference(validator.Client, newIp.Spec.Network.Name, newIp.Namespace, newIp.Spec.Network.Type)
	if err != nil {
		return nil, fmt.Errorf("network:%v in namespace:%v could not be read because:%w", newIp.Spec.Network.Name, newIp.Namespace, err)
	}
	err = validateNetworkRef(newIp, validator.Client)
	if err != nil {
		return nil, fmt.Errorf("network reference validation failed with error: %w", err)
	}
	err = validateIps(net, newIp)
	if err != nil {
		return nil, fmt.Errorf("IP reservation validation failed with error: %w", err)
	}
	return net, nil
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

func (validator *Validator) updateNetworkStatus(dnet *danmtypes.DanmNet, rip *danmtypes.ReservedIP) error {
	var found bool
	for id, rips := range dnet.Status.ReservedIPs {
		if rips.Namespace == rip.Namespace {
			found = true
			if !slices.Contains(rips.Objects, rip.Name) {
				rips.Objects = append(rips.Objects, rip.Name)
				dnet.Status.ReservedIPs[id] = rips
			}
			break
		}
	}
	if !found {
		netRipStatus := danmtypes.NetRipStatus{
			Namespace: rip.Namespace,
			Objects:   []string{rip.Name},
		}
		dnet.Status.ReservedIPs = append(dnet.Status.ReservedIPs, netRipStatus)
	}
	// TODO: gather some experience from the field first but first glance I don't think we need to pro-actively implement retry on this non-critical provisioning path
	hadConflict, err := netcontrol.UpdateNetStatus(validator.Client, dnet)
	if hadConflict {
		return fmt.Errorf("network status update failed due to resource version mismatch, please retry the operation!")
	}
	return err
}
