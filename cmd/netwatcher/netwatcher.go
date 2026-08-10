package main

import (
	"flag"
	"log"
	"os"

	"github.com/danm-cni/danm/pkg/datastructs"
	"github.com/danm-cni/danm/pkg/netcontrol"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	version, commitHash string
)

func getClientConfig(kubeConfig *string) (*rest.Config, error) {
	if kubeConfig != nil {
		return clientcmd.BuildConfigFromFlags("", *kubeConfig)
	}
	return rest.InClusterConfig()
}

func main() {
	printVersion := flag.Bool("version", false, "prints Git version information of the binary to standard out")
	flag.Parse()
	if *printVersion {
		log.Println("DANM binary was built from release: " + version)
		log.Println("DANM binary was built from commit: " + commitHash)
		return
	}
	log.SetOutput(os.Stdout)
	log.Println("Starting DANM NetWatcher...")
	kubeConfig := flag.String("kubeconf", "", "Path to a kube config. Only required if out-of-cluster.")
	sourceLearning := flag.Bool("multicast", false, "Controls whether VxLAN VTEPs are created with multicast source learning enabled. Default is false.")
	flag.Parse()
	restConfig, err := getClientConfig(kubeConfig)
	if err != nil {
		log.Println("ERROR: Parsing kubeconfig failed with error:" + err.Error() + " , exiting")
		os.Exit(-1)
	}
	netWatcherConfig := datastructs.NetwatcherConfig{
		RestConfig:     restConfig,
		SourceLearning: *sourceLearning,
	}
	stopCh := make(chan struct{})
	netWatcher, err := netcontrol.NewWatcher(netWatcherConfig, &stopCh)
	if err != nil {
		log.Println("ERROR: Creation of NetWatcher failed with error:" + err.Error() + " , exiting")
		os.Exit(-1)
	}
	netWatcher.Run(&stopCh)
	select {}
}
