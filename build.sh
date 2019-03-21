#!/bin/bash

if [ "$#" -ne 1 ]; then
    echo "Must provide docker hub username"
    exit 1
fi

set -e
set -x
export DOCKER_BUILDKIT=1
docker build -t meshnet -f docker/Dockerfile .
docker image tag meshnet $1/meshnet
#docker image push $1/meshnet

rm -rf ./cache


exit 0

