VERSION  ?= 0.2.0
CNI_VERSION = v1beta1
CURRENT_DIR = $(shell pwd)
PROJECT_MODULE = github.com/networkop/meshnet-cni
KUBECONFIG = /home/null/.kube/kind-config-kind
DOCKERID = networkop

export KUBECONFIG

.PHONY: gengo test env upload meshnet stuff


all: proto meshnet

local: 
	-kind create cluster --config kind.yaml

clean:
	kind delete cluster

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

upload:
	CGO_ENABLED=0 GOOS=linux go build -o meshnet plugin/meshnet.go plugin/kube.go
	docker cp meshnet kind-worker:/opt/cni/bin

meshnet:
	DOCKER_BUILDKIT=1 docker build -t meshnet -f docker/Dockerfile .
	docker image tag meshnet $(DOCKERID)/meshnet:v$(VERSION)
	docker image push $(DOCKERID)/meshnet:v$(VERSION)


proto:
	rm -rf ./daemon/generated/meshnet.pb.go
	protoc -I daemon/definitions daemon/definitions/meshnet.proto \
	--go_out=plugins=grpc:daemon/generated/


test: env
	kubectl apply -f tests/3node.yml
	#while [[ $(kubectl get pods -l test=3node -o 'jsonpath={..status.conditions[?(@.type=="Ready")].status}') != "True" ]]; do echo "waiting for pod" && sleep 1; done
	kubectl exec r1 -- ping -c 1 12.12.12.1
	kubectl exec r1 -- ping -c 1 13.13.13.3
	kubectl exec r2 -- ping -c 1 23.23.23.3


install:
	kubectl apply -f manifests/meshnet.yml
