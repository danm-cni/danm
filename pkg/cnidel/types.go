package cnidel

import (
	"github.com/containernetworking/cni/pkg/types"
	"github.com/danm-cni/danm/pkg/datastructs"
	sriov_types "github.com/k8snetworkplumbingwg/sriov-cni/pkg/types"
)

var (
	SupportedNativeCnis = map[string]*datastructs.CniBackendConfig{
		"sriov": &datastructs.CniBackendConfig{
			CNIVersion:   "0.3.1",
			ReadConfig:   datastructs.CniConfigReader(getSriovCniConfig),
			IpamNeeded:   true,
			DeviceNeeded: true,
		},
		"macvlan": &datastructs.CniBackendConfig{
			CNIVersion:   "0.3.1",
			ReadConfig:   datastructs.CniConfigReader(getMacvlanCniConfig),
			IpamNeeded:   true,
			DeviceNeeded: false,
		},
	}
)

// sriovNet represent the configuration of sriov cni v1.0.0
type SriovNet struct {
	sriov_types.NetConf
	// IPAM configuration to be used for this network
	Ipam datastructs.IpamConfig `json:"ipam,omitEmpty"`
}

type MacvlanNet struct {
	types.NetConf
	//Name of the master NIC the MACVLAN slave needs to be connected to
	Master string `json:"master"`
	//The mode in which the MACVLAN slave is configured (default bridge)
	Mode string `json:"mode"`
	//MTU to be set to the MACVLAN slave interface (default 1500)
	MTU int `json:"mtu"`
	//IPAM configuration to be used for this network
	Ipam datastructs.IpamConfig `json:"ipam,omitEmpty"`
}
