package application

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/tidwall/gjson"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"
	"kubesphere.io/ks-upgrade/pkg/model/helper"
	"kubesphere.io/ks-upgrade/v3/api/constants"
	v2 "kubesphere.io/ks-upgrade/v4/api/application/v2"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"strings"

	"kubesphere.io/ks-upgrade/pkg/executor"
	"kubesphere.io/ks-upgrade/pkg/model"
	"kubesphere.io/ks-upgrade/pkg/model/core"
	"kubesphere.io/ks-upgrade/pkg/store"
)

func init() {
	runtime.Must(executor.Register(&factory{}))
}

const jobName = "application"

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
		s3EndPoint:   "minio.kubesphere-system.svc:9000",
	}, nil
}

type upgradeJob struct {
	clientV3        runtimeclient.Client
	clientV4        runtimeclient.Client
	resourceStore   store.ResourceStore
	coreHelper      core.Helper
	extensionRef    *model.ExtensionRef
	s3EndPoint      string
	hostClusterName string
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
	isHost, err := i.coreHelper.IsHostCluster(ctx)
	if err != nil {
		klog.Errorf("[Application] check host cluster err %s", err)
		return err
	}
	if !isHost {
		klog.Infof("[Application] not host cluster, skip")
		return nil
	}

	enabled, err := i.ApplicationEnabled(ctx)
	if err != nil {
		klog.Errorf("[Application] check ApplicationEnabled err %s", err)
		return err
	}
	if !enabled {
		klog.Infof("[Application] ApplicationEnabled is false, skip")
		return nil
	}

	funcList := []funcMap{
		{Name: "helmCategoriesPre", Func: i.helmCategoriesPre},
		{Name: "helmApplicationsPre", Func: i.helmApplicationsPre},
		{Name: "helmApplicationVersionsPre", Func: i.helmApplicationVersionsPre},
		{Name: "helmRepoPre", Func: i.helmRepoPre},
		{Name: "helmReleasesPre", Func: i.helmReleasesPre},
		{Name: "s3Pre", Func: i.s3Pre},
	}
	for _, ins := range funcList {
		err := ins.Func(ctx)
		if err != nil {
			klog.Errorf("[Application] %s %s", ins.Name, err)
			return err
		}
		klog.Infof("[Application] %s success", ins.Name)
	}

	return nil
}

type funcMap struct {
	Name string
	Func func(ctx context.Context) error
}

func (i *upgradeJob) ApplicationEnabled(ctx context.Context) (bool, error) {
	configuration, err := i.coreHelper.GetClusterConfiguration(ctx)
	if err != nil {
		klog.Errorf("[Application] get cluster configuration err %s", err)
		return false, err
	}
	marshal, err := json.Marshal(configuration)
	if err != nil {
		klog.Errorf("[Application] marshal cluster configuration err %s", err)
		return false, err
	}
	enabled := gjson.GetBytes(marshal, "spec.openpitrix.store.enabled").Bool()
	klog.Infof("[Application] ApplicationEnabled is %t", enabled)

	i.hostClusterName = gjson.GetBytes(marshal, "spec.multicluster.hostClusterName").String()
	klog.Infof("[Application] hostClusterName is %s", i.hostClusterName)

	return enabled, nil
}

func (i *upgradeJob) PostUpgrade(ctx context.Context) error {
	isHost, err := i.coreHelper.IsHostCluster(ctx)
	if err != nil {
		klog.Errorf("[Application] check host cluster err %s", err)
		return err
	}
	if !isHost {
		klog.Infof("[Application] not host cluster, skip")
		return nil
	}
	enabled, err := i.ApplicationEnabled(ctx)
	if err != nil {
		klog.Errorf("[Application] check ApplicationEnabled err %s", err)
		return err
	}
	if !enabled {
		klog.Infof("[Application] ApplicationEnabled is false, skip")
		return nil
	}

	ns := corev1.Namespace{}
	ns.Name = v2.ApplicationNamespace
	ns.Labels = map[string]string{
		constants.WorkspaceLabelKey: "system-workspace",
	}
	err = i.clientV4.Create(ctx, &ns)
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		klog.Errorf("[Application] create namespace %s", err)
		return err
	}
	klog.Infof("[Application] create namespace success")

	funcList := []funcMap{
		//keep this order
		{Name: "helmCategoriesAfter", Func: i.helmCategoriesAfter},
		{Name: "helmApplicationsAfter", Func: i.helmApplicationsAfter},
		{Name: "helmApplicationVersionsAfter", Func: i.helmApplicationVersionsAfter},
		{Name: "helmRepoAfter", Func: i.helmRepoAfter},
		{Name: "helmReleasesAfter", Func: i.helmReleasesAfter},
		{Name: "s3After", Func: i.s3After},
	}
	for _, ins := range funcList {
		err = ins.Func(ctx)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				klog.Warningf("[Application] %s: %s", ins.Name, err)
				continue
			}
			klog.Errorf("[Application] %s %s", ins.Name, err)
			return err
		}
		klog.Infof("[Application] %s success", ins.Name)
	}

	if i.extensionRef == nil {
		klog.Errorf("[Application] Application extensionRef is nil, exit")
		return errors.New("extensionRef Application is nil")
	}

	if err = i.coreHelper.CreateInstallPlanFromExtensionRef(ctx, i.extensionRef); err != nil {
		if k8serrors.IsAlreadyExists(err) {
			klog.Infof("[Application] CreateInstallPlan already exists")
			err = nil
		}
		if err != nil {
			klog.Infof("[Application] CreateInstallPlan err %s", err)
			return err
		}
	}
	klog.Infof("[Application] CreateInstallPlan success")
	klog.Infof("[Application] upgrade success")
	return nil
}
