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

// FabricOrdererSpec defines the desired state of FabricOrderer
type FabricOrdererSpec struct {
	Image          string              `json:"image"`
	MetricsImage   string              `json:"metricsimage"`
	Replicas       int32               `json:"replicas"`
	DataVolumeSize resource.Quantity   `json:"datavolumesize"`
	CertVolumeSize resource.Quantity   `json:"cavolumesize"`
	Organization   string              `json:"organization"`
	MspId          string              `json:"mspid"`
	CommonName     string              `json:"commonname"`
	SvcType        corev1.ServiceType  `json:"SvcType,omitempty"`
	Certificate    []CertificateSecret `json:"certificate"`
	TLSCertificate []CertificateSecret `json:"tlscertificate"`
	Genesis        string              `json:"genesis"`
	Containers     []corev1.Container  `json:"containers"`
	NodeOUsEnabled bool                `json:"nodeousenabled"`
}

// FabricOrdererStatus defines the observed state of FabricOrderer
type FabricOrdererStatus struct {
	FabricOrdererState string `json:"fabricordererstate"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// FabricOrderer is the Schema for the fabricorderers API
type FabricOrderer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FabricOrdererSpec   `json:"spec,omitempty"`
	Status FabricOrdererStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// FabricOrdererList contains a list of FabricOrderer
type FabricOrdererList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FabricOrderer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&FabricOrderer{}, &FabricOrdererList{})
}
