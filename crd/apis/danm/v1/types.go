package v1

import (
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type DanmNet struct {
	meta_v1.TypeMeta   `json:",inline"`
	meta_v1.ObjectMeta `json:"metadata"`
	Spec               DanmNetSpec   `json:"spec"`
	Status             DanmNetStatus `json:"status"`
}

type DanmNetSpec struct {
	NetworkID      string        `json:"NetworkID"`
	NetworkType    string        `json:"NetworkType,omitempty"`
	AllowedTenants []string      `json:"AllowedTenants,omitempty"`
	Options        DanmNetOption `json:"Options"`
}

type DanmNetOption struct {
	// The device to where the network is attached
	Device string `json:"host_device,omitempty"`
	// The resource_pool contains allocated device IDs
	DevicePool string `json:"device_pool,omitempty"`
	// the vxlan id on the host device (creation of vxlan interface)
	Vxlan int `json:"vxlan,omitempty"`
	// The name of the interface in the container
	Prefix string `json:"container_prefix,omitempty"`
	// IPv4 specific parameters
	// IPv4 network address
	Cidr string `json:"cidr,omitempty"`
	// IPv4 routes for this network
	Routes map[string]string `json:"routes,omitempty"`
	// bit array of tracking address allocation
	Alloc string `json:"alloc,omitempty"`
	// subset of the IPv4 subnet from which IPs can be allocated
	Pool IpPool `json:"allocation_pool"`
	// IPv6 specific parameters
	// IPv6 unique global address prefix
	Net6 string `json:"net6,omitempty"`
	// IPv6 routes for this network
	Routes6 map[string]string `json:"routes6,omitempty"`
	// bit array tracking IPv6 allocations
	Alloc6 string `json:"alloc6,omitempty"`
	// subset of the IPv6 subnet from which IPs can be allocated
	Pool6 IpPoolV6 `json:"allocation_pool_v6"`
	// Routing table number for policy routing
	RTables int `json:"rt_tables,omitempty"`
	// the VLAN id of the VLAN interface created on top of the host device
	Vlan int `json:"vlan,omitempty"`
	// MTU value of associated host and container network interfaces
	Mtu int `json:"mtu,omitempty"`
}

type IpPool struct {
	Start  string `json:"start,omitempty"`
	End    string `json:"end,omitempty"`
	LastIp string `json:"lastIp,omitempty"`
}

type IpPoolV6 struct {
	IpPool
	Cidr string `json:"cidr"`
}

type DanmNetStatus struct {
	ReservedIPs []NetRipStatus `json:"reservedIPs,omitempty"`
}

type NetRipStatus struct {
	Objects   []string `json:"objects"`
	Namespace string   `json:"namespace,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type DanmNetList struct {
	meta_v1.TypeMeta `json:",inline"`
	meta_v1.ListMeta `json:"metadata"`
	Items            []DanmNet `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type DanmEp struct {
	meta_v1.TypeMeta   `json:",inline"`
	meta_v1.ObjectMeta `json:"metadata"`
	Spec               DanmEpSpec `json:"spec"`
}

type DanmEpSpec struct {
	NetworkName string      `json:"NetworkName"`
	NetworkType string      `json:"NetworkType"`
	EndpointID  string      `json:"EndpointID"`
	Iface       DanmEpIface `json:"Interface"`
	Host        string      `json:"Host,omitempty"`
	Pod         string      `json:"Pod"`
	PodUID      types.UID   `json:"PodUID,omitempty"`
	CID         string      `json:"CID,omitempty"`
	Netns       string      `json:"netns,omitempty"`
	ApiType     string      `json:"apiType"`
}

type DanmEpIface struct {
	Name        string            `json:"Name"`
	Address     string            `json:"Address,omitempty"`
	AddressIPv6 string            `json:"AddressIPv6,omitempty"`
	MacAddress  string            `json:"MacAddress,omitempty"`
	Proutes     map[string]string `json:"proutes,omitempty"`
	Proutes6    map[string]string `json:"proutes6,omitempty"`
	DeviceID    string            `json:"DeviceID,omitempty"`
	Mtu         int               `json:"Mtu,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type DanmEpList struct {
	meta_v1.TypeMeta `json:",inline"`
	meta_v1.ListMeta `json:"metadata"`
	Items            []DanmEp `json:"items"`
}

// VERY IMPORTANT NOT TO CHANGE THIS, INCLUDING THE EMPTY LINE BETWEEN THE ANNOTATIONS!!!
// https://github.com/kubernetes/code-generator/issues/59
// +genclient:nonNamespaced

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient
type TenantConfig struct {
	meta_v1.TypeMeta   `json:",inline"`
	meta_v1.ObjectMeta `json:"metadata"`
	HostDevices        []IfaceProfile    `json:"hostDevices,omitempty"`
	NetworkIds         map[string]string `json:"networkIds,omitempty"`
	AllowedCNets       []AllowedCNet     `json:"allowedClusterNetworks,omitempty"`
}

type IfaceProfile struct {
	Name     string `json:"name"`
	VniType  string `json:"vniType,omitempty"`
	VniRange string `json:"vniRange,omitempty"`
	Alloc    string `json:"alloc,omitempty"`
}

type AllowedCNet struct {
	Name       string   `json:"name"`
	Privileges []string `json:"privileges"`
}

// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type TenantConfigList struct {
	meta_v1.TypeMeta `json:",inline"`
	meta_v1.ListMeta `json:"metadata"`
	Items            []TenantConfig `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type TenantNetwork DanmNet

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type TenantNetworkList struct {
	meta_v1.TypeMeta `json:",inline"`
	meta_v1.ListMeta `json:"metadata"`
	Items            []TenantNetwork `json:"items"`
}

// VERY IMPORTANT NOT TO CHANGE THIS, INCLUDING THE EMPTY LINE BETWEEN THE ANNOTATIONS!!!
// https://github.com/kubernetes/code-generator/issues/59
// +genclient:nonNamespaced

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient
type ClusterNetwork DanmNet

// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ClusterNetworkList struct {
	meta_v1.TypeMeta `json:",inline"`
	meta_v1.ListMeta `json:"metadata"`
	Items            []ClusterNetwork `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ReservedIP struct {
	meta_v1.TypeMeta   `json:",inline"`
	meta_v1.ObjectMeta `json:"metadata"`
	Spec               ReservedIPSpec   `json:"spec"`
	Status             ReservedIPStatus `json:"status"`
}

type ReservedIPSpec struct {
	Network     RipNetworkSelector `json:"network"`
	Ips         []RipIP            `json:"ips"`
	Description string             `json:"description,omitempty"`
}

type RipNetworkSelector struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

type RipIP struct {
	Address   string `json:"address"`
	Shareable bool   `json:"shareable,omitempty"`
}

type ReservedIPStatus struct {
	Reservations []Reservation `json:"reservations"`
}

type Reservation struct {
	Address string   `json:"address"`
	Users   []string `json:"users,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ReservedIPList struct {
	meta_v1.TypeMeta `json:",inline"`
	meta_v1.ListMeta `json:"metadata"`
	Items            []ReservedIP `json:"items"`
}
