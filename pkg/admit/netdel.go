package admit

import (
	"errors"
	"net/http"

	"github.com/danm-cni/danm/pkg/confman"
	"github.com/danm-cni/danm/pkg/danmep"
	"k8s.io/api/admission/v1beta1"
)

// A GIGANTIC DISCLAIMER: THIS DOES NOT WORK BEFORE K8S 1.15!
// See ticket: https://github.com/kubernetes/kubernetes/pull/76346
// Tested with 1.15 though, works like a charm
func (validator *Validator) DeleteNetwork(responseWriter http.ResponseWriter, request *http.Request) {
	admissionReview, err := DecodeAdmissionReview(request)
	if err != nil {
		SendErroneousAdmissionResponse(responseWriter, admissionReview.Request, err)
		return
	}
	oldManifest, err := getNetworkManifest(admissionReview.Request.OldObject.Raw)
	if err != nil {
		SendErroneousAdmissionResponse(responseWriter, admissionReview.Request, err)
		return
	}
	isAnyPodConnectedToNetwork, connectedEp, err := danmep.ArePodsConnectedToNetwork(validator.Client, oldManifest)
	if err != nil {
		SendErroneousAdmissionResponse(responseWriter, admissionReview.Request,
			errors.New("Network cannot be deleted because there is no way to tell if Pods are still using it due to:"+err.Error()))
		return
	}
	if isAnyPodConnectedToNetwork {
		SendErroneousAdmissionResponse(responseWriter, admissionReview.Request,
			errors.New("Network cannot be deleted because there are Pods still connected to it e.g. Pod:"+connectedEp.Spec.Pod+" in namespace:"+connectedEp.ObjectMeta.Namespace))
		return
	}
	if oldManifest.TypeMeta.Kind == "TenantNetwork" && IsTypeDynamic(oldManifest.Spec.NetworkType) {
		tconf, err := confman.GetTenantConfig(validator.Client)
		if err != nil {
			SendErroneousAdmissionResponse(responseWriter, admissionReview.Request,
				errors.New("The network's VNI could not be freed, because:"+err.Error()))
			return
		}
		err = confman.Free(validator.Client, tconf, oldManifest)
		if err != nil {
			SendErroneousAdmissionResponse(responseWriter, admissionReview.Request,
				errors.New("The network's VNI could not be freed, because:"+err.Error()))
			return
		}
	}
	responseAdmissionReview := v1beta1.AdmissionReview{
		Response: CreateReviewResponseFromPatches(nil),
	}
	responseAdmissionReview.Response.UID = admissionReview.Request.UID
	SendAdmissionResponse(responseWriter, responseAdmissionReview)
}
