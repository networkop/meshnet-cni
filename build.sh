#!/usr/bin/env bash

if [ "$#" -ne 1 ]; then
    echo "Must provide docker hub username"
    exit 1
fi

set -e
set -x


mkdir -p "${PWD}/bin"
export PATH="$HOME/.kubeadm-dind-cluster:$PATH"

go build -o bin/meshnet plugin/meshnet.go
CGO_ENABLED=0  go build -o bin/meshnetd daemon/main.go daemon/handler.go daemon/daemon.go

docker build -t meshnetd .
docker image tag meshnetd $1/meshnetd
docker image push $1/meshnetd

cat daemon/meshnetd.yml  | sed "s/IMAGE_NAME/$1\/meshnetd/" | kubectl delete -f daemon/meshnetd.yml || true
cat daemon/meshnetd.yml  | sed "s/IMAGE_NAME/$1\/meshnetd/" | kubectl create -f -

#
# Local environment delete before release
#
# Get the IP of etcd service
ETCD_HOST=$(kubectl get service etcd-client -o json |  jq -r '.spec.clusterIP')

# Update CNI .conf file with the new service IP
jq --arg etcd_host "$ETCD_HOST" '.etcd_host = $etcd_host' etc/cni/net.d/meshnet.conf > etc/cni/net.d/01-meshnet.conf

for container in kube-master kube-node-1 kube-node-2
do
    # Copy local files to each k8s container
    sudo cp bin/meshnet /var/lib/docker/volumes/kubeadm-dind-$container/_data/
    sudo cp etc/cni/net.d/01-meshnet.conf /var/lib/docker/volumes/kubeadm-dind-$container/_data/
    docker exec $container cp /dind/meshnet /opt/cni/bin/
    docker exec $container cp /dind/01-meshnet.conf /etc/cni/net.d/
    docker exec $container bash -c "jq  -s '.[1].delegate = (.[0]|del(.cniVersion))' /etc/cni/net.d/cni.conf /etc/cni/net.d/01-meshnet.conf  | jq .[1] > /etc/cni/net.d/00-meshnet.conf"
done

rm etc/cni/net.d/01-meshnet.conf

