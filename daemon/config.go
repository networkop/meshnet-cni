package main

import (
	"os"
	"strconv"
)

type meshnetConf struct {
	GRPCPort int
}

func loadConfigVars() *meshnetConf {

	config := meshnetConf{}

	grpcPort, err := strconv.Atoi(os.Getenv("GRPC_PORT"))
	if grpcPort == 0 || err != nil {
		config.GRPCPort = defaultPort
	} else {
		config.GRPCPort = grpcPort
	}

	return &config
}
