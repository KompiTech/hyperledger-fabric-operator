/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// FabricPeerSpec defines the desired state of FabricPeer
type FabricPeerSpec struct {
	Image                string                       `json:"image"`
	BuilderImage         string                       `json:"builderimage"`
	RuntimeImage         string                       `json:"runtimeimage"`
	CouchDBImage         string                       `json:"couchdbimage"`
	DINDImage            string                       `json:"dindimage"`
	MetricsImage         string                       `json:"metricsimage"`
	Replicas             int32                        `json:"replicas"`
	DataVolumeSize       resource.Quantity            `json:"datavolumesize,omitempty"`
	CertVolumeSize       resource.Quantity            `json:"certvolumesize,omitempty"`
	Organization         string                       `json:"organization"`
	MspId                string                       `json:"mspid"`
	CommonName           string                       `json:"commonname"`
	BootstrapNodeAddress string                       `json:"bootstrapnodeaddress"`
	SvcType              corev1.ServiceType           `json:"SvcType,omitempty"`
	Certificate          map[string]CertificateSecret `json:"certificate"`
	Containers           []corev1.Container           `json:"containers"`
	NodeOUsEnabled       bool                         `json:"nodeousenabled"`
}

// FabricPeerStatus defines the observed state of FabricPeer
type FabricPeerStatus struct {
	FabricPeerState string `json:"fabricpeerstate"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// FabricPeer is the Schema for the fabricpeers API
type FabricPeer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FabricPeerSpec   `json:"spec,omitempty"`
	Status FabricPeerStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// FabricPeerList contains a list of FabricPeer
type FabricPeerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FabricPeer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&FabricPeer{}, &FabricPeerList{})
}
