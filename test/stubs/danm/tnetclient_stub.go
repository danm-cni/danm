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

type TnetClientStub struct {
	TestTnets          []danmtypes.TenantNetwork
	Statuses           map[string]danmtypes.DanmNetStatus
	UpdateStatusCalled int
}

func newTnetClientStub(nets []danmtypes.TenantNetwork) *TnetClientStub {
	return &TnetClientStub{TestTnets: nets}
}

func (tnetClient *TnetClientStub) Create(ctx context.Context, obj *danmtypes.TenantNetwork, opts meta_v1.CreateOptions) (*danmtypes.TenantNetwork, error) {
	return nil, nil
}

func (tnetClient *TnetClientStub) Update(ctx context.Context, obj *danmtypes.TenantNetwork, opts meta_v1.UpdateOptions) (*danmtypes.TenantNetwork, error) {
	return nil, nil

}

func (tnetClient *TnetClientStub) UpdateStatus(ctx context.Context, tenantNetwork *danmtypes.TenantNetwork, opts meta_v1.UpdateOptions) (*danmtypes.TenantNetwork, error) {
	if tnetClient.Statuses == nil {
		tnetClient.Statuses = make(map[string]danmtypes.DanmNetStatus)
	}
	tnetClient.Statuses[tenantNetwork.Name] = tenantNetwork.Status
	tnetClient.UpdateStatusCalled++
	return tenantNetwork, nil
}

func (tnetClient *TnetClientStub) Delete(ctx context.Context, name string, options meta_v1.DeleteOptions) error {
	return nil
}

func (tnetClient *TnetClientStub) DeleteCollection(ctx context.Context, options meta_v1.DeleteOptions, listOptions meta_v1.ListOptions) error {
	return nil
}

func (tnetClient *TnetClientStub) Get(ctx context.Context, netName string, options meta_v1.GetOptions) (*danmtypes.TenantNetwork, error) {
	if strings.Contains(netName, "error") {
		return nil, errors.New("fatal error, don't retry")
	}
	for _, testNet := range tnetClient.TestTnets {
		if testNet.ObjectMeta.Name == netName {
			return testNet.DeepCopy(), nil
		}
	}
	return nil, apierrors.NewNotFound(danmtypes.SchemeGroupVersion.WithResource("tenantnetworks").GroupResource(), netName)
}

func (tnetClient *TnetClientStub) Watch(ctx context.Context, opts meta_v1.ListOptions) (watch.Interface, error) {
	watch := watch.NewEmptyWatch()
	return watch, nil
}

func (tnetClient *TnetClientStub) List(ctx context.Context, opts meta_v1.ListOptions) (*danmtypes.TenantNetworkList, error) {
	return nil, nil
}

func (tnetClient *TnetClientStub) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts meta_v1.PatchOptions, subresources ...string) (result *danmtypes.TenantNetwork, err error) {
	return nil, nil
}
