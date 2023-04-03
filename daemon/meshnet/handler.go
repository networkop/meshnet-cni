package meshnet

import (
	"context"
	"net"
	"os"

	"github.com/networkop/meshnet-cni/daemon/grpcwire"
	"github.com/networkop/meshnet-cni/daemon/vxlan"
	"github.com/networkop/meshnet-cni/utils/wireutil"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/util/retry"

	"github.com/google/gopacket/pcap"
	mpb "github.com/networkop/meshnet-cni/daemon/proto/meshnet/v1beta1"
)

func (m *Meshnet) getPod(ctx context.Context, name, ns string) (*unstructured.Unstructured, error) {
	mnetdLogger.Infof("Reading pod %s from K8s", name)
	return m.tClient.Topology(ns).Unstructured(ctx, name, metav1.GetOptions{})
}

func (m *Meshnet) updateStatus(ctx context.Context, obj *unstructured.Unstructured, ns string) error {
	mnetdLogger.Infof("Update pod status %s from K8s", obj.GetName())
	_, err := m.tClient.Topology(ns).Update(ctx, obj, metav1.UpdateOptions{})
	return err
}

func (m *Meshnet) Get(ctx context.Context, pod *mpb.PodQuery) (*mpb.Pod, error) {
	mnetdLogger.Infof("Retrieving %s's metadata from K8s...", pod.Name)

	result, err := m.getPod(ctx, pod.Name, pod.KubeNs)
	if err != nil {
		mnetdLogger.Errorf("Failed to read pod %s from K8s", pod.Name)
		return nil, err
	}

	remoteLinks, found, err := unstructured.NestedSlice(result.Object, "spec", "links")
	if err != nil || !found || remoteLinks == nil {
		mnetdLogger.Errorf("Could not find 'Link' array in pod's spec")
		return nil, err
	}

	links := make([]*mpb.Link, len(remoteLinks))
	for i := range links {
		remoteLink, ok := remoteLinks[i].(map[string]interface{})
		if !ok {
			mnetdLogger.Errorf("Unrecognised 'Link' structure")
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
	nodeIP := os.Getenv("HOST_IP")
	nodeIntf := os.Getenv("HOST_INTF")

	return &mpb.Pod{
		Name:   pod.Name,
		SrcIp:  srcIP,
		NetNs:  netNs,
		KubeNs: pod.KubeNs,
		Links:  links,
		NodeIp: nodeIP,
		NodeIntf: nodeIntf,
	}, nil
}

func (m *Meshnet) SetAlive(ctx context.Context, pod *mpb.Pod) (*mpb.BoolResponse, error) {
	mnetdLogger.Infof("Setting %s's SrcIp=%s and NetNs=%s", pod.Name, pod.SrcIp, pod.NetNs)

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, err := m.getPod(ctx, pod.Name, pod.KubeNs)
		if err != nil {
			mnetdLogger.Errorf("Failed to read pod %s from K8s", pod.Name)
			return err
		}

		if err = unstructured.SetNestedField(result.Object, pod.SrcIp, "status", "src_ip"); err != nil {
			mnetdLogger.Errorf("Failed to update pod's src_ip")
		}

		if err = unstructured.SetNestedField(result.Object, pod.NetNs, "status", "net_ns"); err != nil {
			mnetdLogger.Errorf("Failed to update pod's net_ns")
		}

		return m.updateStatus(ctx, result, pod.KubeNs)
	})

	if retryErr != nil {
		log.WithFields(log.Fields{
			"daemon":   "meshnetd",
			"err":      retryErr,
			"function": "SetAlive",
		}).Errorf("Failed to update pod %s alive status", pod.Name)
		return &mpb.BoolResponse{Response: false}, retryErr
	}

	return &mpb.BoolResponse{Response: true}, nil
}

func (m *Meshnet) Skip(ctx context.Context, skip *mpb.SkipQuery) (*mpb.BoolResponse, error) {
	mnetdLogger.Infof("Skipping of pod %s by pod %s", skip.Peer, skip.Pod)

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, err := m.getPod(ctx, skip.Pod, skip.KubeNs)
		if err != nil {
			mnetdLogger.Errorf("Failed to read pod %s from K8s", skip.Pod)
			return err
		}

		skipped, _, _ := unstructured.NestedSlice(result.Object, "status", "skipped")

		newSkipped := append(skipped, skip.Peer)

		if err := unstructured.SetNestedField(result.Object, newSkipped, "status", "skipped"); err != nil {
			mnetdLogger.Errorf("Failed to updated skipped list")
			return err
		}

		return m.updateStatus(ctx, result, skip.KubeNs)
	})
	if retryErr != nil {
		log.WithFields(log.Fields{
			"daemon":   "meshnetd",
			"err":      retryErr,
			"function": "Skip",
		}).Errorf("Failed to update skip pod %s status", skip.Pod)
		return &mpb.BoolResponse{Response: false}, retryErr
	}

	return &mpb.BoolResponse{Response: true}, nil
}

func (m *Meshnet) SkipReverse(ctx context.Context, skip *mpb.SkipQuery) (*mpb.BoolResponse, error) {
	mnetdLogger.Infof("Reverse-skipping of pod %s by pod %s", skip.Peer, skip.Pod)

	var podName string
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// setting the value for peer pod
		peerPod, err := m.getPod(ctx, skip.Peer, skip.KubeNs)
		if err != nil {
			mnetdLogger.Errorf("Failed to read pod %s from K8s", skip.Pod)
			return err
		}
		podName = peerPod.GetName()

		// extracting peer pod's skipped list and adding this pod's name to it
		peerSkipped, _, _ := unstructured.NestedSlice(peerPod.Object, "status", "skipped")
		newPeerSkipped := append(peerSkipped, skip.Pod)

		mnetdLogger.Infof("Updating peer skipped list")
		// updating peer pod's skipped list locally
		if err := unstructured.SetNestedField(peerPod.Object, newPeerSkipped, "status", "skipped"); err != nil {
			mnetdLogger.Errorf("Failed to updated reverse-skipped list for peer pod %s", peerPod.GetName())
			return err
		}

		// sending peer pod's updates to k8s
		return m.updateStatus(ctx, peerPod, skip.KubeNs)
	})
	if retryErr != nil {
		log.WithFields(log.Fields{
			"daemon":   "meshnetd",
			"err":      retryErr,
			"function": "SkipReverse",
		}).Errorf("Failed to update peer pod %s skipreverse status", podName)
		return &mpb.BoolResponse{Response: false}, retryErr
	}

	retryErr = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// setting the value for this pod
		thisPod, err := m.getPod(ctx, skip.Pod, skip.KubeNs)
		if err != nil {
			mnetdLogger.Errorf("Failed to read pod %s from K8s", skip.Pod)
			return err
		}

		// extracting this pod's skipped list and removing peer pod's name from it
		thisSkipped, _, _ := unstructured.NestedSlice(thisPod.Object, "status", "skipped")
		newThisSkipped := make([]interface{}, 0)

		log.WithFields(log.Fields{
			"daemon":      "meshnetd",
			"thisSkipped": thisSkipped,
		}).Info("THIS SKIPPED:")

		for _, el := range thisSkipped {
			elString, ok := el.(string)
			if ok {
				if elString != skip.Peer {
					mnetdLogger.Infof("Appending new element %s", elString)
					newThisSkipped = append(newThisSkipped, elString)
				}
			}
		}

		log.WithFields(log.Fields{
			"daemon":         "meshnetd",
			"newThisSkipped": newThisSkipped,
		}).Info("NEW THIS SKIPPED:")

		// updating this pod's skipped list locally
		if len(newThisSkipped) != 0 {
			if err := unstructured.SetNestedField(thisPod.Object, newThisSkipped, "status", "skipped"); err != nil {
				mnetdLogger.Errorf("Failed to cleanup skipped list for pod %s", thisPod.GetName())
				return err
			}

			// sending this pod's updates to k8s
			return m.updateStatus(ctx, thisPod, skip.KubeNs)
		}
		return nil
	})
	if retryErr != nil {
		log.WithFields(log.Fields{
			"daemon":   "meshnetd",
			"err":      retryErr,
			"function": "SkipReverse",
		}).Error("Failed to update this pod skipreverse status")
		return &mpb.BoolResponse{Response: false}, retryErr
	}

	return &mpb.BoolResponse{Response: true}, nil
}

func (m *Meshnet) IsSkipped(ctx context.Context, skip *mpb.SkipQuery) (*mpb.BoolResponse, error) {
	mnetdLogger.Infof("Checking if %s is skipped by %s", skip.Peer, skip.Pod)

	result, err := m.getPod(ctx, skip.Peer, skip.KubeNs)
	if err != nil {
		mnetdLogger.Errorf("Failed to read pod %s from K8s", skip.Pod)
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
		mnetdLogger.Errorf("Failed to Update Vxlan")
		return &mpb.BoolResponse{Response: false}, nil
	}
	return &mpb.BoolResponse{Response: true}, nil
}

// ------------------------------------------------------------------------------------------------------
func (m *Meshnet) RemGRPCWire(ctx context.Context, wireDef *mpb.WireDef) (*mpb.BoolResponse, error) {
	if err := grpcwire.DeleteWiresByPod(wireDef.KubeNs, wireDef.LocalPodName); err != nil {
		return &mpb.BoolResponse{Response: false}, err
	}
	return &mpb.BoolResponse{Response: true}, nil
}

// ------------------------------------------------------------------------------------------------------
func (m *Meshnet) AddGRPCWireLocal(ctx context.Context, wireDef *mpb.WireDef) (*mpb.BoolResponse, error) {
	locInf, err := net.InterfaceByName(wireDef.VethNameLocalHost)
	if err != nil {
		log.WithFields(log.Fields{
			"daemon":  "meshnetd",
			"overlay": "gRPC",
		}).Errorf("[ADD-WIRE:LOCAL-END]For pod %s failed to retrieve interface ID for interface %v. error:%v", wireDef.LocalPodName, wireDef.VethNameLocalHost, err)
		return &mpb.BoolResponse{Response: false}, err
	}

	// update tx checksuming to off
	err = wireutil.SetTxChecksumOff(wireDef.IntfNameInPod, wireDef.LocalPodNetNs)
	if err != nil {
		log.Errorf("Error in setting tx checksum-off on interface %s, ns %s, pod %s: %v", wireDef.IntfNameInPod, wireDef.LocalPodNetNs, wireDef.LocalPodName, err)
		// not returning
	} else {
		log.Infof("Setting tx checksum-off on interface %s, pod %s is successful", wireDef.IntfNameInPod, wireDef.LocalPodName)
	}

	//Using google gopacket for packet receive. An alternative could be using socket. Not sure it it provides any advantage over gopacket.
	wrHandle, err := pcap.OpenLive(wireDef.VethNameLocalHost, 65365, true, pcap.BlockForever)
	if err != nil {
		log.WithFields(log.Fields{
			"daemon":  "meshnetd",
			"overlay": "gRPC",
		}).Fatalf("[ADD-WIRE:LOCAL-END]Could not open interface for send/recv packets for containers. error:%v", err)
		return &mpb.BoolResponse{Response: false}, err
	}

	aWire := grpcwire.GRPCWire{
		UID: int(wireDef.LinkUid),

		LocalNodeIfaceID:   int64(locInf.Index),
		LocalNodeIfaceName: wireDef.VethNameLocalHost,
		LocalPodIP:         wireDef.LocalPodIp,
		LocalPodIfaceName:  wireDef.IntfNameInPod,
		LocalPodName:       wireDef.LocalPodName,
		LocalPodNetNS:      wireDef.LocalPodNetNs,

		PeerIfaceID: wireDef.PeerIntfId,
		PeerNodeIP:  wireDef.PeerIp,

		Originator:   grpcwire.HOST_CREATED_WIRE,
		OriginatorIP: "unknown", /*+++todo retrieve host ip and set it here. Needed only for debugging */

		StopC:     make(chan struct{}),
		Namespace: wireDef.KubeNs,
	}

	grpcwire.AddWire(&aWire, wrHandle)

	log.WithFields(log.Fields{
		"daemon":  "meshnetd",
		"overlay": "gRPC",
	}).Infof("[ADD-WIRE:LOCAL-END]For pod %s@%s starting the local packet receive thread", wireDef.LocalPodName, wireDef.IntfNameInPod)
	// TODO: handle error here
	go grpcwire.RecvFrmLocalPodThread(&aWire)

	return &mpb.BoolResponse{Response: true}, nil
}

// ------------------------------------------------------------------------------------------------------
func (m *Meshnet) SendToOnce(ctx context.Context, pkt *mpb.Packet) (*mpb.BoolResponse, error) {
	wrHandle, err := grpcwire.GetHostIntfHndl(pkt.RemotIntfId)
	if err != nil {
		log.WithFields(log.Fields{
			"daemon":  "meshnetd",
			"overlay": "gRPC",
		}).Errorf("SendToOnce (wire id - %v): Could not find local handle. err:%v", pkt.RemotIntfId, err)
		return &mpb.BoolResponse{Response: false}, err
	}

	// In case any per packet log need to be generated.
	// pktType := grpcwire.DecodePkt(pkt.Frame)
	// log.Printf("Daemon(SendToOnce): Received [pkt: %s, bytes: %d, for local interface id: %d]. Sending it to local container", pktType, len(pkt.Frame), pkt.RemotIntfId)
	// log.Printf("Daemon(SendToOnce): Received [bytes: %d, for local interface id: %d]. Sending it to local container", len(pkt.Frame), pkt.RemotIntfId)

	err = wrHandle.WritePacketData(pkt.Frame)
	if err != nil {
		log.WithFields(log.Fields{
			"daemon":  "meshnetd",
			"overlay": "gRPC",
		}).Errorf("SendToOnce (wire id - %v): Could not write packet(%d bytes) to local interface. err:%v", pkt.RemotIntfId, len(pkt.Frame), err)
		return &mpb.BoolResponse{Response: false}, err
	}

	return &mpb.BoolResponse{Response: true}, nil
}

// ---------------------------------------------------------------------------------------------------------------
func (m *Meshnet) AddGRPCWireRemote(ctx context.Context, wireDef *mpb.WireDef) (*mpb.WireCreateResponse, error) {
	stopC := make(chan struct{})
	wire, err := grpcwire.CreateGRPCWireRemoteTriggered(wireDef, stopC)
	if err == nil {
		log.WithFields(log.Fields{
			"daemon":  "meshnetd",
			"overlay": "gRPC",
		}).Infof("[ADD-WIRE:REMOTE-END]For pod %s@%s starting the local packet receive thread", wireDef.LocalPodName, wireDef.IntfNameInPod)
		go grpcwire.RecvFrmLocalPodThread(wire)

		return &mpb.WireCreateResponse{Response: true, PeerIntfId: wire.LocalNodeIfaceID}, nil
	}
	log.WithFields(log.Fields{
		"daemon":  "meshnetd",
		"overlay": "gRPC",
	}).Errorf("[ADD-WIRE:REMOTE-END] err: %v", err)
	return &mpb.WireCreateResponse{Response: false, PeerIntfId: wireDef.PeerIntfId}, err
}

// ---------------------------------------------------------------------------------------------------------------
// GRPCWireExists will return the wire if it exists.
func (m *Meshnet) GRPCWireExists(ctx context.Context, wireDef *mpb.WireDef) (*mpb.WireCreateResponse, error) {
	wire, ok := grpcwire.GetWireByUID(wireDef.LocalPodNetNs, int(wireDef.LinkUid))
	if !ok || wire == nil {
		return &mpb.WireCreateResponse{Response: false, PeerIntfId: wireDef.PeerIntfId}, nil
	}
	return &mpb.WireCreateResponse{Response: ok, PeerIntfId: wire.PeerIfaceID}, nil
}

// ---------------------------------------------------------------------------------------------------------------
// Given the pod name and the pod interface, GenerateNodeInterfaceName generates the corresponding interface name in the node.
// This pod interface and the node interface later become the two end of a veth-pair
func (m *Meshnet) GenerateNodeInterfaceName(ctx context.Context, in *mpb.GenerateNodeInterfaceNameRequest) (*mpb.GenerateNodeInterfaceNameResponse, error) {
	locIfNm, err := grpcwire.GenNodeIfaceName(in.PodName, in.PodIntfName)
	if err != nil {
		return &mpb.GenerateNodeInterfaceNameResponse{Ok: false, NodeIntfName: ""}, err
	}
	return &mpb.GenerateNodeInterfaceNameResponse{Ok: true, NodeIntfName: locIfNm}, nil
}
