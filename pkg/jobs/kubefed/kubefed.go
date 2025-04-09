package kubefed

import (
	"context"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"
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

const (
	jobName          = "kubefed"
	kubefedNamespace = "kube-federation-system"
)

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
	isHost, err := i.coreHelper.IsHostCluster(ctx)
	if err != nil {
		return false, err
	}
	if !isHost {
		i.log.Info("not the host cluster, skip upgrading")
		return false, nil
	}

	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err = i.clientV3.Get(ctx, types.NamespacedName{Name: "kubefedclusters.core.kubefed.io"}, crd); err != nil {
		if errors.IsNotFound(err) {
			i.log.Info("the target is signal cluster mode, skip installing")
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (i *upgradeJob) PreUpgrade(ctx context.Context) error {
	needRun, err := i.needRun(ctx)
	if err != nil {
		return err
	}
	if !needRun {
		return nil
	}

	i.log.Info("deleting unneeded FederatedTypeConfig")
	return i.deleteFederatedTypeConfig(ctx)
}

func (i *upgradeJob) deleteFederatedTypeConfig(ctx context.Context) error {
	// The following resources are no longer managed by kubefed
	for _, name := range []string{
		"configs.notification.kubesphere.io",
		"notificationmanagers.notification.kubesphere.io",
		"receivers.notification.kubesphere.io",
		"routers.notification.kubesphere.io",
		"silences.notification.kubesphere.io",
		"globalrolebindings.iam.kubesphere.io",
		"globalroles.iam.kubesphere.io",
		"groupbindings.iam.kubesphere.io",
		"groups.iam.kubesphere.io",
		"users.iam.kubesphere.io",
		"workspacerolebindings.iam.kubesphere.io",
		"workspaceroles.iam.kubesphere.io",
		"workspaces.tenant.kubesphere.io",
	} {
		federatedTypeConfig := &unstructured.Unstructured{}
		federatedTypeConfig.SetAPIVersion("core.kubefed.io/v1beta1")
		federatedTypeConfig.SetKind("FederatedTypeConfig")
		federatedTypeConfig.SetNamespace(kubefedNamespace)
		federatedTypeConfig.SetName(name)
		if err := i.clientV3.Delete(ctx, federatedTypeConfig); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (i *upgradeJob) PostUpgrade(ctx context.Context) error {
	needRun, err := i.needRun(ctx)
	if err != nil {
		return err
	}
	if !needRun {
		return nil
	}

	// update CRDs, sets the kubesphere.io/resource-served label
	i.log.Info("updating CRDs")
	if err = i.updateCRDs(ctx); err != nil {
		return err
	}

	i.log.Info("updating helm metadata")
	if err = i.setHelmMetaInfo(ctx); err != nil {
		return err
	}

	// create InstallPlan
	i.log.Info("creating InstallPlan")
	if err = i.coreHelper.CreateInstallPlanFromExtensionRef(ctx, i.extensionRef); err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func (i *upgradeJob) setHelmMetaInfo(ctx context.Context) error {
	for _, name := range []string{
		"applications.app.k8s.io",
		"clusterrolebindings.rbac.authorization.k8s.io",
		"clusterroles.rbac.authorization.k8s.io",
		"configmaps",
		"deployments.apps",
		"ingresses.networking.k8s.io",
		"jobs.batch",
		"limitranges",
		"namespaces",
		"persistentvolumeclaims",
		"replicasets.apps",
		"secrets",
		"serviceaccounts",
		"services",
		"statefulsets.apps",
	} {
		federatedTypeConfig := &unstructured.Unstructured{}
		federatedTypeConfig.SetAPIVersion("core.kubefed.io/v1beta1")
		federatedTypeConfig.SetKind("FederatedTypeConfig")
		if err := i.clientV4.Get(ctx, types.NamespacedName{Namespace: kubefedNamespace, Name: name}, federatedTypeConfig); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return err
		}
		federatedTypeConfig.SetLabels(map[string]string{
			"app.kubernetes.io/managed-by": "Helm",
		})
		federatedTypeConfig.SetAnnotations(map[string]string{
			"meta.helm.sh/release-name":      "kubefed",
			"meta.helm.sh/release-namespace": kubefedNamespace,
		})
		if err := i.clientV4.Update(ctx, federatedTypeConfig); err != nil {
			return err
		}
	}
	return nil
}

func (i *upgradeJob) updateCRDs(ctx context.Context) error {
	for _, name := range []string{
		"federatedapplications.types.kubefed.io",
		"federatedclusterrolebindings.types.kubefed.io",
		"federatedclusterroles.types.kubefed.io",
		"federatedconfigmaps.types.kubefed.io",
		"federateddeployments.types.kubefed.io",
		"federatedingresses.types.kubefed.io",
		"federatedjobs.types.kubefed.io",
		"federatedlimitranges.types.kubefed.io",
		"federatednamespaces.types.kubefed.io",
		"federatedpersistentvolumeclaims.types.kubefed.io",
		"federatedreplicasets.types.kubefed.io",
		"federatedsecrets.types.kubefed.io",
		"federatedserviceaccounts.types.kubefed.io",
		"federatedservices.types.kubefed.io",
		"federatedstatefulsets.types.kubefed.io",
	} {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		if err := i.clientV4.Get(ctx, types.NamespacedName{Name: name}, crd); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return err
		}
		if crd.Labels["kubesphere.io/resource-served"] == "true" {
			continue
		}

		if crd.Labels == nil {
			crd.Labels = make(map[string]string)
		}
		crd.Labels["kubesphere.io/resource-served"] = "true"
		if err := i.clientV4.Update(ctx, crd); err != nil {
			return err
		}
	}
	return nil
}
