.PHONY: kust-install
kust-install: 
	go get sigs.k8s.io/kustomize/kustomize/v3

.PHONY: kust-ensure 
kust-ensure: 
	@which $(GOPATH)/bin/kustomize >/dev/null 2>&1 || \
		make kust-install

.PHONY: kustomize
kustomize: kust-ensure
	cd manifests/overlays/e2e && $(GOPATH)/bin/kustomize edit set image ${DOCKER_IMAGE}:${COMMIT}
	cd -
	cd manifests/overlays/grpc-link-e2e && $(GOPATH)/bin/kustomize edit set image ${DOCKER_IMAGE}:${COMMIT}


.PHONY: kustomize-kops
kustomize-kops: kust-ensure 
	kubectl apply -k manifests/overlays/kops/ 
