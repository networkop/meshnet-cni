// Copyright 2021 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1beta1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

//go:generate controller-gen object paths=$GOFILE

type TopologySpec struct {
	metav1.TypeMeta `json:",inline"`
	Links           []Link `json:"links"`
}

type TopologyStatus struct {
	metav1.TypeMeta `json:",inline"`
	Skipped         []Skipped `json:"skipped"`
	SrcIP           string    `json:"src_ip"`
	NetNS           string    `json:"net_ns"`
	ContainerID     string    `json:"container_id"`
}

type Skipped struct {
	PodName string `json:"pod_name"`
	LinkId  int64  `json:"link_id"`
}

type Link struct {
	LocalIntf string `json:"local_intf"`
	LocalIP   string `json:"local_ip"`
	PeerIntf  string `json:"peer_intf"`
	PeerIP    string `json:"peer_ip"`
	PeerPod   string `json:"peer_pod"`
	UID       int    `json:"uid"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type Topology struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Status TopologyStatus `json:"status"`
	Spec   TopologySpec   `json:"spec"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type TopologyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []Topology `json:"items"`
}
