KIND_CLUSTER_NAME := "kind"
GOPATH = ${HOME}/go/bin

.PHONY: kust-install
kust-install: 
	GO111MODULE="on" go install sigs.k8s.io/kustomize/kustomize/v3@v3.5.4

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