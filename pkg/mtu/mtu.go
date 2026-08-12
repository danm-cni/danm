package mtu

import (
	"errors"
	"strconv"

	danmtypes "github.com/danm-cni/danm/crd/apis/danm/v1"
)

const (
	// VxlanOverhead is the outer-header cost for VXLAN over an IPv4 underlay:
	// inner Ethernet (14) + outer IPv4 (20) + UDP (8) + VXLAN (8) = 50 bytes.
	VxlanOverhead = 50
	// VxlanOverheadV6 is the outer-header cost for VXLAN over an IPv6 underlay:
	// inner Ethernet (14) + outer IPv6 (40) + UDP (8) + VXLAN (8) = 70 bytes.
	VxlanOverheadV6 = 70
	DefaultMtu      = 1500
)

// ValidateForDev checks that an already-resolved MTU can actually be
// configured on the underlying host device, including possible VxLAN overhead
// (VxlanOverhead/VxlanOverheadV6). A devMTU <= 0 means the device MTU could not be determined,
// so there is nothing to validate against and the caller decides what that means. No clamping is
// performed: if the requested MTU does not fit, a descriptive error is
// returned so the operation can FAIL loudly instead of silently producing a
// mixed-MTU network.
func ValidateForDev(dnet *danmtypes.DanmNet, devMTU int, devName string) error {
	if devMTU <= 0 {
		return nil
	}
	overhead := calculateOverheadForNet(dnet)
	usable := devMTU - overhead
	if GetMtuForNet(dnet) <= usable {
		return nil
	}
	return errors.New("mtu " + strconv.Itoa(dnet.Spec.Options.Mtu) +
		" cannot be configured: host device " + devName + " has mtu " + strconv.Itoa(devMTU) +
		overheadSuffix(overhead) + " (usable " + strconv.Itoa(usable) +
		") - raise the device or lower the requested payload MTU")
}

func GetMtuForNet(dnet *danmtypes.DanmNet) int {
	if dnet.Spec.Options.Mtu == 0 {
		return DefaultMtu
	}
	return dnet.Spec.Options.Mtu
}

func overheadSuffix(overhead int) string {
	if overhead == 0 {
		return ""
	}
	return " minus " + strconv.Itoa(overhead) + " bytes encap overhead"
}

func calculateOverheadForNet(dnet *danmtypes.DanmNet) int {
	if dnet.Spec.Options.Vxlan == 0 {
		return 0
	}
	if dnet.Spec.Options.Net6 != "" {
		return VxlanOverheadV6
	}
	return VxlanOverhead
}
