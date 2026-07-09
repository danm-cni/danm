package admit

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/rand"
	"net/http"
	"reflect"
	"strings"
	"time"

	danmtypes "github.com/danm-cni/danm/crd/apis/danm/v1"
	danmclientset "github.com/danm-cni/danm/crd/client/clientset/versioned"
	"github.com/danm-cni/danm/pkg/confman"
	"github.com/danm-cni/danm/pkg/metacni"
	"k8s.io/api/admission/v1beta1"
)

var (
	NetworkPatchPaths = map[string]string{
		"NetworkType": "/spec/NetworkType",
		"NetworkID":   "/spec/NetworkID",
		"Alloc":       "/spec/Options/alloc",
		"Alloc6":      "/spec/Options/alloc6",
		"Pool":        "/spec/Options/allocation_pool",
		"Pool6":       "/spec/Options/allocation_pool_v6",
		"Device":      "/spec/Options/host_device",
		"Vlan":        "/spec/Options/vlan",
		"Vxlan":       "/spec/Options/vxlan",
	}
)

type Validator struct {
	Client danmclientset.Interface
}

func CreateNewValidator() (*Validator, error) {
	validator := Validator{}
	danmClient, err := metacni.CreateDanmClient("")
	if err != nil {
		return nil, err
	}
	validator.Client = danmClient
	return &validator, nil
}

func (validator *Validator) ValidateNetwork(responseWriter http.ResponseWriter, request *http.Request) {
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
	newManifest, err := getNetworkManifest(admissionReview.Request.Object.Raw)
	if err != nil {
		SendErroneousAdmissionResponse(responseWriter, admissionReview.Request, err)
		return
	}
	origNewManifest := *newManifest
	isManifestValid, err := validateNetworkByType(oldManifest, newManifest, admissionReview.Request.Operation, validator.Client)
	if !isManifestValid {
		SendErroneousAdmissionResponse(responseWriter, admissionReview.Request, err)
		return
	}
	err = mutateNetManifest(validator.Client, newManifest)
	if err != nil {
		SendErroneousAdmissionResponse(responseWriter, admissionReview.Request, err)
		return
	}
	err = postValidateManifest(newManifest)
	if err != nil {
		SendErroneousAdmissionResponse(responseWriter, admissionReview.Request, err)
		return
	}
	responseAdmissionReview := v1beta1.AdmissionReview{
		Response: CreateReviewResponseFromPatches(createPatchListFromNetChanges(origNewManifest, newManifest)),
	}
	responseAdmissionReview.Response.UID = admissionReview.Request.UID
	SendAdmissionResponse(responseWriter, responseAdmissionReview)
}

func getNetworkManifest(objectToReview []byte) (*danmtypes.DanmNet, error) {
	networkManifest := danmtypes.DanmNet{}
	if objectToReview == nil {
		return &networkManifest, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(objectToReview))
	//We are using Decoder interface, because it can notify us if any unknown fields were put into the object
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&networkManifest)
	if err != nil {
		return nil, errors.New("ERROR: unknown fields are not allowed:" + err.Error())
	}
	return &networkManifest, nil
}

func validateNetworkByType(oldManifest, newManifest *danmtypes.DanmNet, opType v1beta1.Operation, client danmclientset.Interface) (bool, error) {
	validatorMapping, isTypeHandled := danmValidationConfig[newManifest.TypeMeta.Kind]
	if !isTypeHandled {
		return false, errors.New("K8s API type:" + newManifest.TypeMeta.Kind + " is not handled by DANM webhook")
	}
	for _, validator := range validatorMapping {
		err := validator(oldManifest, newManifest, opType, client)
		if err != nil {
			return false, err
		}
	}
	return true, nil
}

func mutateNetManifest(danmClient danmclientset.Interface, dnet *danmtypes.DanmNet) error {
	if dnet.Spec.NetworkType == "" {
		dnet.Spec.NetworkType = "ipvlan"
	}
	var err error
	//L3, freshly added network
	if dnet.TypeMeta.Kind == "TenantNetwork" {
		err = addTenantSpecificDetails(danmClient, dnet)
	}
	return err
}

// This is needed because some mandatory validation rules might be only enforced during mutation phase.
// So we cannot validate those rules beforehand, but we also can't be sure they are satisfied by variable user configuration.
// Example is NetworkID related validations for TenantNetworks
// TODO: make this also fancy when more post validation needs surface
func postValidateManifest(dnet *danmtypes.DanmNet) error {
	return validateNetworkId(nil, dnet, "", nil)
}

// TODO: we could easily add CIDR + allocation pool overwrites as well for TenantNetworks, if needed
// Open an issue with your use-case if you see the need!
func addTenantSpecificDetails(danmClient danmclientset.Interface, tnet *danmtypes.DanmNet) error {
	tconf, err := confman.GetTenantConfig(danmClient)
	if err != nil {
		return err
	}
	if IsTypeDynamic(tnet.Spec.NetworkType) {
		err = allocateDetailsForDynamicBackends(danmClient, tnet, tconf)
		if err != nil {
			return err
		}
	}
	for nType, nId := range tconf.NetworkIds {
		//K8s API server converts first character to uppercase... This is why we can't have nice things
		if strings.EqualFold(nType, tnet.Spec.NetworkType) {
			tnet.Spec.NetworkID = nId
			break
		}
	}
	return nil
}

func allocateDetailsForDynamicBackends(danmClient danmclientset.Interface, tnet *danmtypes.DanmNet, tconf *danmtypes.TenantConfig) error {
	var pfProfiles []danmtypes.IfaceProfile
	for _, iface := range tconf.HostDevices {
		if tnet.Spec.Options.DevicePool != "" && tnet.Spec.Options.DevicePool == iface.Name {
			//This is the interface profile belonging to the network's DevicePool
			return attachNetworkToIfaceProfile(danmClient, tnet, tconf, iface)
		} else if tnet.Spec.Options.Device == iface.Name && !strings.Contains(iface.Name, "/") {
			//This is the interface profile matching the requested host_device
			return attachNetworkToIfaceProfile(danmClient, tnet, tconf, iface)
		}
		//DevicePools generally look like this: "xyz.abc.io/resource_name".
		//Here we separate "real" NICs from abstract K8s Devices
		if !strings.Contains(iface.Name, "/") {
			pfProfiles = append(pfProfiles, iface)
		}
	}
	//Explicitly requested physical interfaces shall be also explicitly allowed by the administrator
	if tnet.Spec.Options.DevicePool != "" || tnet.Spec.Options.Device != "" {
		return errors.New("The provided physical interface is not allowed to be used by tenants!")
	}
	if len(pfProfiles) == 0 {
		return errors.New("There are no suitable interface profiles configured for TenantNetworks!")
	}
	rand.Seed(time.Now().UnixNano())
	chosenProfile := pfProfiles[rand.Intn(len(pfProfiles))]
	//Otherwise we randomly choose an interface profile and attach the TenantNetwork to it
	return attachNetworkToIfaceProfile(danmClient, tnet, tconf, chosenProfile)
}

func attachNetworkToIfaceProfile(danmClient danmclientset.Interface, tnet *danmtypes.DanmNet, tconf *danmtypes.TenantConfig, iface danmtypes.IfaceProfile) error {
	if tnet.Spec.Options.Device == "" && tnet.Spec.Options.DevicePool == "" {
		tnet.Spec.Options.Device = iface.Name
	}
	if (iface.VniType == "vlan" && tnet.Spec.Options.Vlan == 0) ||
		(iface.VniType == "vxlan" && tnet.Spec.Options.Vxlan == 0) {
		vni, err := confman.Reserve(danmClient, tconf, iface)
		if err != nil {
			return errors.New("cannot reserve VNI for interface:" + iface.Name + " , because:" + err.Error())
		}
		if iface.VniType == "vlan" {
			tnet.Spec.Options.Vlan = vni
		} else {
			tnet.Spec.Options.Vxlan = vni
		}
	}
	return nil
}

func createPatchListFromNetChanges(origNetwork danmtypes.DanmNet, changedNetwork *danmtypes.DanmNet) []Patch {
	patchList := make([]Patch, 0)
	if origNetwork.Spec.Options.Alloc != changedNetwork.Spec.Options.Alloc {
		//TODO: Could (?) use some reflecting here to determine name of the struct field
		patchList = append(patchList, CreateGenericPatchFromChange(NetworkPatchPaths["Alloc"], changedNetwork.Spec.Options.Alloc))
	}
	if origNetwork.Spec.Options.Alloc6 != changedNetwork.Spec.Options.Alloc6 {
		patchList = append(patchList, CreateGenericPatchFromChange(NetworkPatchPaths["Alloc6"], changedNetwork.Spec.Options.Alloc6))
	}
	if origNetwork.Spec.NetworkType != changedNetwork.Spec.NetworkType {
		patchList = append(patchList, CreateGenericPatchFromChange(NetworkPatchPaths["NetworkType"], changedNetwork.Spec.NetworkType))
	}
	if origNetwork.Spec.NetworkID != changedNetwork.Spec.NetworkID {
		patchList = append(patchList, CreateGenericPatchFromChange(NetworkPatchPaths["NetworkID"], changedNetwork.Spec.NetworkID))
	}
	if !reflect.DeepEqual(origNetwork.Spec.Options.Pool, changedNetwork.Spec.Options.Pool) {
		patchList = append(patchList, CreateGenericPatchFromChange(NetworkPatchPaths["Pool"], changedNetwork.Spec.Options.Pool))
	}
	if !reflect.DeepEqual(origNetwork.Spec.Options.Pool6, changedNetwork.Spec.Options.Pool6) {
		patchList = append(patchList, CreateGenericPatchFromChange(NetworkPatchPaths["Pool6"], changedNetwork.Spec.Options.Pool6))
	}
	if origNetwork.Spec.Options.Device != changedNetwork.Spec.Options.Device {
		patchList = append(patchList, CreateGenericPatchFromChange(NetworkPatchPaths["Device"], changedNetwork.Spec.Options.Device))
	}
	if origNetwork.Spec.Options.Vlan != changedNetwork.Spec.Options.Vlan {
		patchList = append(patchList, CreateGenericPatchFromChange(NetworkPatchPaths["Vlan"], changedNetwork.Spec.Options.Vlan))
	}
	if origNetwork.Spec.Options.Vxlan != changedNetwork.Spec.Options.Vxlan {
		patchList = append(patchList, CreateGenericPatchFromChange(NetworkPatchPaths["Vxlan"], changedNetwork.Spec.Options.Vxlan))
	}
	return patchList
}
