package admit

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/danm-cni/danm/pkg/cnidel"
	"k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

type Patch struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

func DecodeAdmissionReview(httpRequest *http.Request) (v1beta1.AdmissionReview, error) {
	var payload []byte
	reviewRequest := v1beta1.AdmissionReview{}
	if httpRequest.Body == nil {
		return reviewRequest, errors.New("Received review request is empty!")
	}
	payload, err := io.ReadAll(httpRequest.Body)
	if err != nil {
		return reviewRequest, err
	}
	codecs := serializer.NewCodecFactory(runtime.NewScheme())
	deserializer := codecs.UniversalDeserializer()
	_, _, err = deserializer.Decode(payload, nil, &reviewRequest)
	return reviewRequest, err
}

func SendErroneousAdmissionResponse(responseWriter http.ResponseWriter, request *v1beta1.AdmissionRequest, err error) {
	failedResponse := &v1beta1.AdmissionResponse{
		Result: &metav1.Status{
			Message: err.Error(),
		},
		Allowed: false,
	}
	if request != nil {
		failedResponse.UID = request.UID
		log.Println("ERROR: Admitting request:" + request.Name + " operation:" + string(request.Operation) + " failed with error:" + err.Error())
	}
	responseAdmissionReview := v1beta1.AdmissionReview{
		Response: failedResponse,
	}
	SendAdmissionResponse(responseWriter, responseAdmissionReview)
}

func SendAdmissionResponse(responseWriter http.ResponseWriter, reviewResponse v1beta1.AdmissionReview) {
	respBytes, err := json.Marshal(reviewResponse)
	if err != nil {
		log.Println("ERROR: Failed to send AdmissionResponse for request:" + string(reviewResponse.Response.UID) + " because JSON marshalling failed with error:" + err.Error())
	}
	responseWriter.Header().Set("Content-Type", "application/json")
	_, err = responseWriter.Write(respBytes)
	if err != nil {
		log.Println("ERROR: Failed to send AdmissionRespons for request:" + string(reviewResponse.Response.UID) + " because putting the HTTP response on the wire failed with error:" + err.Error())
	}
}

func CreateReviewResponseFromPatches(patchList []Patch) *v1beta1.AdmissionResponse {
	reviewResponse := v1beta1.AdmissionResponse{Allowed: true}
	var patches []byte
	var err error
	if len(patchList) > 0 {
		patches, err = json.Marshal(patchList)
		if err != nil {
			reviewResponse.Allowed = false
			reviewResponse.Result = &metav1.Status{Message: "List of patches could not be encoded, because:" + err.Error()}
			return &reviewResponse
		}
	}
	if len(patches) > 0 {
		reviewResponse.Patch = patches
		pt := v1beta1.PatchTypeJSONPatch
		reviewResponse.PatchType = &pt
	}
	return &reviewResponse
}

func CreateGenericPatchFromChange(path string, value interface{}) Patch {
	patch := Patch{
		Op:    "replace",
		Path:  path,
		Value: value,
	}
	return patch
}

func IsTypeDynamic(cniType string) bool {
	neType := strings.ToLower(cniType)
	if _, ok := cnidel.SupportedNativeCnis[neType]; ok || neType == "" || neType == "ipvlan" {
		return true
	}
	return false
}

func getMaxAllowedVni(vniType string) int {
	switch vniType {
	case VniTypeVlan:
		return MaxAllowedVniVlan
	case VniTypeVxlan:
		return MaxAllowedVniVxlan
	}
	return 0
}
