package meshnet

import (
	"context"

	"github.com/networkop/meshnet-cni/daemon/vxlan"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/util/retry"

	mpb "github.com/networkop/meshnet-cni/daemon/proto/meshnet/v1beta1"
)

func (m *Meshnet) getPod(ctx context.Context, name, ns string) (*unstructured.Unstructured, error) {
	log.Infof("Reading pod %s from K8s", name)
	return m.tClient.Topology(ns).Unstructured(ctx, name, metav1.GetOptions{})
}

func (m *Meshnet) updateStatus(ctx context.Context, obj *unstructured.Unstructured, ns string) error {
	log.Infof("Update pod status %s from K8s", obj.GetName())
	_, err := m.tClient.Topology(ns).Update(ctx, obj, metav1.UpdateOptions{})
	return err
}

func (m *Meshnet) Get(ctx context.Context, pod *mpb.PodQuery) (*mpb.Pod, error) {
	log.Infof("Retrieving %s's metadata from K8s...", pod.Name)

	result, err := m.getPod(ctx, pod.Name, pod.KubeNs)
	if err != nil {
		log.Errorf("Failed to read pod %s from K8s", pod.Name)
		return nil, err
	}

	remoteLinks, found, err := unstructured.NestedSlice(result.Object, "spec", "links")
	if err != nil || !found || remoteLinks == nil {
		log.Errorf("Could not find 'Link' array in pod's spec")
		return nil, err
	}

	links := make([]*mpb.Link, len(remoteLinks))
	for i := range links {
		remoteLink, ok := remoteLinks[i].(map[string]interface{})
		if !ok {
			log.Errorf("Unrecognised 'Link' structure")
			return nil, err
		}
		newLink := &mpb.Link{}
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

	return &mpb.Pod{
		Name:   pod.Name,
		SrcIp:  srcIP,
		NetNs:  netNs,
		KubeNs: pod.KubeNs,
		Links:  links,
	}, nil
}

func (m *Meshnet) SetAlive(ctx context.Context, pod *mpb.Pod) (*mpb.BoolResponse, error) {
	log.Infof("Setting %s's SrcIp=%s and NetNs=%s", pod.Name, pod.SrcIp, pod.NetNs)

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, err := m.getPod(ctx, pod.Name, pod.KubeNs)
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

		return m.updateStatus(ctx, result, pod.KubeNs)
	})

	if retryErr != nil {
		log.WithFields(log.Fields{
			"err":      retryErr,
			"function": "SetAlive",
		}).Errorf("Failed to update pod %s alive status", pod.Name)
		return &mpb.BoolResponse{Response: false}, retryErr
	}

	return &mpb.BoolResponse{Response: true}, nil
}

func (m *Meshnet) Skip(ctx context.Context, skip *mpb.SkipQuery) (*mpb.BoolResponse, error) {
	log.Infof("Skipping of pod %s by pod %s", skip.Peer, skip.Pod)

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, err := m.getPod(ctx, skip.Pod, skip.KubeNs)
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

		return m.updateStatus(ctx, result, skip.KubeNs)
	})
	if retryErr != nil {
		log.WithFields(log.Fields{
			"err":      retryErr,
			"function": "Skip",
		}).Errorf("Failed to update skip pod %s status", skip.Pod)
		return &mpb.BoolResponse{Response: false}, retryErr
	}

	return &mpb.BoolResponse{Response: true}, nil
}

func (m *Meshnet) SkipReverse(ctx context.Context, skip *mpb.SkipQuery) (*mpb.BoolResponse, error) {
	log.Infof("Reverse-skipping of pod %s by pod %s", skip.Peer, skip.Pod)

	var podName string
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// setting the value for peer pod
		peerPod, err := m.getPod(ctx, skip.Peer, skip.KubeNs)
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
		return m.updateStatus(ctx, peerPod, skip.KubeNs)
	})
	if retryErr != nil {
		log.WithFields(log.Fields{
			"err":      retryErr,
			"function": "SkipReverse",
		}).Errorf("Failed to update peer pod %s skipreverse status", podName)
		return &mpb.BoolResponse{Response: false}, retryErr
	}

	retryErr = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// setting the value for this pod
		thisPod, err := m.getPod(ctx, skip.Pod, skip.KubeNs)
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
			return m.updateStatus(ctx, thisPod, skip.KubeNs)
		}
		return nil
	})
	if retryErr != nil {
		log.WithFields(log.Fields{
			"err":      retryErr,
			"function": "SkipReverse",
		}).Error("Failed to update this pod skipreverse status")
		return &mpb.BoolResponse{Response: false}, retryErr
	}

	return &mpb.BoolResponse{Response: true}, nil
}

func (m *Meshnet) IsSkipped(ctx context.Context, skip *mpb.SkipQuery) (*mpb.BoolResponse, error) {
	log.Infof("Checking if %s is skipped by %s", skip.Peer, skip.Pod)

	result, err := m.getPod(ctx, skip.Peer, skip.KubeNs)
	if err != nil {
		log.Errorf("Failed to read pod %s from K8s", skip.Pod)
		return nil, err
	}

	skipped, _, _ := unstructured.NestedSlice(result.Object, "status", "skipped")

	for _, peer := range skipped {
		if skip.Pod == peer.(string) {
			return &mpb.BoolResponse{Response: true}, nil
		}
	}

	return &mpb.BoolResponse{Response: false}, nil
}

func (m *Meshnet) Update(ctx context.Context, pod *mpb.RemotePod) (*mpb.BoolResponse, error) {
	if err := vxlan.CreateOrUpdate(pod); err != nil {
		log.Errorf("Failed to Update Vxlan")
		return &mpb.BoolResponse{Response: false}, nil
	}
	return &mpb.BoolResponse{Response: true}, nil
}
