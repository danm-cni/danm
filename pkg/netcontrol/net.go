package netcontrol

import (
	"errors"
	"net"
	"strconv"
	"syscall"

	"github.com/apparentlymart/go-cidr/cidr"
	danmtypes "github.com/danm-cni/danm/crd/apis/danm/v1"
	"github.com/vishvananda/netlink"
)

const (
	ip4MulticastCidr = "239.0.0.0/8"
	ip6MulticastCidr = "ff02::0/16"
)

// LinkInfo is an absract struct to represent a host NIC of a special type: either VLAN, or VxLAN
// The ID of the link is stored together with its Golang representation
type LinkInfo struct {
	interfaceId int
	link        netlink.Link
}

func deleteNetworks(dnet *danmtypes.DanmNet) error {
	if dnet.Spec.Options.Device == "" {
		return nil
	}
	var combinedErrorMessage string
	vxlanId := dnet.Spec.Options.Vxlan
	netId := dnet.Spec.NetworkID
	tempErr := deleteHostInterface(vxlanId, "vx_"+netId)
	if tempErr != nil {
		combinedErrorMessage = tempErr.Error() + "\n"
	}
	vlanId := dnet.Spec.Options.Vlan
	tempErr = deleteHostInterface(vlanId, determineVlanHdev(vlanId, netId, dnet.Spec.Options.Device))
	if tempErr != nil {
		combinedErrorMessage += tempErr.Error()
	}
	if combinedErrorMessage != "" {
		return errors.New(combinedErrorMessage)
	}
	return nil
}

func deleteHostInterface(ifId int, ifName string) error {
	if ifId == 0 {
		return nil
	}
	iface, err := netlink.LinkByName(ifName)
	if err != nil {
		return nil
	}
	err = netlink.LinkDel(iface)
	if err != nil {
		return errors.New("Deletion of interface:" + ifName + " failed with error:" + err.Error())
	}
	return nil
}

func setupHost(dnet *danmtypes.DanmNet, sourceLearning bool) error {
	if dnet.Spec.Options.Device == "" {
		return nil
	}
	netId := dnet.Spec.NetworkID
	hdev := dnet.Spec.Options.Device
	vxlanId := dnet.Spec.Options.Vxlan
	vlanId := dnet.Spec.Options.Vlan
	// Nothing to do here
	if vxlanId == 0 && vlanId == 0 {
		return nil
	}
	err := setupVlan(vlanId, netId, hdev)
	if err != nil {
		return err
	}
	return setupVxlan(vxlanId, netId, hdev, sourceLearning)
}

func setupVlan(vlanId int, netId, hdev string) error {
	vlanName := determineVlanHdev(vlanId, netId, hdev)
	shouldInterfaceBeCreated, hostLink, err := shouldInterfaceBeCreated(vlanId, vlanName, hdev)
	if err != nil {
		return errors.New("cannot set-up host VLAN interface:" + err.Error())
	} else if !shouldInterfaceBeCreated {
		return nil
	}
	vlan := &netlink.Vlan{
		LinkAttrs: netlink.LinkAttrs{
			Name:        vlanName,
			ParentIndex: hostLink.link.Attrs().Index,
		},
		VlanId: hostLink.interfaceId,
	}
	err = addLink(vlan)
	if err != nil {
		return errors.New("cannot add VLAN interface to host due to:" + err.Error())
	}
	return nil
}

func shouldInterfaceBeCreated(ifId int, ifName string, hostDevice string) (bool, LinkInfo, error) {
	hostLink := LinkInfo{}
	if ifId == 0 {
		return false, hostLink, nil
	}
	_, err := netlink.LinkByName(ifName)
	if err == nil {
		return false, hostLink, nil
	}
	dev, err := netlink.LinkByName(hostDevice)
	if err != nil {
		return false, hostLink, errors.New("host device:" + hostDevice + " is not present in the system")
	}
	hostLink.interfaceId = ifId
	hostLink.link = dev
	return true, hostLink, nil
}

func addLink(link netlink.Link) error {
	err := netlink.LinkAdd(link)
	if err != nil {
		return err
	}
	err = netlink.LinkSetUp(link)
	if err != nil {
		return err
	}
	return nil
}

// DetermineVlanHdev returns to which interface a Pod NIC should be connected to in-case VLANs can be in use
// In case VLANs are defined, it returns it in a uniform name, used commonly across DANM
// If the VLAN ID is not defined, then it returns the host device
func determineVlanHdev(vlanId int, netId, hdev string) string {
	if vlanId == 0 {
		return hdev
	}
	return netId + "." + strconv.Itoa(vlanId)
}

func setupVxlan(vxlanId int, netId, hdev string, sourceLearning bool) error {
	vxlanName := "vx_" + netId
	shouldInterfaceBeCreated, hostLink, err := shouldInterfaceBeCreated(vxlanId, vxlanName, hdev)
	if err != nil {
		return errors.New("cannot set-up host VxLAN interface:" + err.Error())
	} else if !shouldInterfaceBeCreated {
		return nil
	}
	isHostIfaceIpv4 := true
	addr := parseVxlanHostIp(netlink.FAMILY_V4, hostLink.link)
	if addr.String() == "<nil>" {
		isHostIfaceIpv4 = false
		addr = parseVxlanHostIp(netlink.FAMILY_V6, hostLink.link)
	}
	//TODO: technically it is enough if the host interface with the source IP exists but it does not necessarily need to be the literal parent...
	//We could parse all host interfaces to see if any of them matches the intended egress
	if addr.String() == "<nil>" {
		return errors.New("VxLAN interface cannot be set-up on top of a host interface:" + hdev + ", which does not have an IP")
	}
	vxlan := &netlink.Vxlan{
		LinkAttrs: netlink.LinkAttrs{
			Name: vxlanName,
		},
		VxlanId:      hostLink.interfaceId,
		VtepDevIndex: hostLink.link.Attrs().Index,
		Port:         4789,
		SrcAddr:      addr,
		Learning:     sourceLearning,
		L2miss:       sourceLearning,
		L3miss:       sourceLearning,
	}
	if sourceLearning {
		var mcastIP net.IP
		if isHostIfaceIpv4 {
			mcastIP, err = getMulticastIp(netlink.FAMILY_V4, strconv.Itoa(vxlanId))
		} else {
			mcastIP, err = getMulticastIp(netlink.FAMILY_V6, strconv.Itoa(vxlanId))
		}
		if err != nil {
			return err
		}
		vxlan.Group = mcastIP
	}
	err = addLink(vxlan)
	if err != nil {
		return errors.New("cannot add VxLAN interface to the host due to:" + err.Error())
	}
	return nil
}

func getMulticastIp(ipFamily int, vxlanId string) (net.IP, error) {
	vxlanIdInt, err := strconv.Atoi(vxlanId)
	if err != nil {
		return nil, err
	}
	multicastCidr := ""
	switch ipFamily {
	case netlink.FAMILY_V4:
		multicastCidr = ip4MulticastCidr
	case netlink.FAMILY_V6:
		multicastCidr = ip6MulticastCidr
	}
	_, mcastNet, err := net.ParseCIDR(multicastCidr)
	if err != nil {
		return nil, errors.New("Unable to parse multicast CIDR " + multicastCidr + " due to " + err.Error())
	}
	mcastIP, err := cidr.Host(mcastNet, vxlanIdInt)
	if err != nil {
		return nil, errors.New("Unable to parse multicast IP due to:" + err.Error())
	}
	return mcastIP, nil
}

func parseVxlanHostIp(ipFamily int, hdev netlink.Link) net.IP {
	var hostAddr net.IP
	addresses, err := netlink.AddrList(hdev, ipFamily)
	if err != nil {
		return hostAddr
	}
	for _, x := range addresses {
		if x.Scope == syscall.RT_SCOPE_UNIVERSE {
			hostAddr = x.IPNet.IP
			return hostAddr
		}
	}
	return hostAddr
}
