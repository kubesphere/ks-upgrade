package application

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/klog/v2"
	constants4 "kubesphere.io/ks-upgrade/pkg/constants"
	"kubesphere.io/ks-upgrade/pkg/storage"
	"kubesphere.io/ks-upgrade/v3/api/application/v1alpha1"
	"kubesphere.io/ks-upgrade/v3/api/constants"
	v2 "kubesphere.io/ks-upgrade/v4/api/application/v2"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

func (i *upgradeJob) helmReleasesPre(ctx context.Context) error {
	obj := v1alpha1.HelmReleaseList{}
	key := fmt.Sprintf("%s-%s", jobName, "helmreleases.application.kubesphere.io.v1alpha1")
	err := i.resourceStore.Load(key, &obj)
	if err != nil && errors.Is(err, storage.BackupKeyNotFound) {
		if err := i.clientV3.List(ctx, &obj, &runtimeclient.ListOptions{}); err != nil {
			klog.Errorf("[Application] failed to list helm releases: %v", err)
			return err
		}
		return i.resourceStore.Save(key, &obj)
	}
	return nil
}

func (i *upgradeJob) helmReleasesAfter(ctx context.Context) error {
	oldList := v1alpha1.HelmReleaseList{}
	key := fmt.Sprintf("%s-%s", jobName, "helmreleases.application.kubesphere.io.v1alpha1")
	err := i.resourceStore.Load(key, &oldList)
	if err != nil {
		return err
	}

	newList := v2.ApplicationReleaseList{}
	for _, oldItem := range oldList.Items {
		newItem := v2.ApplicationRelease{
			Spec: v2.ApplicationReleaseSpec{
				AppID:        oldItem.Spec.ApplicationId,
				AppVersionID: oldItem.Spec.ApplicationVersionId,
				Values:       oldItem.Spec.Values,
				AppType:      v2.AppTypeHelm,
			},
			Status: v2.ApplicationReleaseStatus{
				State:      oldItem.Status.State,
				Message:    oldItem.Status.Message,
				LastUpdate: oldItem.Status.LastUpdate,
			},
		}

		newItem.Name = oldItem.Spec.Name
		var clusterName string
		if oldItem.GetLabels()[constants.ClusterNameLabelKey] == "" {
			clusterName = i.hostClusterName
		} else {
			clusterName = oldItem.GetLabels()[constants.ClusterNameLabelKey]
		}
		if clusterName == "" {
			clusterName = "host"
		}
		klog.Infof("[Application] helm release %s for cluster %s", oldItem.Name, clusterName)

		newItem.SetLabels(map[string]string{
			v2.AppIDLabelKey:              oldItem.GetLabels()[constants.ChartApplicationIdLabelKey],
			v2.AppVersionIDLabelKey:       oldItem.GetLabels()[constants.ChartApplicationVersionIdLabelKey],
			constants.ClusterNameLabelKey: clusterName,
			constants.WorkspaceLabelKey:   oldItem.GetLabels()[constants.WorkspaceLabelKey],
			constants.NamespaceLabelKey:   oldItem.GetLabels()[constants.NamespaceLabelKey],
		})
		newItem.SetAnnotations(map[string]string{
			v2.AppOriginalNameLabelKey:          oldItem.Spec.ChartName,
			constants4.DisplayNameAnnotationKey: oldItem.Spec.Name,
			constants4.DescriptionAnnotationKey: oldItem.Spec.Description,
			constants4.CreatorAnnotationKey:     oldItem.GetAnnotations()[constants.CreatorAnnotationKey],
		})
		newItem.SetFinalizers([]string{"helmrelease.application.kubesphere.io"})

		if strings.HasPrefix(oldItem.Spec.RepoId, "repo-") && oldItem.Spec.RepoId != "repo-helm" {
			shortName := GenerateShortNameMD5Hash(oldItem.Spec.ChartName)
			ver := FormatVersion(oldItem.Spec.ChartVersion)
			appID := fmt.Sprintf("%s-%s", oldItem.Spec.RepoId, shortName)
			appvID := fmt.Sprintf("%s-%s-%s", oldItem.Spec.RepoId, shortName, ver)
			newItem.Spec.AppID = appID
			newItem.Spec.AppVersionID = appvID
			newItem.Labels[v2.AppIDLabelKey] = appID
			newItem.Labels[v2.AppVersionIDLabelKey] = appvID
		}
		newItem.Status.SpecHash = newItem.HashSpec()
		newList.Items = append(newList.Items, newItem)
	}
	for _, item := range newList.Items {
		err = i.clientV4.Create(ctx, &item)
		if k8serrors.IsAlreadyExists(err) {
			continue
		}
		if err != nil {
			klog.Errorf("[Application] failed to create release: %v", err)
			return err
		}
	}
	klog.Infof("[Application] %d helm releases are migrated", len(newList.Items))

	return nil
}

func GenerateShortNameMD5Hash(input string) string {
	input = strings.ToLower(input)
	errs := validation.IsDNS1123Subdomain(input)
	if len(input) > 14 || len(errs) != 0 {
		hash := md5.New()
		hash.Write([]byte(input))
		hashInBytes := hash.Sum(nil)
		hashString := hex.EncodeToString(hashInBytes)
		return hashString[:10]
	}
	return input
}

func FormatVersion(input string) string {
	if len(validation.IsDNS1123Subdomain(input)) == 0 {
		return input
	}
	hash := sha1.Sum([]byte(input))
	formattedVersion := hex.EncodeToString(hash[:])[:12]
	klog.Warningf("Version: %s does not meet the Kubernetes naming standard, replacing with SHA-1 hash: %s", input, formattedVersion)
	return formattedVersion
}
