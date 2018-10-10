# meshnet CNI

meshnet-cni - a (K8s) CNI plugin to create arbitrary network topologies out of point-to-point links with the help of (koko)[https://github.com/redhat-nfvpe/koko]. Heavily inspired by [Ratchet-CNI](https://github.com/dougbtv/ratchet-cni), [kokonet](https://github.com/s1061123/kokonet) and [Multus](https://github.com/intel/multus-cni).

## Architecture
The plugin consists of three main components:

* A CNI binary `meshnet` reponsible for network configuration of a pod
* A daemon `meshnetd` deployed as a daemonset and reponsible for Vxlan configuration updates after remote VTEP changes
* An `etcd` service storing topology information and runtime pod metadata (e.g. pod IP address and NetNS), which are consumed by the CNI binary

Here's a simplified overview of `meshnet` architecture from the perspective of `kube-node-1`.

![architecture](arch.png)

Here's the order of operation of `meshnet-CNI`:

1. kubelet calls `meshnet` binary and passes standard CNI arguments and environment variables
2. `meshnet` delegates the provisioning of the primary `eth0` interface to the plugin specified in the `delegate` field of the configuration file (as shown below)
3. `meshnet` updates etcd with pod's metadata and retrieves topology metadata from etcd
4. It then iterates over each link in the topology and sets it up according the location of the remote pod:  
    4.1. If remote pod is on the same node - it connects them via a veth link pair  
    4.2. If remote pod is on a different node - sets up a local vxlan interface and sends a API call to the remote node's `meshnetd` daemon   
    When `meshnetd` recieves an API call from a remote node, it tries to idempotently apply the VxLan configuration to the interface of the local pod



## Plugin configuration example

```yaml
{
  "cniVersion": "0.1.0",
  "name": "my_network",       <--- Arbitrary name
  "type": "meshnet",          <--- The name of CNI plugin binary
  "etcd_host": "10.97.209.1", <--- IP address of etcd service 
  "etcd_port": "2379",
  "delegate": {               <--- Plugin responsible for the first interface (eth0)
    "name": "dind0",
    "bridge": "dind0",
    "type": "bridge",
    "isDefaultGateway": true,
    "ipMasq": true,
    "ipam": {
      "type": "host-local",
      "subnet": "10.244.1.0/24",
      "gateway": "10.244.1.1"
    }
  }
}
```

## Usage example

> Note: go 1.11 or later is required

Clone this project and:

```
go build ./...
```

Build a local test dind kubernetes cluster

```
./reinit.sh
```

The last step also deploys a new etcd cluster required by `meshnet`.

Build `meshnet`, `meshnetd` and copy them along with CNI .conf file to all mebers of the cluster

```
./build.sh
```

Pre-seed etcd with topolog information (in this case 3 pods connected as triangle)

```
tests/3node-alpine-test.sh
```

Create 3 test pods (2 pods on 1 node and 1 pod on another)

```
cat tests/2test.yml | kubectl create -f -
```

Check that all pods are running

```
kubectl --namespace=default get pods -o wide  |  grep pod
pod1                                  1/1       Running   0          3m        10.244.2.31   kube-node-1
pod2                                  1/1       Running   0          3m        10.244.2.32   kube-node-1
pod3                                  1/1       Running   0          3m        10.244.1.15   kube-master
```

Test connectivity between pods

```
kubectl exec pod1 -- sudo ping -c 1 12.12.12.2
kubectl exec pod1 -- sudo ping -c 1 13.13.13.3
kubectl exec pod2 -- sudo ping -c 1 23.23.23.3
```

Destroy 3 test pods

```
cat tests/3test.yml | kubectl delete --grace-period=0 --force -f -
```