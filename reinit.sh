set -x
cd utils
wget -N https://raw.githubusercontent.com/kubernetes-sigs/kubeadm-dind-cluster/master/fixed/dind-cluster-v1.11.sh 
chmod +x ./dind-cluster-v1.11.sh 
export PATH="$HOME/.kubeadm-dind-cluster:$PATH"

./dind-cluster-v1.11.sh down
./dind-cluster-v1.11.sh up

kubectl taint nodes --all node-role.kubernetes.io/master-
cd ../
