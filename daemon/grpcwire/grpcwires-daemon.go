//package vxlan
package grpcwire

import (
	"context"
	"fmt"
	"net"
	"strconv"
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
)

const locPdIntfNmPrefix = "-grpc"

type IntfIndex struct {
	currId int64
	mu     sync.Mutex
}

/* In a given node a veth-pair connects a pod with the meshnet daemon hosted in the node. This meshnet
daemon provides the grpc-wire to the local pod and connet it with the remote pod over grpc. The node
end of the veth-pair must have uniquince name with in the node. A node can have multiple pods. So there
will be multiple veth-pairs for conneting multiple nodes and each of them (the node end) must have unique
names. IntfIndex provides the sequentially incresing number which makes the name unique when added as
suffix to the name.

The reason we can not use meshnet link uid, as uid unique in a topology. What happens when multiple
topologies are running in the same cluster ?  I am not sure in that case all the link across multiple
topologies will have unique uid or not. If not, then then using IntfIndex will still make the interface
name unique (when used in the name).
*/
var indexGen IntfIndex

func GetNextIndex() int64 {
	indexGen.mu.Lock()
	defer indexGen.mu.Unlock()
	indexGen.currId++
	return indexGen.currId
}

const (
	HOST_CREATED_WIRE = iota
	PEER_CREATED_WIRE
)

type GRPCWire struct {
	Uid       int    // uid identify a perticular link in a topology as per meshnet crd
	Namespace string // K8s namesapce this wire belongs to

	/* Node information */
	LocalNodeIntfID int64  // OS assigned interface ID of local node interface for this wire
	LocalNodeIntfNm string // name of local node interface for this wire

	/* Pod information : where this wire is terminating in this node */
	LocalPodIP     string // IP address of the local container who will consume packets over this wire. This is for debugging. This is generally not available when links are getting created and is not necessary also.
	LocalPodNm     string // Name the local pod who will consume packets over this wire.
	LocalPodIntfNm string // Name the interface which is inside the local container who will consume packets over this wire. This is for debugging
	LocalPodNetNS  string

	/*Peer pod information*/
	PeerInffID int64  // Peer node interface ID for this wire
	PeerPodIP  string // Peer pod IP

	IsReady       bool   // Is this wire ip.
	HowCreated    int    // create by local host or create on trigger from remote host. This is for debugging.
	CreaterHostIP string // IP address of the host created it. This is for debugging.

	StopC chan bool // the channel to send stop signal to the receive thread.
}

type PodLink struct {
	netns   string
	linkuid int
}
type IdToWireMap struct {
	//map[link-uid]->wire-pointer
	allWires map[PodLink]*GRPCWire
	mu       sync.Mutex
}

/* A grpc-wire creation can be triggered by either host at the end of the wire. They can even trigger
 * it simultaneously. Irrespective of who triggers, successful creation needs activities at both host
 * end. Our intention is to finish the wire creation at the first trigger. This map keeps the list of
 * wires which are already crated and must not be recreated, if any second trigger is received.
 */
var wiresByUID = IdToWireMap{
	allWires: make(map[PodLink]*GRPCWire),
}

type GRPCWireHandleMap struct {
	/* Used when a packet is received, then we know the id of the interface to which the packet to be delivered.
	   This map take interface-id as key adn returns the corresponding pcap-handle for delivering the packet.
	   map[interface-id]->pcap-handle */
	allHandles map[int64]*pcap.Handle
	mu         sync.Mutex
}

/*
  map[host-interface-index]->pacp-handle to deliver packets to the container
*/
var wireHndlsByIntfIdx GRPCWireHandleMap

//-------------------------------------------------------------------------------------------------
func GetAllActiveWires(kubeNs string, podNm string) ([]*GRPCWire, bool) {

	wiresByUID.mu.Lock()
	defer wiresByUID.mu.Unlock()
	ret := make([]*GRPCWire, 0, 10)

	for _, wire := range wiresByUID.allWires {
		if (wire.LocalPodNm == podNm) && (wire.Namespace == kubeNs) {
			ret = append(ret, wire)
		}
	}
	return ret, true
}

//-------------------------------------------------------------------------------------------------

func GetActiveWire(linkuid int, netns string) (*GRPCWire, bool) {

	if wiresByUID.allWires == nil {
		return &GRPCWire{}, false
	}

	val, ok := wiresByUID.allWires[PodLink{
		netns:   netns,
		linkuid: linkuid}]

	if ok {
		return val, true
	}

	return nil, false
}

//-------------------------------------------------------------------------------------------------
func AddActiveWire(wire *GRPCWire, handle *pcap.Handle) int {
	/* Populate the actiwire map and returns the number of currently added active wire. */
	wiresByUID.mu.Lock()
	defer wiresByUID.mu.Unlock()

	if wiresByUID.allWires == nil {
		wiresByUID.allWires = make(map[PodLink]*GRPCWire)
	}

	if wireHndlsByIntfIdx.allHandles == nil {
		wireHndlsByIntfIdx.allHandles = make(map[int64]*pcap.Handle)
	}

	/*+++think: if this ware is already present in the map then it will be overwritten.
	            It seems to be ok to overwrite. Think more in what situation this may
				not be the desired behavior and we need to throw an error. */
	wire.IsReady = true

	wiresByUID.allWires[PodLink{
		netns:   wire.LocalPodNetNS,
		linkuid: wire.Uid}] = wire

	wireHndlsByIntfIdx.allHandles[wire.LocalNodeIntfID] = handle
	return len(wiresByUID.allWires)
}

//-------------------------------------------------------------------------------------------------
func RemActiveWireMaps(wire *GRPCWire) int {
	/* Populate the actiwire map and returns the number of currently added active wire. */
	wiresByUID.mu.Lock()
	defer wiresByUID.mu.Unlock()

	uid := wire.Uid
	intfID := wire.LocalNodeIntfID
	netns := wire.LocalPodNetNS

	if wiresByUID.allWires != nil {
		delete(wiresByUID.allWires, PodLink{
			netns:   netns,
			linkuid: uid})
	}

	if wireHndlsByIntfIdx.allHandles != nil {
		delete(wireHndlsByIntfIdx.allHandles, intfID)
	}

	return len(wiresByUID.allWires)
}

//-----------------------------------------------------------------------------------------------------------

func RemWireFrmPod(kubeNs string, podNm string) error {

	log.Infof("Removing grpc-wire for pod %s in namespace: %s ", podNm, kubeNs)
	allwires, ok := GetAllActiveWires(kubeNs, podNm)
	if !ok || len(allwires) == 0 {
		log.Infof("No grpc-wire for pod %s", podNm)
		return nil
	}

	resp := true
	var fstErr error = nil
	for _, w := range allwires {
		err := RemWire(w)
		if err != nil {
			log.Infof("Error while removing GRPC wire for  pod: %s@%s. err:%v", w.LocalPodIntfNm, w.LocalPodNm, err)
			// instead of failing, just log the error and move on
			resp = false
			if fstErr == nil {
				fstErr = err
				// even if we have an error for this link, we still try to remove the other links
			}
		}
		if resp {
			log.Infof("Removed all grpc-wire for pod: %s@%s", w.LocalPodIntfNm, w.LocalPodNm)
		}
	}

	return fmt.Errorf("Error while removing GRPC wire for pod %s. err:%v", podNm, fstErr)
}

//----------------------------------------------------------------------------------------------------------
func RemWire(wire *GRPCWire) error {

	/* stop the packet receive thread for this pod */
	wire.StopC <- true

	/* Remove the veth from the node */
	intf, err := net.InterfaceByIndex(int(wire.LocalNodeIntfID))
	if err != nil {
		return fmt.Errorf("Could not retrive interface data for interface index %d, for link %d. err:%v", wire.LocalNodeIntfID, wire.Uid, err)
	}
	myVeth := koko.VEth{}
	myVeth.LinkName = intf.Name
	if err = myVeth.RemoveVethLink(); err != nil {
		return fmt.Errorf("Error removing Veth link: %s", err)
	}

	RemActiveWireMaps(wire)

	log.Infof("Successfully removed grpc wire for link %d.", wire.Uid)
	return nil
}

//-----------------------------------------------------------------------------------------------------------
func GetHostIntfHndl(intfID int64) (*pcap.Handle, error) {

	val, ok := wireHndlsByIntfIdx.allHandles[intfID]
	if ok {
		return val, nil
	}
	return nil, fmt.Errorf("Interface %d is not active.", intfID)

}

//-----------------------------------------------------------------------------------------------------------
func CreateGRPCWireRemoteTriggered(wireDefRemot *mpb.WireDef, stopC *chan bool) (*GRPCWire, error) {

	var err error

	/* If this link already created then do nothing. This can happen due to a race between the local and remote peer.
	   We will allow only one to complete the creation. */
	grpcwire, ok := GetActiveWire(int(wireDefRemot.LinkUid), wireDefRemot.LocalPodNetNs)
	if (ok == true) && (grpcwire.IsReady == true) {
		who := ""
		if grpcwire.HowCreated == HOST_CREATED_WIRE {
			who = "local host"
		} else if grpcwire.HowCreated == PEER_CREATED_WIRE {
			who = "remote peer"
		} else {
			who = "unknown host"
		}
		log.Infof("This grpc-wire is already created by %s. Local interface id : %d peer interface id : %d", who, grpcwire.LocalNodeIntfID, grpcwire.PeerInffID)
		return grpcwire, nil
	}

	idVeth := GetNextIndex()
	/*Linux has problem if the interface name is big. (+++todo: add the documentation here) */
	nmLen1 := len(wireDefRemot.IntfNameInPod)
	nmLen2 := len(wireDefRemot.LocalPodNm)
	if nmLen1 > 5 {
		nmLen1 = 5
	}
	if nmLen2 > 5 {
		nmLen2 = 5
	}

	//eth1host1-<index>
	outIfNm := wireDefRemot.IntfNameInPod[0:nmLen1] + wireDefRemot.LocalPodNm[0:nmLen2] + "-" + strconv.FormatInt(idVeth, 10)

	currNs, err := ns.GetCurrentNS()
	/*+++todo : add error handling */
	hostEndVeth := koko.VEth{
		NsName:   currNs.Path(),
		LinkName: outIfNm,
	}

	inIfNm := wireDefRemot.IntfNameInPod
	inContainerVeth := koko.VEth{
		NsName:   wireDefRemot.LocalPodNetNs,
		LinkName: inIfNm,
	}

	if wireDefRemot.LocalPodIp != "" {
		ipAddr, ipSubnet, err := net.ParseCIDR(wireDefRemot.LocalPodIp)
		if err != nil {
			return nil, fmt.Errorf("While creating remote end of GRPC wire(%s@%s), failed to parse CIDR %s: %s",
				inIfNm, wireDefRemot.LocalPodNm, wireDefRemot.LocalPodIp, err)
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
	locInf, err := net.InterfaceByName(hostEndVeth.LinkName)
	if err != nil {
		log.Fatalf("Could not get interface index for %s. error:%v", hostEndVeth.LinkName, err)
		return nil, err
	}
	log.Infof("On Remote Trigger: Successfully created vEth pair (in(name):%s <--> out(name-index):%s:%d).", inIfNm, outIfNm, locInf.Index)
	aWire := GRPCWire{
		Uid: int(wireDefRemot.LinkUid),

		LocalNodeIntfID: int64(locInf.Index),
		LocalNodeIntfNm: hostEndVeth.LinkName,
		LocalPodIP:      "Not Available",
		LocalPodIntfNm:  wireDefRemot.IntfNameInPod,
		LocalPodNm:      wireDefRemot.LocalPodNm,
		LocalPodNetNS:   wireDefRemot.LocalPodNetNs,

		PeerInffID: wireDefRemot.PeerIntfId,
		PeerPodIP:  wireDefRemot.PeerIp,

		IsReady:       true,
		HowCreated:    PEER_CREATED_WIRE,
		CreaterHostIP: wireDefRemot.PeerIp,

		StopC:     *stopC,
		Namespace: wireDefRemot.KubeNs,
	}

	handle, err := pcap.OpenLive(hostEndVeth.LinkName, 65365, true, pcap.BlockForever)
	if err != nil {
		log.Fatalf("Could not open interface for sed/recv packets for containers. error:%v", err)
		return nil, err
	}

	AddActiveWire(&aWire, handle)
	return &aWire, nil
}

//-----------------------------------------------------------------------------------------------------------
func RecvFrmLocalPodThread(wire *GRPCWire) error {

	defaultPort := "51111" //+++todo: use proper constant as defined in some other file
	pktBuffSz := int32(1024 * 64)

	url := strings.TrimSpace(fmt.Sprintf("%s:%s", wire.PeerPodIP, defaultPort))
	handler, err := pcap.OpenLive(wire.LocalNodeIntfNm, pktBuffSz, true, pcap.BlockForever)
	if err != nil {
		log.Fatalf("Receive Thread for local pod failed to open interface: %s, PCAP ERROR: %v", wire.LocalNodeIntfNm, err)
		return err
	}
	defer handler.Close()

	err = handler.SetDirection(pcap.DirectionIn)
	if err != nil {
		log.Fatalf("Receive Thread for local pod failed to set up capture direction: %s, PCAP ERROR: %v", wire.LocalNodeIntfNm, err)
		return err
	}

	remote, err := grpc.Dial(url, grpc.WithInsecure())
	if err != nil {
		log.Infof("RecvFrmLocalPodThread:Failed to connect to remote %s", url)
		return err
	}
	defer remote.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	source := gopacket.NewPacketSource(handler, handler.LinkType())

	wireClient := mpb.NewWireProtocolClient(remote)

	in := source.Packets()
	for {
		var packet gopacket.Packet
		select {
		case <-(wire.StopC):
			log.Printf("Receive thread closing connection with peer: %s@%d", wire.PeerPodIP, wire.PeerInffID)
			break
		case packet = <-in:
			// Decode for debugging
			// pktType := "Others"
			// ethernetLayer := packet.Layer(layers.LayerTypeEthernet)
			// // if ethernetLayer != nil {
			// ethernetPacket, _ := ethernetLayer.(*layers.Ethernet)
			// if ethernetPacket.EthernetType == 0x86DD {
			// 	pktType = "IPv6"
			// } else if ethernetPacket.EthernetType == 0x0806 {
			// 	pktType = "ARP"
			// } else if ethernetPacket.EthernetType == 0x0800 {
			// 	pktType = "IPv4"
			// }
			// ipLayer := packet.Layer(layers.LayerTypeIPv4)
			// if ipLayer != nil {
			// 	ipPacket, _ := ipLayer.(*layers.IPv4)
			// 	if ipPacket.Protocol == 0x1 {
			// 		pktType = "IPv4:ICMP"
			// 	}
			// }
			// log.Printf("+++Daemon(RecvFrmLocalPodThread): Sent pkt to remote[pkt_type:%s, bytes: %d, Dest intf: %s@%d]", pktType, len(packet.Data()), wire.PeerPodIP, wire.PeerInffID)
			data := packet.Data()
			payload := &mpb.Packet{
				RemotIntfId: wire.PeerInffID,
				Frame:       data,
				FrameLen:    int64(len(data)),
			}

			/*+++TODO: Ethernet has a minimum frame size of 64 bytes, comprising an 18-byte header and a payload of 46 bytes.
			It also has a maximum frame size of 1518 bytes, in which case the payload is 1500 bytes.
			This logic needs to be better, take the interface MTU not hardeoded value of 1518 */
			if payload.FrameLen > 1518 {
				pktType := DecodePkt(payload.Frame)
				log.Infof("+++Daemon(RecvFrmLocalPodThread): Dropping:unusually large packet received from local pod. size: %d, pkt:%s", payload.FrameLen, pktType)
			}
			ok, err := (wireClient).SendToOnce(ctx, payload)
			if err != nil || !ok.Response {
				if err != nil {
					log.Infof("+++Daemon(RecvFrmLocalPodThread): Failed to send packet to remote. err=%v", err)
				} else {
					err = fmt.Errorf("RecvFrmLocalPodThread:Failed to send packet to remote. GRPC return code: false")
					log.Infof("+++Daemon(RecvFrmLocalPodThread):err= %v", err)
				}
				return err
			}
		}
	}
	return nil
}

//------------------------------------------------------------------------------------------------------
func DecodePkt(frame []byte) string {
	pktType := "Unknown"

	packet := gopacket.NewPacket(frame, layers.LayerTypeEthernet, gopacket.Default)
	ethernetLayer := packet.Layer(layers.LayerTypeEthernet)
	if ethernetLayer != nil {
		ethernetPacket, _ := ethernetLayer.(*layers.Ethernet)
		pktType = "Ethernet"
		if ethernetPacket.EthernetType == 0x0800 {
			pktType = pktType + ":IPv4"
			ipLayer := packet.Layer(layers.LayerTypeIPv4)
			if ipLayer != nil {
				ipPacket, _ := ipLayer.(*layers.IPv4)
				pktType = pktType + fmt.Sprintf("[s:%s, d:%s]", ipPacket.SrcIP.String(), ipPacket.DstIP.String())
				if ipPacket.Protocol == 0x1 {
					pktType = pktType + ":ICMP"
				} else if ipPacket.Protocol == 0x6 {
					pktType = pktType + "TCP"
					tcpLayer := packet.Layer(layers.LayerTypeTCP)
					if tcpLayer != nil {
						tcpPkt := tcpLayer.(*layers.TCP)
						if tcpPkt.DstPort == 179 {
							pktType = pktType + ":BGP"
						} else {
							pktType = pktType + fmt.Sprint("[Port:%d]", tcpPkt.DstPort)
						}
					}
				} else {
					pktType = fmt.Sprint("IPv4 with protocol : %d", ipPacket.Protocol)
				}
			}
		} else if ethernetPacket.EthernetType == 0x86DD {
			pktType = "IPv6"
		} else if ethernetPacket.EthernetType <= 1500 {
			llcLayer := packet.Layer(layers.LayerTypeLLC)
			if llcLayer != nil {
				llcPacket, _ := llcLayer.(*layers.LLC)
				if llcPacket.DSAP == 0xFE && llcPacket.SSAP == 0xFE && llcPacket.Control == 0x3 {
					pktType = "LLC"
					if llcPacket.Payload[0] == 0x83 {
						pktType = "ISIS"
					}
				}

			} else {
				pktType = fmt.Sprintf("EthType = %d", ethernetPacket.EthernetType)
			}
		} else if ethernetPacket.EthernetType == 0x0806 {
			pktType = "ARP"
		} else if (ethernetPacket.EthernetType == 0x8100) || (ethernetPacket.EthernetType == 0x88A8) {
			pktType = "VLAN"
		} else {
			pktType = fmt.Sprintf("EthType = %d", ethernetPacket.EthernetType)
		}
	}

	return pktType
}
