package main

import (
	log "github.com/sirupsen/logrus"

	//pod "github.com/networkop/meshnet-cni/pkg/apis/networkop/v1beta1"
	//crdclient "github.com/networkop/meshnet-cni/pkg/client/clientset/versioned"
	//metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

//type kubeClient struct {
//	client *crdclient.Clientset
//}

//func newKubeClient() (*kubeClient, error) {
//
//	kubeConfig, err := rest.InClusterConfig()
//	if err != nil {
//		log.Printf("Failed to connect to K8s API server")
//		return nil, err
//	}
//
//	crdClient, err := crdclient.NewForConfig(kubeConfig)
//	if err != nil {
//		return nil, err
//	}
//
//	return &kubeClient{
//		client: crdClient,
//	}, nil
//}

func newDynamicClient() (dynamic.Interface, error) {

	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		log.Printf("Failed to connect to K8s API server")
		return nil, err
	}

	dynamicClient, _ := dynamic.NewForConfig(kubeConfig)

	return dynamicClient, err
}

//// getPod retrieves pod's metadata from CR
//func (c *kubeClient) getPod(name, ns string) (*pod.Topology, error) {
//	return c.client.NetworkopV1beta1().Topologies(ns).Get(name, metav1.GetOptions{})
//}
//
//// setPodAlive sets pod's srcIP and NetNS
//func (c *kubeClient) setPodAlive(pod *pod.Topology, netns, srcip, kubens string) (*pod.Topology, error) {
//	pod.Status.SrcIp = srcip
//	pod.Status.NetNs = netns
//	pod.Status.Skipped = []string{}
//	return c.updateStatus(pod, kubens)
//}
//
//func (c *kubeClient) updateStatus(pod *pod.Topology, ns string) (*pod.Topology, error) {
//	newpod, err := c.client.NetworkopV1beta1().Topologies(ns).UpdateStatus(pod)
//	if err != nil {
//		return pod, err
//	}
//	return newpod, nil
//}
//
//func (c *kubeClient) isAlive(pod *pod.Topology, ns string) bool {
//	newpod, _ := c.getPod(pod.Name, ns)
//	return newpod.Status.SrcIp != "" && newpod.Status.NetNs != ""
//}
//
//func (c *kubeClient) skipPod(myPod, peerPod *pod.Topology, ns string) (*pod.Topology, error) {
//	myPod.Status.Skipped = append(myPod.Status.Skipped, peerPod.Name)
//	return c.updateStatus(myPod, ns)
//}
//
//func (c *kubeClient) skipPodReversed(myPod, peerPod *pod.Topology, ns string) (*pod.Topology, error) {
//	peerPod.Status.Skipped = append(peerPod.Status.Skipped, myPod.Name)
//	return c.updateStatus(peerPod, "")
//}
//
//func (c *kubeClient) isSkipped(myPod, peerPod *pod.Topology, ns string) bool {
//	for _, pod := range myPod.Status.Skipped {
//		if pod == peerPod.Name {
//			return true
//		}
//	}
//	return false
//}
//
