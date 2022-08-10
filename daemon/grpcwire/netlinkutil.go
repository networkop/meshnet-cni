package grpcwire

import (
	"fmt"
	"math/rand"
	"net"
	"os"
	"time"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
)

// Creat a veth pair in the current name space and set MTU. It does not make the links to be up
func CreatVethPair(name string, peerName string, mtu int) error {

	veth := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{
			Name:  name,
			Flags: net.FlagUp,
			MTU:   mtu,
		},
		PeerName: peerName,
	}

	err := netlink.LinkAdd(veth)
	if err != nil {
		switch {
		case os.IsExist(err):
			return fmt.Errorf("veth name (%v) already exists", name)
		default:
			return fmt.Errorf("netlink failed to make veth pair: %v", err)
		}
	}

	return nil
}

// returns true if the given interface is already present in the given network namespace
func LinkInNetNS(netNs ns.NetNS, ifaceName string) (result bool, err error) {

	result = false
	err = netNs.Do(func(_ ns.NetNS) error {
		link, err := netlink.LinkByName(ifaceName)
		if link != nil {
			result = true
		}
		return err
	})

	return result, err
}

// rename an interface in the given network namespace
func RenameIntf(netNs ns.NetNS, currIntfName string, newIntfName string) error {

	err := netNs.Do(func(_ ns.NetNS) error {
		link, err := netlink.LinkByName(currIntfName)
		if err != nil {
			return fmt.Errorf("failed rename interface %s, err:%v", currIntfName, err)
		}

		if link == nil {
			return fmt.Errorf("renaming failed, interface %s not found in namespace", currIntfName)
		}

		if err = netlink.LinkSetDown(link); err != nil {
			return fmt.Errorf("renaming failed, interface %s not abel to bring it down: %v", currIntfName, err)
		}

		if currIntfName != newIntfName {
			if err = netlink.LinkSetName(link, newIntfName); err != nil {
				return fmt.Errorf("failed to rename interface %s to %s: %v", currIntfName, newIntfName, err)
			}
		}

		if err = netlink.LinkSetUp(link); err != nil {
			return fmt.Errorf("failed to set interface %s up: %v", newIntfName, err)
		}

		if err = netlink.LinkSetUp(link); err != nil {
			return fmt.Errorf("renaming failed, interface %s not abel to bring it up: %v", currIntfName, err)
		}

		return err
	})

	return err
}

// assign ip to an interface in the given network namespace
func AssignIntfIP(netNsName string, intfName string, ipStr string) error {

	aNetNs, err := GetNetworkNs(netNsName)
	if err != nil {
		return fmt.Errorf("couldn't access namespace %s :%v", netNsName, err)
	}
	defer aNetNs.Close()

	ipAddr, ipSubnet, err := net.ParseCIDR(ipStr)
	if err != nil {
		return fmt.Errorf("failed to parse CIDR %s: %w", ipStr, err)
	}
	aIPNet := net.IPNet{
		IP:   ipAddr,
		Mask: ipSubnet.Mask,
	}

	err = aNetNs.Do(func(_ ns.NetNS) error {
		link, err := netlink.LinkByName(intfName)
		if err != nil {
			return fmt.Errorf("failed to assign ip %s to interface %s, err:%v", ipStr, intfName, err)
		}

		addr := &netlink.Addr{IPNet: &aIPNet, Label: ""}
		if err = netlink.AddrAdd(link, addr); err != nil {
			return fmt.Errorf("failed to add IP addr %s to %s: %v", ipStr, intfName, err)
		}
		return err
	})

	return err
}

// generate a random interface name in the format "tmp<11 digit number>"
func CreateRandomIntfName() (string, error) {
	retry := 1
	maxRetry := 3
	rand.Seed(time.Now().UnixNano())
	for retry <= maxRetry {
		iName := fmt.Sprintf("tmp%.11v", rand.Uint32())
		_, err := net.InterfaceByName(iName)
		if err == nil {
			// This interface exists, retry with another random name
			retry += 1
			continue
		}
		return iName, nil
	}
	return "", fmt.Errorf("tried %d times, could no generate temporary interface name", maxRetry)
}

func GetNetworkNs(nsName string) (ns.NetNS, error) {

	var aNetNs ns.NetNS
	var err error

	// access the network namespace of the 2nd peer
	if nsName == "" {
		if aNetNs, err = ns.GetCurrentNS(); err != nil {
			return nil, err
		}
	} else {
		if aNetNs, err = ns.GetNS(nsName); err != nil {
			return nil, err
		}
	}
	return aNetNs, nil
}

// Create a veth pair and puts each end in desired network namespace. Empty namespace name is interpreted as current namespace.
// It also sets MTU and makes the interface up.
func CreatVethPairInNS(peer1Name string, peer2Name string, peer1NetNsName string, peer2NetNsName string, mtu int) error {
	var ok = false
	var err error = nil
	var peer1NetNs ns.NetNS
	var peer2NetNs ns.NetNS
	var linkPeer1 netlink.Link
	var linkPeer2 netlink.Link

	// do extensive error checking.
	// if the network namespaces are the same then the peer names must be different.
	if (peer1NetNsName == peer2NetNsName) && (peer1Name == peer2Name) {
		return fmt.Errorf("can't crete veth pair with identical pair name (%s) for same namespace (%s)", peer1Name, peer1NetNsName)
	}

	// access the network namespace of the 1st peer
	if peer1NetNs, err = GetNetworkNs(peer1NetNsName); err != nil {
		return err
	}
	defer peer1NetNs.Close()

	// error check : is the interface with the same name already exists in the destination namespace,
	// then we can't create a veth pair
	if ok, err = LinkInNetNS(peer1NetNs, peer1Name); err == nil || ok {
		return fmt.Errorf("ling %s exist. can't crete a new one with same name: %v.", peer1Name, err)
	}

	// access the network namespace of the 2nd peer
	if peer2NetNs, err = GetNetworkNs(peer2NetNsName); err != nil {
		return err
	}
	defer peer1NetNs.Close()

	// error check : is the interface with the same name already exists in the destination namespace,
	// then we can't create a veth pair
	if ok, err = LinkInNetNS(peer2NetNs, peer2Name); err == nil || ok {
		return fmt.Errorf("ling %s exist. can't crete a new one with same name: %v.", peer2Name, err)
	}

	// veth pair has to be created in some namespace first and then each end needs to be moved to the
	// destination name space.
	// Create a veth pair with temporary names in the current namespace.
	// This is to avoid name clash in the current namespace. It may so happen
	// we want to create eth1 in pod1 namespace but the current namespace already have an eth1.
	// So we can't create a veth pair with an "end-name" of "eth1" in current namespace
	peer1NameTemp := peer1Name
	peer2NameTemp := peer2Name

	if peer1NetNsName != "" {
		// we will move this interface to a destination NS at the end. for now need a temp name in current NS
		if peer1NameTemp, err = CreateRandomIntfName(); err != nil {
			return err
		}
	}

	if peer2NetNsName != "" {
		// we will move this interface to a destination NS at the end. for now need a temp name in current NS
		if peer2NameTemp, err = CreateRandomIntfName(); err != nil {
			return err
		}
	}

	// create the veth pair in the current network namespace
	if err = CreatVethPair(peer1NameTemp, peer2NameTemp, mtu); err != nil {
		return fmt.Errorf("%v", err)
	}

	// if a destination namespace is mentioned then move the local temp interface to the
	// destination namespace and rename it.
	// otherwise the created interface remains in the current namespace as desired.
	if linkPeer1, err = netlink.LinkByName(peer1NameTemp); err != nil {
		return fmt.Errorf("Cannot get interface %s: %v", peer1NameTemp, err)
	}

	if linkPeer2, err = netlink.LinkByName(peer2NameTemp); err != nil {
		return fmt.Errorf("Cannot get interface %s: %v", peer2NameTemp, err)
	}

	if peer1NetNsName != "" {
		if err = PushLinkToNetNS(linkPeer1, peer1NetNsName); err != nil {
			return fmt.Errorf("Cannot move interface %s: %v", peer1NameTemp, err)
		}
		if err = RenameIntf(peer1NetNs, peer1NameTemp, peer1Name); err != nil {
			return err
		}
	}

	// if a destination namespace is mentioned then move the local temp interface to the
	// destination namespace and rename it.
	// otherwise the created interface remains in the current namespace as desired.
	if peer2NetNsName != "" {
		if err = PushLinkToNetNS(linkPeer2, peer2NetNsName); err != nil {
			return fmt.Errorf("Cannot move interface %s: %v", peer2NameTemp, err)
		}

		if err = RenameIntf(peer2NetNs, peer2NameTemp, peer2Name); err != nil {
			return err
		}
	}

	// https://pkg.go.dev/github.com/lstoll/cni/pkg/ns#section-readme
	// https://github.com/golang/go/wiki/LockOSThread
	// Read about closure

	return nil
}

// pushes the given link to given network namespace
func PushLinkToNetNS(link netlink.Link, nsName string) error {

	var netNs ns.NetNS
	var err error

	if len(nsName) == 0 {
		return fmt.Errorf("failed to move link to namespace. no namespace specified")
	}
	if netNs, err = ns.GetNS(nsName); err != nil {
		return fmt.Errorf("%v", err)
	}
	defer netNs.Close()

	if err = netlink.LinkSetNsFd(link, int(netNs.Fd())); err != nil {
		return fmt.Errorf("%v", err)
	}

	return nil
}

func makeLinkUP(link netlink.Link) error {
	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("failed to set %q up: %v", link.Attrs().Name, err)
	}
	return nil
}

func setLinkIP(link netlink.Link, ipAddr net.IPNet) error {

	// TODO: Add IPv6 address support
	if ipAddr.IP.To4() == nil {
		return fmt.Errorf("supports IPv4 address only. given link:%s address:%s", link.Attrs().Name, ipAddr.IP)
	}

	addr := &netlink.Addr{
		IPNet: &ipAddr,
		Label: "",
	}
	if err := netlink.AddrAdd(link, addr); err != nil {
		return fmt.Errorf("failed to add IPv4 addr %v to %q: %v", addr, link.Attrs().Name, err)
	}

	return nil

}

//link, err := netlink.LinkByName(vethLinkName)
