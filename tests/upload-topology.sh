#!/usr/bin/env bash

set -e
set -x

ETCD_HOST=$(kubectl get service etcd-client -o json |  jq -r '.spec.clusterIP')
ENDPOINTS=$ETCD_HOST:2379

echo "Copying JSON files to kube-master"
sudo cp tests/*.json /var/lib/docker/volumes/kubeadm-dind-kube-master/_data/

echo "Copying etcdctl to kube-master"
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


for container in kube-master kube-node-1 kube-node-2
do
    # Merge the default CNI plugin with meshnet
    docker exec $container bash -c "jq  -s '.[1].delegate = (.[0]|del(.cniVersion))' /etc/cni/net.d/cni.conf /etc/cni/net.d/meshnet.conf  | jq .[1] > /etc/cni/net.d/00-meshnet.conf"
    docker exec $container bash -c "sed -i 's/ETCD_HOST/$ETCD_HOST/' /etc/cni/net.d/00-meshnet.conf"
done

