set -x
 
cd utils

wget -N https://raw.githubusercontent.com/kubernetes-sigs/kubeadm-dind-cluster/master/fixed/dind-cluster-v1.11.sh 

chmod +x ./dind-cluster-v1.11.sh 
 
export PATH="$HOME/.kubeadm-dind-cluster:$PATH"

./dind-cluster-v1.11.sh down
 
EXTRA_PORTS=32379 ./dind-cluster-v1.11.sh up

ETCD_PORT=$(docker inspect kube-node-1 --format='{{ (index (index .NetworkSettings.Ports "32379/tcp") 0).HostPort }}')

echo "export ENDPOINTS=127.0.0.1:$ETCD_PORT"
