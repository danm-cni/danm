package danm

import (
	"context"
	"errors"
	"strconv"
	"strings"

	danmtypes "github.com/danm-cni/danm/crd/apis/danm/v1"
	"github.com/danm-cni/danm/pkg/bitarray"
	"github.com/danm-cni/danm/pkg/datastructs"
	"github.com/danm-cni/danm/test/utils"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
)

type TconfClientStub struct {
	TestTconfs           []danmtypes.TenantConfig
	ReservedVnis         []utils.ReservedVnisList
	TimesUpdateWasCalled int
	ExhaustAllocs        []int
}

func newTconfClientStub(tconfs []danmtypes.TenantConfig, vnis []utils.ReservedVnisList, exhaustAllocs []int) *TconfClientStub {
	return &TconfClientStub{TestTconfs: tconfs, ReservedVnis: vnis, ExhaustAllocs: exhaustAllocs}
}

func (tconfClient *TconfClientStub) Create(ctx context.Context, obj *danmtypes.TenantConfig, opts meta_v1.CreateOptions) (*danmtypes.TenantConfig, error) {
	return nil, nil
}

func (tconfClient *TconfClientStub) Update(ctx context.Context, obj *danmtypes.TenantConfig, opts meta_v1.UpdateOptions) (*danmtypes.TenantConfig, error) {
	tconfClient.TimesUpdateWasCalled++
	for _, vniReservation := range tconfClient.ReservedVnis {
		for index, hostProfile := range obj.HostDevices {
			if hostProfile.Name == vniReservation.ProfileName && hostProfile.VniType == vniReservation.VniType {
				ba := bitarray.NewBitArrayFromBase64(hostProfile.Alloc)
				for _, reservation := range vniReservation.Reservations {
					if !ba.Get(uint32(reservation.Vni)) && reservation.Set {
						return nil, errors.New("Reservation failure, VNI:" + strconv.Itoa(reservation.Vni) + " should have been reserved in TenantConfig:" + obj.ObjectMeta.Name + " profile no:" + strconv.Itoa(index))
					}
					if ba.Get(uint32(reservation.Vni)) && !reservation.Set {
						return nil, errors.New("Reservation failure, VNI:" + strconv.Itoa(reservation.Vni) + " should have been free in TenantConfig:" + obj.ObjectMeta.Name + " profile no.:" + strconv.Itoa(index))
					}
				}
			} else {
				if hostProfile.Alloc != utils.ExhaustedAllocFor5k {
					return nil, errors.New("Unexpected VNI was freed in Interface profile named:" + hostProfile.Name)
				}
			}
		}
	}
	if strings.Contains(obj.ObjectMeta.Name, "conflict") && tconfClient.TimesUpdateWasCalled == 1 {
		for tconfIndex, tconf := range tconfClient.TestTconfs {
			if tconf.ObjectMeta.Name != obj.ObjectMeta.Name {
				continue
			}
			for ifaceIndex, iface := range tconf.HostDevices {
				if iface.Alloc != "" {
					iface.Alloc = utils.AllocFor5k
					utils.ReserveVnis(&iface, tconfClient.ExhaustAllocs)
				}
				tconfClient.TestTconfs[tconfIndex].HostDevices[ifaceIndex] = iface
			}
			return nil, errors.New(datastructs.OptimisticLockErrorMsg)
		}
	}
	if strings.HasPrefix(obj.ObjectMeta.Name, "error") {
		return nil, errors.New("here you go")
	}
	return &danmtypes.TenantConfig{}, nil
}

func (tconfClient *TconfClientStub) Delete(ctx context.Context, name string, options meta_v1.DeleteOptions) error {
	return nil
}

func (tconfClient *TconfClientStub) DeleteCollection(ctx context.Context, options meta_v1.DeleteOptions, listOptions meta_v1.ListOptions) error {
	return nil
}

func (tconfClient *TconfClientStub) Get(ctx context.Context, tconfName string, options meta_v1.GetOptions) (*danmtypes.TenantConfig, error) {
	for _, tconf := range tconfClient.TestTconfs {
		if tconf.ObjectMeta.Name == tconfName {
			if strings.Contains(tconfName, "error") {
				return nil, errors.New("here you go")
			}
			return &tconf, nil
		}
	}
	return nil, nil
}

func (tconfClient *TconfClientStub) Watch(ctx context.Context, opts meta_v1.ListOptions) (watch.Interface, error) {
	watch := watch.NewEmptyWatch()
	return watch, nil
}

func (tconfClient *TconfClientStub) List(ctx context.Context, opts meta_v1.ListOptions) (*danmtypes.TenantConfigList, error) {
	if tconfClient.TestTconfs == nil {
		return nil, nil
	}
	if strings.HasPrefix(tconfClient.TestTconfs[0].ObjectMeta.Name, "error") && !strings.Contains(tconfClient.TestTconfs[0].ObjectMeta.Name, "update") {
		return nil, errors.New("error happened")
	}
	tconfList := danmtypes.TenantConfigList{Items: tconfClient.TestTconfs}
	return &tconfList, nil
}

func (tconfClient *TconfClientStub) Patch(nctx context.Context, ame string, pt types.PatchType, data []byte, opts meta_v1.PatchOptions, subresources ...string) (result *danmtypes.TenantConfig, err error) {
	return nil, nil
}
