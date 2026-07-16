package main

import (
	"encoding/json"
	"errors"
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
		return errors.New("could not read expected CNI config from disk, because:" + err.Error())
	}
	err = json.Unmarshal(expectedCniConf, &tcConf)
	if err != nil {
		return errors.New("could not unmarshal test CNI config, because:" + err.Error())
	}
	err = checkEnvVars(tcConf.Env)
	if err != nil {
		return errors.New("ENV variables were not set to expected value:" + err.Error())
	}
	if tcConf.CniExpectations.CniType == "sriov" {
		err = validateSriovConfig(args.StdinData, expectedCniConf)
	} else if tcConf.CniExpectations.CniType == "macvlan" {
		err = validateMacvlanConfig(args.StdinData, expectedCniConf, tcConf)
	} else if tcConf.CniExpectations.CniType == "flannel" {
		err = validateFlannelConfig(args.StdinData, expectedCniConf)
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
			return errors.New("expected env value:" + expValue + " for key:" + key + " does not match observed env value:" + realValue)
		}
	}
	return nil
}

func validateSriovConfig(receivedCniConfig, expectedCniConfig []byte) error {
	var recSriovConf cnidel.SriovNet
	err := json.Unmarshal(receivedCniConfig, &recSriovConf)
	if err != nil {
		return errors.New("Received SR-IOV config could not be unmarshalled, because:" + err.Error())
	}
	log.Printf("Received SR-IOV config:%v", recSriovConf)
	var expSriovConf SriovCniTestConfig
	err = json.Unmarshal(expectedCniConfig, &expSriovConf)
	if err != nil {
		return errors.New("Expected SR-IOV config could not be unmarshalled, because:" + err.Error())
	}
	log.Printf("Expected SR-IOV config:%v", expSriovConf.CniConf)
	if !reflect.DeepEqual(recSriovConf, expSriovConf.CniConf) {
		return errors.New("Received SR-IOV delegate configuration does not match with expected!")
	}
	return nil
}

func validateMacvlanConfig(receivedCniConfig, expectedCniConfig []byte, tcConf TestConfig) error {
	var recMacvlanConf cnidel.MacvlanNet
	err := json.Unmarshal(receivedCniConfig, &recMacvlanConf)
	if err != nil {
		return errors.New("Received CNI config could not be unmarshalled, because:" + err.Error())
	}
	log.Printf("Received CNI config:%v", recMacvlanConf)
	var expMacvlanConf MacvlanCniTestConfig
	err = json.Unmarshal(expectedCniConfig, &expMacvlanConf)
	if err != nil {
		return errors.New("Expected CNI config could not be unmarshalled, because:" + err.Error())
	}
	if tcConf.CniExpectations.Ip6 != "" {
		if recMacvlanConf.Ipam.Ips == nil {
			return errors.New("Received CNI config does not contain IPv6 address under ipam section, but it shall!")
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
		return errors.New("Received delegate configuration does not match with expected!")
	}
	return nil
}

func validateFlannelConfig(receivedCniConfig, expectedCniConfig []byte) error {
	var recFlannelConf FlannelConf
	err := json.Unmarshal(receivedCniConfig, &recFlannelConf)
	if err != nil {
		return errors.New("Received Flannel config could not be unmarshalled, because:" + err.Error())
	}
	log.Printf("Received Flannel config:%v", recFlannelConf)
	var expFlannelConf FlannelCniTestConfig
	err = json.Unmarshal(expectedCniConfig, &expFlannelConf)
	if err != nil {
		return errors.New("Expected Flannel config could not be unmarshalled, because:" + err.Error())
	}
	log.Printf("Expected Flannel config:%v", expFlannelConf.CniConf)
	if !reflect.DeepEqual(recFlannelConf, expFlannelConf.CniConf) {
		return errors.New("Received Flannel delegate configuration does not match with expected!")
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
	if version == 4 {
		cniRes.IP4 = &ipConf
	} else if version == 6 {
		cniRes.IP6 = &ipConf
	}
}

func testDelete(args *skel.CmdArgs) error {
	var tcConf TestConfig
	expectedCniConf, err := os.ReadFile(cniTestConfigFile)
	if err != nil {
		return errors.New("DEL could not read expected CNI config from disk, because:" + err.Error())
	}
	err = json.Unmarshal(expectedCniConf, &tcConf)
	if err != nil {
		return errors.New("DEL could not unmarshal test CNI config, because:" + err.Error())
	}
	err = checkEnvVars(tcConf.Env)
	if err != nil {
		return errors.New("DEL ENV variables were not set to expected value:" + err.Error())
	}
	if tcConf.CniExpectations.CniType == "macvlan" {
		err = validateMacvlanConfig(args.StdinData, expectedCniConf, tcConf)
	} else if tcConf.CniExpectations.CniType == "flannel" {
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
	skel.PluginMain(testSetup, testCheck, testDelete, datastructs.SupportedCniVersions, "")
}
