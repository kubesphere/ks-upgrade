package iam

import (
	"context"
	"encoding/json"
	"errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/klog/v2"
	"kubesphere.io/ks-upgrade/pkg/storage"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func objectMigration(v4c client.Client, ctx context.Context, upgraded client.Object) error {
	copyObject := upgraded.DeepCopyObject().(client.Object)
	err := v4c.Get(ctx, types.NamespacedName{Namespace: upgraded.GetNamespace(), Name: upgraded.GetName()}, copyObject)
	if err != nil {
		if apierrors.IsNotFound(err) {
			err := v4c.Create(ctx, upgraded)
			if err != nil && (!apierrors.IsNotFound(err) || !isNamespaceTerminating(err)) {
				return err
			}
			return nil
		}
		return err
	}
	klog.Infof("updating %s/%s", upgraded.GetNamespace(), upgraded.GetName())
	return v4c.Update(ctx, upgraded)
}

func updateCRDStoreVersion(c client.Client, ctx context.Context, resource, newVersion string) error {
	if newVersion == "" {
		return nil
	}
	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := c.Get(ctx, client.ObjectKey{Name: resource}, crd); err != nil {
		klog.Errorf("failed to get user crd: %v", err)
		return err
	}

	crd.Status.StoredVersions = []string{newVersion}
	klog.Infof("update crd \"%s\" version to \"%s\"", resource, newVersion)
	if err := c.Status().Update(ctx, crd); err != nil {
		klog.Errorf("failed to update user crd: %v", err)
		return err
	}
	return nil
}

func (j *upgradeJob) RemoveLegacyCRDVersions(ctx context.Context, resource, newVersion, oldVersion string) error {
	if newVersion == "" {
		return nil
	}
	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := j.clientV4.Get(ctx, client.ObjectKey{Name: resource}, crd); err != nil {
		klog.Errorf("failed to get user crd: %v", err)
		return err
	}
	for i := 0; i < len(crd.Spec.Versions); i++ {
		version := crd.Spec.Versions[i]
		if version.Name == oldVersion {
			crd.Spec.Versions = append(crd.Spec.Versions[:i], crd.Spec.Versions[i+1:]...)
			break
		}
	}
	if err := j.clientV4.Update(ctx, crd); err != nil {
		klog.Errorf("failed to update user crd: %v", err)
		return err
	}
	klog.Infof("upgrade crd \"%s\" version \"%s\" to \"%s\" successfully", resource, oldVersion, newVersion)
	return nil
}

type UpgradeStatus struct {
	Status string    `json:"status"`
	Time   time.Time `json:"time"`
}

const UpgradeStatusSuccess = "success"

func (j *upgradeJob) GetUpgradeStatus() (*UpgradeStatus, error) {
	raw, err := j.resourceStore.LoadRaw("iam-upgrade-status")
	if err != nil {
		if errors.Is(err, storage.BackupKeyNotFound) {
			return &UpgradeStatus{}, nil
		}
		return nil, err
	}
	status := &UpgradeStatus{}
	err = json.Unmarshal(raw, status)
	if err != nil {
		return nil, err
	}
	return status, nil
}

func (j *upgradeJob) SetUpgradeStatus(status string) error {
	upgradeStatus := &UpgradeStatus{
		Status: status,
		Time:   time.Now(),
	}
	marshal, _ := json.Marshal(upgradeStatus)
	return j.resourceStore.SaveRaw("iam-upgrade-status", marshal)
}

func deleteObjectWithFinalizer(c client.Client, ctx context.Context, object client.Object) error {
	remainingObject := object.DeepCopyObject().(client.Object)
	remainingObject.SetFinalizers([]string{})
	klog.Infof("removing finalizer from %s/%s", object.GetNamespace(), object.GetName())
	err := c.Update(ctx, remainingObject)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		return nil
	}
	klog.Infof("deleting %s/%s", object.GetNamespace(), object.GetName())
	err = c.Delete(ctx, object)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}
