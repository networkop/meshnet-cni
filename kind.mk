KIND_CLUSTER_NAME := "kind"

.PHONY: kind-install
kind-install: 
	GO111MODULE="on" go get -u sigs.k8s.io/kind@master

.PHONY: kind-stop
kind-stop: 
	@kind delete cluster --name $(KIND_CLUSTER_NAME) || \
		echo "kind cluster is not running"

.PHONY: kind-ensure 
kind-ensure: 
	@which kind >/dev/null 2>&1 || \
		make kind-install

.PHONY: kind-start
kind-start: kind-ensure 
	@kind get clusters | grep $(KIND_CLUSTER_NAME)  >/dev/null 2>&1 || \
		kind create cluster --name "$(KIND_CLUSTER_NAME)" --config ./kind.yaml

