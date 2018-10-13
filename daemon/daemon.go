package main

import (
	"fmt"
	"net"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/redhat-nfvpe/koko/api"
	"github.com/vishvananda/netlink"
)

type vtepData struct {
	NetNS    string `json:"net_ns"`
	LinkName string `json:"link_name"`
	LinkIP   string `json:"link_ip"`
	PeerVtep string `json:"peer_vtep"`
	VNI      int    `json:"vni"`
}

// Retrieves netlink.Link from NetNS
func getLinkFromNS(nsName string, linkName string) (result netlink.Link) {
	var vethNs ns.NetNS
	var err error

	// If namespace doesn't exist, do nothing and return empty result
	if vethNs, err = ns.GetNS(nsName); err != nil {
		return
	}

	defer vethNs.Close()
	// We can ignore the error returned here as we will create that interface instead
	_ = vethNs.Do(func(_ ns.NetNS) error {
		result, err = netlink.LinkByName(linkName)
		return err
	})

	return
}

func (v *vtepData) createOrUpdate() error {
	/// Looking up default interface
	_, srcIntf, err := getVxlanSource()
	if err != nil {
		return err
	}

	// Creating koko Veth struct
	veth := api.VEth{}
	veth.NsName = v.NetNS
	veth.LinkName = v.LinkName
	if v.LinkIP != "" {
		ipAddr, ipSubnet, err := net.ParseCIDR(v.LinkIP)
		if err != nil {
			return fmt.Errorf(" MESHNETD: Error parsing CIDR %s: %s", v.LinkIP, err)
		}
		veth.IPAddr = []net.IPNet{{
			IP:   ipAddr,
			Mask: ipSubnet.Mask,
		}}
	}

	// Creating koko vxlan struct
	vxlan := api.VxLan{
		ParentIF: srcIntf,
		IPAddr:   net.ParseIP(v.PeerVtep),
		ID:       v.VNI,
	}

	// Try ti read interface attributes from netlink
	link := getLinkFromNS(veth.NsName, veth.LinkName)

	// Check if interface already exists
	vxlanLink, ok := link.(*netlink.Vxlan)
	if ok { // the link we've found is a vxlan link

		if vxlanLink.VxlanId != vxlan.ID || vxlanLink.Group.Equal(vxlan.IPAddr) { // If Vxlan attrs are different
			// We remove the existing link and add a new one
			if err = veth.RemoveVethLink(); err != nil {
				return fmt.Errorf(" MESHNETD: Error when removing an old Vxlan interface with koko: %s", err)
			}

			if err = api.MakeVxLan(veth, vxlan); err != nil {
				return fmt.Errorf(" MESHNETD: Error when creating a Vxlan interface with koko: %s", err)
			}
		} // If Vxlan attrs are the same, do nothing

	} else { // the link we've found isn't a vxlan or doesn't exist
		// In this case we simply create a new one
		if err = api.MakeVxLan(veth, vxlan); err != nil {
			return fmt.Errorf(" MESHNETD: Error when creating a Vxlan interface with koko: %s", err)
		}
	}

	return nil
}

// Uses netlink to query the IP and LinkName of the interface with default route
func getVxlanSource() (srcIP, srcIntf string, err error) {
	// Looking up a default route to get the intf and IP for vxlan
	r, err := netlink.RouteGet(net.IPv4(1, 1, 1, 1))
	if (err != nil) && len(r) < 1 {
		return "", "", fmt.Errorf(" MESHNETD: Error getting default route: %s\n%+v", err, r)
	}
	srcIP = r[0].Src.String()

	link, err := netlink.LinkByIndex(r[0].LinkIndex)
	if err != nil {
		return "", "", fmt.Errorf(" MESHNETD: Error looking up link by its index: %s", err)
	}
	srcIntf = link.Attrs().Name

	return
}
