package main

import (
	"flag"
	"log"
	"os"

	"github.com/danm-cni/danm/pkg/datastructs"
	"github.com/danm-cni/danm/pkg/metacni"

	"github.com/containernetworking/cni/pkg/skel"
)

var (
	version, commitHash string
)

func main() {
	printVersion := flag.Bool("version", false, "prints Git version information of the binary to standard out")
	flag.Parse()
	if *printVersion {
		log.Println("DANM binary was built from release: " + version)
		log.Println("DANM binary was built from commit: " + commitHash)
		return
	}
	var err error
	f, err := os.OpenFile("/var/log/danm.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0640)
	if err != nil {
		log.Println("ERROR: cannot create log file, because:" + err.Error())
	}
	defer f.Close()
	log.SetOutput(f)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	skel.PluginMain(metacni.CreateInterfaces, metacni.GetInterfaces, metacni.DeleteInterfaces, datastructs.SupportedCniVersions, "")
}
