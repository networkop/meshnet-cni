

k3d-up:
	k3d create cluster --no-lb -w 2 -v /tmp/var:/var/run/netns:shared
	KUBECONFIG=$(shell k3d get kubeconfig k3s-default) kubectl config view --minify --flatten --context=k3d-k3s-default > kubeconfig

k3d-down:
	k3d delete cluster k3s-default

k3d-load:
	k3d load image networkop/meshnet:k3d-test

k3d-install:
	KUBECONFIG=$(shell k3d get kubeconfig k3s-default) kubectl apply -k manifests/base/

k3d-show:
	KUBECONFIG=$(shell k3d get kubeconfig k3s-default) kubectl get pod -n meshnet

k3d-test:
	KUBECONFIG=$(shell k3d get kubeconfig k3s-default) kubectl apply -f tests/3node.yml
	KUBECONFIG=$(shell k3d get kubeconfig k3s-default) kubectl wait --timeout=120s --for condition=Ready pod -l test=3node 
	KUBECONFIG=$(shell k3d get kubeconfig k3s-default) kubectl exec r1 -- ping -c 1 12.12.12.2
	KUBECONFIG=$(shell k3d get kubeconfig k3s-default) kubectl exec r1 -- ping -c 1 13.13.13.3
	KUBECONFIG=$(shell k3d get kubeconfig k3s-default) kubectl exec r2 -- ping -c 1 23.23.23.3