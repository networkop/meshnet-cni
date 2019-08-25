DOCKERID ?= networkop
CURRENT_DIR = $(shell pwd)
PROJECT_MODULE = github.com/networkop/meshnet-cni
KUBECONFIG = $(shell ${GOPATH}/kind get kubeconfig-path --name="kind")
GOPATH = ${HOME}/go/bin

ifdef GITHUB_REF
	BRANCH ?= $(shell echo ${GITHUB_REF} | cut -d'/' -f3)
else
	BRANCH ?= $(shell git branch | grep \* | cut -d ' ' -f2)
endif 

ifeq ($(BRANCH), master)
  VERSION ?= latest
else
  VERSION ?= $(BRANCH)
endif

export KUBECONFIG

include .mk/kind.mk
include .mk/kustomize.mk

.PHONY: build gengo test upload meshnet stuff local wait-for-meshnet ci-install ci-build uninstall

build: meshnet

local: kind-start
	
clean: kind-stop

gengo:
	sudo rm -rf ./pkg/client/*
	sudo rm -rf ./pkg/apis/networkop/v1beta1/zz_generated.deepcopy.go
	docker build -f ./hack/Dockerfile -t kubernetes-codegen:latest $(CURRENT_DIR)
	docker run --rm -v "${CURRENT_DIR}:/go/src/${PROJECT_MODULE}" \
           kubernetes-codegen:latest ./generate-groups.sh all \
		   $(PROJECT_MODULE)/pkg/client \
		   $(PROJECT_MODULE)/pkg/apis \
		   networkop:v1beta1

stuff:
	go run main.go

.PHONY: 
local-build:
	CGO_ENABLED=0 GOOS=linux go build -o meshnet plugin/meshnet.go plugin/kube.go
	CGO_ENABLED=0 GOOS=linux go build -o meshnetd daemon/*.go

upload:
	$(GOPATH)/kind load docker-image $(DOCKERID)/meshnet:$(VERSION)

meshnet:
	DOCKER_BUILDKIT=1 docker build -t meshnet -f docker/Dockerfile .
	docker image tag meshnet $(DOCKERID)/meshnet:$(VERSION)

release:
	docker image push $(DOCKERID)/meshnet:$(VERSION)

proto:
	rm -rf ./daemon/generated/meshnet.pb.go
	protoc -I daemon/definitions daemon/definitions/meshnet.proto \
	--go_out=plugins=grpc:daemon/generated/

test: wait-for-meshnet
	kubectl apply -f tests/3node.yml
	kubectl wait --timeout=60s --for condition=Ready pod -l test=3node 
	kubectl exec r1 -- ping -c 1 12.12.12.1
	kubectl exec r1 -- ping -c 1 13.13.13.3
	kubectl exec r2 -- ping -c 1 23.23.23.3

wait-for-meshnet:
	kubectl wait --for condition=Ready pod -l name=meshnet -n meshnet   

ci-install: kustomize

install: 
	kubectl apply -f manifests/meshnet.yml

uninstall:
	-kubectl delete -f manifests/meshnet.yml

github-ci: build clean local upload install test

