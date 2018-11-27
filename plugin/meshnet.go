// Copyright 2015 CNI authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// Original author @networkop
// Heavily inspired by Ratchet-CNI, kokonet and standard CNI plugins

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/containernetworking/cni/pkg/invoke"
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/vishvananda/netlink"
	"go.etcd.io/etcd/clientv3"

	"github.com/davecgh/go-spew/spew"
	koko "github.com/redhat-nfvpe/koko/api"
)

const (
	etcdHost       = "localhost"
	etcdPort       = "2379"
	requestTimeout = 20 * time.Second
	dialTimeout    = 4 * time.Second
	vxlanBase      = 5000
	defaultPort    = "8181"
)

type netConf struct {
	types.NetConf
	EtcdHost string                 `json:"etcd_host"`
	EtcdPort string                 `json:"etcd_port"`
	Delegate map[string]interface{} `json:"delegate"`
}

type k8sArgs struct {
	types.CommonArgs
	K8S_POD_NAME               types.UnmarshallableString
	K8S_POD_NAMESPACE          types.UnmarshallableString
	K8S_POD_INFRA_CONTAINER_ID types.UnmarshallableString
}

type linkInfo struct {
	PeerPod   string `json:"peer_pod"`
	LocalIntf string `json:"local_intf"`
	LocalIP   string `json:"local_ip,omitempty"`
	PeerIntf  string `json:"peer_intf"`
	PeerIP    string `json:"peer_ip,omitempty"`
	UID       int    `json:"uid"`
}

type podMeta struct {
	Name  string
	SrcIP string
	NetNS string
	Links *[]linkInfo
}

type agentPayload struct {
	NetNS    string `json:"net_ns"`
	LinkName string `json:"link_name"`
	LinkIP   string `json:"link_ip"`
	PeerVtep string `json:"peer_vtep"`
	VNI      int    `json:"vni"`
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func init() {
	// this ensures that main runs only on main thread (thread group leader).
	// since namespace ops (unshare, setns) are done for a single thread, we
	// must ensure that the goroutine does not jump from OS thread to thread
	runtime.LockOSThread()
}

// Loads information from cni.conf
func loadConf(bytes []byte) (*netConf, error) {
	n := &netConf{}
	if err := json.Unmarshal(bytes, n); err != nil {
		return nil, fmt.Errorf("failed to load netconf: %v", err)
	}
	if n.EtcdHost == "" {
		n.EtcdHost = etcdHost
	}
	if n.EtcdPort == "" {
		n.EtcdHost = etcdPort
	}

	return n, nil
}

// Queries etcd with key == podName and gets a []linkInfo along with srcIP and NetNS
func (pod *podMeta) getPodMetadata(ctx context.Context, kv clientv3.KV) error {

	srcIP := fmt.Sprintf("/%s/src_ip", pod.Name)
	resp, err := kv.Get(ctx, srcIP)
	if err != nil {
		return err
	} else if len(resp.Kvs) < 1 {
		// This could mean the peer pod hasn't come up, so we're just returning nil
		log.Printf("Could not find `src_ip` key for pod %s", pod.Name)
		return nil
	}

	pod.SrcIP = string(resp.Kvs[0].Value)

	netNS := fmt.Sprintf("/%s/net_ns", pod.Name)
	resp, err = kv.Get(ctx, netNS)
	if err != nil {
		return err
	} else if len(resp.Kvs) < 1 {
		return fmt.Errorf("Could not find `net_ns` key for pod %s", pod.Name)
	}

	pod.NetNS = string(resp.Kvs[0].Value)

	links := fmt.Sprintf("/%s/links", pod.Name)
	linkInfo := &[]linkInfo{}
	resp, err = kv.Get(ctx, links)
	if err != nil {
		return err
	}

	if len(resp.Kvs) < 1 {
		// Instead of failing we simply keep the empty linkInfo struct
		// This way we simply run the master plugin and don't create any p2p links
		log.Printf("Could not retrieve linkInfo metadata for pod %s", pod.Name)
	} else {
		if err = json.Unmarshal(resp.Kvs[0].Value, linkInfo); err != nil {
			return err
		}
	}
	pod.Links = linkInfo

	return nil
}

// Sets srcIP and NetNS in PodMeta and saves it in etcd
func (pod *podMeta) setPodAlive(ctx context.Context, kv clientv3.KV, netNS, srcIP string) error {
	log.Printf("Setting srcIP:%s and NetNS:%s for POD:%s", srcIP, netNS, pod.Name)

	// Update etcd state
	srcIPKey := fmt.Sprintf("/%s/src_ip", pod.Name)
	_, err := kv.Put(ctx, srcIPKey, srcIP)
	if err != nil {
		return err
	}

	NetNSKey := fmt.Sprintf("/%s/net_ns", pod.Name)
	_, err = kv.Put(ctx, NetNSKey, netNS)
	if err != nil {
		return err
	}

	return nil
}

func (pod *podMeta) setSkipped(ctx context.Context, kv clientv3.KV, peer string) error {
	log.Printf("Setting skipped veth flag for %s and peer %s", pod.Name, peer)
	skippedKey := fmt.Sprintf("/%s/skipped/%s", pod.Name, peer)
	_, err := kv.Put(ctx, skippedKey, "true")
	if err != nil {
		return err
	}
	return nil
}

func (pod *podMeta) setSkippedReversed(ctx context.Context, kv clientv3.KV, peer string) error {
	log.Printf("Setting skipped reversed veth flag for %s and peer %s", pod.Name, peer)
	skippedKey := fmt.Sprintf("/%s/skipped/%s", peer, pod.Name)
	_, err := kv.Put(ctx, skippedKey, "true")
	if err != nil {
		return err
	}
	return nil
}

func (pod *podMeta) isSkipped(ctx context.Context, kv clientv3.KV, peer string) (bool, error) {
	log.Printf("Reading skipped veth flag from %s for peer %s", pod.Name, peer)
	skippedKey := fmt.Sprintf("/%s/skipped/%s", peer, pod.Name)
	resp, err := kv.Get(ctx, skippedKey)
	if err != nil {
		return false, err
	} else if len(resp.Kvs) < 1 {
		log.Printf("Could not find `skipped` flag for pod %s", pod.Name)
		return false, nil
	}
	skippedFlag := string(resp.Kvs[0].Value)
	return skippedFlag == "true", nil
}

// Checks if SrcIP and NetNS are set in pod's metadata struct
func (pod *podMeta) isAlive() bool {
	return pod.SrcIP != "" && pod.NetNS != ""
}

// Uses netlink to query the IP and LinkName of the interface with default route
func getVxlanSource() (srcIP, srcIntf string, err error) {
	// Looking up a default route to get the intf and IP for vxlan
	r, err := netlink.RouteGet(net.IPv4(1, 1, 1, 1))
	if (err != nil) && len(r) < 1 {
		return "", "", fmt.Errorf("Error getting default route: %s\n%+v", err, r)
	}
	srcIP = r[0].Src.String()

	link, err := netlink.LinkByIndex(r[0].LinkIndex)
	if err != nil {
		return "", "", fmt.Errorf("Error looking up link by its index: %s", err)
	}
	srcIntf = link.Attrs().Name

	return
}

// Creates koko.Veth from NetNS and LinkName
func makeVeth(netNS, linkName string, ip string) (*koko.VEth, error) {
	log.Printf("Creating Veth struct with NetNS:%s and intfName: %s, IP:%s", netNS, linkName, ip)
	veth := koko.VEth{}
	veth.NsName = netNS
	veth.LinkName = linkName
	if ip != "" {
		ipAddr, ipSubnet, err := net.ParseCIDR(ip)
		if err != nil {
			return nil, fmt.Errorf("Error parsing CIDR %s: %s", ip, err)
		}
		veth.IPAddr = []net.IPNet{{
			IP:   ipAddr,
			Mask: ipSubnet.Mask,
		}}
	}

	return &veth, nil
}

// Creates koko.Vxlan from ParentIF, destination IP and VNI
func makeVxlan(srcIntf string, peerIP string, idx int) *koko.VxLan {
	return &koko.VxLan{
		ParentIF: srcIntf,
		IPAddr:   net.ParseIP(peerIP),
		ID:       vxlanBase + idx,
	}
}

// DelegateAdd call
func delegateAdd(ctx context.Context, netconf map[string]interface{}, intfName string) (result types.Result, err error) {
	netconfBytes, err := json.Marshal(netconf)
	if err != nil {
		return nil, fmt.Errorf("Error serialising delegate config %v", err)
	}

	if os.Setenv("CNI_IFNAME", intfName) != nil {
		return nil, fmt.Errorf("Error setting CNI_IFNAME")
	}

	log.Printf("About to delegate Add to %s", netconf["type"].(string))
	result, err = invoke.DelegateAdd(ctx, netconf["type"].(string), netconfBytes, nil)
	if err != nil {
		return nil, fmt.Errorf("Error invoking Delegate Add %v", err)
	}
	return result, nil
}

// DelegateDel call
func delegateDel(ctx context.Context, netconf map[string]interface{}, intfName string) error {
	netconfBytes, err := json.Marshal(netconf)
	if err != nil {
		return fmt.Errorf("Error serialising delegate config %v", err)
	}
	if os.Setenv("CNI_IFNAME", intfName) != nil {
		return fmt.Errorf("Error setting CNI_IFNAME")
	}
	err = invoke.DelegateDel(ctx, netconf["type"].(string), netconfBytes, nil)
	if err != nil {
		return fmt.Errorf("Error invoking Delegate Del %v", err)
	}
	return nil
}

// Adds interfaces to a POD
func cmdAdd(args *skel.CmdArgs) error {
	// Setting up context
	ctx, _ := context.WithTimeout(context.Background(), requestTimeout)

	// Parsing cni .conf file
	n, err := loadConf(args.StdinData)
	if err != nil {
		return err
	}

	// Parsing CNI_ARGS environment variable
	cniArgs := k8sArgs{}
	if err = types.LoadArgs(args.Args, &cniArgs); err != nil {
		return err
	}
	log.SetPrefix(fmt.Sprintf("---| %s |===> ", cniArgs.K8S_POD_NAME))
	log.Printf("Processing ADD for POD %s", cniArgs.K8S_POD_NAME)

	// Verifying and calling delegateAdd for the master plugin
	if n.Delegate == nil {
		return fmt.Errorf(`"delegate" is a required field in config, it should be the config of the main plugin to use`)
	}
	r, err := delegateAdd(ctx, n.Delegate, args.IfName)
	if err != nil {
		return err
	}
	log.Printf("Master plugin finished")

	// Finding the source IP and interface for VXLAN VTEP
	srcIP, srcIntf, err := getVxlanSource()
	if err != nil {
		return err
	}
	log.Printf("Default route is via %s@%s", srcIP, srcIntf)

	// setting up etcd client
	endpoints := []string{fmt.Sprintf("http://%s:%s", n.EtcdHost, n.EtcdPort)}

	cli, err := clientv3.New(clientv3.Config{
		DialTimeout: dialTimeout,
		Endpoints:   endpoints,
	})
	if err != nil {
		return err
	}
	defer cli.Close()
	kv := clientv3.NewKV(cli)

	// Initialise pod's metadata struct
	localPod := &podMeta{
		Name: string(cniArgs.K8S_POD_NAME),
	}

	// Marking pod as "alive" by setting its srcIP and NetNS
	if err = localPod.setPodAlive(ctx, kv, args.Netns, srcIP); err != nil {
		return err
	}

	// Query etcd for this pod's metadata
	if err = localPod.getPodMetadata(ctx, kv); err != nil {
		return err
	}

	for _, link := range *localPod.Links { // Iterate over each link of the local pod

		// Build koko's veth struct for local intf
		myVeth, err := makeVeth(args.Netns, link.LocalIntf, link.LocalIP)
		if err != nil {
			return err
		}

		// Initialising peer pod's metadata
		peerPod := &podMeta{
			Name: link.PeerPod,
		}
		if err = peerPod.getPodMetadata(ctx, kv); err != nil {
			return err
		}

		//log.Printf("Local Pod metadata: %+v", spew.Sdump(localPod))
		//log.Printf("Peer Pod metadata: %+v", spew.Sdump(peerPod))

		// Going through several possible situations
		if peerPod.isAlive() { // This means we're coming up AFTER our peer so things are pretty easy

			log.Printf("Peer pod %s is alive", peerPod.Name)

			if peerPod.SrcIP == localPod.SrcIP { // This means we're on the same host

				log.Printf("%s and %s are on the same host", localPod.Name, peerPod.Name)

				// Creating koko's Veth struct for peer intf
				peerVeth, err := makeVeth(peerPod.NetNS, link.PeerIntf, link.PeerIP)
				if err != nil {
					return err
				}

				// Checking if interfaces already exist
				iExist, _ := koko.IsExistLinkInNS(myVeth.NsName, myVeth.LinkName)
				pExist, _ := koko.IsExistLinkInNS(peerVeth.NsName, peerVeth.LinkName)

				if (iExist == true) && (pExist == true) { // If both link exist, we don't need to do anything

					log.Printf("Interface %s already exists in namespace (%s)",
						myVeth.LinkName, myVeth.NsName)

				} else if (iExist != true) && (pExist == true) { // If only peer link exists, we need to destroy it first

					if err := peerVeth.RemoveVethLink(); err != nil {
						log.Printf("Failed to remove a stale interface %s of my peer %s", peerVeth.LinkName, link.PeerPod)
						return err
					}

					if err = koko.MakeVeth(*myVeth, *peerVeth); err != nil {
						log.Printf("Error creating VEth pair after peer link remove: %s", err)
						return err
					}

				} else if (iExist == true) && (pExist != true) { // If only local link exists, we need to destroy it first

					if err := myVeth.RemoveVethLink(); err != nil {
						log.Printf("Failed to remove a local stale VEth interface %s for pod %s", myVeth.LinkName, localPod.Name)
						return err
					}

					if err = koko.MakeVeth(*myVeth, *peerVeth); err != nil {
						log.Printf("Error creating VEth pair after local link remove: %s", err)
						return err
					}

				} else { // if neither link exists, we have two options

					isSkipped, err := localPod.isSkipped(ctx, kv, link.PeerPod)
					if err != nil {
						return err
					}

					// Comparing names to determine higher priority
					higherPrio := localPod.Name > link.PeerPod

					if isSkipped || higherPrio { // If peer POD skipped us (booted before us) or we have a higher priority
						log.Printf("Peer POD has skipped us or we have a higher IP")
						if err = koko.MakeVeth(*myVeth, *peerVeth); err != nil {
							log.Printf("Error when creating a new VEth pair with koko: %s", err)
							log.Printf("MY VETH STRUCT: %+v", spew.Sdump(myVeth))
							log.Printf("PEER STRUCT: %+v", spew.Sdump(peerVeth))
							return err
						}
					} else { // peerPod has higherPrio and hasn't skipped us
						// In this case we do nothing, since the pod with a higher IP is supposed to connect veth pair
						log.Printf("Doing nothing, expecting peer POD to connect veth pair")
						continue
					}
				}

			} else { // This means we're on different hosts

				log.Printf("%s and %s are on different hosts", localPod.Name, peerPod.Name)

				// Creating koko's Vxlan struct
				vxlan := makeVxlan(srcIntf, peerPod.SrcIP, link.UID)
				// Checking if interface already exists
				iExist, _ := koko.IsExistLinkInNS(myVeth.NsName, myVeth.LinkName)
				if iExist == true { // If VXLAN intf exists, we need to remove it first
					if err := myVeth.RemoveVethLink(); err != nil {
						log.Printf("Failed to remove a local stale VXLAN interface %s for pod %s", myVeth.LinkName, localPod.Name)
						return err
					}
				}
				if err = koko.MakeVxLan(*myVeth, *vxlan); err != nil {
					log.Printf("Error when creating a Vxlan interface with koko: %s", err)
					return err
				}

				// Now we need to make an API call to update the remote VTEP to point to us
				payload := agentPayload{
					NetNS:    peerPod.NetNS,
					LinkName: link.PeerIntf,
					LinkIP:   link.PeerIP,
					PeerVtep: localPod.SrcIP,
					VNI:      link.UID + vxlanBase,
				}
				// Encode payload
				jsonBytes, err := json.Marshal(payload)
				if err != nil {
					return err
				}
				// Build URL
				port := getEnv("MESHNETD_PORT", defaultPort)
				url := fmt.Sprintf("http://%s:%s/vtep", peerPod.SrcIP, port)
				log.Printf("Sending payload %+v to remote URL: %s", payload, url)
				// Send Post request and catch any errors
				if err = putRequest(url, bytes.NewBuffer(jsonBytes)); err != nil {
					return err
				}
			}

		} else { // This means that our peer pod hasn't come up yet
			// Since there's no way of telling if our peer is going to be on this host or another,
			// the only option is to do nothing, assuming that the peer POD will do all the plumbing when it comes up
			log.Printf("Peer pod %s isn't alive yet, continuing", peerPod.Name)
			// Here we need to set the skipped flag so that our peer can configure VEth interface when it comes up later
			if err = localPod.setSkipped(ctx, kv, link.PeerPod); err != nil {
				return err
			}
		}
	}

	return r.Print()
}

// Sends PUT request to remote meshnet daemon
func putRequest(url string, data io.Reader) error {
	client := &http.Client{}
	req, err := http.NewRequest(http.MethodPut, url, data)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		bytes, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		return fmt.Errorf(string(bytes))
	}
	return nil
}

// Deletes interfaces from a POD
func cmdDel(args *skel.CmdArgs) error {
	// Setting up context for cmdDel
	ctx, _ := context.WithTimeout(context.Background(), requestTimeout)

	// Parsing cni .conf file
	n, err := loadConf(args.StdinData)
	if err != nil {
		return err
	}

	// Parsing CNI_ARGS environment variable
	cniArgs := k8sArgs{}
	if err = types.LoadArgs(args.Args, &cniArgs); err != nil {
		return err
	}
	log.SetPrefix(fmt.Sprintf("xxx| %s |===> ", cniArgs.K8S_POD_NAME))
	log.Printf("Processing Del for POD %s", cniArgs.K8S_POD_NAME)

	// setting up etcd client
	endpoints := []string{fmt.Sprintf("http://%s:%s", n.EtcdHost, n.EtcdPort)}

	cli, err := clientv3.New(clientv3.Config{
		DialTimeout: dialTimeout,
		Endpoints:   endpoints,
	})
	if err != nil {
		return err
	}
	defer cli.Close()
	kv := clientv3.NewKV(cli)

	// Initialise local pod's metadata struct
	localPod := &podMeta{
		Name: string(cniArgs.K8S_POD_NAME),
	}

	// By setting srcIP and NetNS to "" we're marking this POD as dead
	if err = localPod.setPodAlive(ctx, kv, "", ""); err != nil {
		return err
	}

	// Query etcd for local pod's metadata
	if err = localPod.getPodMetadata(ctx, kv); err != nil {
		return err
	}

	for _, link := range *localPod.Links { // Iterate over each link of the local pod

		// Creating koko's Veth struct for local intf
		myVeth, err := makeVeth(args.Netns, link.LocalIntf, link.LocalIP)
		if err != nil {
			return err
		}

		// API call to koko to remove local Veth link
		if err = myVeth.RemoveVethLink(); err != nil {
			// instead of failing, just log the error and move on
			log.Printf("Error removing Veth link: %s", err)
		}

		// Setting reversed skipped flag so that this pod will try to connect veth pair on restart
		if err = localPod.setSkippedReversed(ctx, kv, link.PeerPod); err != nil {
			return err
		}
	}

	// Verifying and calling DelegateDel for the master plugin
	if n.Delegate == nil {
		return fmt.Errorf(`"delegate" is a required field in config, it should be the config of the main plugin to use`)
	}
	if err = delegateDel(ctx, n.Delegate, args.IfName); err != nil {
		return err
	}

	return nil
}

func main() {
	// TODO: implement plugin version
	skel.PluginMain(cmdAdd, cmdGet, cmdDel, version.All, "TODO")
}

func cmdGet(args *skel.CmdArgs) error {
	// TODO: implement
	return fmt.Errorf("not implemented")
}
