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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kubesphere.io/ks-upgrade/v3/api/constants"
)

// ManifestSpec defines the desired state of Manifest
type ManifestSpec struct {
	Cluster          string            `json:"cluster,omitempty"`
	Namespace        string            `json:"namespace,omitempty"`
	Description      string            `json:"description,omitempty"`
	AppName          string            `json:"app,omitempty"`
	AppVersion       string            `json:"appVersion"`
	CustomResource   string            `json:"customResource" yaml:"customResource"`
	RelatedResources []RelatedResource `json:"relatedResources,omitempty" yaml:"relatedResources"`
	// expected manifest version, when this version is not equal status.version, the manifest need update
	// this filed should be modified when any filed of the spec modified.
	Version int `json:"version"`
}

// ManifestStatus defines the observed state of Manifest
type ManifestStatus struct {
	State                 string                  `json:"state,omitempty"`
	ResourceState         string                  `json:"resourceState,omitempty"`
	RelatedResourceStates []*RelatedResourceState `json:"relatedResourceState,omitempty"`
	Conditions            []*condition            `json:"conditions,omitempty"`
	Version               int                     `json:"version,omitempty"`
	LastUpdate            *metav1.Time            `json:"lastUpdate,omitempty"`
}

type RelatedResource struct {
	Name string `json:"name"`
	Data string `json:"data" yaml:"data"`
}

type RelatedResourceState struct {
	ResourceName  string `json:"resourceName"`
	ResourceState string `json:"resourceState"`
}

type condition struct {
	Code string `json:"code,omitempty"`
	Msg  string `json:"msg,omitempty"`
	Data string `json:"data,omitempty"`
}

// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.resourceState"
// +kubebuilder:printcolumn:name="AppVersion",type="string",JSONPath=".spec.appVersion"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +genclient
// +genclient:nonNamespaced
// +kubebuilder:subresource:status
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Manifest is the Schema for the manifests API
type Manifest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ManifestSpec   `json:"spec,omitempty"`
	Status ManifestStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ManifestList contains a list of Manifest
type ManifestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Manifest `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Manifest{}, &ManifestList{})
}

func (in *Manifest) GetManifestCluster() string {
	return getValue(in.Annotations, ClusterNameAnnotationKey)
}

func (in *Manifest) GetManifestWorkspace() string {
	return getValue(in.Labels, constants.WorkspaceLabelKey)
}

func (in *Manifest) GetManifestNamespace() string {
	return getValue(in.Labels, constants.NamespaceLabelKey)
}

func (in *Manifest) GetCreator() string {
	return getValue(in.Annotations, constants.CreatorAnnotationKey)
}
