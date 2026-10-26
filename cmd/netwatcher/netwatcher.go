package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/danm-cni/danm/pkg/datastructs"
	"github.com/danm-cni/danm/pkg/netcontrol"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const shutdownTimeout = 10 * time.Second

var (
	version, commitHash string
)

func getClientConfig(kubeConfig string) (*rest.Config, error) {
	if kubeConfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeConfig)
	}
	return rest.InClusterConfig()
}

func main() {
	printVersion := flag.Bool("version", false, "prints Git version information of the binary to standard out")
	kubeConfig := flag.String("kubeconf", "", "Path to a kube config. Only required if out-of-cluster.")
	sourceLearning := flag.Bool("multicast", false, "Controls whether VxLAN VTEPs are created with multicast source learning enabled. Default is false.")
	flag.Parse()
	if *printVersion {
		log.Println("DANM binary was built from release: " + version)
		log.Println("DANM binary was built from commit: " + commitHash)
		return
	}
	log.SetOutput(os.Stdout)
	log.Println("Starting DANM NetWatcher...")
	restConfig, err := getClientConfig(*kubeConfig)
	if err != nil {
		log.Println("ERROR: Parsing kubeconfig failed with error:" + err.Error() + " , exiting")
		os.Exit(-1)
	}
	netWatcherConfig := datastructs.NetwatcherConfig{
		RestConfig:     restConfig,
		SourceLearning: *sourceLearning,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	netWatcher, err := netcontrol.NewWatcher(ctx, netWatcherConfig)
	if err != nil {
		log.Println("ERROR: Creation of NetWatcher failed with error:" + err.Error() + " , exiting")
		os.Exit(-1)
	}
	err = netWatcher.Run(ctx)
	if err != nil {
		log.Println("ERROR: Starting NetWatcher failed with error:" + err.Error() + " , exiting")
		os.Exit(-1)
	}
	log.Println("INFO: NetWatcher caches are synced, watching for network changes")
	<-ctx.Done()
	log.Println("INFO: Termination signal received, shutting down NetWatcher")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	err = netWatcher.Shutdown(shutdownCtx)
	if err != nil {
		log.Println("WARNING: NetWatcher did not shut down gracefully:" + err.Error())
	}
}
