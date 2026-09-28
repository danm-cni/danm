package admit

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strconv"

	danmtypes "github.com/danm-cni/danm/crd/apis/danm/v1"
	"github.com/danm-cni/danm/pkg/bitarray"
	admv1beta1 "k8s.io/api/admission/v1beta1"
	"k8s.io/utils/cpuset"
)

const (
	//This is just a dimensioning decision to avoid reserving unnecessarily big bitarrays in TenantConfig
	//VxLAN VNI ranges are much, much bigger than VLAN though so need to find the right balance between maximum value and storage space
	//TODO: maybe reusing the bitarray library is not the best for this purpose, consider list? we should allow the entire standard VxLAN range (16M,ish) without taking up too much space in etcd
	MaxAllowedVniVxlan = 100000
	MaxAllowedVniVlan  = 4094
	VniTypeVlan        = "vlan"
	VniTypeVxlan       = "vxlan"
	HostDevicePath     = "/hostDevices"
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
	responseAdmissionReview := admv1beta1.AdmissionReview{
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
func validateConfig(oldManifest, newManifest *danmtypes.TenantConfig, opType admv1beta1.Operation) (bool, error) {
	if newManifest.TypeMeta.Kind != "TenantConfig" {
		return false, errors.New("K8s API type:" + newManifest.TypeMeta.Kind + " is not handled by DANM webhook")
	}
	err := validateTenantconfig(oldManifest, newManifest, opType)
	if err != nil {
		return false, err
	}
	return true, nil
}

func validateTenantconfig(oldManifest, newManifest *danmtypes.TenantConfig, opType admv1beta1.Operation) error {
	if len(newManifest.HostDevices) == 0 && len(newManifest.NetworkIds) == 0 {
		return errors.New("Either hostDevices, or networkIds must be provided!")
	}
	var err error
	for _, ifaceConf := range newManifest.HostDevices {
		err = validateIfaceConfig(ifaceConf, opType)
		if err != nil {
			return err
		}
	}
	for nType, nId := range newManifest.NetworkIds {
		if nType == "" || nId == "" {
			return errors.New("neither NetworkID, nor NetworkType can be empty in a NetworkID mapping!")
		}
		if len(nId) > MaxNidLength && len(newManifest.HostDevices) > 0 {
			return errors.New("NetworkID:" + nId + " cannot be longer than " + strconv.Itoa(MaxNidLength) + " characters when HostDevices is present (otherwise VLAN and VxLAN host interface creation might fail due to kernel iface name length restriction)!")
		}
	}
	return nil
}

func validateIfaceConfig(ifaceConf danmtypes.IfaceProfile, opType admv1beta1.Operation) error {
	if ifaceConf.Name == "" {
		return errors.New("name attribute of a hostDevice must not be empty!")
	}
	if (ifaceConf.VniType == "" && ifaceConf.VniRange != "") ||
		(ifaceConf.VniRange == "" && ifaceConf.VniType != "") {
		return errors.New("vniRange and vniType attributes must be provided together for interface:" + ifaceConf.Name)
	}
	if ifaceConf.VniType != "" && ifaceConf.VniType != VniTypeVlan && ifaceConf.VniType != VniTypeVxlan {
		return errors.New(ifaceConf.VniType + " is not in allowed vniType values: {vlan,vxlan} for interface:" + ifaceConf.Name)
	}
	if opType == admv1beta1.Create && ifaceConf.Alloc != "" {
		return errors.New("Allocation bitmask for interface: " + ifaceConf.Name + " shall not be manually defined upon creation!")
	}
	//I know this type is for CPU sets, but isn't it just perfect for handling arbitrarily defined integer ranges?
	vniSet, err := cpuset.Parse(ifaceConf.VniRange)
	if err != nil {
		return errors.New("vniRange for interface:" + ifaceConf.Name + " must be improperly formatted because its parsing fails with:" + err.Error())
	}
	maxAllowedVni := getMaxAllowedVni(ifaceConf.VniType)
	filteredSet := filterVnis(vniSet, maxAllowedVni)
	if filteredSet.Size() > 0 {
		return errors.New("vniRange for interface:" + ifaceConf.Name + " is invalid, because it cannot contain VNIs over the maximum supported number for its VNI Type that is:" + strconv.Itoa(maxAllowedVni))
	}
	return nil
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
		bitArray, _ := bitarray.NewBitArray(uint32(getMaxAllowedVni(ifaceConf.VniType)) + 1)
		tconf.HostDevices[ifaceIndex].Alloc = bitArray.Encode()
	}
}
