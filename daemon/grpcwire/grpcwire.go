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
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	mpb "github.com/networkop/meshnet-cni/daemon/proto/meshnet/v1beta1"
	koko "github.com/redhat-nfvpe/koko/api"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type intfIndex struct {
	mu     sync.Mutex
	currId int64
}

/* In a given node a veth-pair connects a pod with the meshnet daemon hosted in the node. This meshnet
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
	PeerPodIP   string // Peer pod IP

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
func AddWire(wire *GRPCWire, handle *pcap.Handle) int {
	/* Populate the active wire map and returns the number of currently added active wires. */

	/* if this wire is already present in the map then it will be overwritten.
	   It seems to be ok to overwrite. Think more in what situation this may
	   not be the desired behavior and we need to throw an error. */
	wire.IsReady = true

	wires.Add(wire, handle)
	return len(wires.wires)
}

//-------------------------------------------------------------------------------------------------
// DeleteWire cleans up the active wire map and returns the number of currently added active wire.
func DeleteWire(wire *GRPCWire) int {
	wires.Delete(wire)
	return len(wires.wires)
}

//-----------------------------------------------------------------------------------------------------------

func DeleteWiresByPod(namespace string, podName string) error {

	log.Infof("Removing grpc-wire for pod %s:%s", namespace, podName)
	wires, ok := GetWiresByPod(namespace, podName)
	if !ok || len(wires) == 0 {
		log.Infof("No grpc-wire for pod %s:%s", namespace, podName)
		return nil
	}

	resp := true
	var fstErr error
	for _, w := range wires {
		if err := RemoveWire(w); err != nil {
			log.Infof("Error while removing GRPC wire for  pod: %s@%s. err:%v", w.LocalPodIfaceName, w.LocalPodName, err)
			// instead of failing, just log the error and move on
			resp = false
			if fstErr == nil {
				fstErr = err
				// even if we have an error for this link, we still try to remove the other links
			}
		}
		if resp {
			log.Infof("Removed all grpc-wire for pod: %s@%s", w.LocalPodIfaceName, w.LocalPodName)
		}
	}

	// only reports the first error
	return fmt.Errorf("failed to remove gRPC wire for pod %s: %w", podName, fstErr)
}

//----------------------------------------------------------------------------------------------------------
func RemoveWire(wire *GRPCWire) error {

	/* stop the packet receive thread for this pod */
	close(wire.StopC)

	/* Remove the veth from the node */
	intf, err := net.InterfaceByIndex(int(wire.LocalNodeIfaceID))
	if err != nil {
		return fmt.Errorf("failed to etrieve interface data for interface index %d, for link %d: %w", wire.LocalNodeIfaceID, wire.UID, err)
	}
	myVeth := koko.VEth{}
	myVeth.LinkName = intf.Name
	if err = myVeth.RemoveVethLink(); err != nil {
		return fmt.Errorf("failed to remove veth link: %w", err)
	}

	DeleteWire(wire)

	log.Infof("Successfully removed grpc wire for link %d.", wire.UID)
	return nil
}

//-----------------------------------------------------------------------------------------------------------
func GetHostIntfHndl(intfID int64) (*pcap.Handle, error) {

	val, ok := wires.GetHandle(intfID)
	if ok {
		return val, nil
	}
	return nil, fmt.Errorf("node interface %d is not found in local db", intfID)

}

//-----------------------------------------------------------------------------------------------------------
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

//-----------------------------------------------------------------------------------------------------------
//When the remote peer tells the local node to create the local end of the grpc-wire
func CreateGRPCWireRemoteTriggered(wireDef *mpb.WireDef, stopC chan struct{}) (*GRPCWire, error) {

	var err error

	/* If this link already created then do nothing. This can happen due to a race between the local and remote peer.
	   We will allow only one to complete the creation. */
	grpcWire, ok := GetWireByUID(wireDef.LocalPodNetNs, int(wireDef.LinkUid))
	if ok && grpcWire.IsReady {
		log.Infof("This grpc-wire is already created by %s. Local interface id : %d peer interface id : %d", grpcWire.Originator, grpcWire.LocalNodeIfaceID, grpcWire.PeerIfaceID)
		return grpcWire, nil
	}

	outIfNm, err := GenNodeIfaceName(wireDef.LocalPodName, wireDef.IntfNameInPod)
	if err != nil {
		return nil, fmt.Errorf("could not get current network namespace: %v", err)
	}

	currNs, err := ns.GetCurrentNS()
	if err != nil {
		return nil, fmt.Errorf("could not get current network namespace: %v", err)
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
		log.Infof("Error creating vEth pair (in:%s <--> out:%s).  Error-> %s", inIfNm, outIfNm, err)
		return nil, err
	}
	locIface, err := net.InterfaceByName(hostEndVeth.LinkName)
	if err != nil {
		log.Fatalf("Could not get interface index for %s. error:%v", hostEndVeth.LinkName, err)
		return nil, err
	}
	log.Infof("On Remote Trigger from %s:%d : Successfully created vEth pair (in(name):%s <--> out(name)-index:%s:%d).", wireDef.PeerIp, wireDef.PeerIntfId, inIfNm, outIfNm, locIface.Index)
	aWire := &GRPCWire{
		UID: int(wireDef.LinkUid),

		LocalNodeIfaceID:   int64(locIface.Index),
		LocalNodeIfaceName: hostEndVeth.LinkName,
		LocalPodIP:         wireDef.LocalPodIp,
		LocalPodIfaceName:  wireDef.IntfNameInPod,
		LocalPodName:       wireDef.LocalPodName,
		LocalPodNetNS:      wireDef.LocalPodNetNs,

		PeerIfaceID: wireDef.PeerIntfId,
		PeerPodIP:   wireDef.PeerIp,

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
		log.Fatalf("Could not open interface for sed/recv packets for containers. error:%v", err)
		return nil, err
	}

	AddWire(aWire, wrHandle)
	return aWire, nil
}

//-----------------------------------------------------------------------------------------------------------
func RecvFrmLocalPodThread(wire *GRPCWire) error {

	defaultPort := "51111"             //+++todo: use proper constant as defined in some other file
	pktBuffSz := int32(1024 * 64 * 10) //keep buffer for MAX 10 64K frames

	url := strings.TrimSpace(fmt.Sprintf("%s:%s", wire.PeerPodIP, defaultPort))
	/* Utilizing google gopacket for polling for packets from the node. This seems to be the
	   simplest way to get all packets.
	   As an alternative to google gopacket(pcap), a socket based implementation is possible.
	   Not sure if socket based implementation can bring any advantage or not.

	   Near term will replace pcap by socket.
	*/
	rdHandl, err := pcap.OpenLive(wire.LocalNodeIfaceName, pktBuffSz, true, pcap.BlockForever)
	if err != nil {
		log.Fatalf("Receive Thread for local pod failed to open interface: %s, PCAP ERROR: %v", wire.LocalNodeIfaceName, err)
		return err
	}
	defer rdHandl.Close()

	err = rdHandl.SetDirection(pcap.Direction(pcap.DirectionIn))
	if err != nil {
		log.Fatalf("Receive Thread for local pod failed to set up capture direction: %s, PCAP ERROR: %v", wire.LocalNodeIfaceName, err)
		return err
	}

	remote, err := grpc.Dial(url, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Infof("RecvFrmLocalPodThread:Failed to connect to remote %s", url)
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
			log.Printf("Receive thread closing connection with peer: %s@%d", wire.PeerPodIP, wire.PeerIfaceID)
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
				log.Infof("Daemon(RecvFrmLocalPodThread): unusually large packet received from local pod (may be GRO enabled). size: %d, pkt:%s", len(data), pktType)
				/* When Generic Receive Offload (GRO) is enabled then containers can send packets larger than MTU size packet. Do not drop these
				   packets, deliver it to the receiving container to process.
				*/
				//continue
			}

			ok, err := wireClient.SendToOnce(ctx, payload)
			if err != nil || !ok.Response {
				if err != nil {
					log.Infof("Daemon(RecvFrmLocalPodThread): Failed to send packet to remote. err=%v", err)
				} else {
					err = fmt.Errorf("RecvFrmLocalPodThread:Failed to send packet to remote. GRPC return code: false")
					log.Infof("Daemon(RecvFrmLocalPodThread):err= %v", err)
				}
				/* +++ we generate the error and continue. As the above errors will happen when the remote end is not yet ready.
				       It will eventually get ready and if it can not then some will stop this thread.
				return err
				*/
			}
		}
	}
}

//------------------------------------------------------------------------------------------------------
func DecodeFrame(frame []byte) string {
	pktTypeStr := ""
	numPkts := 1
	totalLen := len(frame)
	etherHdrLen := 14
	totalDecodedLen := 0

	for {
		packet := gopacket.NewPacket(frame, layers.LayerTypeEthernet, gopacket.Default)
		ethernetLayer := packet.Layer(layers.LayerTypeEthernet)
		if ethernetLayer != nil {
			ethernetPacket, _ := ethernetLayer.(*layers.Ethernet)
			pktTypeStr += fmt.Sprintf("Pkt no %d: ", numPkts) + "Ethernet"
			decodedLen, typeStr := DecodePkt(packet, ethernetPacket.NextLayerType(), ethernetPacket.Length)
			pktTypeStr += typeStr
			totalDecodedLen += etherHdrLen + decodedLen
			remainingLen := totalLen - totalDecodedLen
			if remainingLen >= 14 {
				numPkts++
				frame = frame[totalDecodedLen:]
				pktTypeStr += "\n            "
			} else {
				break
			}
		} else {
			break
		}
	}
	if numPkts > 1 {
		pktTypeStr = "Multi Pkts: " + pktTypeStr
	}

	return pktTypeStr
}

func DecodePkt(packet gopacket.Packet, layerType gopacket.LayerType, length uint16) (int, string) {
	var typeStr, pktTypeStr string
	decodedLen := 0

	if layerType == layers.LayerTypeIPv4 {
		decodedLen, typeStr = decodeIPv4Pkt(packet)
		pktTypeStr += typeStr
	} else if layerType == layers.LayerTypeIPv6 {
		decodedLen, typeStr = decodeIPv6Pkt(packet)
		pktTypeStr += typeStr
	} else if layerType == layers.LayerTypeLLC {
		llcLayer := packet.Layer(layers.LayerTypeLLC)
		if llcLayer != nil {
			pktTypeStr += ":LLC"
			llcPacket, _ := llcLayer.(*layers.LLC)
			if llcPacket.DSAP == 0xFE && llcPacket.SSAP == 0xFE && llcPacket.Control == 0x3 {
				if llcPacket.Payload[0] == 0x83 {
					pktTypeStr += ":ISIS"
				}
			}
		}
		decodedLen = int(length)
	} else if layerType == layers.LayerTypeARP {
		pktTypeStr += ":ARP"
		decodedLen = 28
	} else if layerType == layers.LayerTypeDot1Q {
		pktTypeStr += ":VLAN"
		//fmt.Printf("VLAN\n")
		vlanHdrLen := 4
		vlanLayer := packet.Layer(layers.LayerTypeDot1Q)
		if vlanLayer != nil {
			vlanPacket, _ := vlanLayer.(*layers.Dot1Q)
			nextLayer := vlanPacket.NextLayerType()
			if nextLayer == gopacket.LayerTypeZero {
				// this may be LLC layer. try to match with known LLC 0xFEFE03
				if vlanPacket.Payload[0] == 0xFE && vlanPacket.Payload[1] == 0xFE && vlanPacket.Payload[2] == 0x03 {
					packet := gopacket.NewPacket(vlanPacket.Payload, layers.LayerTypeLLC, gopacket.Default)
					decodedLen, typeStr = DecodePkt(packet, layers.LayerTypeLLC, uint16(vlanPacket.Type))
					pktTypeStr += typeStr
					decodedLen += vlanHdrLen
				}
			} else {
				packet := gopacket.NewPacket(vlanPacket.Payload, nextLayer, gopacket.Default)
				decodedLen, typeStr = DecodePkt(packet, nextLayer, 0)
				pktTypeStr += typeStr
				decodedLen += vlanHdrLen
			}
		} else {
			pktTypeStr += ":No VLAN Hdr"
		}
	}

	return decodedLen, pktTypeStr
}

func decodeIPv4Pkt(packet gopacket.Packet) (int, string) {
	var decodedLen int = 0
	pktTypeStr := ":IPv4"

	ipLayer := packet.Layer(layers.LayerTypeIPv4)
	if ipLayer != nil {
		ipPacket, _ := ipLayer.(*layers.IPv4)
		decodedLen = int(ipPacket.Length)

		pktTypeStr += fmt.Sprintf("[s:%s, d:%s]", ipPacket.SrcIP.String(), ipPacket.DstIP.String())
		if ipPacket.Protocol == layers.IPProtocolICMPv4 {
			pktTypeStr += ":ICMP"
		} else if ipPacket.Protocol == layers.IPProtocolTCP {
			pktTypeStr += ":TCP"
			tcpLayer := packet.Layer(layers.LayerTypeTCP)
			if tcpLayer != nil {
				tcpPkt := tcpLayer.(*layers.TCP)
				if tcpPkt.DstPort == 179 {
					pktTypeStr += ":BGP"
				} else {
					pktTypeStr += fmt.Sprintf(":[Port:%d]", tcpPkt.DstPort)
				}
			}
		} else {
			pktTypeStr += fmt.Sprintf(":IPv4 with protocol : %d", ipPacket.Protocol)
		}
	}
	return decodedLen, pktTypeStr
}

func decodeIPv6Pkt(packet gopacket.Packet) (int, string) {
	var decodedLen int = 0
	pktTypeStr := ":IPv6"

	ipLayer := packet.Layer(layers.LayerTypeIPv6)
	if ipLayer != nil {
		ipPacket, _ := ipLayer.(*layers.IPv6)
		decodedLen = int(ipPacket.Length)

		pktTypeStr += fmt.Sprintf("[s:%s, d:%s]", ipPacket.SrcIP.String(), ipPacket.DstIP.String())
		if ipPacket.NextHeader == layers.IPProtocolICMPv6 {
			pktTypeStr += ":ICMPv6"
		} else if ipPacket.NextHeader == layers.IPProtocolTCP {
			pktTypeStr += ":TCP"
			tcpLayer := packet.Layer(layers.LayerTypeTCP)
			if tcpLayer != nil {
				tcpPkt := tcpLayer.(*layers.TCP)
				if tcpPkt.DstPort == 179 {
					pktTypeStr += ":BGP"
				} else {
					pktTypeStr += fmt.Sprintf("[Port:%d]", tcpPkt.DstPort)
				}
			}
		} else {
			pktTypeStr += fmt.Sprintf(":IPv6 with protocol : %d", ipPacket.NextHeader)
		}
	}
	return decodedLen, pktTypeStr
}
