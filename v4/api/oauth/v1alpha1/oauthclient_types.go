/*
Copyright 2023.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type GrantMethod string

var (
	GrantMethodAuto   GrantMethod = "auto"
	GrantMethodPrompt GrantMethod = "prompt"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,categories=oauth

// OAuthClient is the Schema for the oauthclients API
type OAuthClient struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Secret string `json:"secret"`

	// +kubebuilder:validation:Enum=auto;prompt
	GrantMethod GrantMethod `json:"grantMethod"`

	// +listType=set
	// +optional
	RedirectURIs []string `json:"redirectURIs,omitempty"`
	// +kubebuilder:default=7200
	// +kubebuilder:validation:Minimum=600
	// +optional
	AccessTokenMaxAge int64 `json:"accessTokenMaxAge,omitempty"`
	// +kubebuilder:default=7200
	// +kubebuilder:validation:Minimum=600
	// +optional
	AccessTokenInactivityTimeout int64 `json:"accessTokenInactivityTimeout,omitempty"`
}

// +kubebuilder:object:root=true

// OAuthClientList contains a list of OAuthClient
type OAuthClientList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OAuthClient `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OAuthClient{}, &OAuthClientList{})
}
