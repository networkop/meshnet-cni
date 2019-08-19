//package main
//
//import (
//	"context"
//
//	pb "github.com/networkop/meshnet-cni/daemon/generated"
//	log "github.com/sirupsen/logrus"
//)
//
//func (s *meshnetd) Get(ctx context.Context, localPod *pb.PodQuery) (*pb.Pod, error) {
//	log.Infof("Retrieving %s/%s metadata from K8s...", localPod.KubeNs, localPod.Name)
//
//	remotePod, err := s.kube.getPod(localPod.Name, localPod.KubeNs)
//	if err != nil {
//		log.Errorf("Failed to retrieve pod metadata from K8s")
//		return nil, err
//	}
//
//	links := make([]*pb.Link, len(remotePod.Spec.Links))
//	for i := range links {
//		remoteLink := remotePod.Spec.Links[i]
//		links[i] = &pb.Link{
//			PeerPod:   remoteLink.PeerPod,
//			PeerIntf:  remoteLink.PeerIntf,
//			LocalIntf: remoteLink.LocalIntf,
//			LocalIp:   remoteLink.LocalIp,
//			PeerIp:    remoteLink.PeerIp,
//			Uid:       uint32(remoteLink.Uid),
//		}
//	}
//
//	return &pb.Pod{
//		Name:   localPod.Name,
//		SrcIp:  remotePod.Status.SrcIp,
//		NetNs:  remotePod.Status.NetNs,
//		KubeNs: localPod.KubeNs,
//		Links:  links,
//	}, nil
//}
//
//func (s *meshnetd) SetAlive(ctx context.Context, localPod *pb.Pod) (*pb.BoolResponse, error) {
//	log.Infof("Un/Setting %s/%s SrcIp and NetNs", localPod.KubeNs, localPod.Name)
//
//	remotePod, err := s.kube.getPod(localPod.Name, localPod.KubeNs)
//	if err != nil {
//		log.Errorf("Failed to retrieve pod metadata from K8s")
//		return nil, err
//	}
//
//	_, err = s.kube.setPodAlive(remotePod, localPod.NetNs, localPod.SrcIp, localPod.KubeNs)
//	if err != nil {
//		log.Errorf("Failed to set pod alive status")
//		return nil, err
//	}
//
//	return &pb.BoolResponse{Response: true}, nil
//}
//
//func (s *meshnetd) IsAlive(ctx context.Context, localPod *pb.Pod) (*pb.BoolResponse, error) {
//	log.Infof("Checking if %s/%s is alive", localPod.KubeNs, localPod.Name)
//	remotePod, err := s.kube.getPod(localPod.Name, localPod.KubeNs)
//	if err != nil {
//		log.Errorf("Failed to retrieve pod metadata from K8s")
//		return nil, err
//	}
//
//	return &pb.BoolResponse{Response: s.kube.isAlive(remotePod, localPod.KubeNs)}, nil
//}
//func (s *meshnetd) Skip(ctx context.Context, skip *pb.SkipQuery) (*pb.BoolResponse, error) {
//	log.Infof("Skipping pod %s by pod %s", skip.Pod, skip.Peer)
//
//	thisPod, err := s.kube.getPod(skip.Pod, skip.KubeNs)
//	if err != nil {
//		log.Errorf("Failed to retrieve pod metadata from K8s")
//		return nil, err
//	}
//
//	thatPod, err := s.kube.getPod(skip.Peer, skip.KubeNs)
//	if err != nil {
//		log.Errorf("Failed to retrieve pod metadata from K8s")
//		return nil, err
//	}
//
//	_, err = s.kube.skipPod(thisPod, thatPod, skip.KubeNs)
//	if err != nil {
//		log.Errorf("Failed to set skip flag for pod %s", skip.Pod)
//		return &pb.BoolResponse{Response: false}, err
//	}
//	return &pb.BoolResponse{Response: true}, nil
//}
//
//func (s *meshnetd) IsSkipped(ctx context.Context, skip *pb.SkipQuery) (*pb.BoolResponse, error) {
//	log.Infof("Checking if %s/%s is skipped by %s", skip.KubeNs, skip.Pod, skip.Peer)
//
//	thisPod, err := s.kube.getPod(skip.Pod, skip.KubeNs)
//	if err != nil {
//		log.Errorf("Failed to retrieve pod metadata from K8s")
//		return nil, err
//	}
//
//	thatPod, err := s.kube.getPod(skip.Peer, skip.KubeNs)
//	if err != nil {
//		log.Errorf("Failed to retrieve pod metadata from K8s")
//		return nil, err
//	}
//
//	return &pb.BoolResponse{Response: s.kube.isSkipped(thisPod, thatPod, skip.KubeNs)}, nil
//}
//
//// func (s *meshnetd) Update(ctx context.Context, pod *pb.RemotePod) (*pb.BoolResponse, error) { return nil, nil }
//