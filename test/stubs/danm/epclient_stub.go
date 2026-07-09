package danm

import (
	"context"
	"errors"
	"strings"

	danmtypes "github.com/danm-cni/danm/crd/apis/danm/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
)

type EpClientStub struct {
	TestEps []danmtypes.DanmEp
}

func newEpClientStub(eps []danmtypes.DanmEp) EpClientStub {
	return EpClientStub{TestEps: eps}
}

func (epClient EpClientStub) Create(ctx context.Context, obj *danmtypes.DanmEp, options meta_v1.CreateOptions) (*danmtypes.DanmEp, error) {
	return nil, nil
}

func (epClient EpClientStub) Update(ctx context.Context, obj *danmtypes.DanmEp, options meta_v1.UpdateOptions) (*danmtypes.DanmEp, error) {
	return nil, nil
}

func (epClient EpClientStub) Delete(ctx context.Context, name string, options meta_v1.DeleteOptions) error {
	return nil
}

func (epClient EpClientStub) DeleteCollection(ctx context.Context, options meta_v1.DeleteOptions, listOptions meta_v1.ListOptions) error {
	return nil
}

func (epClient EpClientStub) Get(ctx context.Context, epName string, options meta_v1.GetOptions) (*danmtypes.DanmEp, error) {
	for _, testNet := range epClient.TestEps {
		if testNet.Spec.NetworkName == epName {
			return &testNet, nil
		}
	}
	return nil, nil
}

func (epClient EpClientStub) Watch(ctx context.Context, opts meta_v1.ListOptions) (watch.Interface, error) {
	watch := watch.NewEmptyWatch()
	return watch, nil
}

func (epClient EpClientStub) List(ctx context.Context, opts meta_v1.ListOptions) (*danmtypes.DanmEpList, error) {
	if epClient.TestEps == nil {
		return nil, nil
	}
	for _, ep := range epClient.TestEps {
		if strings.HasPrefix(ep.ObjectMeta.Name, "error") {
			return nil, errors.New("error happened")
		}
	}
	epList := danmtypes.DanmEpList{Items: epClient.TestEps}
	return &epList, nil
}

func (epClient EpClientStub) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts meta_v1.PatchOptions, subresources ...string) (result *danmtypes.DanmEp, err error) {
	return nil, nil
}
