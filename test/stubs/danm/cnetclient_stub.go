package danm

import (
	"context"
	"errors"
	"strings"

	danmtypes "github.com/danm-cni/danm/crd/apis/danm/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
)

type CnetClientStub struct {
	TestCnets          []danmtypes.ClusterNetwork
	Statuses           map[string]danmtypes.DanmNetStatus
	UpdateStatusCalled int
}

func newCnetClientStub(nets []danmtypes.ClusterNetwork) *CnetClientStub {
	return &CnetClientStub{TestCnets: nets}
}

func (cnetClient *CnetClientStub) Create(ctx context.Context, obj *danmtypes.ClusterNetwork, opts meta_v1.CreateOptions) (*danmtypes.ClusterNetwork, error) {
	return nil, nil
}

func (cnetClient *CnetClientStub) Update(ctx context.Context, obj *danmtypes.ClusterNetwork, opts meta_v1.UpdateOptions) (*danmtypes.ClusterNetwork, error) {
	return nil, nil

}

func (cnetClient *CnetClientStub) UpdateStatus(ctx context.Context, clusterNetwork *danmtypes.ClusterNetwork, opts meta_v1.UpdateOptions) (*danmtypes.ClusterNetwork, error) {
	if cnetClient.Statuses == nil {
		cnetClient.Statuses = make(map[string]danmtypes.DanmNetStatus)
	}
	cnetClient.Statuses[clusterNetwork.Name] = clusterNetwork.Status
	cnetClient.UpdateStatusCalled++
	return clusterNetwork, nil
}

func (cnetClient *CnetClientStub) Delete(ctx context.Context, name string, options meta_v1.DeleteOptions) error {
	return nil
}

func (cnetClient *CnetClientStub) DeleteCollection(ctx context.Context, options meta_v1.DeleteOptions, listOptions meta_v1.ListOptions) error {
	return nil
}

func (cnetClient *CnetClientStub) Get(ctx context.Context, netName string, options meta_v1.GetOptions) (*danmtypes.ClusterNetwork, error) {
	if strings.Contains(netName, "error") {
		return nil, errors.New("fatal error, don't retry")
	}
	for _, testNet := range cnetClient.TestCnets {
		if testNet.ObjectMeta.Name == netName {
			return testNet.DeepCopy(), nil
		}
	}
	return nil, apierrors.NewNotFound(danmtypes.SchemeGroupVersion.WithResource("clusternetworks").GroupResource(), netName)
}

func (cnetClient *CnetClientStub) Watch(ctx context.Context, opts meta_v1.ListOptions) (watch.Interface, error) {
	watch := watch.NewEmptyWatch()
	return watch, nil
}

func (cnetClient *CnetClientStub) List(ctx context.Context, opts meta_v1.ListOptions) (*danmtypes.ClusterNetworkList, error) {
	return nil, nil
}

func (cnetClient *CnetClientStub) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts meta_v1.PatchOptions, subresources ...string) (result *danmtypes.ClusterNetwork, err error) {
	return nil, nil
}
