DOCKER_IMAGE := networkop/meshnet
GOPATH ?= ${HOME}/go/bin
ARCHS := "linux/amd64,linux/arm64"
#ARCHS := "linux/amd64"

COMMIT := $(shell git describe --dirty --always)
TAG := $(shell git describe --tags --dirty || echo latest)


include .mk/kind.mk
include .mk/ci.mk
include .mk/kustomize.mk
include .mk/buf.mk

.PHONY: all
all: docker

## Run unit tests
test:
	go test ./...

# Build local binaries
local-build:
	CGO_ENABLED=0 GOOS=linux go build -o meshnet plugin/meshnet.go 
	CGO_ENABLED=0 GOOS=linux go build -o meshnetd daemon/*.go

.PHONY: docker
## Build the docker image
docker:
	@echo 'Creating docker image ${DOCKER_IMAGE}:${COMMIT}'
	@docker buildx create --use --name=multiarch --node multiarch && \
	docker buildx build --load \
	  --build-arg LDFLAGS=${LDFLAGS} \
	  --platform "linux/amd64" \
	  --tag ${DOCKER_IMAGE}:${COMMIT} \
	  -f docker/Dockerfile \
	  .

.PHONY: release
## Release the current code with git tag and `latest`
release: 
	docker buildx build --push \
		--build-arg LDFLAGS=${LDFLAGS} \
		--platform ${ARCHS} \
		-t ${DOCKER_IMAGE}:${TAG} \
		-t ${DOCKER_IMAGE}:latest \
		.

## Generate GRPC code
proto: buf-generate

## Targets below are for integration testing only

.PHONY: up
## Build test environment
up: kind-start

.PHONY: down
## Desroy test environment
down: kind-stop

.PHONY: e2e
## Run the end-to-end test
e2e: wait-for-meshnet
	kubectl apply -f tests/3node.yml
	kubectl wait --timeout=120s --for condition=Ready pod -l test=3node 
	kubectl exec r1 -- ping -c 1 12.12.12.1
	kubectl exec r1 -- ping -c 1 13.13.13.3
	kubectl exec r2 -- ping -c 1 23.23.23.3

wait-for-meshnet:
	kubectl wait --for condition=Ready pod -l name=meshnet -n meshnet   
	sleep 5

.PHONY: install
## Install meshnet into a test cluster
install: kind-load kind-wait-for-cni kustomize kind-connect
	kustomize build manifests/overlays/e2e  | kubectl apply -f -

.PHONY: uninstall
## Uninstall meshnet from a test cluster
uninstall: kind-connect
	-kustomize build manifests/overlays/e2e  | kubectl delete -f -

github-ci: kust-ensure build clean local upload install e2e


# From: https://gist.github.com/klmr/575726c7e05d8780505a
help:
	@echo "$$(tput sgr0)";sed -ne"/^## /{h;s/.*//;:d" -e"H;n;s/^## //;td" -e"s/:.*//;G;s/\\n## /---/;s/\\n/ /g;p;}" ${MAKEFILE_LIST}|awk -F --- -v n=$$(tput cols) -v i=15 -v a="$$(tput setaf 6)" -v z="$$(tput sgr0)" '{printf"%s%*s%s ",a,-i,$$1,z;m=split($$2,w," ");l=n-i;for(j=1;j<=m;j++){l-=length(w[j])+1;if(l<= 0){l=n-i-length(w[j])-1;printf"\n%*s ",-i," ";}printf"%s ",w[j];}printf"\n";}'
