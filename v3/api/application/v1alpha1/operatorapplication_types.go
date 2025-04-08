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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kubesphere.io/ks-upgrade/v3/api/constants"
)

// OperatorApplicationSpec defines the desired state of OperatorApplication
type OperatorApplicationSpec struct {
	AppName       string `json:"name"`
	Description   string `json:"description,omitempty"`
	DescriptionZh string `json:"description_zh,omitempty"`
	Abstraction   string `json:"abstraction,omitempty"`
	AbstractionZh string `json:"abstraction_zh,omitempty"`
	Screenshots   string `json:"screenshots,omitempty"`
	ScreenshotsZh string `json:"screenshots_zh,omitempty"`
	AppHome       string `json:"appHome,omitempty"`
	Icon          string `json:"icon,omitempty"`
	Owner         string `json:"owner,omitempty"`
}

// OperatorApplicationStatus defines the observed state of OperatorApplication
type OperatorApplicationStatus struct {
	LatestVersion string       `json:"latestVersion,omitempty"`
	State         string       `json:"state,omitempty"`
	UpdateTime    *metav1.Time `json:"updateTime,omitempty"`
	StatusTime    *metav1.Time `json:"statusTime,omitempty"`
}

//+genclient
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster
//+genclient:nonNamespaced
//+k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// OperatorApplication is the Schema for the operatorapplications API
type OperatorApplication struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OperatorApplicationSpec   `json:"spec,omitempty"`
	Status OperatorApplicationStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// OperatorApplicationList contains a list of OperatorApplication
type OperatorApplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OperatorApplication `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OperatorApplication{}, &OperatorApplicationList{})
}

func (in *OperatorApplication) GetTrueName() string {
	return in.Spec.AppName
}

func (in *OperatorApplication) GetState() string {
	if in.Status.State == "" {
		return StateActive
	}
	return in.Status.State
}

func (in *OperatorApplication) GetCreationTime() time.Time {
	return in.CreationTimestamp.Time
}

func (in *OperatorApplication) GetLatestVersion() string {
	return in.Status.LatestVersion
}

func (in *OperatorApplication) GetApplicationId() string {
	return in.Name
}

func (in *OperatorApplication) GetCategoryId() string {
	return getValue(in.Labels, constants.CategoryIdLabelKey)
}

func (in *OperatorApplication) GetWorkspace() string {
	ws := getValue(in.Labels, constants.WorkspaceLabelKey)
	if ws == "" {
		return getValue(in.Labels, OriginWorkspaceLabelKey)
	}
	return ws
}

func (in *OperatorApplication) GetCreator() string {
	return getValue(in.Annotations, constants.CreatorAnnotationKey)
}

func (in *OperatorApplication) GetAppName() string {
	return in.Name
}

func (in *OperatorApplication) GetUpdateTime() *metav1.Time {
	return in.Status.UpdateTime
}

func (in *OperatorApplication) GetStatusTime() *metav1.Time {
	return in.Status.StatusTime
}

func (in *OperatorApplication) GetAbstraction() string {
	return in.Spec.Abstraction
}

func (in *OperatorApplication) GetAbstractionZh() string {
	return in.Spec.AbstractionZh
}

func (in *OperatorApplication) GetDescription() string {
	return in.Spec.Description
}

func (in *OperatorApplication) GetDescriptionZh() string {
	return in.Spec.DescriptionZh
}

func (in *OperatorApplication) GetScreenshots() string {
	return in.Spec.Screenshots
}

func (in *OperatorApplication) GetScreenshotsZh() string {
	return in.Spec.ScreenshotsZh
}

func (in *OperatorApplication) GetAttachments() []string {
	return []string{}
}

func (in *OperatorApplication) GetAppHome() string {
	return in.Spec.AppHome
}

func (in *OperatorApplication) GetIcon() string {
	return in.Spec.Icon
}

func (in *OperatorApplication) GetAnnotations() map[string]string {
	return in.Annotations
}
