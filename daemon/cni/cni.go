package cni

import (
	"encoding/json"
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
func loadConfList() (*types.NetConfList, error) {
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

	// we only care about the first file
	bytes, _ := ioutil.ReadFile(files[0])
	configParsed := types.NetConfList{}
	err = json.Unmarshal(bytes, &configParsed)

	return &configParsed, err
}

func saveConfList(c *types.NetConfList) error {
	bytes, err := json.MarshalIndent(c, "", "\t")
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

	conf.Plugins = append(conf.Plugins, &types.NetConf{
		Type: defaultPluginName,
		Name: defaultPluginName,
	})

	return saveConfList(conf)
}

// Cleanup removes meshnet CNI configuration
func Cleanup() {
	if err := os.Remove(meshnetCNIPath); err != nil {
		log.Infof("Failed to remove file %s: %v", meshnetCNIPath, err)
	}
}
