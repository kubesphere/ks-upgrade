package tower

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"kubesphere.io/ks-upgrade/pkg/executor"
	"kubesphere.io/ks-upgrade/pkg/model"
	"kubesphere.io/ks-upgrade/pkg/model/core"
	"kubesphere.io/ks-upgrade/pkg/model/helper"
	"kubesphere.io/ks-upgrade/pkg/store"
)

func init() {
	runtime.Must(executor.Register(&factory{}))
}

const jobName = "tower"

var _ executor.UpgradeJob = &upgradeJob{}
var _ executor.InjectClientV3 = &upgradeJob{}
var _ executor.InjectClientV4 = &upgradeJob{}
var _ executor.InjectResourceStore = &upgradeJob{}

type factory struct{}

func (f *factory) Name() string {
	return jobName
}

func (f *factory) Create(_ executor.DynamicOptions, extensionRef *model.ExtensionRef) (executor.UpgradeJob, error) {
	return &upgradeJob{
		extensionRef: extensionRef,
		log:          klog.NewKlogr().WithName(jobName),
	}, nil
}

type upgradeJob struct {
	clientV3      runtimeclient.Client
	clientV4      runtimeclient.Client
	resourceStore store.ResourceStore
	coreHelper    core.Helper
	extensionRef  *model.ExtensionRef
	log           klog.Logger
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

func (i *upgradeJob) needRun(ctx context.Context) (bool, error) {
	cc, err := i.coreHelper.GetClusterConfiguration(ctx)
	if err != nil {
		return false, err
	}
	clusterRole, _, err := unstructured.NestedString(cc, "spec", "multicluster", "clusterRole")
	if err != nil {
		return false, err
	}

	if clusterRole == "host" {
		return true, nil
	}
	i.log.Info("not the host cluster, skip upgrading")
	return false, nil
}

func (i *upgradeJob) PreUpgrade(ctx context.Context) error {
	needRun, err := i.needRun(ctx)
	if err != nil {
		return err
	}
	if !needRun {
		return nil
	}

	i.log.Info("updating helm metadata")
	if err = i.setServiceHelmMetaInfo(ctx); err != nil {
		return err
	}
	if err = i.setDeploymentHelmMetaInfo(ctx); err != nil {
		return err
	}

	i.log.Info("updating tower service type to NodePort")
	return i.updateServiceType(ctx)
}

func (i *upgradeJob) setServiceHelmMetaInfo(ctx context.Context) error {
	service := &corev1.Service{}
	if err := i.clientV3.Get(ctx, client.ObjectKey{Namespace: core.KubeSphereNamespace, Name: "tower"}, service); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if service.Labels == nil {
		service.Labels = make(map[string]string)
	}
	service.Labels["app.kubernetes.io/managed-by"] = "Helm"
	if service.Annotations == nil {
		service.Annotations = make(map[string]string)
	}
	service.Annotations["meta.helm.sh/release-name"] = "tower"
	service.Annotations["meta.helm.sh/release-namespace"] = "extension-tower"
	return i.clientV3.Update(ctx, service)
}

func (i *upgradeJob) setDeploymentHelmMetaInfo(ctx context.Context) error {
	deploy := &appsv1.Deployment{}
	if err := i.clientV3.Get(ctx, client.ObjectKey{Namespace: core.KubeSphereNamespace, Name: "tower"}, deploy); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if deploy.Labels == nil {
		deploy.Labels = make(map[string]string)
	}
	deploy.Labels["app.kubernetes.io/managed-by"] = "Helm"
	if deploy.Annotations == nil {
		deploy.Annotations = make(map[string]string)
	}
	deploy.Annotations["meta.helm.sh/release-name"] = "tower"
	deploy.Annotations["meta.helm.sh/release-namespace"] = "extension-tower"
	return i.clientV3.Update(ctx, deploy)
}

func (i *upgradeJob) updateServiceType(ctx context.Context) error {
	service := &corev1.Service{}
	if err := i.clientV3.Get(ctx, client.ObjectKey{Namespace: core.KubeSphereNamespace, Name: "tower"}, service); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if service.Spec.Type == corev1.ServiceTypeNodePort {
		return nil
	}
	if service.Spec.Type == corev1.ServiceTypeLoadBalancer && len(service.Status.LoadBalancer.Ingress) > 0 {
		i.log.Info("The LB is already ready, skip updating")
		return nil
	}

	service.Spec.Type = corev1.ServiceTypeNodePort
	return i.clientV3.Update(ctx, service)
}

func (i *upgradeJob) PostUpgrade(ctx context.Context) error {
	needRun, err := i.needRun(ctx)
	if err != nil {
		return err
	}
	if !needRun {
		return nil
	}
	// create InstallPlan
	i.log.Info("creating InstallPlan")
	if err = i.coreHelper.CreateInstallPlanFromExtensionRef(ctx, i.extensionRef); err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	return nil
}
