package main

import (
	"flag"
	"os"
	"strconv"

	"github.com/networkop/meshnet-cni/daemon/cni"
	"github.com/networkop/meshnet-cni/daemon/meshnet"
	log "github.com/sirupsen/logrus"
)

const (
	defaultPort = 51111
)

func main() {

	if err := cni.Init(); err != nil {
		log.Errorf("Failed to initialise CNI plugin: %v", err)
		os.Exit(1)
	}
	defer cni.Cleanup()

	isDebug := flag.Bool("d", false, "enable degugging")
	grpcPort, err := strconv.Atoi(os.Getenv("GRPC_PORT"))
	if err != nil || grpcPort == 0 {
		grpcPort = defaultPort
	}
	flag.Parse()
	log.SetLevel(log.InfoLevel)
	if *isDebug {
		log.SetLevel(log.DebugLevel)
		log.Debug("Verbose logging enabled")
	}

	m, err := meshnet.New(meshnet.Config{
		Port: grpcPort,
	})
	if err != nil {
		log.Errorf("Failed to create meshnet: %v", err)
		os.Exit(1)
	}
	log.Info("Starting meshnet daemon...with grpc support")

	if err := m.Serve(); err != nil {
		log.Errorf("Daemon exited badly: %v", err)
		os.Exit(1)
	}
}
