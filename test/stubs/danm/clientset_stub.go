package danm

import (
	danmv1 "github.com/danm-cni/danm/crd/client/clientset/versioned/typed/danm/v1"
	"github.com/danm-cni/danm/test/utils"
	discovery "k8s.io/client-go/discovery"
)

type ClientSetStub struct {
	DanmClient *ClientStub
}

func (c *ClientSetStub) DanmV1() danmv1.DanmV1Interface {
	return c.DanmClient
}

func (c *ClientSetStub) Danm() danmv1.DanmV1Interface {
	return c.DanmClient
}

func (c *ClientSetStub) Discovery() discovery.DiscoveryInterface {
	return nil
}

func NewClientSetStub(objects utils.TestArtifacts) *ClientSetStub {
	var clientSet ClientSetStub
	clientSet.DanmClient = newClientStub(objects)
	return &clientSet
}
