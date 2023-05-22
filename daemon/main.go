package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"strconv"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/networkop/meshnet-cni/daemon/cni"
	"github.com/networkop/meshnet-cni/daemon/grpcwire"
	"github.com/networkop/meshnet-cni/daemon/meshnet"
	mpb "github.com/networkop/meshnet-cni/daemon/proto/meshnet/v1beta1"
	"github.com/networkop/meshnet-cni/daemon/vxlan"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	defaultPort = 51111
	httpPort    = "51112"
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

	meshnet.InitLogger()
	grpcwire.InitLogger()
	vxlan.InitLogger()

	m, err := meshnet.New(meshnet.Config{
		Port: grpcPort,
	})
	if err != nil {
		log.Errorf("Failed to create meshnet: %v", err)
		os.Exit(1)
	}
	log.Info("Starting meshnet daemon...with grpc support")

	// Start meshnet in a goroutine b/c we have more to do.
	go m.Serve()
	log.Info("Server started.")

	// create a grcp client
	conn, err := grpc.DialContext(
		context.Background(),
		"0.0.0.0:"+strconv.Itoa(grpcPort),
		grpc.WithBlock(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalln("Failed to dail gRPC server:", err)
	}
	log.Infof("Connected to grpc server on %d", grpcPort)

	// Create an HTTP service for the Grafana service handler and fire it up
	gwmux := runtime.NewServeMux()
	err = mpb.RegisterGrafanaHandler(context.Background(), gwmux, conn)
	if err != nil {
		log.Fatalln("Failed to register HTTP gateway:", err)
	}
	gwServer := &http.Server{
		Addr:    ":" + httpPort,
		Handler: gwmux,
	}
	log.Infof("Serving gRPC-Gateway on :%s", httpPort)
	log.Fatalln(gwServer.ListenAndServe())
}
