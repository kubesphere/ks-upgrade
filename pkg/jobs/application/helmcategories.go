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
	v2 "kubesphere.io/ks-upgrade/v4/api/application/v2"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func (i *upgradeJob) helmCategoriesPre(ctx context.Context) error {
	obj := v1alpha1.HelmCategoryList{}
	key := fmt.Sprintf("%s-%s", jobName, "helmcategories.application.kubesphere.io.v1alpha1")
	err := i.resourceStore.Load(key, &obj)
	if err != nil && errors.Is(err, storage.BackupKeyNotFound) {
		if err := i.clientV3.List(ctx, &obj, &runtimeclient.ListOptions{}); err != nil {
			klog.Errorf("[Application] failed to list helm categories: %v", err)
			return err
		}
		return i.resourceStore.Save(key, &obj)
	}
	return nil
}

func (i *upgradeJob) helmCategoriesAfter(ctx context.Context) error {
	obj := v1alpha1.HelmCategoryList{}
	key := fmt.Sprintf("%s-%s", jobName, "helmcategories.application.kubesphere.io.v1alpha1")
	err := i.resourceStore.Load(key, &obj)
	if err != nil {
		return err
	}
	newObj := v2.CategoryList{}
	for _, oldItem := range obj.Items {
		if oldItem.Name == "ctg-uncategorized" {
			continue
		}
		newItem := v2.Category{
			Spec: v2.CategorySpec{
				Icon: oldItem.Spec.Description,
			},
		}
		newItem.Name = oldItem.Name
		newItem.Annotations = make(map[string]string)
		newItem.Annotations[constants4.DisplayNameAnnotationKey] = oldItem.Spec.Name
		newObj.Items = append(newObj.Items, newItem)
	}
	for _, item := range newObj.Items {
		err = i.clientV4.Create(ctx, &item)
		if k8serrors.IsAlreadyExists(err) {
			continue
		}
		if err != nil {
			klog.Errorf("[Application] failed to create helm category: %v", err)
			return err
		}
	}
	klog.Infof("[application] %d helm categories are migrated", len(newObj.Items))

	return nil
}
