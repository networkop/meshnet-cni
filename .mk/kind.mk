# KIND cluster name
KIND_CLUSTER_NAME ?= $(BIN)
export KUBECONFIG=kubeconfig


.PHONY: kind-install
kind-install: 
	GO111MODULE="on" go get sigs.k8s.io/kind@v0.7.0

.PHONY: kind-stop
kind-stop: 
	@$(GOPATH)/kind delete cluster --name $(KIND_CLUSTER_NAME) || \
		echo "kind cluster is not running"

.PHONY: kind-ensure 
kind-ensure: 
	@which $(GOPATH)/kind >/dev/null 2>&1 || \
		make kind-install

.PHONY: kind-start
kind-start: kind-ensure 
	@$(GOPATH)/kind get clusters | grep $(KIND_CLUSTER_NAME)  >/dev/null 2>&1 || \
		$(GOPATH)/kind create cluster --name $(KIND_CLUSTER_NAME) --kubeconfig kubeconfig --config ./kind.yaml

.PHONY: kind-wait-for-cni
kind-wait-for-cni:
	kubectl wait --timeout=60s --for condition=Ready pod -l app=kindnet -n kube-system