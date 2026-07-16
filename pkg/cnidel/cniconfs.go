package cnidel

import (
	"encoding/json"
	"errors"
	"os"

	danmtypes "github.com/danm-cni/danm/crd/apis/danm/v1"
	"github.com/danm-cni/danm/pkg/datastructs"
	"github.com/danm-cni/danm/pkg/netcontrol"
	sriov_utils "github.com/k8snetworkplumbingwg/sriov-cni/pkg/utils"
)

// This function creates CNI configuration for all static-level backends
// The CNI binary matching with NetowrkType is invoked with the CNI config file matching with NetworkID parameter
func readCniConfigFile(cniconfDir string, netInfo *danmtypes.DanmNet, ipamOptions datastructs.IpamConfig) ([]byte, error) {
	cniConfig := netInfo.Spec.NetworkID
	rawConfig, err := os.ReadFile(cniconfDir + "/" + cniConfig + ".conf")
	if err != nil {
		return nil, errors.New("Could not load CNI config file: " + cniConfig + ".conf for plugin:" + netInfo.Spec.NetworkType + " from directory:" + cniconfDir)
	}
	//Only overwrite "ipam" of the static CNI config if user wants
	if len(ipamOptions.Ips) > 0 {
		ipamRaw, _ := json.Marshal(ipamOptions)
		ipamInGenericFormat := map[string]interface{}{}
		json.Unmarshal(ipamRaw, &ipamInGenericFormat)
		rawConfig = netcontrol.PatchCniConf(rawConfig, "ipam", ipamInGenericFormat)
	}
	return rawConfig, nil
}

// This function creates CNI configuration for the dynamic-level SR-IOV backend
func getSriovCniConfig(netInfo *danmtypes.DanmNet, ipamOptions datastructs.IpamConfig, ep *danmtypes.DanmEp, cniVersion string) ([]byte, error) {
	var sriovConfig SriovNet
	// initialize common fields of "github.com/containernetworking/cni/pkg/types".NetConf
	sriovConfig.CNIVersion = cniVersion
	sriovConfig.Name = netInfo.Spec.NetworkID
	sriovConfig.Type = "sriov"
	pfname, err := sriov_utils.GetPfName(ep.Spec.Iface.DeviceID)
	if err != nil {
		return nil, errors.New("failed to get the name of the sriov PF for device " + ep.Spec.Iface.DeviceID + " due to:" + err.Error())
	}
	sriovConfig.Master = pfname
	sriovConfig.Vlan = &netInfo.Spec.Options.Vlan
	sriovConfig.DeviceID = ep.Spec.Iface.DeviceID
	if len(ipamOptions.Ips) > 0 {
		sriovConfig.Ipam = ipamOptions
	}
	rawConfig, err := json.Marshal(sriovConfig)
	if err != nil {
		return nil, errors.New("Error putting together CNI config for SR-IOV plugin: " + err.Error())
	}
	return rawConfig, nil
}

// This function creates CNI configuration for the dynamic-level MACVLAN backend
func getMacvlanCniConfig(netInfo *danmtypes.DanmNet, ipamOptions datastructs.IpamConfig, ep *danmtypes.DanmEp, cniVersion string) ([]byte, error) {
	var macvlanConfig MacvlanNet
	// initialize common fields of "github.com/containernetworking/cni/pkg/types".NetConf
	macvlanConfig.CNIVersion = cniVersion
	macvlanConfig.Name = netInfo.Spec.NetworkID
	// initialize MacvlanNet specific fields:
	macvlanConfig.Master = netcontrol.DetermineHostDeviceName(netInfo)
	macvlanConfig.Mode = "bridge" //TODO: make these params configurable if required
	macvlanConfig.MTU = 1500
	if len(ipamOptions.Ips) > 0 {
		macvlanConfig.Ipam = ipamOptions
	}
	rawConfig, err := json.Marshal(macvlanConfig)
	if err != nil {
		return nil, errors.New("Error putting together CNI config for MACVLAN plugin: " + err.Error())
	}
	return rawConfig, nil
}
