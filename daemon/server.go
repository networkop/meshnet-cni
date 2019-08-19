package main

import (
	"fmt"
	"log"
	"net"

	pb "github.com/networkop/meshnet-cni/daemon/generated"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"k8s.io/client-go/dynamic"
)

type meshnetd struct {
	config *meshnetConf
	kube   dynamic.Interface
}

func (s *meshnetd) Start() error {
	grpcPort := s.config.GRPCPort
	log.Printf("Trying to listen on GRPC port %d", grpcPort)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", grpcPort))
	if err != nil {
		log.Fatalf("Failed to bind grpc listener: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterLocalServer(grpcServer, s)
	pb.RegisterRemoteServer(grpcServer, s)
	reflection.Register(grpcServer)

	go grpcServer.Serve(lis)
	log.Printf("GRPC server has started")

	// Space to add HTTP REST server

	return nil
}
