package application

import (
	"context"
	"errors"
	"fmt"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
	constants4 "kubesphere.io/ks-upgrade/pkg/constants"
	"kubesphere.io/ks-upgrade/pkg/storage"
	"kubesphere.io/ks-upgrade/v3/api/application/v1alpha1"
	"kubesphere.io/ks-upgrade/v3/api/constants"
	v2 "kubesphere.io/ks-upgrade/v4/api/application/v2"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func (i *upgradeJob) helmRepoPre(ctx context.Context) error {
	obj := v1alpha1.HelmRepoList{}
	key := fmt.Sprintf("%s-%s", jobName, "helmrepos.application.kubesphere.io.v1alpha1")
	err := i.resourceStore.Load(key, &obj)
	if err != nil && errors.Is(err, storage.BackupKeyNotFound) {
		if err := i.clientV3.List(ctx, &obj, &runtimeclient.ListOptions{}); err != nil {
			klog.Errorf("[Application] failed to list helm repos: %v", err)
			return err
		}
		return i.resourceStore.Save(key, &obj)
	}
	return nil
}

func (i *upgradeJob) helmRepoAfter(ctx context.Context) error {
	oldList := v1alpha1.HelmRepoList{}
	key := fmt.Sprintf("%s-%s", jobName, "helmrepos.application.kubesphere.io.v1alpha1")
	err := i.resourceStore.Load(key, &oldList)
	if err != nil {
		return err
	}

	newList := v2.RepoList{}
	for _, oldItem := range oldList.Items {
		newItem := v2.Repo{
			Spec: v2.RepoSpec{
				Url: oldItem.Spec.Url,
				Credential: v2.RepoCredential{
					Username:              oldItem.Spec.Credential.Username,
					Password:              oldItem.Spec.Credential.Password,
					CertFile:              oldItem.Spec.Credential.CertFile,
					KeyFile:               oldItem.Spec.Credential.KeyFile,
					CAFile:                oldItem.Spec.Credential.CAFile,
					InsecureSkipTLSVerify: oldItem.Spec.Credential.InsecureSkipTLSVerify,
				},
				Description: oldItem.Spec.Description,
				SyncPeriod:  oldItem.Spec.SyncPeriod,
			},
		}
		labels := make(map[string]string)
		if oldItem.GetLabels()[constants.WorkspaceLabelKey] != "" {
			labels[constants.WorkspaceLabelKey] = oldItem.GetLabels()[constants.WorkspaceLabelKey]
		}
		newItem.SetLabels(labels)
		ant := make(map[string]string)
		ant[constants4.DisplayNameAnnotationKey] = oldItem.Spec.Name
		newItem.SetAnnotations(ant)
		newItem.Name = oldItem.Name
		newList.Items = append(newList.Items, newItem)
	}
	for _, item := range newList.Items {
		err = i.clientV4.Create(ctx, &item)
		if k8serrors.IsAlreadyExists(err) {
			continue
		}
		if err != nil {
			klog.Errorf("[Application] failed to create repo: %v", err)
			return err
		}
	}
	klog.Infof("[Application] %d repos are migrated", len(newList.Items))

	return nil
}
