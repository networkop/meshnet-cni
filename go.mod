module github.com/networkop/meshnet-cni

go 1.16

require (
	github.com/containernetworking/cni v0.8.1
	github.com/containernetworking/plugins v0.9.1
	github.com/davecgh/go-spew v1.1.1
	github.com/grpc-ecosystem/go-grpc-middleware v1.3.0
	github.com/redhat-nfvpe/koko v0.0.0-20210415181932-a18aa44814ea
	github.com/sirupsen/logrus v1.8.1
	github.com/vishvananda/netlink v1.1.1-0.20201029203352-d40f9887b852
	google.golang.org/grpc v1.38.0
	google.golang.org/protobuf v1.26.0
	k8s.io/apimachinery v0.21.1
	k8s.io/client-go v0.21.1
)
