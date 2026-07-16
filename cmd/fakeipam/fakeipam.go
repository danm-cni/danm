package main

import (
	"encoding/json"
	"errors"
	"net"
	"strconv"

	"github.com/containernetworking/cni/pkg/skel"
	current "github.com/containernetworking/cni/pkg/types/040"
	"github.com/danm-cni/danm/pkg/datastructs"
)

//Fakeipam plugin is a very simple CNI-style IPAM plugin, which prints the received IP allocation information to its output.
//It is used by DANM to integrate to 3rd-party CNI plugins until its own IPAM plugin is separated into a srandalone binary.
//First, DANM internally handles IPAM duties, then invokes the 3rd-party CNI (e.g. SRIOV) with the full CNI config.
//3rd-party CNIs would invoke the configured fakeipam plugin according to the CNI interface specification.
//At the end, fakeipam will simply regurgitate the IP allocation information originally coming from DANM.

type cniConfig struct {
	Ipam datastructs.IpamConfig `json:"ipam"`
}

func reserveIp(args *skel.CmdArgs) error {
	ipamConf, err := loadIpamConfig(args.StdinData)
	if err != nil {
		return err
	}
	cniRes, err := createCniResult(ipamConf)
	if err != nil {
		return err
	}
	return cniRes.Print()
}

func loadIpamConfig(rawConfig []byte) (datastructs.IpamConfig, error) {
	cniConf := cniConfig{}
	err := json.Unmarshal(rawConfig, &cniConf)
	if err != nil {
		return datastructs.IpamConfig{}, err
	}
	if len(cniConf.Ipam.Ips) == 0 {
		return datastructs.IpamConfig{}, errors.New("No IP was passed to fake IPAM")
	}
	return cniConf.Ipam, nil
}

func createCniResult(ipamConf datastructs.IpamConfig) (*current.Result, error) {
	var resultIPs = []*current.IPConfig{}
	for _, ipamIp := range ipamConf.Ips {
		ip, ipNet, err := net.ParseCIDR(ipamIp.IpCidr)
		if err != nil {
			return &current.Result{}, errors.New("Unable to parse the given IpamConfig.IpCidr: " + ipamIp.IpCidr)
		}
		ipNet.IP = ip
		resultIPs = append(resultIPs, &current.IPConfig{Version: strconv.Itoa(ipamIp.Version), Address: *ipNet})
	}
	cniRes := &current.Result{CNIVersion: "0.3.1", IPs: resultIPs}
	return cniRes, nil
}

func freeIp(args *skel.CmdArgs) error {
	return nil
}

func checkIp(args *skel.CmdArgs) error {
	return nil
}

func main() {
	skel.PluginMain(reserveIp, checkIp, freeIp, datastructs.SupportedCniVersions, "")
}
