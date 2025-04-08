package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
	constants4 "kubesphere.io/ks-upgrade/pkg/constants"
	"kubesphere.io/ks-upgrade/pkg/storage"
	"kubesphere.io/ks-upgrade/v3/api/application/v1alpha1"
	"kubesphere.io/ks-upgrade/v3/api/constants"
	v2 "kubesphere.io/ks-upgrade/v4/api/application/v2"
	"regexp"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

func (i *upgradeJob) helmApplicationsPre(ctx context.Context) error {
	obj := v1alpha1.HelmApplicationList{}
	key := fmt.Sprintf("%s-%s", jobName, "helmapplications.application.kubesphere.io.v1alpha1")
	err := i.resourceStore.Load(key, &obj)
	if err != nil && errors.Is(err, storage.BackupKeyNotFound) {
		if err := i.clientV3.List(ctx, &obj, &runtimeclient.ListOptions{}); err != nil {
			klog.Errorf("[Application] failed to list helm applications: %v", err)
			return err
		}
		return i.resourceStore.Save(key, &obj)
	}
	return nil
}

func (i *upgradeJob) helmApplicationsAfter(ctx context.Context) error {
	oldList := v1alpha1.HelmApplicationList{}
	key := fmt.Sprintf("%s-%s", jobName, "helmapplications.application.kubesphere.io.v1alpha1")
	err := i.resourceStore.Load(key, &oldList)
	if err != nil {
		return err
	}
	newList := v2.ApplicationList{}
	for _, oldItem := range oldList.Items {
		if strings.HasSuffix(oldItem.Name, v1alpha1.HelmApplicationAppStoreSuffix) {
			continue
		}
		newItem := v2.Application{
			Spec: v2.ApplicationSpec{
				AppHome: oldItem.Spec.AppHome,
				AppType: v2.AppTypeHelm,
				Icon:    oldItem.Spec.Icon,
			},
			Status: v2.ApplicationStatus{
				State:      oldItem.Status.State,
				UpdateTime: oldItem.Status.UpdateTime,
			},
		}
		newItem.Name = oldItem.Name
		var workspace string
		if oldItem.GetLabels()[constants.WorkspaceLabelKey] == "system-workspace" {
			workspace = ""
		} else {
			workspace = oldItem.GetLabels()[constants.WorkspaceLabelKey]
		}
		newItem.SetLabels(map[string]string{
			constants.WorkspaceLabelKey: workspace,
			v2.AppCategoryNameKey:       oldItem.GetLabels()[constants.CategoryIdLabelKey],
			v2.AppTypeLabelKey:          v2.AppTypeHelm,
			v2.RepoIDLabelKey:           v2.UploadRepoKey,
		})

		re := regexp.MustCompile(`(\d+\.\d+\.\d+)`)
		matches := re.FindStringSubmatch(oldItem.Status.LatestVersion)
		latestAppVersion := "0.0.0"
		if len(matches) > 1 {
			latestAppVersion = matches[1]
		}

		newItem.SetAnnotations(map[string]string{
			v2.AppMaintainersKey:                oldItem.GetAnnotations()[constants.CreatorAnnotationKey],
			v2.LatestAppVersionKey:              latestAppVersion,
			constants4.DisplayNameAnnotationKey: oldItem.Spec.Name,
			constants4.DescriptionAnnotationKey: oldItem.Spec.Description,
			v2.AppOriginalNameLabelKey:          oldItem.Spec.Name,
		})

		newList.Items = append(newList.Items, newItem)
	}

	for _, item := range newList.Items {
		err = i.clientV4.Create(ctx, &item)
		if k8serrors.IsAlreadyExists(err) {
			continue
		}
		if err != nil {
			klog.Errorf("failed to create helm application: %v", err)
			return err
		}
	}

	for _, item := range newList.Items {
		tmp := v2.Application{}
		tmp.Name = item.Name
		tmp.Status = item.Status
		patch, _ := json.Marshal(tmp)
		err = i.clientV4.Status().Patch(ctx, &tmp, runtimeclient.RawPatch(runtimeclient.Merge.Type(), patch))
	}
	klog.Infof("[application] %d helm applications are migrated", len(newList.Items))

	return nil
}
