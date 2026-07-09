package danm

import (
	client "github.com/danm-cni/danm/crd/client/clientset/versioned/typed/danm/v1"
	"github.com/danm-cni/danm/test/utils"
	rest "k8s.io/client-go/rest"
)

type ClientStub struct {
	Objects     utils.TestArtifacts
	NetClient   *NetClientStub
	TconfClient *TconfClientStub
}

func (client *ClientStub) DanmNets(namespace string) client.DanmNetInterface {
	if client.NetClient == nil {
		client.NetClient = newNetClientStub(client.Objects.TestNets, client.Objects.ReservedIps)
	}
	return client.NetClient
}

func (client *ClientStub) DanmEps(namespace string) client.DanmEpInterface {
	return newEpClientStub(client.Objects.TestEps)
}

func (client *ClientStub) TenantConfigs() client.TenantConfigInterface {
	if client.TconfClient == nil {
		client.TconfClient = newTconfClientStub(client.Objects.TestTconfs, client.Objects.ReservedVnis, client.Objects.ExhaustAllocs)
	}
	return client.TconfClient
}

func (client *ClientStub) TenantNetworks(namespace string) client.TenantNetworkInterface {
	return nil
}

func (client *ClientStub) ClusterNetworks() client.ClusterNetworkInterface {
	return nil
}

func (client *ClientStub) RESTClient() rest.Interface {
	return nil
}

func newClientStub(ta utils.TestArtifacts) *ClientStub {
	return &ClientStub{
		Objects: ta,
	}
}
