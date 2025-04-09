package storageutils

import (
	"context"
	"errors"
	"fmt"
	"time"

	"helm.sh/helm/v3/pkg/storage/driver"
	v1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	apiext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"
	"kubesphere.io/ks-upgrade/pkg/executor"
	"kubesphere.io/ks-upgrade/pkg/helm"
	"kubesphere.io/ks-upgrade/pkg/model"
	"kubesphere.io/ks-upgrade/pkg/model/core"
	"kubesphere.io/ks-upgrade/pkg/model/helper"
	"kubesphere.io/ks-upgrade/pkg/storage"
	"kubesphere.io/ks-upgrade/pkg/store"
	v4clusterv1alpha1 "kubesphere.io/ks-upgrade/v4/api/cluster/v1alpha1"
	"kubesphere.io/ks-upgrade/v4/api/core/v1alpha1"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func init() {
	runtime.Must(executor.Register(&factory{}))
}

const (
	jobName               = "storage-utils"
	jobDone               = jobName + "-done"
	vsGVR                 = "volumesnapshots.snapshot.storage.k8s.io"
	vscontentGVR          = "volumesnapshotcontents.snapshot.storage.k8s.io"
	vsclassGVR            = "volumesnapshotclasses.snapshot.storage.k8s.io"
	accessorGVR           = "accessors.storage.kubesphere.io"
	webhookName           = "storageclass-accessor.storage.kubesphere.io"
	labelKubesphereServed = "kubesphere.io/resource-served"
	optionRerun           = "rerun"
	oldNs                 = "kube-system"
	stsName               = "snapshot-controller"
	helmReleaseName       = "snapshot-controller"
	oldStsKey             = "old-" + stsName
)

var _ executor.UpgradeJob = &upgradeJob{}
var _ executor.InjectClientV3 = &upgradeJob{}
var _ executor.InjectClientV4 = &upgradeJob{}
var _ executor.InjectResourceStore = &upgradeJob{}
var _ executor.InjectModelHelperFactory = &upgradeJob{}
var _ executor.InjectHelmClient = &upgradeJob{}

type factory struct {
}

func (f *factory) Name() string {
	return jobName
}

func (f *factory) Create(options executor.DynamicOptions, extensionRef *model.ExtensionRef) (executor.UpgradeJob, error) {
	return &upgradeJob{
		options:      options,
		extensionRef: extensionRef,
	}, nil
}

type upgradeJob struct {
	clientV3      runtimeclient.Client
	clientV4      runtimeclient.Client
	coreHelper    core.Helper
	resourceStore store.ResourceStore
	options       executor.DynamicOptions
	extensionRef  *model.ExtensionRef
	helmClient    helm.HelmClient
}

func (i *upgradeJob) InjectClientV3(client runtimeclient.Client) {
	i.clientV3 = client
}

func (i *upgradeJob) InjectClientV4(client runtimeclient.Client) {
	i.clientV4 = client
}

func (i *upgradeJob) InjectModelHelperFactory(factory helper.ModelHelperFactory) {
	i.coreHelper = factory.CoreHelper()
}

func (i *upgradeJob) InjectResourceStore(store store.ResourceStore) {
	i.resourceStore = store
}

func (i *upgradeJob) InjectHelmClient(client helm.HelmClient) {
	i.helmClient = client
}

func (i *upgradeJob) PreUpgrade(ctx context.Context) error {
	var err error
	sts := &appsv1.StatefulSet{}
	err = i.clientV3.Get(ctx, types.NamespacedName{
		Namespace: oldNs,
		Name:      stsName,
	}, sts)
	if err == nil {
		klog.Infof("statefulset %s detected in namespace %s", sts.Name, sts.Namespace)
		klog.Infof("save data key: %s value: %+v", oldStsKey, sts)
		err = i.resourceStore.Save(oldStsKey, sts)
		if err != nil {
			return err
		}
	} else if !k8serrors.IsNotFound(err) {
		return err
	}
	return nil
}

func (i *upgradeJob) PostUpgrade(ctx context.Context) error {
	var err error

	if i.extensionRef == nil {
		return fmt.Errorf("extensionRef is nil")
	}
	klog.Infof("extensionRef: %+v", i.extensionRef)

	// check if job needs re-run
	if i.options != nil {
		value := i.options[optionRerun]
		if fmt.Sprint(value) == "true" {
			klog.Infof("delete data for key %s", jobDone)
			err = i.resourceStore.Delete(jobDone)
			if err != nil && !errors.Is(err, storage.BackupKeyNotFound) {
				return err
			}
		}
	}

	// check if job already done
	var date []byte
	date, err = i.resourceStore.LoadRaw(jobDone)
	if err != nil && !errors.Is(err, storage.BackupKeyNotFound) {
		return err
	}
	if string(date) != "" {
		klog.Infof("job %s already done at %s", jobName, date)
		return nil
	}

	// add label to crd if crd exists
	klog.Info("update label of snapshot crds")
	err = i.updateCrds(ctx)
	if err != nil {
		return err
	}

	// uninstall old snapshot-controller helm release
	_, err = i.helmClient.Status(oldNs, helmReleaseName)
	if err == nil {
		klog.Infof("uninstall old helm release name: %s namespace: %s", helmReleaseName, oldNs)
		_, err = i.helmClient.Uninstall(oldNs, helmReleaseName)
		if err != nil {
			return err
		}
	} else if !errors.Is(err, driver.ErrReleaseNotFound) {
		return err
	}

	// delete old storageclass-accessor webhook
	webhook := &v1.ValidatingWebhookConfiguration{
		ObjectMeta: v12.ObjectMeta{
			Name: webhookName,
		},
	}
	klog.Infof("delete ValidatingWebhookConfiguration %s", webhookName)
	err = i.clientV4.Delete(ctx, webhook)
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	var isHostCluster bool
	isHostCluster, err = i.coreHelper.IsHostCluster(ctx)
	if err != nil {
		return err
	}
	if isHostCluster {
		// compute ClusterScheduling if not set
		if i.extensionRef.ClusterScheduling == nil {
			i.extensionRef.ClusterScheduling = &v1alpha1.ClusterScheduling{}
		}
		if i.extensionRef.ClusterScheduling.Placement == nil {
			i.extensionRef.ClusterScheduling.Placement = &v1alpha1.Placement{}
		}
		if len(i.extensionRef.ClusterScheduling.Placement.Clusters) == 0 && i.extensionRef.ClusterScheduling.Placement.ClusterSelector == nil {
			klog.Infof("compute clusters placement since it is not specified")
			var clusters []string
			var clusterList v4clusterv1alpha1.ClusterList
			err = i.coreHelper.ListCluster(ctx, &clusterList)
			if err != nil {
				return err
			}
			for _, cluster := range clusterList.Items {
				clusters = append(clusters, cluster.Name)
			}
			i.extensionRef.ClusterScheduling.Placement.Clusters = clusters
		}

		// create extension install plan
		// TODO(stone): inherit pod configuration of old release/sts (replicaCount, nodeSelector, affinity etc)
		klog.Infof("create install plan for extension %s", i.extensionRef.Name)
		err = i.coreHelper.CreateInstallPlanFromExtensionRef(ctx, i.extensionRef)
		if err != nil && !k8serrors.IsAlreadyExists(err) {
			return err
		}
	}

	// save job done time
	date = []byte(time.Now().UTC().String())
	klog.Infof("save data key: %s value: %s", jobDone, date)
	err = i.resourceStore.SaveRaw(jobDone, date)
	return err
}

func (i *upgradeJob) updateCrds(ctx context.Context) error {
	var err error
	gvrs := []string{vsGVR, vscontentGVR, vsclassGVR, accessorGVR}
	for _, gvr := range gvrs {
		crd := &apiext.CustomResourceDefinition{}
		err = i.clientV4.Get(ctx, types.NamespacedName{Name: gvr}, crd)
		if err == nil {
			if crd.Labels == nil {
				crd.Labels = make(map[string]string)
			}
			served := crd.Labels[labelKubesphereServed]
			if served != "true" {
				crd.Labels[labelKubesphereServed] = "true"
				klog.Infof("update label of crd %s", gvr)
				err = i.clientV4.Update(ctx, crd)
				if err != nil {
					return err
				}
			}
		} else if !k8serrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}
