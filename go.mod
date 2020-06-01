module github.com/networkop/meshnet-cni

require (
	github.com/Microsoft/go-winio v0.4.11 // indirect
	github.com/containernetworking/cni v0.7.0-alpha1.0.20180926164302-777324ca6b08
	github.com/containernetworking/plugins v0.7.4
	github.com/davecgh/go-spew v1.1.1
	github.com/docker/go-connections v0.4.0 // indirect
	github.com/gorilla/context v1.1.1 // indirect
	github.com/konsorten/go-windows-terminal-sequences v1.0.2 // indirect
	github.com/morikuni/aec v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.0.1 // indirect
	github.com/redhat-nfvpe/koko v0.0.0-20191015061157-740c789b4501
	github.com/sirupsen/logrus v1.4.2
	github.com/vishvananda/netlink v1.0.0
	github.com/vishvananda/netns v0.0.0-20191106174202-0a2b9b5464df // indirect
	golang.org/x/oauth2 v0.0.0-20190604053449-0f29369cfe45 // indirect
	google.golang.org/appengine v1.6.1 // indirect
	google.golang.org/grpc v1.27.0
	k8s.io/apimachinery v0.17.0
	k8s.io/client-go v12.0.0+incompatible
	sigs.k8s.io/kind v0.2.1 // indirect
	sigs.k8s.io/kustomize/kustomize/v3 v3.6.1 // indirect
)

replace k8s.io/client-go => k8s.io/client-go v0.0.0-20190620085101-78d2af792bab

go 1.13
