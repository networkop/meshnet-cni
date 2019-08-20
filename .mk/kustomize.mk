KIND_CLUSTER_NAME := "kind"
GOPATH = ${HOME}/go/bin

.PHONY: kust-install
kust-install: 
	GO111MODULE="on" go install sigs.k8s.io/kustomize/v3/cmd/kustomize

.PHONY: kust-ensure 
kust-ensure: 
	@which $(GOPATH)/kustomize >/dev/null 2>&1 || \
		make kust-install

.PHONY: kustomize
kustomize: kust-ensure 
	@cd manifests/ && $(GOPATH)/kustomize edit set image $(DOCKERID)/meshnet:$(VERSION)
	@$(GOPATH)/kustomize build manifests | kubectl apply -f -

