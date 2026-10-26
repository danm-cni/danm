package netcontrol

import (
	"errors"
	"log"
	"net"
	"strconv"
	"strings"
	"syscall"

	"github.com/apparentlymart/go-cidr/cidr"
	danmtypes "github.com/danm-cni/danm/crd/apis/danm/v1"
	"github.com/danm-cni/danm/pkg/mtu"
	"github.com/vishvananda/netlink"
)

const (
	ip4MulticastCidr = "239.0.0.0/8"
	ip6MulticastCidr = "ff02::0/16"
	//managedAliasPrefix tags the host interfaces created by NetWatcher, the alias also carries the NetworkID the interface belongs to
	managedAliasPrefix = "danm-netwatcher:"
)

// hostInterface is a VLAN or VxLAN interface on the host belonging to a NetworkID
type hostInterface struct {
	link        netlink.Link
	vlan        int
	vxlan       int
	parentIndex int
	isTagged    bool
}

// findHostInterfaces returns the VLAN and VxLAN interfaces of networkID present on the host
// An interface belongs to networkID if it is tagged with it, or if it is named the way NetWatcher names the interfaces of networkID
// The latter covers interfaces created by older NetWatcher versions, and the ones whose tagging failed after creation
// A VLAN interface only matches by name if the name also contains its own VLAN ID, so similarly named interfaces created by others are left alone
func findHostInterfaces(networkID string, links []netlink.Link) []hostInterface {
	var hostIfaces []hostInterface
	for _, link := range links {
		isTagged := link.Attrs().Alias == managedAliasPrefix+networkID
		switch typedLink := link.(type) {
		case *netlink.Vlan:
			if isTagged || (typedLink.VlanId != 0 && typedLink.Attrs().Name == determineVlanHdev(typedLink.VlanId, networkID, "")) {
				hostIfaces = append(hostIfaces, hostInterface{link: link, vlan: typedLink.VlanId, parentIndex: typedLink.Attrs().ParentIndex, isTagged: isTagged})
			}
		case *netlink.Vxlan:
			if isTagged || typedLink.Attrs().Name == "vx_"+networkID {
				hostIfaces = append(hostIfaces, hostInterface{link: link, vxlan: typedLink.VxlanId, parentIndex: typedLink.VtepDevIndex, isTagged: isTagged})
			}
		}
	}
	return hostIfaces
}

func isHostInterfaceDesired(hostIface hostInterface, networks []*danmtypes.DanmNet, links []netlink.Link) bool {
	for _, dnet := range networks {
		options := dnet.Spec.Options
		if linkIndex(links, options.Device) != hostIface.parentIndex {
			continue
		}
		isVlanDesired := hostIface.vlan != 0 && hostIface.vlan == options.Vlan &&
			hostIface.link.Attrs().Name == determineVlanHdev(options.Vlan, dnet.Spec.NetworkID, options.Device)
		isVxlanDesired := hostIface.vxlan != 0 && hostIface.vxlan == options.Vxlan && hostIface.link.Attrs().Name == "vx_"+dnet.Spec.NetworkID
		if isVlanDesired || isVxlanDesired {
			return true
		}
	}
	return false
}

func taggedNetworkIDs(links []netlink.Link) []string {
	var networkIDs []string
	for _, link := range links {
		networkID, isTagged := strings.CutPrefix(link.Attrs().Alias, managedAliasPrefix)
		if isTagged && networkID != "" {
			networkIDs = append(networkIDs, networkID)
		}
	}
	return networkIDs
}

func tagHostInterface(link netlink.Link, networkID string) error {
	err := netlink.LinkSetAlias(link, managedAliasPrefix+networkID)
	if err != nil {
		return errors.New("tagging host interface:" + link.Attrs().Name + " failed:" + err.Error())
	}
	return nil
}

func linkIndex(links []netlink.Link, name string) int {
	for _, link := range links {
		if link.Attrs().Name == name {
			return link.Attrs().Index
		}
	}
	return -1
}

func linkName(links []netlink.Link, index int) string {
	for _, link := range links {
		if link.Attrs().Index == index {
			return link.Attrs().Name
		}
	}
	return ""
}

func setupHost(dnet *danmtypes.DanmNet, sourceLearning bool) error {
	if dnet.Spec.Options.Device == "" {
		return nil
	}
	// Nothing to do here
	if dnet.Spec.Options.Vxlan == 0 && dnet.Spec.Options.Vlan == 0 {
		return nil
	}
	err := setupVlan(dnet)
	if err != nil {
		return err
	}
	return setupVxlan(dnet, sourceLearning)
}

func setupVlan(dnet *danmtypes.DanmNet) error {
	if dnet.Spec.Options.Vlan == 0 {
		return nil
	}
	vlanName := determineVlanHdev(dnet.Spec.Options.Vlan, dnet.Spec.NetworkID, dnet.Spec.Options.Device)
	shouldInterfaceBeManipulated, hostLink, err := shouldInterfaceBeManipulated(dnet, vlanName)
	if err != nil {
		return errors.New("cannot set-up host VLAN interface:" + err.Error())
	} else if !shouldInterfaceBeManipulated {
		return nil
	}
	vlan := &netlink.Vlan{
		LinkAttrs: netlink.LinkAttrs{
			Name:        vlanName,
			ParentIndex: hostLink.Attrs().Index,
			MTU:         mtu.GetMtuForNet(dnet),
		},
		VlanId: dnet.Spec.Options.Vlan,
	}
	err = addLink(vlan, dnet.Spec.NetworkID)
	if err != nil {
		return errors.New("cannot add VLAN interface to host due to:" + err.Error())
	}
	return nil
}

func shouldInterfaceBeManipulated(dnet *danmtypes.DanmNet, ifName string) (bool, netlink.Link, error) {
	var hostLink netlink.Link
	dev, err := netlink.LinkByName(dnet.Spec.Options.Device)
	if err != nil {
		return false, hostLink, errors.New("host device:" + dnet.Spec.Options.Device + " is not present in the system")
	}
	existingLink, err := netlink.LinkByName(ifName)
	if err == nil {
		if shouldWeResizeExistingLink(existingLink, dev, dnet) {
			resizeExistingLink(existingLink, mtu.GetMtuForNet(dnet))
		}
		return false, hostLink, nil
	}
	//We don't want to accidentally create mixed MTU networks depending on which kernel driver was used to create the host device,
	// so let's just fail creation itself.
	err = mtu.ValidateForDev(dnet, dev.Attrs().MTU, dev.Attrs().Name)
	if err != nil {
		return false, hostLink, errors.New("cannot set-up host interface: " + err.Error())
	}
	return true, dev, nil
}

func addLink(link netlink.Link, networkID string) error {
	err := netlink.LinkAdd(link)
	if err != nil {
		return err
	}
	err = tagHostInterface(link, networkID)
	if err != nil {
		netlink.LinkDel(link)
		return err
	}
	err = netlink.LinkSetUp(link)
	if err != nil {
		netlink.LinkDel(link)
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

func setupVxlan(dnet *danmtypes.DanmNet, sourceLearning bool) error {
	vxlanName := "vx_" + dnet.Spec.NetworkID
	if dnet.Spec.Options.Vxlan == 0 {
		return nil
	}
	shouldInterfaceBeManipulated, hostLink, err := shouldInterfaceBeManipulated(dnet, vxlanName)
	if err != nil {
		return errors.New("cannot set-up host VxLAN interface:" + err.Error())
	} else if !shouldInterfaceBeManipulated {
		return nil
	}
	isHostIfaceIpv4 := true
	addr := parseVxlanHostIp(netlink.FAMILY_V4, hostLink)
	if addr.String() == "<nil>" {
		isHostIfaceIpv4 = false
		addr = parseVxlanHostIp(netlink.FAMILY_V6, hostLink)
	}
	//TODO: technically it is enough if the host interface with the source IP exists but it does not necessarily need to be the literal parent...
	//We could parse all host interfaces to see if any of them matches the intended egress
	if addr.String() == "<nil>" {
		return errors.New("VxLAN interface cannot be set-up on top of a host interface:" + dnet.Spec.Options.Device + ", which does not have an IP")
	}
	vxlan := &netlink.Vxlan{
		LinkAttrs: netlink.LinkAttrs{
			Name: vxlanName,
			MTU:  mtu.GetMtuForNet(dnet),
		},
		VxlanId:      dnet.Spec.Options.Vxlan,
		VtepDevIndex: hostLink.Attrs().Index,
		Port:         4789,
		SrcAddr:      addr,
		Learning:     sourceLearning,
		L2miss:       sourceLearning,
		L3miss:       sourceLearning,
	}
	if sourceLearning {
		var mcastIP net.IP
		if isHostIfaceIpv4 {
			mcastIP, err = getMulticastIp(netlink.FAMILY_V4, strconv.Itoa(dnet.Spec.Options.Vxlan))
		} else {
			mcastIP, err = getMulticastIp(netlink.FAMILY_V6, strconv.Itoa(dnet.Spec.Options.Vxlan))
		}
		if err != nil {
			return err
		}
		vxlan.Group = mcastIP
	}
	err = addLink(vxlan, dnet.Spec.NetworkID)
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

// Note: allowing lowering MTUs on existing networks rests upon webhook rejecting such changes when the network has connected Pods
// Theoretically such a Pod could be instantiated after admission but before we get here, however this edge case is currently not worth validating
func shouldWeResizeExistingLink(link netlink.Link, parent netlink.Link, dnet *danmtypes.DanmNet) bool {
	desired := mtu.GetMtuForNet(dnet)
	current := link.Attrs().MTU
	if desired == current {
		return false
	}
	if err := mtu.ValidateForDev(dnet, parent.Attrs().MTU, parent.Attrs().Name); err != nil {
		log.Println("INFO: cannot change the MTU of existing host interface for network:" + dnet.Spec.NetworkID + " because:" + err.Error())
		return false
	}
	return true
}

func resizeExistingLink(link netlink.Link, desiredMtu int) {
	if err := netlink.LinkSetMTU(link, desiredMtu); err != nil {
		log.Println("WARNING: changing MTU of existing host interface " + link.Attrs().Name + " from " + strconv.Itoa(link.Attrs().MTU) + " to " +
			strconv.Itoa(desiredMtu) + " failed:" + err.Error())
		return
	}
	log.Println("INFO: successfully changed MTU of existing host interface " + link.Attrs().Name + " from " +
		strconv.Itoa(link.Attrs().MTU) + " to " + strconv.Itoa(desiredMtu))
}
