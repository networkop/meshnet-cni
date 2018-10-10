#!/usr/bin/env bash

set -e
set -x

ETCD_HOST=$(kubectl get service etcd-client -o json |  jq -r '.spec.clusterIP')
ENDPOINTS=$ETCD_HOST:2379


# Copy json files to kube-master
sudo cp tests/*.json /var/lib/docker/volumes/kubeadm-dind-kube-master/_data/

# Copy etcdctl to kube-master
sudo cp utils/etcdctl /var/lib/docker/volumes/kubeadm-dind-kube-master/_data/
docker exec kube-master cp /dind/etcdctl /usr/local/bin/

for pod in pod1 pod2 pod3
do
    # First cleanup any existing state
    docker exec -it kube-master sh -c "ETCDCTL_API=3 etcdctl --endpoints=$ENDPOINTS del --prefix=true \"/$pod\""

    # Next Update the links database
    docker exec -it kube-master sh -c "cat /dind/$pod.json | ETCDCTL_API=3 etcdctl --endpoints=$ENDPOINTS put /$pod/links"

    # Print the contents of links databse
    docker exec -it kube-master sh -c "ETCDCTL_API=3 etcdctl --endpoints=$ENDPOINTS get --prefix=true \"/$pod\""
done
