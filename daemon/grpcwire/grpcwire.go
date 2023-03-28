package grpcwire

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/google/gopacket"
	"github.com/google/gopacket/pcap"
	mpb "github.com/networkop/meshnet-cni/daemon/proto/meshnet/v1beta1"
	"github.com/networkop/meshnet-cni/utils/wireutil"
	koko "github.com/redhat-nfvpe/koko/api"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var grpcOvrlyLogger *log.Entry = nil

func InitLogger() {
	grpcOvrlyLogger = log.WithFields(log.Fields{"daemon": "meshnetd", "overlay": "gRPC"})
}

type intfIndex struct {
	mu     sync.Mutex
	currId int64
}

/*
	In a given node a veth-pair connects a pod with the meshnet daemon hosted in the node. This meshnet

daemon provides the grpc-wire service to connect the local pod with the remote pod over grpc. The node
end of the veth-pair must have unique name with in the node. A node can have multiple pods. So there
will be multiple veth-pairs for connecting multiple nodes to meshnet daemon and each of them (the node end) must have unique
names. IntfIndex provides the sequentially increasing number which makes the name unique when added as
suffix to the name.
*/
var indexGen intfIndex

func NextIndex() int64 {
	indexGen.mu.Lock()
	defer indexGen.mu.Unlock()
	indexGen.currId++
	return indexGen.currId
}

/*+++tbf: These constants has no utility other that helping in debugging. These can be removed later. */
type grpcWireOriginator int

func (g grpcWireOriginator) String() string {
	switch g {
	case HOST_CREATED_WIRE:
		return "host originated"
	case PEER_CREATED_WIRE:
		return "peer originated"
	}
	return "unknown originator"
}

const (
	HOST_CREATED_WIRE grpcWireOriginator = iota
	PEER_CREATED_WIRE
)

type GRPCWire struct {
	UID       int    // uid identify a particular link in a topology as per meshnet crd
	Namespace string // K8s namespace this wire belongs to

	/* Node information */
	LocalNodeIfaceID   int64  // OS assigned interface ID of local node interface
	LocalNodeIfaceName string // name of local node interface

	/* Pod information : where this wire is terminating in this node */
	LocalPodIP        string // IP address of the local container who will consume packets over this wire. This is for debugging. This is generally not available when links are getting created and is not necessary also.
	LocalPodName      string // Name the local pod who will consume packets over this wire.
	LocalPodIfaceName string // Name the interface which is inside the local pod who will consume packets over this wire. This is for debugging
	LocalPodNetNS     string

	/*Peer pod information*/
	PeerIfaceID int64  // Peer node interface ID
	PeerNodeIP  string // Peer node IP

	IsReady      bool               // Is this wire ip.
	Originator   grpcWireOriginator // create by local host or create on trigger from remote host. This is for debugging.
	OriginatorIP string             // IP address of the host created it. This is for debugging.

	StopC chan struct{} // the channel to send stop signal to the receive thread.
}

type linkKey struct {
	namespace string
	linkUID   int
}

type wireMap struct {
	mu      sync.Mutex
	wires   map[linkKey]*GRPCWire
	handles map[int64]*pcap.Handle
}

func (w *wireMap) GetWire(namespace string, linkUID int) (*GRPCWire, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	wire, ok := w.wires[linkKey{
		namespace: namespace,
		linkUID:   linkUID,
	}]
	return wire, ok
}

//func (w *wireMap) IsWireReady(namespace string, linkUID int) bool {
//	w.mu.Lock()
//	defer w.mu.Unlock()
//	wire, ok := w.wires[linkKey{
//		namespace: namespace,
//		linkUID:   linkUID,
//	}]
//	if ok {
//		return wire.IsReady
//	}
//	return false
//}

func (w *wireMap) GetHandle(key int64) (*pcap.Handle, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	handle, ok := w.handles[key]
	return handle, ok
}

func (w *wireMap) Add(wire *GRPCWire, handle *pcap.Handle) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.wires[linkKey{
		namespace: wire.LocalPodNetNS,
		linkUID:   wire.UID,
	}] = wire

	w.handles[wire.LocalNodeIfaceID] = handle
	return nil
}

func (w *wireMap) Delete(wire *GRPCWire) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	err := w.DeleteWoLock(wire)
	return err
}

func (w *wireMap) DeleteWoLock(wire *GRPCWire) error {
	delete(w.wires, linkKey{
		namespace: wire.LocalPodNetNS,
		linkUID:   wire.UID,
	})
	delete(w.handles, wire.LocalNodeIfaceID)
	return nil
}

/* A grpc-wire creation (between pod A and pod B) can be triggered by either host hosting pod A, B. They
 * can even trigger it simultaneously. Irrespective of who triggers, successful wire creation needs
 * activities at both hosts end. Our intention is to finish the wire creation at the first trigger.
 * This map keeps the list of wires which are already crated and must not be recreated, if any second
 * trigger is received. This situation occurs when both the host triggers wire creation almost simultaneously.
 */
var wires = &wireMap{
	wires: map[linkKey]*GRPCWire{},
	/* Used when a packet is received, then we know the id of the interface to which the packet to be delivered.
	   This map take interface-id as key and returns the corresponding handle for delivering the packet.
	   map[interface-id]->handle */
	handles: map[int64]*pcap.Handle{},
}

// FindWiresByPod returns a list of wires matching the namespace and pod.
func GetWiresByPod(namespace string, podName string) ([]*GRPCWire, bool) {
	wires.mu.Lock()
	defer wires.mu.Unlock()
	var rWires []*GRPCWire

	for _, wire := range wires.wires {
		if wire.LocalPodName == podName && wire.Namespace == namespace {
			rWires = append(rWires, wire)
		}
	}
	return rWires, true
}

//-------------------------------------------------------------------------------------------------

// GetWireByUID returns wire matching the provided namespace and linkUID.
func GetWireByUID(namespace string, linkUID int) (*GRPCWire, bool) {
	return wires.GetWire(namespace, linkUID)
}

//-------------------------------------------------------------------------------------------------

// getOrUpdateWireByUID returns wire matching the provided namespace and linkUID.
func getOrUpdateWireByUID(namespace string, linkUID int, peerIntfId int64) (*GRPCWire, bool) {
	wires.mu.Lock()
	defer wires.mu.Unlock()
	wire, ok := wires.wires[linkKey{
		namespace: namespace,
		linkUID:   linkUID,
	}]
	if ok {
		grpcOvrlyLogger.Infof("getOrUpdateWireByUID:[ADD-WIRE]: wire found %s@%s-%d@%d, link uid %d, is ready %t",
			wire.LocalPodName, wire.LocalPodIfaceName, wire.LocalNodeIfaceID, wire.PeerIfaceID, linkUID, wire.IsReady)
		if !wire.IsReady {
			wire.PeerIfaceID = peerIntfId
			wire.IsReady = true
			grpcOvrlyLogger.Infof("getOrUpdateWireByUID:[ADD-WIRE]: updated wire  %s@%s-%d@%d, link uid %d, is ready %t",
				wire.LocalPodName, wire.LocalPodIfaceName, wire.LocalNodeIfaceID, wire.PeerIfaceID, linkUID, wire.IsReady)
		}
	}
	return wire, ok
}

//-------------------------------------------------------------------------------------------------

// WireDownByUID returns wire matching the provided namespace and linkUID.
func WireDownByUID(namespace string, linkUID int) error {
	//return wires.GetWire(namespace, linkUID)
	wires.mu.Lock()
	defer wires.mu.Unlock()

	wire, ok := wires.wires[linkKey{
		namespace: namespace,
		linkUID:   linkUID,
	}]
	if ok {
		grpcOvrlyLogger.Infof("WireDownByUID: Making wire down from db, %s@%s-%s@%d, peer fid %d, link uid %d",
			wire.LocalPodName, wire.LocalPodIfaceName, wire.LocalNodeIfaceName, wire.LocalNodeIfaceID, wire.PeerIfaceID, linkUID)
		if wire.IsReady { // explain it later why this condition here
			grpcOvrlyLogger.Infof("WireDownByUID: Closing recv thread %s@%s-%s@%d, peer fid %d, link uid %d",
				wire.LocalPodName, wire.LocalPodIfaceName, wire.LocalNodeIfaceName, wire.LocalNodeIfaceID, wire.PeerIfaceID, linkUID)
			close(wire.StopC)
		}
		wire.IsReady = false
	} else {
		grpcOvrlyLogger.Infof("WireDownByUID: Did not find entry to make down from db, uid %d, ns %s",
			linkUID, namespace)
	}
	return nil
}

// DelWireByUID returns wire matching the provided namespace and linkUID.
// tbd: not used
//func DelWireByUID(namespace string, linkUID int) error {
//	//return wires.GetWire(namespace, linkUID)
//	wires.mu.Lock()
//	defer wires.mu.Unlock()
//
//	wire, ok := wires.wires[linkKey{
//		namespace: namespace,
//		linkUID:   linkUID,
//	}]
//	if ok {
//		close(wire.StopC)
//		delete(wires.handles, wire.LocalNodeIfaceID)
//
//		intf, err := net.InterfaceByIndex(int(wire.LocalNodeIfaceID))
//		if err != nil {
//			grpcOvrlyLogger.Errorf("ERROR: [WIRE-DELETE]DelWireByUID:Interface index %d for wire %d, is already cleaned up, err %v.", wire.LocalNodeIfaceID, wire.UID, err)
//		} else {
//			myVeth := koko.VEth{}
//			myVeth.LinkName = intf.Name
//			if err = myVeth.RemoveVethLink(); err != nil {
//				grpcOvrlyLogger.Errorf("ERROR: [WIRE-DELETE]DelWireByUID:failed to remove veth link: %v", err)
//			}
//		}
//		grpcOvrlyLogger.Infof("DelWireByUID:[WIRE-DELETE]: Removing wire info from db, %s@%s-%s@%d, peer fid %d",
//			wire.LocalPodName, wire.LocalPodIfaceName, wire.LocalNodeIfaceName, wire.LocalNodeIfaceID, wire.PeerIfaceID)
//
//		delete(wires.wires, linkKey{
//			namespace: namespace,
//			linkUID:   linkUID,
//		})
//	} else {
//		grpcOvrlyLogger.Infof("DelWireByUID:[WIRE-DELETE]: Did not find entry to remove from db, uid %d, ns %s",
//			linkUID, namespace)
//	}
//	return nil
//}

// -------------------------------------------------------------------------------------------------
func AddWire(wire *GRPCWire, handle *pcap.Handle) int {
	/* Populate the active wire map and returns the number of currently added active wires. */

	/* if this wire is already present in the map then it will be overwritten.
	   It seems to be ok to overwrite. Think more in what situation this may
	   not be the desired behavior and we need to throw an error. */
	wire.IsReady = true

	wires.Add(wire, handle)
	return len(wires.wires)
}

// -------------------------------------------------------------------------------------------------
// DeleteWire cleans up the active wire map and returns the number of currently added active wire.
func DeleteWire(wire *GRPCWire) int {
	wires.Delete(wire)
	return len(wires.wires)
}

//-----------------------------------------------------------------------------------------------------------
//func DeleteWiresByPod2(namespace string, podName string) error {
//	wires, ok := GetWiresByPod(namespace, podName)
//	if !ok || len(wires) == 0 {
//		grpcOvrlyLogger.Infof("[WIRE-DELETE]:All grpc-wires for pod %s:%s is already deleted", namespace, podName)
//		return nil
//	}
//	grpcOvrlyLogger.Infof("[WIRE-DELETE]:Removing %d grpc-wires for pod %s@%s", len(wires), podName, namespace)
//	var errs errlist.List
//	for _, w := range wires {
//		grpcOvrlyLogger.Infof("ERROR: [WIRE-DELETE]:Removing local-iface@pod : %s@%s for wire UID: %d", w.LocalPodIfaceName, w.LocalPodName, w.UID)
//		if err := RemoveWire(w); err != nil {
//			grpcOvrlyLogger.Infof("ERROR: [WIRE-DELETE]:Error Removing local-iface@pod : %s@%s for wire UID: %d : %v", w.LocalPodIfaceName, w.LocalPodName, w.UID, err)
//			errs.Add(err)
//		}
//	}
//	if errs.Err() != nil {
//		return fmt.Errorf("ERROR: [WIRE-DELETE]:failed to remove all grpc-wires for pod %s@%s: %w", podName, namespace, errs.Err())
//	}
//	grpcOvrlyLogger.Infof("[WIRE-DELETE]:Successfully removed all grpc-wires for pod %s@%s", podName, namespace)
//	return nil
//}

func DeleteWiresByPod(namespace string, podName string) error {
	wires.mu.Lock()
	defer wires.mu.Unlock()

	for _, wire := range wires.wires {
		if wire.LocalPodName == podName && wire.Namespace == namespace {
			close(wire.StopC)

			/* Remove the veth from the node */
			intf, err := net.InterfaceByIndex(int(wire.LocalNodeIfaceID))
			if err != nil {
				grpcOvrlyLogger.Errorf("[WIRE-DELETE]:Interface index %d for wire %d, is already cleaned up.", wire.LocalNodeIfaceID, wire.UID)
			} else {
				myVeth := koko.VEth{}
				myVeth.LinkName = intf.Name
				if err = myVeth.RemoveVethLink(); err != nil {
					grpcOvrlyLogger.Errorf("[WIRE-DELETE]:failed to remove veth link: %w", err)
				}
			}
			grpcOvrlyLogger.Infof("DeleteWiresByPod:[WIRE-DELETE]: Removing wire info from db, %s@%s-%s@%d, peer fid %d",
				wire.LocalPodName, wire.LocalPodIfaceName, wire.LocalNodeIfaceName, wire.LocalNodeIfaceID, wire.PeerIfaceID)

			wires.DeleteWoLock(wire)
		}
	}
	return nil
}

// ----------------------------------------------------------------------------------------------------------
func RemoveWire(wire *GRPCWire) error {

	if wire == nil {
		grpcOvrlyLogger.Infof("[WIRE-DELETE]:Null wire. This ware is already removed")
		return nil
	}

	/* stop the packet receive thread for this pod */
	grpcOvrlyLogger.Infof("[RemoveWire]: closing connection, %s%s", wire.LocalPodName, wire.LocalPodIfaceName)
	close(wire.StopC)

	/* Remove the veth from the node */
	intf, err := net.InterfaceByIndex(int(wire.LocalNodeIfaceID))
	if err != nil {
		grpcOvrlyLogger.Infof("[WIRE-DELETE]:Interface index %d for wire %d, is already cleaned up.", wire.LocalNodeIfaceID, wire.UID)
	} else {
		myVeth := koko.VEth{}
		myVeth.LinkName = intf.Name
		if err = myVeth.RemoveVethLink(); err != nil {
			return fmt.Errorf("[WIRE-DELETE]:failed to remove veth link: %w", err)
		}
	}

	DeleteWire(wire)
	grpcOvrlyLogger.Infof("[WIRE-DELETE]:Successfully removed grpc wire for link %d.", wire.UID)

	return nil
}

// -----------------------------------------------------------------------------------------------------------
func GetHostIntfHndl(intfID int64) (*pcap.Handle, error) {

	val, ok := wires.GetHandle(intfID)
	if ok {
		return val, nil
	}
	return nil, fmt.Errorf("node interface %d is not found in local db", intfID)

}

// -----------------------------------------------------------------------------------------------------------
// Generate the name of the interface to be placed on the node
func GenNodeIfaceName(podName string, podIfaceName string) (string, error) {
	// Linux has issue if interface name is too long. Generate a smaller name.
	// In recent kernel versions this is defined by IFNAMSIZ to be 16 bytes, so 15 user-visible bytes
	// (assuming it includes a trailing null). IFNAMSIZ is used in defining struct net_device's name.
	// The name must not contain / or any whitespace characters
	//
	//TODO: This method needs to be robust. It monotonically increases the index and never
	//      decreases it, even if the interfaces are deleted. So far this will work for accumulated
	//      1K interfaces per node under the current naming scheme. This is too small.
	//      Using 14 digit random number and checking if any interface with generated name exists and if
	//      exists then generate another random number (try 3 times before giving up). This will make it robust.
	//      This reduces the readability and corelation between the “pod-interface” and corresponding
	//      “node-interface”, for example eth1host1-<3-digit-index> will become "12345678901234".
	id := NextIndex()

	ifaceName := fmt.Sprintf("%.5s%.5s-%04d", podName, podIfaceName, id)

	return ifaceName, nil
}

// -----------------------------------------------------------------------------------------------------------
// When the remote peer tells the local node to create the local end of the grpc-wire
func CreateGRPCWireRemoteTriggered(wireDef *mpb.WireDef, stopC chan struct{}) (*GRPCWire, error) {

	var err error

	/* If this link already created then do nothing. This can happen due to a race between the local and remote peer.
	   We will allow only one to complete the creation. */
	grpcWire, ok := getOrUpdateWireByUID(wireDef.LocalPodNetNs, int(wireDef.LinkUid), wireDef.PeerIntfId)
	if ok {
		grpcWire.StopC = stopC
		if grpcWire.IsReady {
			grpcOvrlyLogger.Infof("[ADD-WIRE:REMOTE-END] This grpc-wire is already created by %s. Local interface id : %d peer interface id : %d", grpcWire.Originator, grpcWire.LocalNodeIfaceID, grpcWire.PeerIfaceID)
			return grpcWire, nil
		} else {
			grpcOvrlyLogger.Infof("[ADD-WIRE:REMOTE-END] Making up this grpc-wire created by %s. Local interface id : %d peer interface id : %d",
				grpcWire.Originator, grpcWire.LocalNodeIfaceID, grpcWire.PeerIfaceID)
			return grpcWire, nil
		}
	}

	outIfNm, err := GenNodeIfaceName(wireDef.LocalPodName, wireDef.IntfNameInPod)
	if err != nil {
		return nil, fmt.Errorf("[ADD-WIRE:REMOTE-END] could not get current network namespace: %v", err)
	}

	currNs, err := ns.GetCurrentNS()
	if err != nil {
		return nil, fmt.Errorf("[ADD-WIRE:REMOTE-END] could not get current network namespace: %v", err)
	}

	/* Create the veth to connect the pod with the meshnet daemon running on the node */
	hostEndVeth := koko.VEth{
		NsName:   currNs.Path(),
		LinkName: outIfNm,
	}

	inIfNm := wireDef.IntfNameInPod
	inContainerVeth := koko.VEth{
		NsName:   wireDef.LocalPodNetNs,
		LinkName: inIfNm,
	}

	if wireDef.LocalPodIp != "" {
		ipAddr, ipSubnet, err := net.ParseCIDR(wireDef.LocalPodIp)
		if err != nil {
			return nil, fmt.Errorf("failed to create remote end of GRPC wire(%s@%s), failed to parse CIDR %s: %w",
				inIfNm, wireDef.LocalPodName, wireDef.LocalPodIp, err)
		}
		inContainerVeth.IPAddr = []net.IPNet{{
			IP:   ipAddr,
			Mask: ipSubnet.Mask,
		}}
	}

	if err = koko.MakeVeth(inContainerVeth, hostEndVeth); err != nil {
		grpcOvrlyLogger.Errorf("[ADD-WIRE:REMOTE-END] Error creating vEth pair (in:%s <--> out:%s).  Error-> %s", inIfNm, outIfNm, err)
		return nil, err
	}
	if err := wireutil.SetTxChecksumOff(inContainerVeth.LinkName, inContainerVeth.NsName); err != nil {
		grpcOvrlyLogger.Errorf("Error in setting tx checksum-off on interface %s, pod %s: %v", inContainerVeth.LinkName, wireDef.LocalPodName, err)
		// not returning
	}
	locIface, err := net.InterfaceByName(hostEndVeth.LinkName)
	if err != nil {
		grpcOvrlyLogger.Errorf("[ADD-WIRE:REMOTE-END] Could not get interface index for %s. error:%v", hostEndVeth.LinkName, err)
		return nil, err
	}
	grpcOvrlyLogger.Infof("[ADD-WIRE:REMOTE-END] Trigger from %s:%d : Successfully created pod to node vEth pair %s@%s <--> %s(%d).",
		wireDef.PeerIp, wireDef.PeerIntfId, inIfNm, wireDef.LocalPodName, outIfNm, locIface.Index)
	aWire := &GRPCWire{
		UID: int(wireDef.LinkUid),

		LocalNodeIfaceID:   int64(locIface.Index),
		LocalNodeIfaceName: hostEndVeth.LinkName,
		LocalPodIP:         wireDef.LocalPodIp,
		LocalPodIfaceName:  wireDef.IntfNameInPod,
		LocalPodName:       wireDef.LocalPodName,
		LocalPodNetNS:      wireDef.LocalPodNetNs,

		PeerIfaceID: wireDef.PeerIntfId,
		PeerNodeIP:  wireDef.PeerIp,

		IsReady:      true,
		Originator:   PEER_CREATED_WIRE,
		OriginatorIP: wireDef.PeerIp,

		StopC:     stopC,
		Namespace: wireDef.KubeNs,
	}
	/* Utilizing google gopacket for polling for packets from the node. This seems to be the
	   simplest way to get all packets.
	   As an alternative to google gopacket(pcap), a socket based implementation is possible.
	   Not sure if socket based implementation can bring any advantage or not.

	   Near term will replace pcap by socket.
	*/
	wrHandle, err := pcap.OpenLive(hostEndVeth.LinkName, 65365, true, pcap.BlockForever)
	if err != nil {
		grpcOvrlyLogger.Errorf("[ADD-WIRE:REMOTE-END] Could not open interface (%d) for sed/recv packets for containers. error:%v", locIface.Index, err)
		return nil, err
	}

	AddWire(aWire, wrHandle)

	//gWire, ok := GetWireByUID(wireDef.LocalPodNetNs, int(wireDef.LinkUid))
	//if ok {
	//	grpcOvrlyLogger.Infof("[ADD-WIRE:REMOTE-END] Added wire info, %s@%s-%s@%d-%d", gWire.LocalPodName, gWire.LocalPodIfaceName, gWire.LocalNodeIfaceName, gWire.LocalNodeIfaceID, gWire.PeerIfaceID)
	//} else {
	//	grpcOvrlyLogger.Errorf("ERROR: [ADD-WIRE:REMOTE-END] Did not find added wire info, %s@%s@%d-%d", wireDef.LocalPodName, wireDef.IntfNameInPod, locIface.Index, wireDef.PeerIntfId)
	//}

	return aWire, nil
}

// -----------------------------------------------------------------------------------------------------------
// When the remote peer tells the local node to remove the local end of the grpc-wire info
func GRPCWireDownRemoteTriggered(wireDef *mpb.WireDef) error {

	err := WireDownByUID(wireDef.LocalPodNetNs, int(wireDef.LinkUid))
	if err != nil {
		grpcOvrlyLogger.Infof("[DOWN-WIRE:REMOTE-END] Failed in making down wire end in pod %s@%s,. Link uid : %d",
			wireDef.LocalPodName, wireDef.IntfNameInPod, wireDef.LinkUid)
		return nil
	}

	return nil
}

// -----------------------------------------------------------------------------------------------------------
func RecvFrmLocalPodThread(wire *GRPCWire) error {

	defaultPort := "51111"             //+++todo: use proper constant as defined in some other file
	pktBuffSz := int32(1024 * 64 * 10) //keep buffer for MAX 10 64K frames

	url := strings.TrimSpace(fmt.Sprintf("%s:%s", wire.PeerNodeIP, defaultPort))
	/* Utilizing google gopacket for polling for packets from the node. This seems to be the
	   simplest way to get all packets.
	   As an alternative to google gopacket(pcap), a socket based implementation is possible.
	   Not sure if socket based implementation can bring any advantage or not.

	   Near term will replace pcap by socket.
	*/

	_, err := net.InterfaceByName(wire.LocalNodeIfaceName)
	if err != nil {
		grpcOvrlyLogger.Errorf("[ADD-WIRE:LOCAL-END]For pod %s failed to retrieve interface %s/%d. error: %v", wire.LocalPodName, wire.LocalNodeIfaceName, wire.LocalNodeIfaceID, err)
		return err
	}

	rdHandl, err := pcap.OpenLive(wire.LocalNodeIfaceName, pktBuffSz, true, pcap.BlockForever)
	if err != nil {
		grpcOvrlyLogger.Errorf("Receive Thread for local pod failed to open interface: %s/%d, PCAP ERROR: %v", wire.LocalNodeIfaceName, wire.LocalNodeIfaceID, err)
		return err
	}
	defer rdHandl.Close()

	err = rdHandl.SetDirection(pcap.Direction(pcap.DirectionIn))
	if err != nil {
		grpcOvrlyLogger.Errorf("Receive Thread for local pod failed to set up capture direction: %s/%d, PCAP ERROR: %v", wire.LocalNodeIfaceName, wire.LocalNodeIfaceID, err)
		return err
	}

	remote, err := grpc.Dial(url, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		grpcOvrlyLogger.Infof("RecvFrmLocalPodThread:Failed to connect to remote %s/%d", url, wire.LocalNodeIfaceID)
		return err
	}
	defer remote.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	source := gopacket.NewPacketSource(rdHandl, rdHandl.LinkType())
	wireClient := mpb.NewWireProtocolClient(remote)

	in := source.Packets()
	var packet gopacket.Packet
	for {
		select {
		case <-wire.StopC:
			grpcOvrlyLogger.Infof("RecvFrmLocalPodThread: closing connection with remote peer-iface@peer-node-ip: %d@%s/%d from %s@%s",
				wire.PeerIfaceID, wire.PeerNodeIP, wire.LocalNodeIfaceID, wire.LocalPodName, wire.LocalPodIfaceName)
			return io.EOF
		case packet = <-in:
			data := packet.Data()
			payload := &mpb.Packet{
				RemotIntfId: wire.PeerIfaceID,
				Frame:       data,
			}

			/*+++TODO: Ethernet has a minimum frame size of 64 bytes, comprising an 18-byte header and a payload of 46 bytes.
			It also has a maximum frame size of 1518 bytes, in which case the payload is 1500 bytes.
			This logic needs to be better, take the interface MTU not hardcoded value of 1518.
			This is a very unusual condition to receive an packet from the pod with size > MTU. This can only happens if
			things gets really messed up.   */
			if len(data) > 1518 {
				pktType := DecodeFrame(payload.Frame)
				grpcOvrlyLogger.Infof("RecvFrmLocalPodThread: unusually large packet received from local pod (may be GRO enabled). size: %d, pkt:%s", len(data), pktType)
				/* When Generic Receive Offload (GRO) is enabled then containers can send packets larger than MTU size packet. Do not drop these
				   packets, deliver it to the receiving container to process.
				*/
				//continue
			}

			ok, err := wireClient.SendToOnce(ctx, payload)
			if err != nil || !ok.Response {
				grpcOvrlyLogger.Infof("RecvFrmLocalPodThread: Could not deliver pkt %s@%s@%s. Peer not ready, remote iface id %d. err=%v",
					wire.LocalPodName, wire.LocalPodIfaceName, wire.LocalNodeIfaceName, wire.PeerIfaceID, err)
				/* +++ we generate information and continue. As the above errors will happen when the remote end is not yet ready.
				       It will eventually get ready and if it can't then some will stop this thread.
				return err
				*/
			}
		}
	}
}
