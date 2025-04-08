package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	constants4 "kubesphere.io/ks-upgrade/pkg/constants"
	"kubesphere.io/ks-upgrade/pkg/storage"
	"kubesphere.io/ks-upgrade/v3/api/application/v1alpha1"
	"kubesphere.io/ks-upgrade/v3/api/constants"
	v2 "kubesphere.io/ks-upgrade/v4/api/application/v2"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func (i *upgradeJob) helmApplicationVersionsPre(ctx context.Context) error {
	obj := v1alpha1.HelmApplicationVersionList{}
	key := fmt.Sprintf("%s-%s", jobName, "helmapplicationversions.application.kubesphere.io.v1alpha1")
	err := i.resourceStore.Load(key, &obj)
	if err != nil && errors.Is(err, storage.BackupKeyNotFound) {
		if err := i.clientV3.List(ctx, &obj, &runtimeclient.ListOptions{}); err != nil {
			klog.Errorf("[Application] failed to list helm application versions: %v", err)
			return err
		}
		return i.resourceStore.Save(key, &obj)
	}
	return nil
}

func (i *upgradeJob) helmApplicationVersionsAfter(ctx context.Context) error {
	oldList := v1alpha1.HelmApplicationVersionList{}
	key := fmt.Sprintf("%s-%s", jobName, "helmapplicationversions.application.kubesphere.io.v1alpha1")
	err := i.resourceStore.Load(key, &oldList)
	if err != nil {
		return err
	}

	appList := v2.ApplicationList{}
	err = i.clientV4.List(ctx, &appList)
	if err != nil {
		klog.Errorf("failed to list applications: %v", err)
		return err
	}
	appOwnMap := make(map[string]metav1.OwnerReference)
	for _, app := range appList.Items {
		own := metav1.OwnerReference{
			APIVersion: app.APIVersion,
			Kind:       app.Kind,
			Name:       app.Name,
			UID:        app.UID,
		}
		appOwnMap[app.Name] = own
	}

	newList := v2.ApplicationVersionList{}
	for _, oldItem := range oldList.Items {

		updateTime := oldItem.Spec.Created
		if updateTime == nil {
			updateTime = &metav1.Time{
				Time: oldItem.CreationTimestamp.Time,
			}
		}

		newItem := v2.ApplicationVersion{
			Spec: v2.ApplicationVersionSpec{
				VersionName: oldItem.Spec.Version,
				AppHome:     oldItem.Spec.Home,
				Icon:        oldItem.Spec.Icon,
				Created:     oldItem.Spec.Created,
				AppType:     v2.AppTypeHelm,
				Maintainer: func() (rv []v2.Maintainer) {
					for _, maintainer := range oldItem.Spec.Maintainers {
						rv = append(rv, v2.Maintainer{
							Name:  maintainer.Name,
							Email: maintainer.Email,
							URL:   maintainer.URL,
						})
					}
					return rv
				}(),
			},
			Status: v2.ApplicationVersionStatus{
				State:   oldItem.State(),
				Message: "",
				UserName: func() (rv string) {
					if len(oldItem.Status.Audit) > 0 {
						return oldItem.Status.Audit[0].Operator
					}
					return ""
				}(),
				Updated: updateTime,
			},
		}

		own, ok := appOwnMap[oldItem.GetLabels()[constants.ChartApplicationIdLabelKey]]
		if ok {
			newItem.SetOwnerReferences([]metav1.OwnerReference{own})
		}
		newItem.SetFinalizers([]string{v2.StoreCleanFinalizer})
		newItem.Name = oldItem.Name

		newItem.SetLabels(map[string]string{
			v2.AppIDLabelKey:            oldItem.GetLabels()[constants.ChartApplicationIdLabelKey],
			v2.AppTypeLabelKey:          v2.AppTypeHelm,
			v2.RepoIDLabelKey:           v2.UploadRepoKey,
			constants.WorkspaceLabelKey: oldItem.GetLabels()[constants.WorkspaceLabelKey],
		})
		newItem.SetAnnotations(map[string]string{
			v2.AppMaintainersKey:                oldItem.GetAnnotations()[constants.CreatorAnnotationKey],
			constants4.DisplayNameAnnotationKey: oldItem.Spec.Name,
			constants4.DescriptionAnnotationKey: oldItem.Spec.Description,
		})

		newList.Items = append(newList.Items, newItem)
	}
	for _, item := range newList.Items {
		err = i.clientV4.Create(ctx, &item)
		if k8serrors.IsAlreadyExists(err) {
			continue
		}
		if err != nil {
			klog.Errorf("failed to create application version: %v", err)
			return err
		}
	}

	for _, item := range newList.Items {
		tmp := v2.ApplicationVersion{}
		tmp.Name = item.Name
		tmp.Status = item.Status
		patch, _ := json.Marshal(tmp)
		err = i.clientV4.Status().Patch(ctx, &tmp, runtimeclient.RawPatch(runtimeclient.Merge.Type(), patch))
	}
	klog.Infof("[application] %d helm application versions are migrated", len(newList.Items))

	return nil
}
