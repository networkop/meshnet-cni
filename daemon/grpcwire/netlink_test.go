package grpcwire

import (
	"net"
	"os/exec"
	"os/user"
	"runtime"
	"strings"
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
func cleanupVethPair(t *testing.T, netNs ns.NetNS, ifaceName string) error {
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
		t.Errorf("Test_CreatVethPair must be done as root or use sudo")
		return
	}

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
	err = cleanupVethPair(t, currNs, pair2)
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

	if !isRoot() {
		t.Errorf("Test_CreatVethPair must be done as root or use sudo")
		return
	}

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

	out, err := exec.Command("ip", "link", "show", pair1new).Output()
	if err != nil {
		t.Errorf("unable to execute ip command :%v", err)
		return
	}

	aStr := string(out)
	if !strings.Contains(aStr, pair1new) {
		t.Errorf("not able to get interface %s", pair1new)
		return
	}
	t.Logf("\n%s", aStr)

	out, err = exec.Command("ip", "link", "show", pair2new).Output()
	if err != nil {
		t.Errorf("unable to execute ip command :%v", err)
		return
	}

	aStr = string(out)
	if !strings.Contains(aStr, pair2new) {
		t.Errorf("not able to get interface %s", pair2new)
		return
	}

	t.Logf("\n%s", aStr)
	t.Logf("interfaces are renamed correctly %s, %s", pair1new, pair2new)
	//clean up - deleting one end of the pair will delete the other end in the same net namespace
	err = cleanupVethPair(t, currNs, pair1new)
	if err != nil {
		t.Errorf("cleanup: failed to remove link %s : %v", pair1new, err)
		return
	}

}

//--------------------------------------------------------------------------------------------
func Test_AssignIntfIP(t *testing.T) {
	pair1 := "goTstInf1"
	pair2 := "goTstInf2"
	pair1CIDR := "1.1.1.1/28"
	pair1IP := "1.1.1.1"
	var err error

	if !isRoot() {
		t.Errorf("Test_AssignIntfIPfailed. Please run it as root or with sudo")
		return
	}

	fmt.Printf("Create veth pair %s<-->%s\n", pair1, pair2)
	err = CreatVethPair(pair1, pair2, 64000)
	if err != nil {
		t.Errorf("CreatVethPair returned error: %v", err)
		return
	}

	_, err = net.InterfaceByName(pair1)
	if err != nil {
		t.Errorf("could not create interface %s : %v", pair1, err)
	}

	currNs, err := ns.GetCurrentNS()
	if err != nil {
		t.Errorf("failed to get current namespace : %v", err)
		return
	}
	defer currNs.Close()

	err = AssignIntfIP(currNs.Path(), pair1, pair1CIDR)
	if err != nil {
		t.Errorf("AssignIntfIP for %s returned error: %v", pair1, err)
		return
	}

	out, err := exec.Command("ip", "addr", "show", pair1).Output()
	if err != nil {
		t.Errorf("unable to execute ip addr show :%v", err)
		return
	}

	aStr := string(out)
	if !strings.Contains(aStr, pair1IP) {
		t.Errorf("Failed : could not assign IP address %s", pair1IP)
		return
	}
	t.Log(aStr)

	//clean up - deleting one end of the pair will delete the other end in the same net namespace
	err = cleanupVethPair(t, currNs, pair1)
	if err != nil {
		t.Errorf("cleanup: failed to remove link %s : %v", pair1, err)
		return
	}

}

//--------------------------------------------------------------------------------------------

func Test_CreatVethPairInNS(t *testing.T) {

	if !isRoot() {
		t.Errorf("Test_CreatVethPair must be done as root or use sudo")
		return
	}

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
	ns1Veth := "ns1test-if1" // interface inside namespace 1
	ns1CIDR := "1.1.1.1/28"  // ip address of the namespace 1 in CIDR format
	ns1IP := "1.1.1.1"       // ip address of the namespace 1
	ns1Path := fmt.Sprintf("/var/run/netns/%s", ns1Name)

	ns2Name := "gotest-ns2"
	ns2Veth := "ns2test-if2" // interface inside namespace 2
	ns2CIDR := "1.1.1.2/28"  // ip address of the namespace 2 in CIDR format
	ns2IP := "1.1.1.2"       // ip address of the namespace 2
	ns2Path := fmt.Sprintf("/var/run/netns/%s", ns2Name)

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

	// restore original namespace
	if err := netns.Set(origins); err != nil {
		t.Errorf("failed to restore origin namespace:%v", err)
		return
	}

	err = CreatVethPairInNS(ns1Veth, ns2Veth, ns1Path, ns2Path, 6400)
	if err != nil {
		t.Errorf("CreatVethPairInNS returned error: %v", err)
		return
	}
	//sudo ip netns exec TEST ip link show dev eth1

	err = AssignIntfIP(ns1Path, ns1Veth, ns1CIDR)
	if err != nil {
		t.Errorf("AssignIntfIP for %s returned error: %v", ns1Veth, err)
		return
	}

	err = AssignIntfIP(ns2Path, ns2Veth, ns2CIDR)
	if err != nil {
		t.Errorf("AssignIntfIP for %s returned error: %v", ns2Veth, err)
		return
	}

	//sudo ip netns exec gotest-ns2 ping  1.1.1.1 -c 3
	//sudo ip netns exec gotest-ns1 ping  1.1.1.2 -c 3

	out, err := exec.Command("ip", "netns", "exec", ns2Name, "ping", ns1IP, "-c", "3").Output()
	if err != nil {
		t.Errorf("unable to execute ping:%v", err)
		return
	}

	aStr := string(out)
	if strings.Contains(aStr, "Destination Host Unreachable") {
		t.Errorf("from %s not able to ping %s", ns2Name, ns1IP)
		return
	}
	t.Log(aStr)
	if !strings.Contains(aStr, "3 received") {
		t.Errorf("from %s not able to ping %s", ns2Name, ns1IP)
		return
	}
	t.Logf("sudo ip netns exec %s ping %s -c 3 : is successful", ns2Name, ns1IP)

	out, err = exec.Command("ip", "netns", "exec", ns1Name, "ping", ns2IP, "-c", "3").Output()
	if err != nil {
		t.Errorf("unable to execute ping:%v", err)
		return
	}
	aStr = string(out)
	if strings.Contains(aStr, "Destination Host Unreachable") {
		t.Errorf("from %s not able to ping %s", ns1Name, ns2IP)
		return
	}
	t.Log(string(out))
	if !strings.Contains(aStr, "3 packets transmitted, 3 received") {
		t.Errorf("from %s not able to ping %s", ns1Name, ns2IP)
		return
	}
	t.Logf("sudo ip netns exec %s ping %s -c 3 : is successful", ns1Name, ns2IP)

	//Delete them
	err = netns.DeleteNamed(ns1Name)
	if err != nil {
		t.Errorf("could not delete namespace %s: %v", ns1Name, err)
		return
	}
	err = netns.DeleteNamed(ns2Name)
	if err != nil {
		t.Errorf("could not delete namespace %s: %v", ns2Name, err)
		return
	}

	//https://gist.github.com/tormath1/d28b591b8619af41862be70b1eda02f6
	//https://github.com/vishvananda/netlink/blob/main/netlink_test.go

	t.Logf("Test Test_CreatVethPairInNS passed; Creates %s@%s & %s@%s", ns1Veth, ns1Path, ns2Veth, ns2Path)
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

//--------------------------------------------------------------------------------------------
func Test_AddFlowControlOnIface(t *testing.T) {
	ifaceName := "foo"

	var err error

	if !isRoot() {
		t.Fatal("Test_AddFlowControlOnIface must be done as root or use sudo")
	}

    if err := netlink.LinkAdd(&netlink.Ifb{netlink.LinkAttrs{Name: ifaceName}}); err != nil {
        t.Fatal("Could not create interface to test flow control:", err)
    }
	currNs, err := ns.GetCurrentNS()
	if err != nil {
		t.Fatal("Could not retrieve cyrrent ns:", err)
	}
	defer currNs.Close()
	defer cleanupVethPair(t, currNs, ifaceName)

    link, err := netlink.LinkByName(ifaceName)
    if err != nil {
        t.Fatal("Could not get interface by name:", err)
    }
    if err := netlink.LinkSetUp(link); err != nil {
        t.Fatal("Could not set interface up:", err)
    }

    // prepare flow controls to be set on interface
    ctrlMap := make(map[int]float32)
    ctrlMap[CTRL_TYPE_LATENCY] = 150 * 1000
    ctrlMap[CTRL_TYPE_LOSS] = 10
    ctrlMap[CTRL_TYPE_DUPLICATE] = 20
    ctrlMap[CTRL_TYPE_DUPLICATE_CORR] = 30
    ctrlMap[CTRL_TYPE_LOSS_CORR] = 30
    ctrlMap[CTRL_TYPE_JITTER] = 40
    ctrlMap[CTRL_TYPE_DELAY_CORR] = 50
    ctrlMap[CTRL_TYPE_REORDER_PROB] = 60
    ctrlMap[CTRL_TYPE_REORDER_CORR] = 65
    ctrlMap[CTRL_TYPE_CORRUPT_PROB] = 70
    ctrlMap[CTRL_TYPE_CORRUPT_CORR] = 80
    ctrlMap[CTRL_TYPE_LIMIT] = 90
    ctrlMap[CTRL_TYPE_GAP] = 500

    cNetem, err := AddFlowControlOnIface(ifaceName, ctrlMap)
    if err != nil {
        t.Fatal("Failed to set flow control on iface", ifaceName, " :", err)
    }

    qdiscs, err := GetFlowControlList(ifaceName)
    if err != nil {
        t.Fatal("Could not retrieve flow control list in iface", ifaceName, " :", err)
    }

    if len(qdiscs) != 1 {
        t.Fatal("Failed to add flow control correctly")
    }
    netem, ok := qdiscs[0].(*netlink.Netem)
    if !ok {
        t.Fatal("Qdisc type is wrong")
    }
    // Compare the record we got from the list with the one we created
    if netem.Latency != cNetem.Latency {
        t.Fatal("Latency does not match: expected", cNetem.Latency, "got", netem.Latency)
    }
    if netem.DelayCorr != cNetem.DelayCorr{
        t.Fatal("DelayCorr does not match: expected", cNetem.DelayCorr, "got", netem.DelayCorr)
    }
    if netem.Limit != cNetem.Limit {
        t.Fatal("Limit does not match: expected", cNetem.Limit, "got", netem.Limit)
    }
    if netem.Loss != cNetem.Loss {
        t.Fatal("Loss does not match: expected", cNetem.Loss, "got", netem.Loss)
    }
    if netem.LossCorr != cNetem.LossCorr {
        t.Fatal("LossCorr does not match: expected", cNetem.LossCorr, "got", netem.LossCorr)
    }
    if netem.Gap != cNetem.Gap {
        t.Fatal("Gap does not match: expected", cNetem.Gap, "got", netem.Gap)
    }
    if netem.Duplicate != cNetem.Duplicate {
        t.Fatal("Duplicate does not match: expected", cNetem.Duplicate, "got", netem.Duplicate)
    }
    if netem.DuplicateCorr != cNetem.DuplicateCorr {
        t.Fatal("DuplicateCorr does not match: expected", cNetem.DuplicateCorr, "got", netem.DuplicateCorr)
    }
    if netem.Jitter != cNetem.Jitter {
        t.Fatal("Jitter does not match: expected", cNetem.Jitter, "got", netem.Jitter)
    }
    if netem.ReorderProb != cNetem.ReorderProb {
        t.Fatal("ReorderProb does not match: expected", cNetem.ReorderProb, "got", netem.ReorderProb)
    }
    if netem.ReorderCorr != cNetem.ReorderCorr {
        t.Fatal("ReorderCorr does not match: expected", cNetem.ReorderCorr, "got", netem.ReorderCorr)
    }
    if netem.CorruptProb != cNetem.CorruptProb {
        t.Fatal("CorruptProb does not match: expected", cNetem.CorruptProb, "got", netem.CorruptProb)
    }
    if netem.CorruptCorr != cNetem.CorruptCorr {
        t.Fatal("CorruptCorr does not match: expected", cNetem.CorruptCorr, "got", netem.CorruptCorr)
    }

    // Cleanup 
    if err := RemoveFlowControlOnIface(ifaceName); err != nil {
        t.Fatal("Could not remove flow control from interface", ifaceName, " :", err)
    }
    qdiscs, err = GetFlowControlList(ifaceName)
    if err != nil {
        t.Fatal("Could not retrieve flow control after deletion on interface", ifaceName, " :", err)
    }
    if len(qdiscs) != 0 {
        t.Fatal("Failed to remove qdisc/flow control from interface", ifaceName, "after deletion")
    }
	t.Logf("Test passed: Successfully added flow control on interface %s", ifaceName)
}

//--------------------------------------------------------------------------------------------
func Test_ChangeFlowControlOnIface(t *testing.T) {
	ifaceName := "foo"

	var err error

	if !isRoot() {
		t.Fatal("Test_ChangeFlowControlOnIface must be done as root or use sudo")
	}

    if err := netlink.LinkAdd(&netlink.Ifb{netlink.LinkAttrs{Name: ifaceName}}); err != nil {
        t.Fatal("Could not create interface to test flow control:", err)
    }
	currNs, err := ns.GetCurrentNS()
	if err != nil {
		t.Fatal("Could not retrieve cyrrent ns:", err)
	}
	defer currNs.Close()
	defer cleanupVethPair(t, currNs, ifaceName)

    link, err := netlink.LinkByName(ifaceName)
    if err != nil {
        t.Fatal("Could not get interface by name:", err)
    }
    if err := netlink.LinkSetUp(link); err != nil {
        t.Fatal("Could not set interface up:", err)
    }

    // prepare flow controls to be set on interface
    ctrlMap := make(map[int]float32)
    ctrlMap[CTRL_TYPE_LATENCY] = 150 * 1000
    ctrlMap[CTRL_TYPE_LOSS] = 10
    ctrlMap[CTRL_TYPE_DUPLICATE] = 20
    ctrlMap[CTRL_TYPE_DUPLICATE_CORR] = 30
    ctrlMap[CTRL_TYPE_LOSS_CORR] = 30
    ctrlMap[CTRL_TYPE_JITTER] = 40
    ctrlMap[CTRL_TYPE_DELAY_CORR] = 50
    ctrlMap[CTRL_TYPE_REORDER_PROB] = 60
    ctrlMap[CTRL_TYPE_REORDER_CORR] = 65
    ctrlMap[CTRL_TYPE_CORRUPT_PROB] = 70
    ctrlMap[CTRL_TYPE_CORRUPT_CORR] = 80
    ctrlMap[CTRL_TYPE_LIMIT] = 90
    ctrlMap[CTRL_TYPE_GAP] = 500

    cNetem, err := AddFlowControlOnIface(ifaceName, ctrlMap)
    if err != nil {
        t.Fatal("Failed to set flow control on iface", ifaceName, " :", err)
    }

    qdiscs, err := GetFlowControlList(ifaceName)
    if err != nil {
        t.Fatal("Could not retrieve flow control list in iface", ifaceName, " :", err)
    }

    if len(qdiscs) != 1 {
        t.Fatal("Failed to add flow control correctly")
    }
    netem, ok := qdiscs[0].(*netlink.Netem)
    if !ok {
        t.Fatal("Qdisc type is wrong")
    }
    // Compare the record we got from the list with the one we created
    if netem.Latency != cNetem.Latency {
        t.Fatal("Latency does not match: expected", cNetem.Latency, "got", netem.Latency)
    }
    if netem.DelayCorr != cNetem.DelayCorr{
        t.Fatal("DelayCorr does not match: expected", cNetem.DelayCorr, "got", netem.DelayCorr)
    }
    if netem.Limit != cNetem.Limit {
        t.Fatal("Limit does not match: expected", cNetem.Limit, "got", netem.Limit)
    }
    if netem.Loss != cNetem.Loss {
        t.Fatal("Loss does not match: expected", cNetem.Loss, "got", netem.Loss)
    }
    if netem.LossCorr != cNetem.LossCorr {
        t.Fatal("LossCorr does not match: expected", cNetem.LossCorr, "got", netem.LossCorr)
    }
    if netem.Gap != cNetem.Gap {
        t.Fatal("Gap does not match: expected", cNetem.Gap, "got", netem.Gap)
    }
    if netem.Duplicate != cNetem.Duplicate {
        t.Fatal("Duplicate does not match: expected", cNetem.Duplicate, "got", netem.Duplicate)
    }
    if netem.DuplicateCorr != cNetem.DuplicateCorr {
        t.Fatal("DuplicateCorr does not match: expected", cNetem.DuplicateCorr, "got", netem.DuplicateCorr)
    }
    if netem.Jitter != cNetem.Jitter {
        t.Fatal("Jitter does not match: expected", cNetem.Jitter, "got", netem.Jitter)
    }
    if netem.ReorderProb != cNetem.ReorderProb {
        t.Fatal("ReorderProb does not match: expected", cNetem.ReorderProb, "got", netem.ReorderProb)
    }
    if netem.ReorderCorr != cNetem.ReorderCorr {
        t.Fatal("ReorderCorr does not match: expected", cNetem.ReorderCorr, "got", netem.ReorderCorr)
    }
    if netem.CorruptProb != cNetem.CorruptProb {
        t.Fatal("CorruptProb does not match: expected", cNetem.CorruptProb, "got", netem.CorruptProb)
    }
    if netem.CorruptCorr != cNetem.CorruptCorr {
        t.Fatal("CorruptCorr does not match: expected", cNetem.CorruptCorr, "got", netem.CorruptCorr)
    }

    // change control properties
    newCtrlMap := make(map[int]float32)
    newCtrlMap[CTRL_TYPE_LATENCY] = 250 * 1000
    newCtrlMap[CTRL_TYPE_LOSS] = 20
    newCtrlMap[CTRL_TYPE_DUPLICATE] = 30
    newCtrlMap[CTRL_TYPE_DUPLICATE_CORR] = 40
    newCtrlMap[CTRL_TYPE_LOSS_CORR] = 50
    newCtrlMap[CTRL_TYPE_JITTER] = 60
    newCtrlMap[CTRL_TYPE_DELAY_CORR] = 70
    newCtrlMap[CTRL_TYPE_REORDER_PROB] = 72
    newCtrlMap[CTRL_TYPE_REORDER_CORR] = 75
    newCtrlMap[CTRL_TYPE_CORRUPT_PROB] = 78
    newCtrlMap[CTRL_TYPE_CORRUPT_CORR] = 85
    newCtrlMap[CTRL_TYPE_LIMIT] = 90
    newCtrlMap[CTRL_TYPE_GAP] = 1000

    cNetem, err = ChangeFlowControlOnIface(ifaceName, newCtrlMap)
    if err != nil {
        t.Fatal("Failed to set flow control on iface", ifaceName, " :", err)
    }

    qdiscs, err = GetFlowControlList(ifaceName)
    if err != nil {
        t.Fatal("Could not retrieve flow control list in iface", ifaceName, " :", err)
    }

    if len(qdiscs) != 1 {
        t.Fatal("Failed to add flow control correctly")
    }
    netem, ok = qdiscs[0].(*netlink.Netem)
    if !ok {
        t.Fatal("Qdisc type is wrong")
    }
    // Compare the record we got from the list with the one we created
    if netem.Latency != cNetem.Latency {
        t.Fatal("Latency does not match: expected", cNetem.Latency, "got", netem.Latency)
    }
    if netem.DelayCorr != cNetem.DelayCorr{
        t.Fatal("DelayCorr does not match: expected", cNetem.DelayCorr, "got", netem.DelayCorr)
    }
    if netem.Limit != cNetem.Limit {
        t.Fatal("Limit does not match: expected", cNetem.Limit, "got", netem.Limit)
    }
    if netem.Loss != cNetem.Loss {
        t.Fatal("Loss does not match: expected", cNetem.Loss, "got", netem.Loss)
    }
    if netem.LossCorr != cNetem.LossCorr {
        t.Fatal("LossCorr does not match: expected", cNetem.LossCorr, "got", netem.LossCorr)
    }
    if netem.Gap != cNetem.Gap {
        t.Fatal("Gap does not match: expected", cNetem.Gap, "got", netem.Gap)
    }
    if netem.Duplicate != cNetem.Duplicate {
        t.Fatal("Duplicate does not match: expected", cNetem.Duplicate, "got", netem.Duplicate)
    }
    if netem.DuplicateCorr != cNetem.DuplicateCorr {
        t.Fatal("DuplicateCorr does not match: expected", cNetem.DuplicateCorr, "got", netem.DuplicateCorr)
    }
    if netem.Jitter != cNetem.Jitter {
        t.Fatal("Jitter does not match: expected", cNetem.Jitter, "got", netem.Jitter)
    }
    if netem.ReorderProb != cNetem.ReorderProb {
        t.Fatal("ReorderProb does not match: expected", cNetem.ReorderProb, "got", netem.ReorderProb)
    }
    if netem.ReorderCorr != cNetem.ReorderCorr {
        t.Fatal("ReorderCorr does not match: expected", cNetem.ReorderCorr, "got", netem.ReorderCorr)
    }
    if netem.CorruptProb != cNetem.CorruptProb {
        t.Fatal("CorruptProb does not match: expected", cNetem.CorruptProb, "got", netem.CorruptProb)
    }
    if netem.CorruptCorr != cNetem.CorruptCorr {
        t.Fatal("CorruptCorr does not match: expected", cNetem.CorruptCorr, "got", netem.CorruptCorr)
    }

    // Cleanup 
    if err := RemoveFlowControlOnIface(ifaceName); err != nil {
        t.Fatal("Could not remove flow control from interface", ifaceName, " :", err)
    }
    qdiscs, err = GetFlowControlList(ifaceName)
    if err != nil {
        t.Fatal("Could not retrieve flow control after deletion on interface", ifaceName, " :", err)
    }
    if len(qdiscs) != 0 {
        t.Fatal("Failed to remove qdisc/flow control from interface", ifaceName, "after deletion")
    }
	t.Logf("Test passed: Successfully changed flow control on interface %s", ifaceName)
}

