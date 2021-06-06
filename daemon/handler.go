package main

import (
	"context"

	pb "github.com/networkop/meshnet-cni/daemon/proto/meshnet/v1beta1"
	"github.com/networkop/meshnet-cni/daemon/vxlan"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/retry"
)

var gvr = schema.GroupVersionResource{
	Group:    "networkop.co.uk",
	Version:  "v1beta1",
	Resource: "topologies",
}

func (s *meshnetd) getPod(ctx context.Context, name, ns string) (*unstructured.Unstructured, error) {
	log.Infof("Reading pod %s from K8s", name)
	return s.kube.Resource(gvr).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
}

func (s *meshnetd) updateStatus(ctx context.Context, obj *unstructured.Unstructured, ns string) error {
	log.Infof("Update pod status %s from K8s", obj.GetName())
	_, err := s.kube.Resource(gvr).Namespace(ns).UpdateStatus(ctx, obj, metav1.UpdateOptions{})
	return err
}

func (s *meshnetd) Get(ctx context.Context, pod *pb.PodQuery) (*pb.Pod, error) {
	log.Infof("Retrieving %s's metadata from K8s...", pod.Name)

	result, err := s.getPod(ctx, pod.Name, pod.KubeNs)
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

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, err := s.getPod(ctx, pod.Name, pod.KubeNs)
		if err != nil {
			log.Errorf("Failed to read pod %s from K8s", pod.Name)
			return err
		}

		if err = unstructured.SetNestedField(result.Object, pod.SrcIp, "status", "src_ip"); err != nil {
			log.Errorf("Failed to update pod's src_ip")
		}

		if err = unstructured.SetNestedField(result.Object, pod.NetNs, "status", "net_ns"); err != nil {
			log.Errorf("Failed to update pod's net_ns")
		}

		return s.updateStatus(ctx, result, pod.KubeNs)
	})

	if retryErr != nil {
		log.WithFields(log.Fields{
			"err":      retryErr,
			"function": "SetAlive",
		}).Errorf("Failed to update pod %s alive status", pod.Name)
		return &pb.BoolResponse{Response: false}, retryErr
	}

	return &pb.BoolResponse{Response: true}, nil
}

func (s *meshnetd) Skip(ctx context.Context, skip *pb.SkipQuery) (*pb.BoolResponse, error) {
	log.Infof("Skipping of pod %s by pod %s", skip.Peer, skip.Pod)

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, err := s.getPod(ctx, skip.Pod, skip.KubeNs)
		if err != nil {
			log.Errorf("Failed to read pod %s from K8s", skip.Pod)
			return err
		}

		skipped, _, _ := unstructured.NestedSlice(result.Object, "status", "skipped")

		newSkipped := append(skipped, skip.Peer)

		if err := unstructured.SetNestedField(result.Object, newSkipped, "status", "skipped"); err != nil {
			log.Errorf("Failed to updated skipped list")
			return err
		}

		return s.updateStatus(ctx, result, skip.KubeNs)
	})
	if retryErr != nil {
		log.WithFields(log.Fields{
			"err":      retryErr,
			"function": "Skip",
		}).Errorf("Failed to update skip pod %s status", skip.Pod)
		return &pb.BoolResponse{Response: false}, retryErr
	}

	return &pb.BoolResponse{Response: true}, nil
}

func (s *meshnetd) SkipReverse(ctx context.Context, skip *pb.SkipQuery) (*pb.BoolResponse, error) {
	log.Infof("Reverse-skipping of pod %s by pod %s", skip.Peer, skip.Pod)

	var podName string
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// setting the value for peer pod
		peerPod, err := s.getPod(ctx, skip.Peer, skip.KubeNs)
		if err != nil {
			log.Errorf("Failed to read pod %s from K8s", skip.Pod)
			return err
		}
		podName = peerPod.GetName()

		// extracting peer pod's skipped list and adding this pod's name to it
		peerSkipped, _, _ := unstructured.NestedSlice(peerPod.Object, "status", "skipped")
		newPeerSkipped := append(peerSkipped, skip.Pod)

		log.Infof("Updating peer skipped list")
		// updating peer pod's skipped list locally
		if err := unstructured.SetNestedField(peerPod.Object, newPeerSkipped, "status", "skipped"); err != nil {
			log.Errorf("Failed to updated reverse-skipped list for peer pod %s", peerPod.GetName())
			return err
		}

		// sending peer pod's updates to k8s
		return s.updateStatus(ctx, peerPod, skip.KubeNs)
	})
	if retryErr != nil {
		log.WithFields(log.Fields{
			"err":      retryErr,
			"function": "SkipReverse",
		}).Errorf("Failed to update peer pod %s skipreverse status", podName)
		return &pb.BoolResponse{Response: false}, retryErr
	}

	retryErr = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// setting the value for this pod
		thisPod, err := s.getPod(ctx, skip.Pod, skip.KubeNs)
		if err != nil {
			log.Errorf("Failed to read pod %s from K8s", skip.Pod)
			return err
		}

		// extracting this pod's skipped list and removing peer pod's name from it
		thisSkipped, _, _ := unstructured.NestedSlice(thisPod.Object, "status", "skipped")
		newThisSkipped := make([]interface{}, 0)

		log.WithFields(log.Fields{
			"thisSkipped": thisSkipped,
		}).Info("THIS SKIPPED:")

		for _, el := range thisSkipped {
			elString, ok := el.(string)
			if ok {
				if elString != skip.Peer {
					log.Errorf("Appending new element %s", elString)
					newThisSkipped = append(newThisSkipped, elString)
				}
			}
		}

		log.WithFields(log.Fields{
			"newThisSkipped": newThisSkipped,
		}).Info("NEW THIS SKIPPED:")

		// updating this pod's skipped list locally
		if len(newThisSkipped) != 0 {
			if err := unstructured.SetNestedField(thisPod.Object, newThisSkipped, "status", "skipped"); err != nil {
				log.Errorf("Failed to cleanup skipped list for pod %s", thisPod.GetName())
				return err
			}

			// sending this pod's updates to k8s
			return s.updateStatus(ctx, thisPod, skip.KubeNs)
		}
		return nil
	})
	if retryErr != nil {
		log.WithFields(log.Fields{
			"err":      retryErr,
			"function": "SkipReverse",
		}).Error("Failed to update this pod skipreverse status")
		return &pb.BoolResponse{Response: false}, retryErr
	}

	return &pb.BoolResponse{Response: true}, nil
}

func (s *meshnetd) IsSkipped(ctx context.Context, skip *pb.SkipQuery) (*pb.BoolResponse, error) {
	log.Infof("Checking if %s is skipped by %s", skip.Peer, skip.Pod)

	result, err := s.getPod(ctx, skip.Peer, skip.KubeNs)
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
	if err := vxlan.CreateOrUpdate(pod); err != nil {
		log.Errorf("Failed to Update Vxlan")
		return &pb.BoolResponse{Response: false}, nil
	}
	return &pb.BoolResponse{Response: true}, nil
}
