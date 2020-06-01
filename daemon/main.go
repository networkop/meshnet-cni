package main

import (
	"flag"

	log "github.com/sirupsen/logrus"
)

func main() {

	isDebug := flag.Bool("d", false, "enable degugging")
	flag.Parse()

	if *isDebug {
		log.SetLevel(log.DebugLevel)
		log.Debug("Verbose logging enabled")
	} else {
		log.SetLevel(log.InfoLevel)
	}

	kubeClient, err := newDynamicClient()
	if err != nil {
		log.Fatal("Failed to connect to K8s API")
	}

	config := loadConfigVars()

	meshnetd := &meshnetd{
		config: config,
		kube:   kubeClient,
	}

	go func() {
		err := meshnetd.Start()
		if err != nil {
			log.Fatal("Failed to start meshnet daemon")
		}
	}()

	log.Info("meshnet daemon has started...")
	// Wait forever
	ch := make(chan struct{})
	<-ch
}
