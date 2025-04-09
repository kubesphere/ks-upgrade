package iam

import (
	"context"
	"fmt"
	"time"

	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	errors2 "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var kubeFedResource = []string{
	"federatedusers.iam.kubesphere.io",
	"federatedrolebindings.iam.kubesphere.io",
	"federatedroles.iam.kubesphere.io",
	"federatedusers.types.kubefed.io",
	"federatedclusterroles.types.kubefed.io",
	"federatedgroupbindings.types.kubefed.io",
	"federatedglobalroles.types.kubefed.io",
	"federatedworkspaces.types.kubefed.io",
	"federatedglobalrolebindings.types.kubefed.io",
	"federatedworkspacerolebindings.types.kubefed.io",
	"federatedclusterrolebindings.types.kubefed.io",
	"federatedworkspaceroles.types.kubefed.io",
	"federatedgroups.types.kubefed.io",
}

var kubeFedConfigs = []string{
	ResourceClusterRole,
	ResourceUser,
	ResourceGroup,
	ResourceGroupBinding,
	ResourceGlobalRole,
	ResourceWorkspace,
	ResourceWorkspaceRole,
	ResourceGlobalRoleBinding,
	ResourceWorkspaceRoleBinding,
	ResourceClusterRoleBinding,
}

func (j *upgradeJob) scaleDeployment(ctx context.Context, namespace, name string, replicas int32) error {
	kubefed := &appv1.Deployment{}
	err := j.clientV3.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, kubefed)
	if err != nil {
		return fmt.Errorf("scale %s to %d failed: %s", name, replicas, err)
	}

	kubefed.Spec.Replicas = &replicas
	klog.Infof("scaling %s to %d", name, replicas)
	err = j.clientV3.Update(ctx, kubefed)
	if err != nil {
		return fmt.Errorf("scale %s to %d failed: %s", name, replicas, err)
	}

	timeout, cancelFunc := context.WithTimeout(ctx, 30*time.Second)
	defer cancelFunc()
	wait.Until(func() {
		pods := &corev1.PodList{}
		asSelector, err := v1.LabelSelectorAsSelector(kubefed.Spec.Selector)
		if err != nil {
			klog.Errorf("failed to scale down deployment %s", err)
			return
		}
		err = j.clientV3.List(ctx, pods, client.MatchingLabelsSelector{Selector: asSelector})
		if err != nil {
			klog.Errorf("failed to scale down deployment %s", err)
			return
		}
		if len(pods.Items) == int(replicas) {
			cancelFunc()
		}
	}, 1*time.Second, timeout.Done())

	return nil
}

func (j *upgradeJob) RemoveKubeFedResources(ctx context.Context) error {
	err := j.clientV3.Get(ctx, types.NamespacedName{Name: KubeFedNamespace}, &corev1.Namespace{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			klog.Infof("skip handle kubefed resources")
			return nil
		}
		return err
	}

	needScale := true
	kubefedDeployment := &appv1.Deployment{}
	err = j.clientV3.Get(ctx, types.NamespacedName{Namespace: KubeFedNamespace, Name: "kubefed-controller-manager"}, kubefedDeployment)
	if err != nil {
		if apierrors.IsNotFound(err) {
			needScale = false
		} else {
			return err
		}
	}

	if needScale {
		err := j.scaleDeployment(ctx, KubeFedNamespace, "kubefed-controller-manager", 0)
		if err != nil {
			return err
		}
	}

	crd := &apiextensionsv1.CustomResourceDefinition{}
	err = j.clientV3.Get(ctx, types.NamespacedName{Name: "federatedtypeconfigs.core.kubefed.io"}, crd)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if err == nil {
		err = j.deleteKubeFedConfig(ctx, kubeFedConfigs)
		if err != nil {
			return err
		}
	}

	err = j.deleteKubeFedCrds(ctx, kubeFedResource)
	if err != nil {
		return err
	}

	if needScale {
		return j.scaleDeployment(ctx, KubeFedNamespace, "kubefed-controller-manager", 1)
	}

	return nil
}

func (j *upgradeJob) deleteKubeFedCrds(ctx context.Context, crds []string) error {
	errList := []error{}
	for _, name := range crds {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		err := j.clientV3.Get(ctx, types.NamespacedName{Name: name}, crd)
		if err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return err
		}
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   crd.Spec.Group,
			Version: crd.Status.StoredVersions[0],
			Kind:    crd.Spec.Names.ListKind,
		})
		err = j.clientV3.List(ctx, list)
		if err != nil {
			return err
		}
		for _, obj := range list.Items {
			err = deleteObjectWithFinalizer(j.clientV3, ctx, &obj)
			if err != nil {
				return err
			}
		}
		err = j.clientV3.Delete(ctx, crd)
		if err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return err
		}
		j.logger.Info("delete kubefed crd successfully", "crd", name)
	}
	return errors2.NewAggregate(errList)
}

func (j *upgradeJob) deleteKubeFedConfig(ctx context.Context, configNames []string) error {
	errList := []error{}
	for _, cm := range configNames {
		federatedConfig := &unstructured.Unstructured{}
		federatedConfig.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "core.kubefed.io",
			Version: "v1beta1",
			Kind:    "FederatedTypeConfig",
		})
		err := j.clientV3.Get(ctx, types.NamespacedName{Namespace: KubeFedNamespace, Name: cm}, federatedConfig)
		if err != nil {
			if !apierrors.IsNotFound(err) {
				errList = append(errList, err)
			}
			continue
		}
		err = deleteObjectWithFinalizer(j.clientV3, ctx, federatedConfig)
		if err != nil {
			return err
		}
	}
	return errors2.NewAggregate(errList)
}
