export PATH="$HOME/.kubeadm-dind-cluster:$PATH"

./build.sh

tests/3node-alpine-test.sh


for test in 1test.yml 2test.yml 3test.yml
do  
    echo -e "\n############################"
    echo -e "# Running test : $test #"
    echo -e "############################\n"
    cat tests/$test | kubectl create -f -
    sleep 30
    
    kubectl exec pod1 -- sudo ping -c 1 12.12.12.2
    sleep 1
    kubectl exec pod1 -- sudo ping -c 1 13.13.13.3
    sleep 1
    kubectl exec pod2 -- sudo ping -c 1 23.23.23.3
    
    cat tests/$test | kubectl delete --grace-period=0 --force -f -
done