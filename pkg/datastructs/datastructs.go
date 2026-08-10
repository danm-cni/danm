package datastructs

import (
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/version"
	danmtypes "github.com/danm-cni/danm/crd/apis/danm/v1"
	core_v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
)

const (
	OptimisticLockErrorMsg = "the object has been modified; please apply your changes to the latest version and try again"
	MinV4MaskLength        = 32
	MaxV4MaskLength        = 9
	MaxV6PrefixLength      = 105
	MinV6PrefixLength      = 128
)

var (
	SupportedCniVersions = version.PluginSupports("0.3.1")
	LegacyNamingScheme   = "legacy"
)

type NetConf struct {
	types.NetConf
	Kubeconfig   string `json:"kubeconfig,omitempty"`
	CniConfigDir string `json:"cniDir,omitempty"`
	NamingScheme string `json:"namingScheme,omitempty"`
	Master       string `json:"master,omitempty"`
	Vlan         int    `json:"vlan,omitempty"`
	Vxlan        int    `json:"vxlan,omitempty"`
}

type CniConfigReader func(netInfo *danmtypes.DanmNet, ipam IpamConfig, ep *danmtypes.DanmEp, cniVersion string) ([]byte, error)

type CniBackendConfig struct {
	CNIVersion   string
	ReadConfig   CniConfigReader
	IpamNeeded   bool
	DeviceNeeded bool
}

// Interface represents a request coming from the Pod to connect it to one DanmNet during CNI_ADD operation
// It contains the name of the network object the Pod should be connected to, and other optional requests
// Pods can influence the scheme of IP allocation (dynamic, static, none),
// and can ask for the provisioning of policy-based IP routes
type Interface struct {
	Network          string            `json:"network,omitempty"`
	TenantNetwork    string            `json:"tenantNetwork,omitempty"`
	ClusterNetwork   string            `json:"clusterNetwork,omitempty"`
	Ip               string            `json:"ip,omitempty"`
	Ip6              string            `json:"ip6,omitempty"`
	Proutes          map[string]string `json:"proutes,omitempty"`
	Proutes6         map[string]string `json:"proutes6,omitempty"`
	DefaultIfaceName string
	Device           string
	SequenceId       int
}

type IpamConfig struct {
	Type string   `json:"type"`
	Ips  []IpamIp `json:"ips,omitempty"`
}

type IpamIp struct {
	IpCidr  string `json:"ipcidr"`
	Version int    `json:"version"`
}

type CniArgs struct {
	Namespace      string
	Netns          string
	PodName        string
	ContainerId    string
	StdIn          []byte
	Interfaces     []Interface
	Pod            *core_v1.Pod
	DefaultNetwork *danmtypes.DanmNet
}

type NetwatcherConfig struct {
	RestConfig     *rest.Config
	SourceLearning bool
}
