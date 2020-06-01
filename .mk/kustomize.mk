KIND_CLUSTER_NAME := "kind"
GOPATH = ${HOME}/go/bin

.PHONY: kust-install
kust-install: 
	curl -LO https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2Fv3.5.4/kustomize_v3.5.4_linux_amd64.tar.gz && \
	tar zxvf kustomize_v3.5.4_linux_amd64.tar.gz && mv kustomize $(GOPATH)/

.PHONY: kust-ensure 
kust-ensure: 
	@which kustomize >/dev/null 2>&1 || \
		make kust-install

.PHONY: kustomize
kustomize: kust-ensure 
	@cd manifests/base/ && $(GOPATH)/kustomize edit set image $(DOCKERID)/meshnet:$(VERSION)
	kubectl apply -k manifests/base/

.PHONY: kustomize-kops
kustomize-kops: kust-ensure 
	ubectl apply -k manifests/overlays/kops/ 