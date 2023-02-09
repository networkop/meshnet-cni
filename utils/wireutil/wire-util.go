package wireutil

import (
	"fmt"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/safchain/ethtool"
)

func SetTxChecksumOff(intfName, nsName string) error {
	var vethNs ns.NetNS
	var err error

	if vethNs, err = ns.GetNS(nsName); err != nil {
		return fmt.Errorf("Could not get required ns %s: %v", nsName, err)
	}
	defer vethNs.Close()

	err = vethNs.Do(func(_ ns.NetNS) error {
		etlHndl, err := ethtool.NewEthtool()
		if err != nil {
			return fmt.Errorf("Could not open ethtool handle: %v", err)
		}
		defer etlHndl.Close()

		etlConf := map[string]bool{
			"tx-checksum-ipv4":             false,
			"tx-checksum-ipv6":             false,
			"tx-checksum-ip-generic":       false,
			"tx-tcp-segmentation":          false,
			"tx-tcp6-segmentation":         false,
			"tx-checksum-fcoe-crc":         false,
			"tx-checksum-sctp":             false,
			"tx-tcp-ecn-segmentation":      false,
			"tx-tcp-mangleid-segmentation": false,
		}

		err = etlHndl.Change(intfName, etlConf)
		if err != nil {
			return fmt.Errorf("Could not set tx checksum on interface %s, ns %s: %v", intfName, nsName, err)
		}
		return nil
	})
	return err
}
