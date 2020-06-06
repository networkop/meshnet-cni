#!/bin/sh

set_defaults() {
  CNI_CONF_DIR="/etc/cni/net.d"
  CNI_BIN_DIR="/opt/cni/bin"
}

detect_cni() {
  pidof -s containerd &> /dev/null
  if [ $? -eq 0 ]; then 
    CNI_CONF_DIR="/var/lib/rancher/k3s/agent/etc/cni/net.d"
    CNI_BIN_DIR="/bin"
  else
    set_defaults
  fi
}

detect_cni

echo "Distributing files"
if [ -d "$CNI_BIN_DIR" ] && [ -f "./meshnet" ]; then
  cp ./meshnet $CNI_BIN_DIR
fi

if [ -d "$CNI_CONF_DIR" ] && [ -f "./meshnet.conf" ]; then
  cp ./meshnet.conf $CNI_CONF_DIR
fi

if [ ! -f "${CNI_CONF_DIR}/00-meshnet.conf" ]; then
  echo "Mergin existing CNI configuration with meshnet"
  existing=$(ls -1 $CNI_CONF_DIR | egrep "flannel|weave|bridge|calico|contiv|cilium|cni|kindnet" | head -n1)
  has_plugin_section=$(jq 'has("plugins")' $CNI_CONF_DIR/$existing)
  if [ "$has_plugin_section" = true ]; then
    jq -s '.[1].delegate = (.[0].plugins[0])' $CNI_CONF_DIR/$existing ${CNI_CONF_DIR}/meshnet.conf | jq '.[1]' > ${CNI_CONF_DIR}/00-meshnet.conf
  else
    jq -s '.[1].delegate = (.[0])' ${CNI_CONF_DIR}/$existing ${CNI_CONF_DIR}/meshnet.conf | jq '.[1]' > ${CNI_CONF_DIR}/00-meshnet.conf
  fi
else
  echo "Re-using existing CNI config"
fi

echo 'Making sure the name is set for the master plugin'
jq '.delegate.name = "masterplugin"' ${CNI_CONF_DIR}/00-meshnet.conf > /tmp/cni.conf && mv /tmp/cni.conf ${CNI_CONF_DIR}/00-meshnet.conf  

echo "Starting meshnetd daemon"
/meshnetd
