package installplan_create

import (
	"context"
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"kubesphere.io/ks-upgrade/pkg/executor"
	"kubesphere.io/ks-upgrade/pkg/model"
	"kubesphere.io/ks-upgrade/pkg/model/core"
	"kubesphere.io/ks-upgrade/pkg/model/helper"
	"kubesphere.io/ks-upgrade/pkg/storage"
	"kubesphere.io/ks-upgrade/pkg/store"
	iamv1alpha2 "kubesphere.io/ks-upgrade/v3/api/iam/v1alpha2"
)

func init() {
	runtime.Must(executor.Register(&factory{}))
}

const jobName = "sample"

var _ executor.UpgradeJob = &upgradeJob{}
var _ executor.InjectClientV3 = &upgradeJob{}
var _ executor.InjectClientV4 = &upgradeJob{}
var _ executor.InjectResourceStore = &upgradeJob{}

type factory struct {
}

func (f *factory) Name() string {
	return jobName
}

func (f *factory) Create(_ executor.DynamicOptions, extensionRef *model.ExtensionRef) (executor.UpgradeJob, error) {
	return &upgradeJob{
		extensionRef: extensionRef,
	}, nil
}

type upgradeJob struct {
	clientV3      runtimeclient.Client
	clientV4      runtimeclient.Client
	resourceStore store.ResourceStore
	coreHelper    core.Helper
	extensionRef  *model.ExtensionRef
}

var _ executor.EnabledCondition = &upgradeJob{}

func (i *upgradeJob) IsEnabled(ctx context.Context) (bool, error) {
	enabled, err := i.coreHelper.PluginEnabled(ctx, nil, []string{"spec", "plugin", "enabled"}...)
	if err != nil {
		klog.Errorf("check plugin enabled error: %+v", err)
		return false, err
	}
	return enabled, nil
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

func (i *upgradeJob) PreUpgrade(ctx context.Context) error {
	obj := iamv1alpha2.UserList{}
	key := fmt.Sprintf("%s-%s", jobName, "users.iam.kubesphere.io.v1alpha2")
	err := i.resourceStore.Load(key, &obj)
	if err != nil && errors.Is(err, storage.BackupKeyNotFound) {
		if err := i.clientV4.List(ctx, &obj, &runtimeclient.ListOptions{}); err != nil {
			return err
		}
		return i.resourceStore.Save(key, &obj)
	}
	return nil
}

func (i *upgradeJob) PostUpgrade(ctx context.Context) error {
	obj := iamv1alpha2.UserList{}
	key := fmt.Sprintf("%s-%s", jobName, "users.iam.kubesphere.io.v1alpha2")
	err := i.resourceStore.Load(key, &obj)
	if err != nil {
		return err
	}
	for _, item := range obj.Items {
		klog.Infof("restore: %v", item)
	}

	// create InstallPlan
	if i.extensionRef != nil {
		if err := i.coreHelper.CreateInstallPlanFromExtensionRef(ctx, i.extensionRef); err != nil {
			return err
		}
	}
	return nil
}
