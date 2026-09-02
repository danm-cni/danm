package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"reflect"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	types020 "github.com/containernetworking/cni/pkg/types/020"
	current "github.com/containernetworking/cni/pkg/types/040"
	"github.com/danm-cni/danm/pkg/cnidel"
	"github.com/danm-cni/danm/pkg/datastructs"
	"github.com/danm-cni/danm/pkg/metacni"
)

const (
	cniTestConfigFile = "/etc/cni/net.d/cnitest.conf"
)

type TestConfig struct {
	CniExpectations `json:"cniexp"`
}

type CniExpectations struct {
	CniType    string            `json:"cnitype"`
	Ip         string            `json:"ip,omitempty"`
	Ip6        string            `json:"ip6,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	ReturnType string            `json:"return,omitempty"`
}

type SriovCniTestConfig struct {
	CniConf cnidel.SriovNet `json:"cniconf"`
}

type MacvlanCniTestConfig struct {
	CniConf cnidel.MacvlanNet `json:"cniconf"`
}

type FlannelCniTestConfig struct {
	CniConf FlannelConf `json:"cniconf"`
}

type FlannelConf struct {
	Name     string          `json:"name"`
	Type     string          `json:"type"`
	Delegate FlannelDelegate `json:"delegate"`
}

type FlannelDelegate struct {
	IsHairPinMode bool `json:"hairpinMode"`
	IsDefGw       bool `json:"isDefaultGateway"`
}

func testSetup(args *skel.CmdArgs) error {
	var tcConf TestConfig
	expectedCniConf, err := os.ReadFile(cniTestConfigFile)
	if err != nil {
		return fmt.Errorf("could not read expected CNI config from disk, because: %w", err)
	}
	err = json.Unmarshal(expectedCniConf, &tcConf)
	if err != nil {
		return fmt.Errorf("could not unmarshal test CNI config, because: %w", err)
	}
	err = checkEnvVars(tcConf.Env)
	if err != nil {
		return fmt.Errorf("ENV variables were not set to expected value: %w", err)
	}
	switch tcConf.CniExpectations.CniType {
	case "sriov":
		err = validateSriovConfig(args.StdinData, expectedCniConf)
	case "macvlan":
		err = validateMacvlanConfig(args.StdinData, expectedCniConf, tcConf)
	case "flannel":
		err = validateFlannelConfig(args.StdinData, expectedCniConf)
	case "genericStatic":
		err = validateStaticConfig(args.StdinData, expectedCniConf)
	}
	if err != nil {
		return err
	}
	var cniRes types.Result
	if tcConf.CniExpectations.ReturnType == "" || tcConf.CniExpectations.ReturnType == "current" {
		cniRes = createCurrentCniResult(tcConf)
	} else {
		cniRes = createType020CniResult(tcConf)
	}
	return cniRes.Print()
}

func checkEnvVars(vars map[string]string) error {
	for key, expValue := range vars {
		realValue := os.Getenv(key)
		if realValue != expValue {
			return fmt.Errorf("expected env value: %v for key: %v does not match observed env value: %v", expValue, key, realValue)
		}
	}
	return nil
}

func validateSriovConfig(receivedCniConfig, expectedCniConfig []byte) error {
	var recSriovConf cnidel.SriovNet
	err := json.Unmarshal(receivedCniConfig, &recSriovConf)
	if err != nil {
		return fmt.Errorf("Received SR-IOV config could not be unmarshalled, because: %w", err)
	}
	log.Printf("Received SR-IOV config:%v", recSriovConf)
	var expSriovConf SriovCniTestConfig
	err = json.Unmarshal(expectedCniConfig, &expSriovConf)
	if err != nil {
		return fmt.Errorf("Expected SR-IOV config could not be unmarshalled, because: %w", err)
	}
	log.Printf("Expected SR-IOV config:%v", expSriovConf.CniConf)
	if !reflect.DeepEqual(recSriovConf, expSriovConf.CniConf) {
		return fmt.Errorf("Received SR-IOV delegate configuration does not match with expected!")
	}
	return nil
}

func validateMacvlanConfig(receivedCniConfig, expectedCniConfig []byte, tcConf TestConfig) error {
	var recMacvlanConf cnidel.MacvlanNet
	err := json.Unmarshal(receivedCniConfig, &recMacvlanConf)
	if err != nil {
		return fmt.Errorf("Received CNI config could not be unmarshalled, because: %w", err)
	}
	log.Printf("Received MACVLAN CNI config:%v", recMacvlanConf)
	var expMacvlanConf MacvlanCniTestConfig
	err = json.Unmarshal(expectedCniConfig, &expMacvlanConf)
	if err != nil {
		return fmt.Errorf("Expected CNI config could not be unmarshalled, because: %w", err)
	}
	if tcConf.CniExpectations.Ip6 != "" {
		if recMacvlanConf.Ipam.Ips == nil {
			return fmt.Errorf("Received CNI config does not contain IPv6 address under ipam section, but it shall!")
		}
		newIpamConfig := datastructs.IpamConfig{Type: "fakeipam"}
		for _, ip := range recMacvlanConf.Ipam.Ips {
			if ip.Version != 6 {
				newIpamConfig.Ips = append(newIpamConfig.Ips, ip)
			}
		}
		recMacvlanConf.Ipam = newIpamConfig
		log.Printf("Received config after IPv6 adjustment:%v", recMacvlanConf)
	}
	log.Printf("Expected config:%v", expMacvlanConf.CniConf)
	if !reflect.DeepEqual(recMacvlanConf, expMacvlanConf.CniConf) {
		return fmt.Errorf("Received delegate configuration does not match with expected!")
	}
	return nil
}

func validateFlannelConfig(receivedCniConfig, expectedCniConfig []byte) error {
	var recFlannelConf FlannelConf
	err := json.Unmarshal(receivedCniConfig, &recFlannelConf)
	if err != nil {
		return fmt.Errorf("Received Flannel config could not be unmarshalled, because: %w", err)
	}
	log.Printf("Received Flannel config:%v", recFlannelConf)
	var expFlannelConf FlannelCniTestConfig
	err = json.Unmarshal(expectedCniConfig, &expFlannelConf)
	if err != nil {
		return fmt.Errorf("Expected Flannel config could not be unmarshalled, because: %w", err)
	}
	log.Printf("Expected Flannel config:%v", expFlannelConf.CniConf)
	if !reflect.DeepEqual(recFlannelConf, expFlannelConf.CniConf) {
		return fmt.Errorf("Received Flannel delegate configuration does not match with expected!")
	}
	return nil
}

func validateStaticConfig(receivedCniConfig, expectedCniConfig []byte) error {
	var recStaticConf cnidel.MacvlanNet
	err := json.Unmarshal(receivedCniConfig, &recStaticConf)
	if err != nil {
		return fmt.Errorf("Received CNI config could not be unmarshalled, because: %w", err)
	}
	log.Printf("Received Static CNI config:%v", recStaticConf)
	var expStaticConf MacvlanCniTestConfig
	err = json.Unmarshal(expectedCniConfig, &expStaticConf)
	if err != nil {
		return fmt.Errorf("Expected CNI config could not be unmarshalled, because: %w", err)
	}
	log.Printf("Expected Static config:%v", expStaticConf.CniConf)
	if !reflect.DeepEqual(recStaticConf, expStaticConf.CniConf) {
		return fmt.Errorf("Received Static delegate configuration does not match with expected!")
	}
	return nil
}

func createCurrentCniResult(tcConf TestConfig) *current.Result {
	cniRes := current.Result{CNIVersion: "0.3.1"}
	if tcConf.CniExpectations.Ip != "" || tcConf.CniExpectations.Ip6 != "" {
		metacni.AddIfaceToResult("eth0", "hululululu", &cniRes)
	}
	if tcConf.CniExpectations.Ip != "" {
		metacni.AddIpToResult(tcConf.CniExpectations.Ip, "4", &cniRes)
	}
	if tcConf.CniExpectations.Ip6 != "" {
		metacni.AddIpToResult(tcConf.CniExpectations.Ip6, "6", &cniRes)
	}
	return &cniRes
}

func createType020CniResult(tcConf TestConfig) *types020.Result {
	cniRes := types020.Result{CNIVersion: "0.2.0"}
	if tcConf.CniExpectations.Ip != "" {
		addIpToType20(tcConf.CniExpectations.Ip, 4, &cniRes)
	}
	if tcConf.CniExpectations.Ip6 != "" {
		addIpToType20(tcConf.CniExpectations.Ip6, 6, &cniRes)
	}
	return &cniRes
}

func addIpToType20(ip string, version int, cniRes *types020.Result) {
	_, ipNet, _ := net.ParseCIDR(ip)
	ipConf := types020.IPConfig{IP: *ipNet}
	switch version {
	case 4:
		cniRes.IP4 = &ipConf
	case 6:
		cniRes.IP6 = &ipConf
	}
}

func testDelete(args *skel.CmdArgs) error {
	var tcConf TestConfig
	expectedCniConf, err := os.ReadFile(cniTestConfigFile)
	if err != nil {
		return fmt.Errorf("DEL could not read expected CNI config from disk, because: %w", err)
	}
	err = json.Unmarshal(expectedCniConf, &tcConf)
	if err != nil {
		return fmt.Errorf("DEL could not unmarshal test CNI config, because: %w", err)
	}
	err = checkEnvVars(tcConf.Env)
	if err != nil {
		return fmt.Errorf("DEL ENV variables were not set to expected value: %w", err)
	}
	switch tcConf.CniExpectations.CniType {
	case "macvlan":
		err = validateMacvlanConfig(args.StdinData, expectedCniConf, tcConf)
	case "flannel":
		err = validateFlannelConfig(args.StdinData, expectedCniConf)
	}
	return err
}

func testCheck(args *skel.CmdArgs) error {
	return nil
}

func main() {
	var err error
	f, err := os.OpenFile("/var/log/cnitest.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err == nil {
		log.SetOutput(f)
		defer f.Close()
	}
	cniTestFuncs := skel.CNIFuncs{
		Add:   testSetup,
		Check: testCheck,
		Del:   testDelete,
	}
	skel.PluginMainFuncs(cniTestFuncs, datastructs.SupportedCniVersions, "")
}
