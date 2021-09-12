package cni

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/containernetworking/cni/libcni"
	"github.com/containernetworking/cni/pkg/types"
	log "github.com/sirupsen/logrus"
)

const (
	defaultNetDir     = "/etc/cni/net.d"
	defaultCNIFile    = "00-meshnet.conflist"
	defaultPluginName = "meshnet"
)

var meshnetCNIPath = filepath.Join(defaultNetDir, defaultCNIFile)

// This is borrowed from https://tinyurl.com/khjhf9xd
func loadConfList() (map[string]interface{}, error) {
	files, err := libcni.ConfFiles(defaultNetDir, []string{".conflist"})
	switch {
	case err != nil:
		return nil, err
	case len(files) == 0:
		return nil, libcni.NoConfigsFoundError{Dir: defaultNetDir}
	}

	for i, f := range files {
		if strings.Contains(f, "meshnet") {
			files = append(files[:i], files[i+1:]...)
			break
		}
	}

	sort.Strings(files)

	// only pre-parse the top of the CNI file without using the types.NetConfList
	// this is because some generic types do not define the complete config struct
	// e.g. IPAM config will not be parsed at all beyong the `type`
	var conf map[string]interface{}

	// we only care about the first file
	bytes, _ := ioutil.ReadFile(files[0])
	err = json.Unmarshal(bytes, &conf)

	return conf, err
}

func saveConfList(m map[string]interface{}) error {
	bytes, err := json.MarshalIndent(m, "", "\t")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(meshnetCNIPath, bytes, os.FileMode(06444))
}

// Init installs meshnet CNI configuration
func Init() error {

	conf, err := loadConfList()
	if err != nil {
		return err
	}

	var plugins []interface{}
	plug, ok := conf["plugins"]
	if !ok {
		return fmt.Errorf("error parsing configuration list: no 'plugins' key")
	}
	plugins, ok = plug.([]interface{})
	if !ok {
		return fmt.Errorf("error parsing configuration list: invalid 'plugins' type %T", plug)
	}
	if len(plugins) == 0 {
		return fmt.Errorf("error parsing configuration list: no plugins in list")
	}

	plugins = append(plugins, &types.NetConf{
		Type: defaultPluginName,
		Name: defaultPluginName,
	})

	conf["plugins"] = plugins

	return saveConfList(conf)
}

// Cleanup removes meshnet CNI configuration
func Cleanup() {
	if err := os.Remove(meshnetCNIPath); err != nil {
		log.Infof("Failed to remove file %s: %v", meshnetCNIPath, err)
	}
}
