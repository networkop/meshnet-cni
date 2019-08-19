package main

import (
	"context"

	pb "github.com/networkop/meshnet-cni/daemon/generated"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var gvr = schema.GroupVersionResource{
	Group:    "networkop.co.uk",
	Version:  "v1beta1",
	Resource: "topologies",
}

func (s *meshnetd) getPod(name, ns string) (*unstructured.Unstructured, error) {
	log.Infof("Reading pod %s from K8s", name)
	return s.kube.Resource(gvr).Namespace(ns).Get(name, metav1.GetOptions{})
}

func (s *meshnetd) updateStatus(obj *unstructured.Unstructured, ns string) (*unstructured.Unstructured, error) {
	log.Infof("Update pod status %s from K8s", obj.GetName())
	return s.kube.Resource(gvr).Namespace(ns).UpdateStatus(obj, metav1.UpdateOptions{})
}

func (s *meshnetd) Get(ctx context.Context, pod *pb.PodQuery) (*pb.Pod, error) {
	log.Infof("Retrieving %s's metadata from K8s...", pod.Name)

	result, err := s.getPod(pod.Name, pod.KubeNs)
	if err != nil {
		log.Errorf("Failed to read pod %s from K8s", pod.Name)
		return nil, err
	}

	remoteLinks, found, err := unstructured.NestedSlice(result.Object, "spec", "links")
	if err != nil || !found || remoteLinks == nil {
		log.Errorf("Could not find 'Link' array in pod's spec")
		return nil, err
	}

	links := make([]*pb.Link, len(remoteLinks))
	for i := range links {
		remoteLink, ok := remoteLinks[i].(map[string]interface{})
		if !ok {
			log.Errorf("Unrecognised 'Link' structure")
			return nil, err
		}
		newLink := &pb.Link{}
		newLink.PeerPod, _, _ = unstructured.NestedString(remoteLink, "peer_pod")
		newLink.PeerIntf, _, _ = unstructured.NestedString(remoteLink, "peer_intf")
		newLink.LocalIntf, _, _ = unstructured.NestedString(remoteLink, "local_intf")
		newLink.LocalIp, _, _ = unstructured.NestedString(remoteLink, "local_ip")
		newLink.PeerIp, _, _ = unstructured.NestedString(remoteLink, "peer_ip")
		newLink.Uid, _, _ = unstructured.NestedInt64(remoteLink, "uid")
		links[i] = newLink
	}

	srcIP, _, _ := unstructured.NestedString(result.Object, "status", "src_ip")
	netNs, _, _ := unstructured.NestedString(result.Object, "status", "net_ns")

	return &pb.Pod{
		Name:   pod.Name,
		SrcIp:  srcIP,
		NetNs:  netNs,
		KubeNs: pod.KubeNs,
		Links:  links,
	}, nil
}

func (s *meshnetd) SetAlive(ctx context.Context, pod *pb.Pod) (*pb.BoolResponse, error) {
	log.Infof("Setting %s's SrcIp=%s and NetNs=%s", pod.Name, pod.SrcIp, pod.NetNs)

	result, err := s.getPod(pod.Name, pod.KubeNs)
	if err != nil {
		log.Errorf("Failed to read pod %s from K8s", pod.Name)
		return nil, err
	}

	if err = unstructured.SetNestedField(result.Object, pod.SrcIp, "status", "src_ip"); err != nil {
		log.Errorf("Failed to update pod's src_ip")
	}

	if err = unstructured.SetNestedField(result.Object, pod.NetNs, "status", "net_ns"); err != nil {
		log.Errorf("Failed to update pod's net_ns")
	}

	_, err = s.updateStatus(result, pod.KubeNs)
	if err != nil {
		log.Errorf("Failed to update pod %s alive status", pod.Name)
		return &pb.BoolResponse{Response: false}, nil
	}

	return &pb.BoolResponse{Response: true}, nil
}

func (s *meshnetd) Skip(ctx context.Context, skip *pb.SkipQuery) (*pb.BoolResponse, error) {
	log.Infof("Skipping pod %s by pod %s", skip.Pod, skip.Peer)

	result, err := s.getPod(skip.Pod, skip.KubeNs)
	if err != nil {
		log.Errorf("Failed to read pod %s from K8s", skip.Pod)
		return nil, err
	}

	skipped, _, _ := unstructured.NestedSlice(result.Object, "status", "skipped")

	newSkipped := append(skipped, skip.Peer)

	if err := unstructured.SetNestedField(result.Object, newSkipped, "status", "skipped"); err != nil {
		log.Errorf("Failed to updated skipped list")
		return nil, err
	}

	_, err = s.updateStatus(result, skip.KubeNs)
	if err != nil {
		log.Errorf("Failed to update pod %s alive status", skip.Pod)
		return &pb.BoolResponse{Response: false}, nil
	}

	return &pb.BoolResponse{Response: true}, nil
}

func (s *meshnetd) SkipReverse(ctx context.Context, skip *pb.SkipQuery) (*pb.BoolResponse, error) {
	log.Infof("Reverse-skipping of pod %s by pod %s", skip.Pod, skip.Peer)

	// setting the value for peer pod
	peerPod, err := s.getPod(skip.Peer, skip.KubeNs)
	if err != nil {
		log.Errorf("Failed to read pod %s from K8s", skip.Pod)
		return nil, err
	}

	// extracting peer pod's skipped list and adding this pod's name to it
	peerSkipped, _, _ := unstructured.NestedSlice(peerPod.Object, "status", "skipped")
	newPeerSkipped := append(peerSkipped, skip.Pod)

	// updating peer pod's skipped list locally
	if err := unstructured.SetNestedField(peerPod.Object, newPeerSkipped, "status", "skipped"); err != nil {
		log.Errorf("Failed to updated reverse-skipped list for peer pod %s", peerPod.GetName())
		return nil, err
	}

	// sending peer pod's updates to k8s
	_, err = s.updateStatus(peerPod, skip.KubeNs)
	if err != nil {
		log.Errorf("Failed to update pod %s alive status", peerPod.GetName())
		return &pb.BoolResponse{Response: false}, nil
	}

	// setting the value for this pod
	thisPod, err := s.getPod(skip.Pod, skip.KubeNs)
	if err != nil {
		log.Errorf("Failed to read pod %s from K8s", skip.Pod)
		return nil, err
	}

	// extracting this pod's skipped list and removing peer pod's name from it
	thisSkipped, _, _ := unstructured.NestedSlice(thisPod.Object, "status", "skipped")
	newThisSkipped := make([]interface{}, len(thisSkipped))

	for _, el := range thisSkipped {
		if el.(string) != skip.Peer {
			newThisSkipped = append(newThisSkipped, el)
		}
	}

	// updating this pod's skipped list locally
	if err := unstructured.SetNestedField(thisPod.Object, newThisSkipped, "status", "skipped"); err != nil {
		log.Errorf("Failed to cleanup skipped list for pod %s", thisPod.GetName())
		return nil, err
	}

	// sending this pod's updates to k8s
	_, err = s.updateStatus(thisPod, skip.KubeNs)
	if err != nil {
		log.Errorf("Failed to update pod %s alive status", thisPod.GetName())
		return &pb.BoolResponse{Response: false}, nil
	}

	return &pb.BoolResponse{Response: true}, nil
}

func (s *meshnetd) IsSkipped(ctx context.Context, skip *pb.SkipQuery) (*pb.BoolResponse, error) {
	log.Infof("Checking if %s is skipped by %s", skip.Peer, skip.Pod)

	result, err := s.getPod(skip.Peer, skip.KubeNs)
	if err != nil {
		log.Errorf("Failed to read pod %s from K8s", skip.Pod)
		return nil, err
	}

	skipped, _, _ := unstructured.NestedSlice(result.Object, "status", "skipped")

	for _, peer := range skipped {
		if skip.Pod == peer.(string) {
			return &pb.BoolResponse{Response: true}, nil
		}
	}

	return &pb.BoolResponse{Response: false}, nil
}

func (s *meshnetd) Update(ctx context.Context, pod *pb.RemotePod) (*pb.BoolResponse, error) {
	if err := createOrUpdateVxlan(pod); err != nil {
		log.Errorf("Failed to Update Vxlan")
		return &pb.BoolResponse{Response: false}, nil
	}
	return &pb.BoolResponse{Response: true}, nil
}
