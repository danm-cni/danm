package danm

import (
	"context"
	"errors"
	"net"
	"strings"

	danmtypes "github.com/danm-cni/danm/crd/apis/danm/v1"
	"github.com/danm-cni/danm/pkg/bitarray"
	"github.com/danm-cni/danm/pkg/datastructs"
	"github.com/danm-cni/danm/pkg/ipam"
	"github.com/danm-cni/danm/test/utils"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
)

const (
	magicVersion = "42"
)

type NetClientStub struct {
	TestNets             []danmtypes.DanmNet
	ReservedIpsList      []utils.ReservedIpsList
	TimesUpdateWasCalled int
}

func newNetClientStub(nets []danmtypes.DanmNet, ips []utils.ReservedIpsList) *NetClientStub {
	return &NetClientStub{TestNets: nets, ReservedIpsList: ips}
}

func (netClient *NetClientStub) Create(ctx context.Context, obj *danmtypes.DanmNet, opts meta_v1.CreateOptions) (*danmtypes.DanmNet, error) {
	return nil, nil
}

func (netClient *NetClientStub) Update(ctx context.Context, obj *danmtypes.DanmNet, opts meta_v1.UpdateOptions) (*danmtypes.DanmNet, error) {
	netClient.TimesUpdateWasCalled++
	for _, netReservation := range netClient.ReservedIpsList {
		if obj.Spec.NetworkID == netReservation.NetworkId {
			for _, reservation := range netReservation.Reservations {
				ip, _, _ := net.ParseCIDR(reservation.Ip)
				var subnet *net.IPNet
				var alloc string
				if ip.To4() != nil {
					_, subnet, _ = net.ParseCIDR(obj.Spec.Options.Cidr)
					alloc = obj.Spec.Options.Alloc
				} else {
					_, subnet, _ = net.ParseCIDR(obj.Spec.Options.Pool6.Cidr)
					alloc = obj.Spec.Options.Alloc6
				}
				if subnet == nil || !subnet.Contains(ip) {
					continue
				}
				isIpSetInBa := isIpSet(ip, subnet, alloc)
				if !isIpSetInBa && reservation.Set {
					return nil, errors.New("Reservation failure, IP:" + reservation.Ip + " should have been reserved in DanmNet:" + obj.Spec.NetworkID)
				}
				if isIpSetInBa && !reservation.Set {
					return nil, errors.New("Reservation failure, IP:" + reservation.Ip + " should have been free in DanmNet:" + obj.Spec.NetworkID)
				}
			}
		}
	}
	var netIndex int
	for index, net := range netClient.TestNets {
		if net.ObjectMeta.Name == obj.ObjectMeta.Name {
			netIndex = index
		}
	}
	if strings.Contains(obj.Spec.NetworkID, "conflict") && obj.ObjectMeta.ResourceVersion != magicVersion {
		netClient.TestNets[netIndex].ObjectMeta.ResourceVersion = magicVersion
		return nil, errors.New(datastructs.OptimisticLockErrorMsg)
	}
	if strings.Contains(obj.Spec.NetworkID, "error") {
		return nil, errors.New("fatal error, don't retry")
	}
	netClient.TestNets[netIndex] = *obj
	return obj, nil
}

func (netClient *NetClientStub) Delete(ctx context.Context, name string, options meta_v1.DeleteOptions) error {
	return nil
}

func (netClient *NetClientStub) DeleteCollection(ctx context.Context, options meta_v1.DeleteOptions, listOptions meta_v1.ListOptions) error {
	return nil
}

func (netClient *NetClientStub) Get(ctx context.Context, netName string, options meta_v1.GetOptions) (*danmtypes.DanmNet, error) {
	if strings.Contains(netName, "error") {
		return nil, errors.New("fatal error, don't retry")
	}
	for _, testNet := range netClient.TestNets {
		if testNet.ObjectMeta.Name == netName {
			return &testNet, nil
		}
	}
	return nil, errors.New("let's test error case as well")
}

func (netClient *NetClientStub) Watch(ctx context.Context, opts meta_v1.ListOptions) (watch.Interface, error) {
	watch := watch.NewEmptyWatch()
	return watch, nil
}

func (netClient *NetClientStub) List(ctx context.Context, opts meta_v1.ListOptions) (*danmtypes.DanmNetList, error) {
	return nil, nil
}

func (netClient *NetClientStub) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts meta_v1.PatchOptions, subresources ...string) (result *danmtypes.DanmNet, err error) {
	return nil, nil
}

func (netClient *NetClientStub) AddReservedIpsList(reservedIps []utils.ReservedIpsList) {
	netClient.ReservedIpsList = reservedIps
}

func isIpSet(ip net.IP, subnet *net.IPNet, alloc string) bool {
	index := ipam.GetIndexOfIp(ip, subnet)
	ba := bitarray.NewBitArrayFromBase64(alloc)
	return ba.Get(index)
}
