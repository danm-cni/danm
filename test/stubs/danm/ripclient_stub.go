package danm

import (
	"context"

	danmtypes "github.com/danm-cni/danm/crd/apis/danm/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
)

type RipClientStub struct {
	Namespace         string
	DeletedNamespaces []string
	DeletedSelectors  []string
	DeletedLabels     []labels.Set
}

func newRipClientStub() *RipClientStub {
	return &RipClientStub{}
}

func (ripClient *RipClientStub) Create(ctx context.Context, obj *danmtypes.ReservedIP, opts meta_v1.CreateOptions) (*danmtypes.ReservedIP, error) {
	return nil, nil
}

func (ripClient *RipClientStub) Update(ctx context.Context, obj *danmtypes.ReservedIP, opts meta_v1.UpdateOptions) (*danmtypes.ReservedIP, error) {
	return nil, nil
}

func (ripClient *RipClientStub) UpdateStatus(ctx context.Context, obj *danmtypes.ReservedIP, opts meta_v1.UpdateOptions) (*danmtypes.ReservedIP, error) {
	return nil, nil
}

func (ripClient *RipClientStub) Delete(ctx context.Context, name string, options meta_v1.DeleteOptions) error {
	return nil
}

func (ripClient *RipClientStub) DeleteCollection(ctx context.Context, options meta_v1.DeleteOptions, listOptions meta_v1.ListOptions) error {
	ripClient.DeletedNamespaces = append(ripClient.DeletedNamespaces, ripClient.Namespace)
	ripClient.DeletedSelectors = append(ripClient.DeletedSelectors, listOptions.LabelSelector)
	//Set based selectors cannot be represented as a label set, so those are only saved in their raw form
	labelSet, err := labels.ConvertSelectorToLabelsMap(listOptions.LabelSelector)
	if err != nil {
		ripClient.DeletedLabels = append(ripClient.DeletedLabels, nil)
		return nil
	}
	ripClient.DeletedLabels = append(ripClient.DeletedLabels, labelSet)
	return nil
}

func (ripClient *RipClientStub) Get(ctx context.Context, ripName string, options meta_v1.GetOptions) (*danmtypes.ReservedIP, error) {
	return nil, nil
}

func (ripClient *RipClientStub) Watch(ctx context.Context, opts meta_v1.ListOptions) (watch.Interface, error) {
	watch := watch.NewEmptyWatch()
	return watch, nil
}

func (ripClient *RipClientStub) List(ctx context.Context, opts meta_v1.ListOptions) (*danmtypes.ReservedIPList, error) {
	return nil, nil
}

func (ripClient *RipClientStub) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts meta_v1.PatchOptions, subresources ...string) (result *danmtypes.ReservedIP, err error) {
	return nil, nil
}
