module github.com/networkop/meshnet-cni

require (
	github.com/Microsoft/go-winio v0.4.11 // indirect
	github.com/containernetworking/cni v0.7.0-alpha1.0.20180926164302-777324ca6b08
	github.com/containernetworking/plugins v0.7.4
	github.com/davecgh/go-spew v1.1.1
	github.com/docker/distribution v2.7.0-rc.0.0.20181002220433-1cb4180b1a5b+incompatible // indirect
	github.com/docker/docker v0.7.3-0.20181009194718-82a4797499a6 // indirect
	github.com/docker/go-connections v0.4.0 // indirect
	github.com/docker/go-units v0.3.3 // indirect
	github.com/golang/protobuf v1.3.2
	github.com/gorilla/context v1.1.1 // indirect
	github.com/konsorten/go-windows-terminal-sequences v1.0.2 // indirect
	github.com/opencontainers/go-digest v1.0.0-rc1 // indirect
	github.com/opencontainers/image-spec v1.0.1 // indirect
	github.com/pkg/errors v0.8.1 // indirect
	github.com/redhat-nfvpe/koko v0.0.0-20191015061157-740c789b4501
	github.com/sirupsen/logrus v1.4.2
	github.com/stretchr/testify v1.4.0 // indirect
	github.com/vishvananda/netlink v1.0.0
	github.com/vishvananda/netns v0.0.0-20191106174202-0a2b9b5464df // indirect
	golang.org/x/crypto v0.0.0-20190701094942-4def268fd1a4 // indirect
	golang.org/x/net v0.0.0-20190813141303-74dc4d7220e7 // indirect
	golang.org/x/oauth2 v0.0.0-20190604053449-0f29369cfe45 // indirect
	golang.org/x/sys v0.0.0-20190813064441-fde4db37ae7a // indirect
	google.golang.org/appengine v1.6.1 // indirect
	google.golang.org/genproto v0.0.0-20190819201941-24fa4b261c55 // indirect
	google.golang.org/grpc v1.27.0
	k8s.io/api v0.0.0-20190820101039-d651a1528133 // indirect
	k8s.io/apimachinery v0.0.0-20190820100751-ac02f8882ef6
	k8s.io/client-go v12.0.0+incompatible
	sigs.k8s.io/kind v0.2.1 // indirect
)

replace k8s.io/client-go => k8s.io/client-go v0.0.0-20190620085101-78d2af792bab
