module github.com/networkop/meshnet-cni

go 1.17

require (
	github.com/containernetworking/cni v0.8.1
	github.com/containernetworking/plugins v0.9.1
	github.com/davecgh/go-spew v1.1.1
	github.com/fsnotify/fsnotify v1.4.9 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware v1.3.0
	github.com/redhat-nfvpe/koko v0.0.0-20210415181932-a18aa44814ea
	github.com/sirupsen/logrus v1.8.1
	github.com/vishvananda/netlink v1.1.1-0.20201029203352-d40f9887b852
	google.golang.org/grpc v1.45.0
	google.golang.org/protobuf v1.28.0
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b // indirect
	k8s.io/apimachinery v0.21.1
	k8s.io/client-go v0.21.1
)

require github.com/google/gopacket v1.1.19

require (
	github.com/Microsoft/go-winio v0.4.11 // indirect
	github.com/docker/distribution v0.0.0-20181024170156-93e082742a00 // indirect
	github.com/docker/docker v0.0.0-20181024220401-bc4c1c238b55 // indirect
	github.com/docker/go-connections v0.0.0-20180228141015-7395e3f8aa16 // indirect
	github.com/docker/go-units v0.4.0 // indirect
	github.com/go-logr/logr v0.4.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/google/go-cmp v0.5.7 // indirect
	github.com/google/gofuzz v1.1.0 // indirect
	github.com/googleapis/gnostic v0.4.1 // indirect
	github.com/imdario/mergo v0.3.5 // indirect
	github.com/json-iterator/go v1.1.10 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.1 // indirect
	github.com/opencontainers/go-digest v0.0.0-20170607195333-279bed98673d // indirect
	github.com/opencontainers/image-spec v0.0.0-20171030174740-d60099175f88 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/stretchr/testify v1.7.1 // indirect
	github.com/vishvananda/netns v0.0.0-20200728191858-db3c7e526aae // indirect
	golang.org/x/net v0.0.0-20220412020605-290c469a71a5 // indirect
	golang.org/x/oauth2 v0.0.0-20200107190931-bf48bf16ab8d // indirect
	golang.org/x/sys v0.0.0-20220412211240-33da011f77ad // indirect
	golang.org/x/term v0.0.0-20220411215600-e5f449aeb171 // indirect
	golang.org/x/text v0.3.7 // indirect
	golang.org/x/time v0.0.0-20210220033141-f8bda1e9f3ba // indirect
	golang.org/x/xerrors v0.0.0-20220411194840-2f41105eb62f // indirect
	google.golang.org/appengine v1.6.5 // indirect
	google.golang.org/genproto v0.0.0-20220414192740-2d67ff6cf2b4 // indirect
	gopkg.in/check.v1 v1.0.0-20200902074654-038fdea0a05b // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	k8s.io/api v0.21.1 // indirect
	k8s.io/cri-api v0.0.0-20191204094248-a6f63f369f6d // indirect
	k8s.io/klog v1.0.0 // indirect
	k8s.io/klog/v2 v2.8.0 // indirect
	k8s.io/kubernetes v1.14.6 // indirect
	k8s.io/utils v0.0.0-20201110183641-67b214c5f920 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.1.0 // indirect
	sigs.k8s.io/yaml v1.2.0 // indirect
)

replace github.com/networkop/meshnet-cni => ./
