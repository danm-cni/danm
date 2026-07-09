package main

import (
	"crypto/tls"
	"flag"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/danm-cni/danm/pkg/admit"
)

var (
	version, commitHash string
)

func main() {
	cert := flag.String("tls-cert-bundle", "", "file containing the x509 Certificate for HTTPS. (CA cert, if any, concatenated after server cert).")
	key := flag.String("tls-private-key-file", "", "file containing the x509 private key matching --tls-cert-bundle.")
	port := flag.Int("bind-port", 8443, "the port on which to serve. Default is 8443.")
	address := flag.String("bind-address", "", "the IP address on which to listen. Default is all interfaces.")
	printVersion := flag.Bool("version", false, "prints Git version information of the binary to standard out")
	flag.Parse()
	if *printVersion {
		log.Println("DANM binary was built from release: " + version)
		log.Println("DANM binary was built from commit: " + commitHash)
		return
	}
	if cert == nil || key == nil {
		log.Println("ERROR: Configuring TLS is mandatory, --tls-cert-bundle and --tls-private-key-file cannot be empty!")
		return
	}
	tlsConf, err := tls.LoadX509KeyPair(*cert, *key)
	if err != nil {
		log.Println("ERROR: TLS configuration could not be initialized, because:" + err.Error())
		return
	}
	validator, err := admit.CreateNewValidator()
	if err != nil {
		log.Println("ERROR: Cannot create DANM REST client, because:" + err.Error())
		return
	}
	http.HandleFunc("/netvalidation", validator.ValidateNetwork)
	http.HandleFunc("/confvalidation", validator.ValidateTenantConfig)
	http.HandleFunc("/netdeletion", validator.DeleteNetwork)
	server := &http.Server{
		Addr:         *address + ":" + strconv.Itoa(*port),
		TLSConfig:    &tls.Config{Certificates: []tls.Certificate{tlsConf}},
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}
	log.Println("INFO:DANM webhook is about to start listening on " + *address + ":" + strconv.Itoa(*port))
	err = server.ListenAndServeTLS("", "")
	log.Fatal(err)
}
