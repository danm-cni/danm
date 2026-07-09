package admit

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"

	danmtypes "github.com/danm-cni/danm/crd/apis/danm/v1"
	"github.com/danm-cni/danm/pkg/bitarray"
	"k8s.io/api/admission/v1beta1"
)

const (
	//This is just a dimensioning decision to avoid reserving unnecessarily big bitarrays in TenantConfig
	MaxAllowedVni  = 5000
	HostDevicePath = "/hostDevices"
)

func (validator *Validator) ValidateTenantConfig(responseWriter http.ResponseWriter, request *http.Request) {
	admissionReview, err := DecodeAdmissionReview(request)
	if err != nil {
		SendErroneousAdmissionResponse(responseWriter, admissionReview.Request, err)
		return
	}
	oldManifest, err := decodeTenantConfig(admissionReview.Request.OldObject.Raw)
	if err != nil {
		SendErroneousAdmissionResponse(responseWriter, admissionReview.Request, err)
		return
	}
	newManifest, err := decodeTenantConfig(admissionReview.Request.Object.Raw)
	if err != nil {
		SendErroneousAdmissionResponse(responseWriter, admissionReview.Request, err)
		return
	}
	origNewManifest := *newManifest
	//Don't judge until you have tried deep copying an array (not a slice) in Golang
	origDevices := make([]danmtypes.IfaceProfile, len(newManifest.HostDevices))
	copy(origDevices, newManifest.HostDevices)
	origNewManifest.HostDevices = origDevices
	isManifestValid, err := validateConfig(oldManifest, newManifest, admissionReview.Request.Operation)
	if !isManifestValid {
		SendErroneousAdmissionResponse(responseWriter, admissionReview.Request, err)
		return
	}
	mutateConfigManifest(newManifest)
	responseAdmissionReview := v1beta1.AdmissionReview{
		Response: CreateReviewResponseFromPatches(createPatchListFromConfigChanges(origNewManifest, newManifest)),
	}
	responseAdmissionReview.Response.UID = admissionReview.Request.UID
	SendAdmissionResponse(responseWriter, responseAdmissionReview)
}

// TODO: can the return type be interface{}, and somehow encoding be input based?
// Until that, this is unfortunetaly duplicated code
func decodeTenantConfig(objectToReview []byte) (*danmtypes.TenantConfig, error) {
	configManifest := danmtypes.TenantConfig{}
	if objectToReview == nil {
		return &configManifest, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(objectToReview))
	//We are using Decoder interface, because it can notify us if any unknown fields were put into the object
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&configManifest)
	if err != nil {
		return nil, errors.New("ERROR: unknown fields are not allowed:" + err.Error())
	}
	return &configManifest, nil
}

// TODO: as above. Until reflection is figured out, this is somewhat of a duplication
// Maybe a struct wrapping the exact object type could also work (that would push reflection responsibility on the validators though)
func validateConfig(oldManifest, newManifest *danmtypes.TenantConfig, opType v1beta1.Operation) (bool, error) {
	if newManifest.TypeMeta.Kind != "TenantConfig" {
		return false, errors.New("K8s API type:" + newManifest.TypeMeta.Kind + " is not handled by DANM webhook")
	}
	err := validateTenantconfig(oldManifest, newManifest, opType)
	if err != nil {
		return false, err
	}
	return true, nil
}

func createPatchListFromConfigChanges(origConfig danmtypes.TenantConfig, changedConfig *danmtypes.TenantConfig) []Patch {
	patchList := make([]Patch, 0)
	var hostDevicesPatch string
	if !reflect.DeepEqual(origConfig.HostDevices, changedConfig.HostDevices) && len(origConfig.HostDevices) != 0 && len(changedConfig.HostDevices) != 0 {
		hostDevicesPatch = `[`
		for ifaceIndex, ifaceConf := range changedConfig.HostDevices {
			if ifaceIndex != 0 {
				hostDevicesPatch += `,`
			}
			hostDevicesPatch += `{"name":"` + ifaceConf.Name +
				`","vniType":"` + ifaceConf.VniType +
				`","vniRange":"` + ifaceConf.VniRange +
				`","alloc":"` + ifaceConf.Alloc + `"}`
		}
		hostDevicesPatch += `]`
		patchList = append(patchList, CreateGenericPatchFromChange(HostDevicePath, json.RawMessage(hostDevicesPatch)))
	}
	return patchList
}

func mutateConfigManifest(tconf *danmtypes.TenantConfig) {
	for ifaceIndex, ifaceConf := range tconf.HostDevices {
		//We don't want to either re-init existing allocations, or unnecessarily create arrays for non-virtual networks
		if ifaceConf.Alloc != "" || ifaceConf.VniType == "" {
			continue
		}
		bitArray, _ := bitarray.NewBitArray(MaxAllowedVni + 1)
		tconf.HostDevices[ifaceIndex].Alloc = bitArray.Encode()
	}
	return
}
