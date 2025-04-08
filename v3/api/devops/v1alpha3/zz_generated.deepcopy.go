//go:build !ignore_autogenerated
// +build !ignore_autogenerated

/*
Copyright 2022 The KubeSphere Authors.

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

// Code generated by controller-gen. DO NOT EDIT.

package v1alpha3

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ApplicationDestination) DeepCopyInto(out *ApplicationDestination) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ApplicationDestination.
func (in *ApplicationDestination) DeepCopy() *ApplicationDestination {
	if in == nil {
		return nil
	}
	out := new(ApplicationDestination)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Argo) DeepCopyInto(out *Argo) {
	*out = *in
	if in.SourceRepos != nil {
		in, out := &in.SourceRepos, &out.SourceRepos
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Destinations != nil {
		in, out := &in.Destinations, &out.Destinations
		*out = make([]ApplicationDestination, len(*in))
		copy(*out, *in)
	}
	if in.Roles != nil {
		in, out := &in.Roles, &out.Roles
		*out = make([]ProjectRole, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.ClusterResourceWhitelist != nil {
		in, out := &in.ClusterResourceWhitelist, &out.ClusterResourceWhitelist
		*out = make([]v1.GroupKind, len(*in))
		copy(*out, *in)
	}
	if in.NamespaceResourceBlacklist != nil {
		in, out := &in.NamespaceResourceBlacklist, &out.NamespaceResourceBlacklist
		*out = make([]v1.GroupKind, len(*in))
		copy(*out, *in)
	}
	if in.OrphanedResources != nil {
		in, out := &in.OrphanedResources, &out.OrphanedResources
		*out = new(OrphanedResourcesMonitorSettings)
		(*in).DeepCopyInto(*out)
	}
	if in.SyncWindows != nil {
		in, out := &in.SyncWindows, &out.SyncWindows
		*out = make(SyncWindows, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				in, out := &(*in)[i], &(*out)[i]
				*out = new(SyncWindow)
				(*in).DeepCopyInto(*out)
			}
		}
	}
	if in.NamespaceResourceWhitelist != nil {
		in, out := &in.NamespaceResourceWhitelist, &out.NamespaceResourceWhitelist
		*out = make([]v1.GroupKind, len(*in))
		copy(*out, *in)
	}
	if in.SignatureKeys != nil {
		in, out := &in.SignatureKeys, &out.SignatureKeys
		*out = make([]SignatureKey, len(*in))
		copy(*out, *in)
	}
	if in.ClusterResourceBlacklist != nil {
		in, out := &in.ClusterResourceBlacklist, &out.ClusterResourceBlacklist
		*out = make([]v1.GroupKind, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Argo.
func (in *Argo) DeepCopy() *Argo {
	if in == nil {
		return nil
	}
	out := new(Argo)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *BitbucketServerSource) DeepCopyInto(out *BitbucketServerSource) {
	*out = *in
	if in.DiscoverPRFromForks != nil {
		in, out := &in.DiscoverPRFromForks, &out.DiscoverPRFromForks
		*out = new(DiscoverPRFromForks)
		**out = **in
	}
	if in.CloneOption != nil {
		in, out := &in.CloneOption, &out.CloneOption
		*out = new(GitCloneOption)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new BitbucketServerSource.
func (in *BitbucketServerSource) DeepCopy() *BitbucketServerSource {
	if in == nil {
		return nil
	}
	out := new(BitbucketServerSource)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Condition) DeepCopyInto(out *Condition) {
	*out = *in
	in.LastProbeTime.DeepCopyInto(&out.LastProbeTime)
	in.LastTransitionTime.DeepCopyInto(&out.LastTransitionTime)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Condition.
func (in *Condition) DeepCopy() *Condition {
	if in == nil {
		return nil
	}
	out := new(Condition)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DevOpsProject) DeepCopyInto(out *DevOpsProject) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DevOpsProject.
func (in *DevOpsProject) DeepCopy() *DevOpsProject {
	if in == nil {
		return nil
	}
	out := new(DevOpsProject)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *DevOpsProject) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DevOpsProjectList) DeepCopyInto(out *DevOpsProjectList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]DevOpsProject, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DevOpsProjectList.
func (in *DevOpsProjectList) DeepCopy() *DevOpsProjectList {
	if in == nil {
		return nil
	}
	out := new(DevOpsProjectList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *DevOpsProjectList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DevOpsProjectSpec) DeepCopyInto(out *DevOpsProjectSpec) {
	*out = *in
	if in.Argo != nil {
		in, out := &in.Argo, &out.Argo
		*out = new(Argo)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DevOpsProjectSpec.
func (in *DevOpsProjectSpec) DeepCopy() *DevOpsProjectSpec {
	if in == nil {
		return nil
	}
	out := new(DevOpsProjectSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DevOpsProjectStatus) DeepCopyInto(out *DevOpsProjectStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DevOpsProjectStatus.
func (in *DevOpsProjectStatus) DeepCopy() *DevOpsProjectStatus {
	if in == nil {
		return nil
	}
	out := new(DevOpsProjectStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DiscarderProperty) DeepCopyInto(out *DiscarderProperty) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DiscarderProperty.
func (in *DiscarderProperty) DeepCopy() *DiscarderProperty {
	if in == nil {
		return nil
	}
	out := new(DiscarderProperty)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DiscoverPRFromForks) DeepCopyInto(out *DiscoverPRFromForks) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DiscoverPRFromForks.
func (in *DiscoverPRFromForks) DeepCopy() *DiscoverPRFromForks {
	if in == nil {
		return nil
	}
	out := new(DiscoverPRFromForks)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GenericVariable) DeepCopyInto(out *GenericVariable) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GenericVariable.
func (in *GenericVariable) DeepCopy() *GenericVariable {
	if in == nil {
		return nil
	}
	out := new(GenericVariable)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GenericWebhook) DeepCopyInto(out *GenericWebhook) {
	*out = *in
	if in.RequestVariables != nil {
		in, out := &in.RequestVariables, &out.RequestVariables
		*out = make([]GenericVariable, len(*in))
		copy(*out, *in)
	}
	if in.HeaderVariables != nil {
		in, out := &in.HeaderVariables, &out.HeaderVariables
		*out = make([]GenericVariable, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GenericWebhook.
func (in *GenericWebhook) DeepCopy() *GenericWebhook {
	if in == nil {
		return nil
	}
	out := new(GenericWebhook)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GitCloneOption) DeepCopyInto(out *GitCloneOption) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GitCloneOption.
func (in *GitCloneOption) DeepCopy() *GitCloneOption {
	if in == nil {
		return nil
	}
	out := new(GitCloneOption)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GitRepository) DeepCopyInto(out *GitRepository) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GitRepository.
func (in *GitRepository) DeepCopy() *GitRepository {
	if in == nil {
		return nil
	}
	out := new(GitRepository)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *GitRepository) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GitRepositoryList) DeepCopyInto(out *GitRepositoryList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]GitRepository, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GitRepositoryList.
func (in *GitRepositoryList) DeepCopy() *GitRepositoryList {
	if in == nil {
		return nil
	}
	out := new(GitRepositoryList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *GitRepositoryList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GitRepositorySpec) DeepCopyInto(out *GitRepositorySpec) {
	*out = *in
	if in.Secret != nil {
		in, out := &in.Secret, &out.Secret
		*out = new(corev1.SecretReference)
		**out = **in
	}
	if in.Webhooks != nil {
		in, out := &in.Webhooks, &out.Webhooks
		*out = make([]corev1.LocalObjectReference, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GitRepositorySpec.
func (in *GitRepositorySpec) DeepCopy() *GitRepositorySpec {
	if in == nil {
		return nil
	}
	out := new(GitRepositorySpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GitRepositoryStatus) DeepCopyInto(out *GitRepositoryStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GitRepositoryStatus.
func (in *GitRepositoryStatus) DeepCopy() *GitRepositoryStatus {
	if in == nil {
		return nil
	}
	out := new(GitRepositoryStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GitSource) DeepCopyInto(out *GitSource) {
	*out = *in
	if in.CloneOption != nil {
		in, out := &in.CloneOption, &out.CloneOption
		*out = new(GitCloneOption)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GitSource.
func (in *GitSource) DeepCopy() *GitSource {
	if in == nil {
		return nil
	}
	out := new(GitSource)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GithubSource) DeepCopyInto(out *GithubSource) {
	*out = *in
	if in.DiscoverPRFromForks != nil {
		in, out := &in.DiscoverPRFromForks, &out.DiscoverPRFromForks
		*out = new(DiscoverPRFromForks)
		**out = **in
	}
	if in.CloneOption != nil {
		in, out := &in.CloneOption, &out.CloneOption
		*out = new(GitCloneOption)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GithubSource.
func (in *GithubSource) DeepCopy() *GithubSource {
	if in == nil {
		return nil
	}
	out := new(GithubSource)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GitlabSource) DeepCopyInto(out *GitlabSource) {
	*out = *in
	if in.DiscoverPRFromForks != nil {
		in, out := &in.DiscoverPRFromForks, &out.DiscoverPRFromForks
		*out = new(DiscoverPRFromForks)
		**out = **in
	}
	if in.CloneOption != nil {
		in, out := &in.CloneOption, &out.CloneOption
		*out = new(GitCloneOption)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GitlabSource.
func (in *GitlabSource) DeepCopy() *GitlabSource {
	if in == nil {
		return nil
	}
	out := new(GitlabSource)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *JWTToken) DeepCopyInto(out *JWTToken) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new JWTToken.
func (in *JWTToken) DeepCopy() *JWTToken {
	if in == nil {
		return nil
	}
	out := new(JWTToken)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MultiBranchJobTrigger) DeepCopyInto(out *MultiBranchJobTrigger) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MultiBranchJobTrigger.
func (in *MultiBranchJobTrigger) DeepCopy() *MultiBranchJobTrigger {
	if in == nil {
		return nil
	}
	out := new(MultiBranchJobTrigger)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MultiBranchPipeline) DeepCopyInto(out *MultiBranchPipeline) {
	*out = *in
	if in.Discarder != nil {
		in, out := &in.Discarder, &out.Discarder
		*out = new(DiscarderProperty)
		**out = **in
	}
	if in.TimerTrigger != nil {
		in, out := &in.TimerTrigger, &out.TimerTrigger
		*out = new(TimerTrigger)
		**out = **in
	}
	if in.GitSource != nil {
		in, out := &in.GitSource, &out.GitSource
		*out = new(GitSource)
		(*in).DeepCopyInto(*out)
	}
	if in.GitHubSource != nil {
		in, out := &in.GitHubSource, &out.GitHubSource
		*out = new(GithubSource)
		(*in).DeepCopyInto(*out)
	}
	if in.GitlabSource != nil {
		in, out := &in.GitlabSource, &out.GitlabSource
		*out = new(GitlabSource)
		(*in).DeepCopyInto(*out)
	}
	if in.SvnSource != nil {
		in, out := &in.SvnSource, &out.SvnSource
		*out = new(SvnSource)
		**out = **in
	}
	if in.SingleSvnSource != nil {
		in, out := &in.SingleSvnSource, &out.SingleSvnSource
		*out = new(SingleSvnSource)
		**out = **in
	}
	if in.BitbucketServerSource != nil {
		in, out := &in.BitbucketServerSource, &out.BitbucketServerSource
		*out = new(BitbucketServerSource)
		(*in).DeepCopyInto(*out)
	}
	if in.MultiBranchJobTrigger != nil {
		in, out := &in.MultiBranchJobTrigger, &out.MultiBranchJobTrigger
		*out = new(MultiBranchJobTrigger)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MultiBranchPipeline.
func (in *MultiBranchPipeline) DeepCopy() *MultiBranchPipeline {
	if in == nil {
		return nil
	}
	out := new(MultiBranchPipeline)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NoScmPipeline) DeepCopyInto(out *NoScmPipeline) {
	*out = *in
	if in.Discarder != nil {
		in, out := &in.Discarder, &out.Discarder
		*out = new(DiscarderProperty)
		**out = **in
	}
	if in.Parameters != nil {
		in, out := &in.Parameters, &out.Parameters
		*out = make([]ParameterDefinition, len(*in))
		copy(*out, *in)
	}
	if in.TimerTrigger != nil {
		in, out := &in.TimerTrigger, &out.TimerTrigger
		*out = new(TimerTrigger)
		**out = **in
	}
	if in.RemoteTrigger != nil {
		in, out := &in.RemoteTrigger, &out.RemoteTrigger
		*out = new(RemoteTrigger)
		**out = **in
	}
	if in.GenericWebhook != nil {
		in, out := &in.GenericWebhook, &out.GenericWebhook
		*out = new(GenericWebhook)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NoScmPipeline.
func (in *NoScmPipeline) DeepCopy() *NoScmPipeline {
	if in == nil {
		return nil
	}
	out := new(NoScmPipeline)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *OrphanedResourceKey) DeepCopyInto(out *OrphanedResourceKey) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new OrphanedResourceKey.
func (in *OrphanedResourceKey) DeepCopy() *OrphanedResourceKey {
	if in == nil {
		return nil
	}
	out := new(OrphanedResourceKey)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *OrphanedResourcesMonitorSettings) DeepCopyInto(out *OrphanedResourcesMonitorSettings) {
	*out = *in
	if in.Warn != nil {
		in, out := &in.Warn, &out.Warn
		*out = new(bool)
		**out = **in
	}
	if in.Ignore != nil {
		in, out := &in.Ignore, &out.Ignore
		*out = make([]OrphanedResourceKey, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new OrphanedResourcesMonitorSettings.
func (in *OrphanedResourcesMonitorSettings) DeepCopy() *OrphanedResourcesMonitorSettings {
	if in == nil {
		return nil
	}
	out := new(OrphanedResourcesMonitorSettings)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Parameter) DeepCopyInto(out *Parameter) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Parameter.
func (in *Parameter) DeepCopy() *Parameter {
	if in == nil {
		return nil
	}
	out := new(Parameter)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ParameterDefinition) DeepCopyInto(out *ParameterDefinition) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ParameterDefinition.
func (in *ParameterDefinition) DeepCopy() *ParameterDefinition {
	if in == nil {
		return nil
	}
	out := new(ParameterDefinition)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Pipeline) DeepCopyInto(out *Pipeline) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Pipeline.
func (in *Pipeline) DeepCopy() *Pipeline {
	if in == nil {
		return nil
	}
	out := new(Pipeline)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Pipeline) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PipelineList) DeepCopyInto(out *PipelineList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Pipeline, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PipelineList.
func (in *PipelineList) DeepCopy() *PipelineList {
	if in == nil {
		return nil
	}
	out := new(PipelineList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *PipelineList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PipelineRun) DeepCopyInto(out *PipelineRun) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PipelineRun.
func (in *PipelineRun) DeepCopy() *PipelineRun {
	if in == nil {
		return nil
	}
	out := new(PipelineRun)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *PipelineRun) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PipelineRunList) DeepCopyInto(out *PipelineRunList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]PipelineRun, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PipelineRunList.
func (in *PipelineRunList) DeepCopy() *PipelineRunList {
	if in == nil {
		return nil
	}
	out := new(PipelineRunList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *PipelineRunList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PipelineRunSpec) DeepCopyInto(out *PipelineRunSpec) {
	*out = *in
	if in.PipelineRef != nil {
		in, out := &in.PipelineRef, &out.PipelineRef
		*out = new(corev1.ObjectReference)
		**out = **in
	}
	if in.PipelineSpec != nil {
		in, out := &in.PipelineSpec, &out.PipelineSpec
		*out = new(PipelineSpec)
		(*in).DeepCopyInto(*out)
	}
	if in.Parameters != nil {
		in, out := &in.Parameters, &out.Parameters
		*out = make([]Parameter, len(*in))
		copy(*out, *in)
	}
	if in.SCM != nil {
		in, out := &in.SCM, &out.SCM
		*out = new(SCM)
		**out = **in
	}
	if in.Action != nil {
		in, out := &in.Action, &out.Action
		*out = new(Action)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PipelineRunSpec.
func (in *PipelineRunSpec) DeepCopy() *PipelineRunSpec {
	if in == nil {
		return nil
	}
	out := new(PipelineRunSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PipelineRunStatus) DeepCopyInto(out *PipelineRunStatus) {
	*out = *in
	if in.StartTime != nil {
		in, out := &in.StartTime, &out.StartTime
		*out = (*in).DeepCopy()
	}
	if in.CompletionTime != nil {
		in, out := &in.CompletionTime, &out.CompletionTime
		*out = (*in).DeepCopy()
	}
	if in.UpdateTime != nil {
		in, out := &in.UpdateTime, &out.UpdateTime
		*out = (*in).DeepCopy()
	}
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]Condition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PipelineRunStatus.
func (in *PipelineRunStatus) DeepCopy() *PipelineRunStatus {
	if in == nil {
		return nil
	}
	out := new(PipelineRunStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PipelineSpec) DeepCopyInto(out *PipelineSpec) {
	*out = *in
	if in.Pipeline != nil {
		in, out := &in.Pipeline, &out.Pipeline
		*out = new(NoScmPipeline)
		(*in).DeepCopyInto(*out)
	}
	if in.MultiBranchPipeline != nil {
		in, out := &in.MultiBranchPipeline, &out.MultiBranchPipeline
		*out = new(MultiBranchPipeline)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PipelineSpec.
func (in *PipelineSpec) DeepCopy() *PipelineSpec {
	if in == nil {
		return nil
	}
	out := new(PipelineSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PipelineStatus) DeepCopyInto(out *PipelineStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PipelineStatus.
func (in *PipelineStatus) DeepCopy() *PipelineStatus {
	if in == nil {
		return nil
	}
	out := new(PipelineStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ProjectRole) DeepCopyInto(out *ProjectRole) {
	*out = *in
	if in.Policies != nil {
		in, out := &in.Policies, &out.Policies
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.JWTTokens != nil {
		in, out := &in.JWTTokens, &out.JWTTokens
		*out = make([]JWTToken, len(*in))
		copy(*out, *in)
	}
	if in.Groups != nil {
		in, out := &in.Groups, &out.Groups
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ProjectRole.
func (in *ProjectRole) DeepCopy() *ProjectRole {
	if in == nil {
		return nil
	}
	out := new(ProjectRole)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RemoteTrigger) DeepCopyInto(out *RemoteTrigger) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RemoteTrigger.
func (in *RemoteTrigger) DeepCopy() *RemoteTrigger {
	if in == nil {
		return nil
	}
	out := new(RemoteTrigger)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SCM) DeepCopyInto(out *SCM) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SCM.
func (in *SCM) DeepCopy() *SCM {
	if in == nil {
		return nil
	}
	out := new(SCM)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SignatureKey) DeepCopyInto(out *SignatureKey) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SignatureKey.
func (in *SignatureKey) DeepCopy() *SignatureKey {
	if in == nil {
		return nil
	}
	out := new(SignatureKey)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SingleSvnSource) DeepCopyInto(out *SingleSvnSource) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SingleSvnSource.
func (in *SingleSvnSource) DeepCopy() *SingleSvnSource {
	if in == nil {
		return nil
	}
	out := new(SingleSvnSource)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SvnSource) DeepCopyInto(out *SvnSource) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SvnSource.
func (in *SvnSource) DeepCopy() *SvnSource {
	if in == nil {
		return nil
	}
	out := new(SvnSource)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SyncWindow) DeepCopyInto(out *SyncWindow) {
	*out = *in
	if in.Applications != nil {
		in, out := &in.Applications, &out.Applications
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Namespaces != nil {
		in, out := &in.Namespaces, &out.Namespaces
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Clusters != nil {
		in, out := &in.Clusters, &out.Clusters
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SyncWindow.
func (in *SyncWindow) DeepCopy() *SyncWindow {
	if in == nil {
		return nil
	}
	out := new(SyncWindow)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in SyncWindows) DeepCopyInto(out *SyncWindows) {
	{
		in := &in
		*out = make(SyncWindows, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				in, out := &(*in)[i], &(*out)[i]
				*out = new(SyncWindow)
				(*in).DeepCopyInto(*out)
			}
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SyncWindows.
func (in SyncWindows) DeepCopy() SyncWindows {
	if in == nil {
		return nil
	}
	out := new(SyncWindows)
	in.DeepCopyInto(out)
	return *out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TimerTrigger) DeepCopyInto(out *TimerTrigger) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TimerTrigger.
func (in *TimerTrigger) DeepCopy() *TimerTrigger {
	if in == nil {
		return nil
	}
	out := new(TimerTrigger)
	in.DeepCopyInto(out)
	return out
}
