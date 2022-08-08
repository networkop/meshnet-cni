package grpcwire

import (
	"net"
	"os/user"
	"runtime"
	"testing"

	//"github.com/networkop/meshnet-cni/daemon/grpcwire"
	"fmt"

	"github.com/containernetworking/plugins/pkg/ns"
	log "github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

//--------------------------------------------------------------------------------------------------
func isRoot() bool {
	currentUser, err := user.Current()
	if err != nil {
		log.Fatalf("[isRoot] Unable to get current user: %s", err)
	}
	return currentUser.Username == "root"
}

//--------------------------------------------------------------------------------------------------
func Cleanup(t *testing.T, netNs ns.NetNS, ifaceName string) error {
	var err error

	if !isRoot() {
		return fmt.Errorf("cleanup must be done as root")
	}

	err = netNs.Do(func(_ ns.NetNS) error {

		// deleting only one will delete the pair.
		link2, err := netlink.LinkByName(ifaceName)
		if err != nil {
			t.Errorf("failed to lookup %q in %q: %v", ifaceName, netNs.Path(), err)
			return err
		}

		if err = netlink.LinkDel(link2); err != nil {
			t.Errorf("failed to remove link %q in %q: %v", ifaceName, netNs.Path(), err)
			return err
		}

		return nil
	})

	if err != nil {
		t.Errorf("cleanup: failed to remove link : %v", err)
		return err
	}
	return nil
}

//---------------------------------------------------------------------------------------------------

func Test_CreateRandomIntfName(t *testing.T) {

	n, err := CreateRandomIntfName()

	if err != nil {
		t.Errorf("Random name generation test failed : %v", err)
	}

	if len(n) > 14 {
		t.Errorf("Random name test failed : name:%s more than 14 character:%d ", n, len(n))
	}
	t.Logf("Test passed: Generated interface name: %s", n)
}

//--------------------------------------------------------------------------------------------------
func Test_CreatVethPair(t *testing.T) {
	pair1 := "goTstInf1"
	pair2 := "goTstInf2"
	//var link1 netlink.Link
	var err error

	if !isRoot() {
		t.Errorf("Test TestVethPairCreation failed. Please run it as root or with sudo")
		return
	}

	fmt.Printf("Create veth pair %s<-->%s\n", pair1, pair2)
	err = CreatVethPair(pair1, pair2, 64000)
	if err != nil {
		t.Errorf("CreatVethPair returned error: %v", err)
		return
	}

	iface1, err := net.InterfaceByName(pair1)
	if err != nil {
		t.Errorf("Interface creation test failed. No able to create veth pair : %v", err)
	}

	iface2, err := net.InterfaceByName(pair2)
	if err != nil {
		t.Errorf("Interface creation test failed. No able to create veth pair : %v", err)
		return
	}

	fmt.Printf("Created veth pair %s(%d)<-->%s(%d)\n", iface1.Name, iface1.MTU, iface2.Name, iface2.MTU)

	//clean up
	currNs, err := ns.GetCurrentNS()
	if err != nil {
		t.Errorf("failed to get current namespace : %v", err)
		return
	}
	defer currNs.Close()
	err = Cleanup(t, currNs, pair2)
	if err != nil {
		t.Errorf("cleanup: failed to remove link : %v", err)
		return
	}
	t.Logf("Test passed: Successfully created & deleted veth pair %s:%s", pair1, pair2)
}

//--------------------------------------------------------------------------------------------------

func Test_LinkInNetNS(t *testing.T) {

	res := false
	aName := ""
	ifaces := []string{"eth0", "enp0s3", "lo", "docker0"}

	fmt.Printf("This test expects any one of the following interfaces to be present in the machine: %s\n", ifaces)
	fmt.Printf("If none of them are present then update the list with correct ones.\n")

	currNs, err := ns.GetCurrentNS()
	if err != nil {
		t.Errorf("failed to get current namespace : %v", err)
		return
	}
	defer currNs.Close()

	// The test passes if the LinkInNetNS can find any one of the interfaces given in the list.
	for _, aName = range ifaces {
		//fmt.Printf("Searching interface: %s\n", aName)
		ok, err := LinkInNetNS(currNs, aName)
		if err == nil && ok {
			//fmt.Printf("Retrieved interface: %s\n", aName)
			res = true
			break
		}
		//fmt.Printf("Got ok:%v  err:%v\n", ok, err)
	}

	if res == true {
		t.Logf("Test TestLinkSearch passed for interface list:%s and found: %s", ifaces, aName)
		return
	}
	t.Errorf("Test TestLinkSearch failed for interface list:%s\n", ifaces)

}

//--------------------------------------------------------------------------------------------------
func Test_RenameIntf(t *testing.T) {

	pair1 := "goTstInf1"
	pair2 := "goTstInf2"
	pair1new := "goTstInf1new"
	pair2new := "goTstInf2new"
	var err error

	if !isRoot() {
		t.Errorf("Test TestLinkRename failed. Please run it as root or with sudo")
		return
	}

	fmt.Printf("Create veth pair %s<-->%s\n", pair1, pair2)
	err = CreatVethPair(pair1, pair2, 64000)
	if err != nil {
		t.Errorf("CreatVethPair returned error: %v", err)
		return
	}

	iface1, err := net.InterfaceByName(pair1)
	if err != nil {
		t.Errorf("Interface creation test failed. No able to create veth pair : %v", err)
	}

	iface2, err := net.InterfaceByName(pair2)
	if err != nil {
		t.Errorf("Interface creation test failed. No able to create veth pair : %v", err)
		return
	}

	fmt.Printf("Created veth pair %s(%d)<-->%s(%d)\n", iface1.Name, iface1.MTU, iface2.Name, iface2.MTU)

	currNs, err := ns.GetCurrentNS()
	if err != nil {
		t.Errorf("failed to get current namespace : %v", err)
		return
	}
	defer currNs.Close()

	err = RenameIntf(currNs, pair2, pair2new)
	if err != nil {
		t.Errorf("failed to rename %s : %v", pair1, err)
		return
	}

	err = RenameIntf(currNs, pair1, pair1new)
	if err != nil {
		t.Errorf("failed to rename %s : %v", pair1, err)
		return
	}

	//clean up - deleting one end of the pair will delete the other end in the same net namespace
	err = Cleanup(t, currNs, pair1new)
	if err != nil {
		t.Errorf("cleanup: failed to remove link %s : %v", pair1new, err)
		return
	}

}

//--------------------------------------------------------------------------------------------
func Test_CreatVethPairInNS(t *testing.T) {

	// Lock the OS Thread so we don't accidentally switch namespaces
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Save the current network namespace
	origins, err := netns.Get()
	if err != nil {
		t.Errorf("failed to get origin namespace: %v", err)
		return
	}
	defer origins.Close()

	//Create two temp namespace
	ns1Name := "gotest-ns1"
	ns1Veth := "ns1If1"
	ns2Name := "gotest-ns2"
	ns2Veth := "ns2If2"

	ns1, err := netns.NewNamed(ns1Name)
	if err != nil {
		t.Errorf("failed to create namespace %s", ns1Name)
		return
	}
	defer ns1.Close()

	//If want to check manually then use "ip netns list" - show all of the named network namespaces
	//This command displays all of the network namespaces in /var/run/netns
	//sudo ip netns del gotest-ns1

	ns2, err := netns.NewNamed(ns2Name)
	if err != nil {
		t.Errorf("failed to create namespace %s", ns2Name)
		return
	}
	defer ns2.Close()

	if err := netns.Set(origins); err != nil {
		t.Errorf("failed to restore origin namespace:%v", err)
		return
	}

	ns1Path := fmt.Sprintf("/var/run/netns/%s", ns1Name)
	ns2Path := fmt.Sprintf("/var/run/netns/%s", ns2Name)
	t.Logf("Create veth pair %s(%s)<-->%s(%s)\n", ns1Veth, ns1Path, ns2Veth, ns2Path)
	err = CreatVethPairInNS(ns1Veth, ns2Veth, ns1Path, ns2Path, 6400)
	if err != nil {
		t.Errorf("CreatVethPairInNS returned error: %v", err)
		return
	}
	//sudo ip netns exec TEST ip link show dev eth1

	// What that they got created.
	//Delete them

	//func CreatVethPairInNS(peer1Name string, peer2Name string, peer1NetNsName string, peer2NetNsName string, mtu int) error {
	//https://gist.github.com/tormath1/d28b591b8619af41862be70b1eda02f6
	//https://github.com/vishvananda/netlink/blob/main/netlink_test.go

	t.Logf("Test TestAPI_CreatVethPairInNS passed")
	t.Errorf("Test TestAPI_CreatVethPairInNS not yet implemented")
	return
}

//--------------------------------------------------------------------------------------------
// IP Assignment test
//func AssignIntfIP(netNsName string, intfName string, ipStr string) error

// func TestLinkCleanup(t *testing.T) {
// 	//pair1new := "goTstInf1new"
// 	//pair2new := "goTstInf2new"
// 	currNs, err := ns.GetCurrentNS()
// 	if err != nil {
// 		t.Errorf("failed to get current namespace : %v", err)
// 		return
// 	}
// 	defer currNs.Close()
// 	err = Cleanup(t, currNs, "goTstInf2new")
// 	err = Cleanup(t, currNs, "goTstInf1new")

// }
