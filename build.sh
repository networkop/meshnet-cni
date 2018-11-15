#!/usr/bin/env bash

if [ "$#" -ne 1 ]; then
    echo "Must provide docker hub username"
    exit 1
fi

set -e
set -x


# This is not used for dind demo but is required for kubespray demo
docker build -t meshnet -f docker/Dockerfile .
docker image tag meshnet $1/meshnet
docker image push $1/meshnet

exit 0

