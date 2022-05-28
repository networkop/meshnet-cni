package main

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/containernetworking/plugins/pkg/ns"
	mpb "github.com/networkop/meshnet-cni/daemon/proto/meshnet/v1beta1"
	koko "github.com/redhat-nfvpe/koko/api"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

//-----------------------------------------------------------------------------------------------------------
func CreatGRPCChan(link *mpb.Link, localPod *mpb.Pod, peerPod *mpb.Pod, localClient *mpb.LocalClient, cniArgs *k8sArgs, ctx *context.Context) error {
	/* When this function is call it means that the two pods which are attached to this link
	   are both up. They have got the management IP already.
	*/

	log.Infof("==========Start - Plugin CreatGRPCChan ===============")

	defer log.Infof("-x-x-x-x-x-x-x---End - Plugin CreatGRPCChan ---x-x-x-x-x-x-x")
	log.Infof("Setting up grpc-wire:(local-pod:%s:%s@node:%s <----link uid: %d----> remote-pod:%s:%s@node:%s)",
		localPod.Name, link.LocalIntf, localPod.SrcIp,
		link.Uid, peerPod.Name, link.PeerIntf, peerPod.SrcIp)

	if ctx == nil || link == nil {
		return fmt.Errorf("Can't establish grpc channel. Wither link or context not provided. ctx:%p, link:%p", ctx, link)
	}

	log.Infof("Checking if we've been skipped")
	isSkipped, err := (*localClient).IsSkipped(*ctx, &mpb.SkipQuery{
		Pod:    localPod.Name,
		Peer:   peerPod.Name,
		KubeNs: string(cniArgs.K8S_POD_NAMESPACE),
	})

	if err != nil {
		log.Infof("Failed to read skipped status for pd %s", localPod.Name)
		return err
	}

	// Comparing names to determine higher priority
	higherPrio := localPod.Name > peerPod.Name

	if isSkipped.Response == false && higherPrio == false {
		/*  If peer POD skipped us (booted before us) or we have a higher priority then we initiate the tunnel.
		If peer POD has not skipped us (that means yet to boot or just booted) and it has higher priority
		the we do not initiate the grpc tunnel. When the high priority peer pod boots up (or get ready) then
		it will take care of grpc tunnel creation. This is needed to avoid the race condition when both
		the pods are alive, no one has skipped each other and both of them tries to create the tunnel. In
		this situation only high priority pod must creat the tunnel and not the low priority one. This will
		avoid conflict. */
		log.Infof("Pod %s with link uid %d is not skipped. This pod has low priority. Peer pod %s will create this grpc-wire", localPod.Name, link.Uid, peerPod.Name)
		return nil
	}

	wireDef := mpb.WireDef{
		LocalPodNetNs: localPod.NetNs,
		LinkUid:       link.Uid,
		KubeNs:        localPod.KubeNs,
	}
	resp, err := (*localClient).GRPCWireExists(*ctx, &wireDef)
	if resp.Response == true {
		/* While this pod was busy creating other link or was busy with some other task, the remote
		   pod had finished creating this grpc-link.  */
		log.Infof("This grpc wire is already set by the remote peer. Local interface id:%d", resp.PeerIntfId)
		return nil
	}

	/* Creat the local end of the grpc-wire */

	currNs, err := ns.GetCurrentNS()
	if err != nil {
		return fmt.Errorf("Creating GRPC wire: failed to get current ns for pod %s, err: %v", localPod.Name, err)
	}

	// Build koko's veth struct for the intf to be placed inside the pod
	inConIntfNm := link.LocalIntf
	inContainerVeth, err := makeVeth(localPod.NetNs, inConIntfNm, link.LocalIp)
	if err != nil {
		log.Infof("Could not create vEth for pod %s:%s. err%v", localPod.Name, inConIntfNm, err)
		return err
	}

	// inContainerVeth := &koko.VEth{
	// 	LinkName: inConIntfNm,
	// 	NsName:   localPod.NetNs,
	// }

	/* We need to make the node end of the v-eth pair name unique for this node. So get an unique id from daemon */
	localIntfDI, err := (*localClient).GenLocVEthID(*ctx, &mpb.ReqIntfID{InContIntfNm: inConIntfNm})
	if err != nil {
		return fmt.Errorf("Creating GRPC wire: could not get interface number for pod:%s. err:%v", localPod.Name, err)
	}

	// Build koko's veth struct for the intf to be placed in the local node
	//locIfNm := inConIntfNm + "-grpc" + strconv.FormatInt(localIntfDI.LocalIntfId, 10)
	nmLen1 := len(inConIntfNm)
	nmLen2 := len(localPod.Name)
	if nmLen1 > 5 {
		nmLen1 = 5
	}
	if nmLen2 > 5 {
		nmLen2 = 5
	}
	locIfNm := inConIntfNm[0:nmLen1] + localPod.Name[0:nmLen2] + "-g" + strconv.FormatInt(localIntfDI.LocalIntfId, 10)

	hostEndVeth := &koko.VEth{
		LinkName: locIfNm,
		NsName:   currNs.Path()}

	if err = koko.MakeVeth(*inContainerVeth, *hostEndVeth); err != nil { //+++think: order in which the argument are passed - does it matter ?
		return fmt.Errorf("Creating GRPC wire: failed to create vEth-pair inside pod (%s:%s) and on host (%s). err:%s",
			localPod.Name, inContainerVeth.LinkName, hostEndVeth.LinkName, err)
	}

	/* Dial the remote peer to create the remote end of the grpc tunnel. */

	url := fmt.Sprintf("%s:%s", peerPod.SrcIp, defaultPort)
	url = strings.TrimSpace(url)
	remote, err := grpc.Dial(url, grpc.WithInsecure())
	if err != nil {
		return fmt.Errorf("Creating GRPC wire: failed to dial remote gRPC url %s", url)
	}
	remoteClient := mpb.NewRemoteClient(remote)
	locInf, err := net.InterfaceByName(hostEndVeth.LinkName)
	wireDefRemot := mpb.WireDef{
		/*PeerIntfId : this is the interface id on which the host machine will receive grpc
		packets from the remote container, as well it will receive the packets coming
		from the local pod/container. Packets comming from local container will be encapsulated
		in grpc and send it to the peer, thorugh this interface only. The remote pod must send
		packets to this interface for this grpc-wire, to reach the connected container in this node.
		For remote pod this must be the destination interface id to reach the connected pod in this
		node. Deo form remote pods perspective this is "PeerIntfId"  */
		PeerIntfId: int64(locInf.Index),

		/* PeerIp: Ip address of the peer machine/node.
		 * For remote pod this must be the IP address on this host. The remote pod must
		 * Transport packets to this pod (over grpc) in this local node. This is the IP
		 * address of the local node which remote node will do a grpc dial, to send
		 * packets over grpc wire. */
		PeerIp: localPod.SrcIp,

		/* We need to tell the remote node, what is the kne specified in contier intrface name.
		   We also need to tell to which network namespace the pod in remote node belongs to. */
		IntfNameInPod: link.PeerIntf,
		LocalPodNetNs: peerPod.NetNs,
		LocalPodNm:    peerPod.Name, // name of the remote pod

		/*meshnet assigned unique identifier for this link */
		LinkUid:    link.Uid,
		KubeNs:     peerPod.KubeNs,
		LocalPodIp: link.PeerIp,
	}

	log.Infof("Create GRPC wire: dialing remote node-->%s@%s", peerPod.Name, url)
	creatResp, err := (remoteClient).AddGRPCWireRemote(*ctx, &wireDefRemot)
	if err != nil {
		return fmt.Errorf("Error: Failed to create grpc tunnel ar remote end:%s  err:%v", url, err)
	} else if creatResp.Response == false {
		return fmt.Errorf("Remote end of the grpc-wire (local-pod:%s:%s@node:%s <----link uid: %d----> remote-pod:%s:%s@node:%s) is not up",
			localPod.Name, link.LocalIntf, localPod.SrcIp,
			link.Uid, peerPod.Name, link.PeerIntf, peerPod.SrcIp)
	}

	/*remote has finished its job. Register local end of the grpc wire with the daemon and start the packet sending thread. */
	wireDefLocal := mpb.WireDef{
		/*PeerIntfId : this is the interface id to which the host/local machine will send grpc
		  packets for the remote pod. This interface id will be encoded in every packet
		  sent over this grpc-wire. This interface id is created in the remote machine and
		  communicated by the remote machine. Avalability of this interface id indicates remote
		  machine is ready to receive packets over this grpc-wire. Remote machine will use this
		  interface id to pass the packets to the remote pod. */
		PeerIntfId: creatResp.PeerIntfId,

		/* PeerIp : Ip addredd of the remote node, to which this local node is sending packets over
		   this grpc-wire.
		*/
		PeerIp: peerPod.SrcIp,

		/* VethNameLocalHost : name of the local machine interface, from where packets form the local
		   pod will be picked up and transported over grpc to remote. local daemon will receive
		   packets from local pod on this interface.
		*/
		VethNameLocalHost: locIfNm,

		/*meshnet assigned unique identifier for this link */
		LinkUid:       link.Uid,
		LocalPodNm:    localPod.Name,
		IntfNameInPod: link.LocalIntf,
		LocalPodNetNs: localPod.NetNs,
		KubeNs:        localPod.KubeNs,
	}
	log.Infof("Creating GRPC wire: adding the local end of the grpc tunnel.")
	r, err := (*localClient).AddGRPCWireLocal(*ctx, &wireDefLocal)
	if err != nil {
		return fmt.Errorf("Error: Failed to create local end of the tunnel %v", err)
	} else if r.Response == false {
		return fmt.Errorf("Local end of the grpc-wire (local-pod:-%s:%s@node:%s <----link uid: %d----> remote-pod:-%s:%s@node:%s) is not up",
			localPod.Name, link.LocalIntf, localPod.SrcIp,
			link.Uid, peerPod.Name, link.PeerIntf, peerPod.SrcIp)
	}

	log.Infof("Successfully created grpc-wire (local-pod:%s:%s@node:%s:%d <----link uid: %d----> remote-pod:%s:%s@node:%s:%d)",
		localPod.Name, link.LocalIntf, localPod.SrcIp, locInf.Index,
		link.Uid, peerPod.Name, link.PeerIntf, peerPod.SrcIp, creatResp.PeerIntfId)

	return nil
}

//--------------------------------------------------------------------------------------------------------
