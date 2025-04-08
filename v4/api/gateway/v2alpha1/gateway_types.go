package v2alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

func init() {
	SchemeBuilder.Register(&Gateway{}, &GatewayList{})
}

// GatewaySpec defines the desired state of Gateway
type GatewaySpec struct {
	AppVersion string `json:"appVersion"`
	// +kubebuilder:pruning:PreserveUnknownFields
	Values runtime.RawExtension `json:"values"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:storageversion

// Gateway is the Schema for the gateways API
type Gateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              GatewaySpec `json:"spec,omitempty"`
	// +kubebuilder:pruning:PreserveUnknownFields
	Status runtime.RawExtension `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// GatewayList contains a list of Gateway
type GatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Gateway `json:"items"`
}
