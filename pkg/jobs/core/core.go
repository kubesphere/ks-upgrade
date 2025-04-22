package core

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/exp/slices"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	crdv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metaerrors "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	errors2 "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"kubesphere.io/ks-upgrade/pkg/constants"
	"kubesphere.io/ks-upgrade/pkg/executor"
	"kubesphere.io/ks-upgrade/pkg/model"
	"kubesphere.io/ks-upgrade/pkg/model/core"
	"kubesphere.io/ks-upgrade/pkg/model/helper"
	"kubesphere.io/ks-upgrade/pkg/storage"
	"kubesphere.io/ks-upgrade/pkg/store"
	v3clusterv1alpha1 "kubesphere.io/ks-upgrade/v3/api/cluster/v1alpha1"
	tenantv1alpha1 "kubesphere.io/ks-upgrade/v3/api/tenant/v1alpha1"
	tenantv1alpha2 "kubesphere.io/ks-upgrade/v3/api/tenant/v1alpha2"
	"kubesphere.io/ks-upgrade/v3/api/types/v1beta1"
	clusterv1alpha1 "kubesphere.io/ks-upgrade/v4/api/cluster/v1alpha1"
	tenantv1beta1 "kubesphere.io/ks-upgrade/v4/api/tenant/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	restconfig "sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func init() {
	runtime.Must(executor.Register(&factory{}))
}

const (
	jobName                           = "core"
	validatingWebhookBackupKey        = jobName + ".validatingwebhookconfigurations.admissionregistration.k8s.io"
	mutatingWebhookBackupKey          = jobName + ".mutatingwebhookconfigurations.admissionregistration.k8s.io"
	kubeSphereSystemResourceBackupKey = jobName + ".resource.kubesphere.system"
	preUpgradeJobName                 = "ks-core-pre-upgrade"
)

const (
	workspaceTemplateProtection     = "kubesphere.io/protection"
	systemWorkspace                 = "system-workspace"
	hostClusterLabel                = "cluster-role.kubesphere.io/host"
	multiClusterKubeConfigBackupKey = jobName + ".cluster.kubeconfig"
)

const (
	networkPolicyAnnotationKey = "kubesphere.io/network-isolate"
	nsNetworkPolicyCRD         = "namespacenetworkpolicies.network.kubesphere.io"
)

var _ executor.UpgradeJob = &upgradeJob{}
var _ executor.PrepareUpgrade = &upgradeJob{}
var _ executor.DynamicUpgrade = &upgradeJob{}
var _ executor.InjectClientV3 = &upgradeJob{}
var _ executor.InjectClientV4 = &upgradeJob{}
var _ executor.InjectResourceStore = &upgradeJob{}
var _ executor.InjectModelHelperFactory = &upgradeJob{}
var _ executor.EnabledCondition = &upgradeJob{}

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
}

func (i *upgradeJob) IsEnabled(_ context.Context) (bool, error) {
	return true, nil
}

func (i *upgradeJob) PrepareUpgrade(ctx context.Context) error {
	if err := i.scaleDownDeployments(ctx); err != nil {
		return errors.Errorf("failed to scale down deployments: %s", err)
	}

	if err := i.removeNamespaceOwnerReference(ctx); err != nil {
		return errors.Errorf("failed to remove namespace owner reference: %s", err)
	}

	if err := i.deleteKubeSphereWebhook(ctx); err != nil {
		return errors.Errorf("failed to delete kubesphere webhook: %s", err)
	}

	if err := i.refreshHostClusterKubeConfig(ctx); err != nil {
		return errors.Errorf("failed to refresh host cluster kubeconfig: %s", err)
	}

	if err := i.networkPrepareUpgrade(ctx); err != nil {
		return errors.Errorf("failed to prepare upgrade network: %s", err)
	}

	if err := i.patchSystemWorkspaceTemplate(ctx); err != nil {
		return errors.Wrapf(err, "failed to patch system workspace template")
	}

	if err := i.deleteSystemWorkspaceTemplateInMemberCluster(ctx); err != nil {
		return errors.Wrapf(err, "failed to delete system workspace template in member cluster")
	}
	return nil
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
	if err := i.validateClusterRole(ctx); err != nil {
		return errors.Wrapf(err, "failed to validate cluster role")
	}

	if err := i.patchUpgradeJobPv(ctx); err != nil {
		klog.Warningf("patch upgrade job error %s", err)
		return err
	}

	if err := i.backupKubeSphereConfig(ctx); err != nil {
		return errors.Errorf("failed to backup kubesphere config: %s", err)
	}

	klog.Info("updating member clusters")
	if err := i.updateMemberClusters(ctx); err != nil {
		return err
	}

	klog.Info("start to delete kubesphere related resources")
	if err := i.deleteKubeSphereResource(ctx); err != nil {
		return err
	}

	klog.Info("start to update namespace labels")
	if err := i.UpdateNamespaceLabels(ctx); err != nil {
		return err
	}

	klog.Info("start to default workspaces placement")
	if err := i.UpdateWorkspacePlacement(ctx); err != nil {
		return err
	}

	return nil
}

func (i *upgradeJob) PostUpgrade(ctx context.Context) error {
	if err := i.patchWorkspaceTemplates(ctx); err != nil {
		return errors.Wrap(err, "failed to patch workspace templates")
	}
	return nil
}

func (i *upgradeJob) updateMemberClusters(ctx context.Context) error {
	clusterList := clusterv1alpha1.ClusterList{}
	if err := i.clientV4.List(ctx, &clusterList); err != nil {
		return err
	}
	for index := range clusterList.Items {
		cluster := &clusterList.Items[index]
		// skip for host cluster
		if _, ok := cluster.Labels[clusterv1alpha1.HostCluster]; ok {
			continue
		}

		if cluster.Labels == nil {
			cluster.Labels = make(map[string]string)
		}
		cluster.Labels["kubesphere.io/managed"] = "true"

		if cluster.Annotations == nil {
			cluster.Annotations = make(map[string]string)
		}
		// Set the hash value of an empty configuration to avoid it from being reconciled
		cluster.Annotations["kubesphere.io/config-hash"] = "cbf29ce484222325"
		updateClusterCondition(cluster, clusterv1alpha1.ClusterCondition{
			Type:               clusterv1alpha1.ClusterKSCoreReady,
			Status:             corev1.ConditionTrue,
			LastUpdateTime:     metav1.Now(),
			LastTransitionTime: metav1.Now(),
			Reason:             clusterv1alpha1.ClusterKSCoreReady,
			Message:            "KS Core is available now",
		})

		updateClusterCondition(cluster, clusterv1alpha1.ClusterCondition{
			Type:               clusterv1alpha1.ClusterSchedulable,
			Status:             corev1.ConditionFalse,
			LastUpdateTime:     metav1.Now(),
			LastTransitionTime: metav1.Now(),
			Reason:             "Upgrading",
			Message:            "Not schedulable now",
		})

		klog.Infof("updating cluster %s", cluster.Name)
		if err := i.clientV4.Update(ctx, cluster); err != nil {
			return err
		}
	}
	return nil
}

func updateClusterCondition(cluster *clusterv1alpha1.Cluster, condition clusterv1alpha1.ClusterCondition) {
	if cluster.Status.Conditions == nil {
		cluster.Status.Conditions = make([]clusterv1alpha1.ClusterCondition, 0)
	}

	newConditions := make([]clusterv1alpha1.ClusterCondition, 0)
	for _, cond := range cluster.Status.Conditions {
		if cond.Type == condition.Type {
			continue
		}
		newConditions = append(newConditions, cond)
	}

	newConditions = append(newConditions, condition)
	cluster.Status.Conditions = newConditions
}

func (i *upgradeJob) backupKubeSphereConfig(ctx context.Context) error {
	_, err := i.resourceStore.LoadRaw(core.KubeSphereConfigStoreKey)
	if err != nil && errors.Is(err, storage.BackupKeyNotFound) {
		configMap := corev1.ConfigMap{}
		if err := i.clientV3.Get(ctx, types.NamespacedName{Namespace: core.KubeSphereNamespace, Name: core.KubeSphereConfigName}, &configMap); err != nil {
			return err
		}
		value, ok := configMap.Data[core.KubeSphereConfigMapDataKey]
		if !ok {
			return errors.Errorf("failed to get configmap kubesphere.yaml value")
		}
		ksConfig := make(map[string]interface{})
		if err := yaml.Unmarshal([]byte(value), &ksConfig); err != nil {
			return errors.Errorf("failed to unmarshal value from configmap. err: %s", err)
		}

		yamlBytes, err := yaml.Marshal(ksConfig)
		if err != nil {
			return err
		}

		if err := i.resourceStore.SaveRaw(core.KubeSphereConfigStoreKey, yamlBytes); err != nil {
			return err
		}
	}

	return nil
}

func (i *upgradeJob) scaleDownDeployments(ctx context.Context) error {
	needScaleDown := []string{"ks-apiserver", "ks-controller-manager", "ks-console", "ks-installer"}
	deployments := &appsv1.DeploymentList{}
	if err := i.clientV3.List(ctx, deployments, runtimeclient.InNamespace(core.KubeSphereNamespace)); err != nil {
		return err
	}
	for _, deployment := range deployments.Items {
		if !slices.Contains(needScaleDown, deployment.Name) {
			continue
		}
		op, err := controllerutil.CreateOrUpdate(ctx, i.clientV3, &deployment, func() error {
			deployment.Spec.Replicas = new(int32)
			*deployment.Spec.Replicas = 0
			return nil
		})

		if err != nil {
			return err
		}

		klog.Infof("scale down deployment %s/%s %s", deployment.Namespace, deployment.Name, op)
	}
	return wait.PollImmediate(5*time.Second, 5*time.Minute, func() (bool, error) {
		deployments := &appsv1.DeploymentList{}
		if err := i.clientV3.List(ctx, deployments, runtimeclient.InNamespace(core.KubeSphereNamespace)); err != nil {
			klog.Warningf("failed to list deployments: %s", err)
			return false, nil
		}
		for _, deployment := range deployments.Items {
			if !slices.Contains(needScaleDown, deployment.Name) {
				continue
			}
			if deployment.Status.AvailableReplicas != 0 {
				klog.Warningf("deployment %s/%s still has %d available replicas", deployment.Namespace, deployment.Name, deployment.Status.AvailableReplicas)
				return false, nil
			}
		}
		return true, nil
	})
}

func (i *upgradeJob) refreshHostClusterKubeConfig(ctx context.Context) error {
	isHost, err := i.coreHelper.IsHostCluster(ctx)
	if err != nil {
		return err
	}
	if !isHost {
		return nil
	}
	clusters := v3clusterv1alpha1.ClusterList{}
	if err := i.resourceStore.Load(multiClusterKubeConfigBackupKey, &clusters); err != nil {
		if !errors.Is(err, storage.BackupKeyNotFound) {
			return err
		}
		if err = i.coreHelper.ListCluster(ctx, &clusters); err != nil {
			return err
		}
		if err = i.resourceStore.Save(multiClusterKubeConfigBackupKey, &clusters); err != nil {
			return err
		}
		for _, cluster := range clusters.Items {
			if _, ok := cluster.GetLabels()[hostClusterLabel]; ok {
				restConfig, err := restconfig.GetConfig()
				if err != nil {
					return err
				}
				if err = rest.LoadTLSFiles(restConfig); err != nil {
					return err
				}

				secrets := &corev1.SecretList{}
				if err := i.clientV3.List(ctx, secrets, runtimeclient.InNamespace(constants.KubeSphereNamespace)); err != nil {
					return err
				}

				var saTokenSecret *corev1.Secret
				for _, item := range secrets.Items {
					if item.Annotations["kubernetes.io/service-account.name"] == "kubesphere" {
						saTokenSecret = &item
						break
					}
				}

				if saTokenSecret == nil {
					return errors.Errorf("failed to get kubesphere sa token secret")
				}

				// https://kubernetes.io/docs/reference/access-authn-authz/service-accounts-admin/#manual-secret-management-for-serviceaccounts
				restConfig.BearerToken = string(saTokenSecret.Data["token"])

				if restConfig.BearerToken == "" {
					return errors.Errorf("failed to get kubesphere sa bearer token from secret")
				}

				clientConfig := model.CreateKubeConfigFileForRestConfig(restConfig)
				configBytes, err := clientcmd.Write(*clientConfig)
				if err != nil {
					return err
				}

				deployCopy := cluster.DeepCopy()
				deployCopy.Spec.Connection.KubeConfig = configBytes

				klog.Infof("refresh kubeconfig for cluster %s", cluster.Name)
				if err := i.clientV3.Patch(ctx, deployCopy, runtimeclient.MergeFrom(&cluster), &runtimeclient.PatchOptions{}); err != nil {
					return err
				}

				return nil
			}
		}
	}
	return nil
}

func (i *upgradeJob) deleteKubeSphereWebhook(ctx context.Context) error {
	var validatingWebhooksBackup = admissionregistrationv1.ValidatingWebhookConfigurationList{
		Items: make([]admissionregistrationv1.ValidatingWebhookConfiguration, 0),
	}
	var mutatingWebhooksBackup = admissionregistrationv1.MutatingWebhookConfigurationList{
		Items: make([]admissionregistrationv1.MutatingWebhookConfiguration, 0),
	}

	// Backup ValidatingWebhookConfiguration
	if err := i.resourceStore.Load(validatingWebhookBackupKey, &admissionregistrationv1.ValidatingWebhookConfigurationList{}); err != nil {
		if !errors.Is(err, storage.BackupKeyNotFound) {
			return err
		}
		validatingWebhooks := admissionregistrationv1.ValidatingWebhookConfigurationList{}
		if err = i.clientV3.List(ctx, &validatingWebhooks); err != nil {
			return err
		}

		for _, webhook := range validatingWebhooks.Items {
			if len(webhook.Webhooks) > 0 &&
				webhook.Webhooks[0].ClientConfig.Service != nil &&
				webhook.Webhooks[0].ClientConfig.Service.Name == "ks-controller-manager" &&
				webhook.Webhooks[0].ClientConfig.Service.Namespace == constants.KubeSphereNamespace {
				validatingWebhooksBackup.Items = append(validatingWebhooksBackup.Items, webhook)
			}
		}

		if err = i.resourceStore.Save(validatingWebhookBackupKey, &validatingWebhooksBackup); err != nil {
			return err
		}

	}

	// Backup MutatingWebhookConfiguration
	if err := i.resourceStore.Load(mutatingWebhookBackupKey, &admissionregistrationv1.MutatingWebhookConfigurationList{}); err != nil {
		if !errors.Is(err, storage.BackupKeyNotFound) {
			return err
		}
		mutatingWebhooks := admissionregistrationv1.MutatingWebhookConfigurationList{}
		if err = i.clientV3.List(ctx, &mutatingWebhooks); err != nil {
			return err
		}

		for _, webhook := range mutatingWebhooks.Items {
			if len(webhook.Webhooks) > 0 &&
				webhook.Webhooks[0].ClientConfig.Service.Name == "ks-controller-manager" &&
				webhook.Webhooks[0].ClientConfig.Service.Namespace == constants.KubeSphereNamespace {
				mutatingWebhooks.Items = append(mutatingWebhooks.Items, webhook)
			}
		}

		if err = i.resourceStore.Save(mutatingWebhookBackupKey, &mutatingWebhooksBackup); err != nil {
			return err
		}
	}

	// Delete ValidatingWebhookConfiguration
	for _, validatingWebhook := range validatingWebhooksBackup.Items {
		for _, wh := range validatingWebhook.Webhooks {
			if *wh.FailurePolicy == admissionregistrationv1.Fail {
				klog.Infof("Found ValidatingWebhookConfiguration %s %s FailurePolicy is %s", validatingWebhook.Name, wh.Name, *wh.FailurePolicy)
			}
		}
		if len(validatingWebhook.GetFinalizers()) > 0 {
			webhook := validatingWebhook.DeepCopy()
			now := metav1.Now()
			webhook.SetDeletionTimestamp(&now)
			webhook.SetFinalizers(make([]string, 0))
			klog.Infof("Delete ValidatingWebhookConfiguration %s", validatingWebhook.Name)
			if err := i.clientV3.Patch(ctx, webhook, runtimeclient.MergeFrom(&validatingWebhook), &runtimeclient.PatchOptions{}); err != nil {
				return err
			}
		}
		if err := i.clientV3.Delete(ctx, &validatingWebhook, &runtimeclient.DeleteOptions{}); err != nil {
			return err
		}
		klog.Infof("Delete ValidatingWebhookConfiguration %s", validatingWebhook.Name)
	}

	// Delete MutatingWebhookConfiguration
	for _, mutatingWebhook := range mutatingWebhooksBackup.Items {
		for _, wh := range mutatingWebhook.Webhooks {
			if *wh.FailurePolicy == admissionregistrationv1.Fail {
				klog.Infof("Found MutatingWebhookConfiguration %s %s FailurePolicy is %s", mutatingWebhook.Name, wh.Name, *wh.FailurePolicy)
			}
		}
		if len(mutatingWebhook.GetFinalizers()) > 0 {
			webhook := mutatingWebhook.DeepCopy()
			now := metav1.Now()
			webhook.SetDeletionTimestamp(&now)
			webhook.SetFinalizers(make([]string, 0))
			klog.Infof("Delete MutatingWebhookConfiguration %s", mutatingWebhook.Name)
			if err := i.clientV3.Patch(ctx, webhook, runtimeclient.MergeFrom(&mutatingWebhook), &runtimeclient.PatchOptions{}); err != nil {
				return err
			}
		}
		if err := i.clientV3.Delete(ctx, &mutatingWebhook, &runtimeclient.DeleteOptions{}); err != nil {
			return err
		}
		klog.Infof("Delete MutatingWebhookConfiguration %s", mutatingWebhook.Name)
	}
	klog.Infof("Delete %d ValidatingWebhookConfiguration", len(validatingWebhooksBackup.Items))
	klog.Infof("Delete %d MutatingWebhookConfiguration", len(mutatingWebhooksBackup.Items))
	return nil
}

func (i *upgradeJob) DynamicUpgrade(_ context.Context) error {
	//TODO implement me
	klog.Info("Start dynamic-upgrade job")
	return nil
}

func (i *upgradeJob) RollBack(ctx context.Context) error {
	if err := i.recoverKubeSphereWebhook(ctx); err != nil {
		return err
	}
	if err := i.recoverKubeSphereResource(ctx); err != nil {
		return err
	}
	return nil
}

func (i *upgradeJob) recoverKubeSphereWebhook(ctx context.Context) error {
	var validatingWebhooksRollbackCount = 0
	var validatingWebhooksSkipCount = 0
	var validatingWebhooksBackup = admissionregistrationv1.ValidatingWebhookConfigurationList{
		Items: make([]admissionregistrationv1.ValidatingWebhookConfiguration, 0),
	}
	var mutatingWebhooksRollbackCount = 0
	var mutatingWebhooksSkipCount = 0
	var mutatingWebhooksBackup = admissionregistrationv1.MutatingWebhookConfigurationList{
		Items: make([]admissionregistrationv1.MutatingWebhookConfiguration, 0),
	}
	if err := i.resourceStore.Load(validatingWebhookBackupKey, &validatingWebhooksBackup); err != nil {
		if errors.Is(err, storage.BackupKeyNotFound) {
			klog.Warningf("ValidatingWebhookBackup %s not found", validatingWebhookBackupKey)
			return nil
		}
		return err
	}
	if err := i.resourceStore.Load(mutatingWebhookBackupKey, &mutatingWebhooksBackup); err != nil {
		if errors.Is(err, storage.BackupKeyNotFound) {
			klog.Warningf("MutatingWebhookBackupKey %s not found", mutatingWebhookBackupKey)
			return nil
		}
		return err
	}
	for _, validatingWebhook := range validatingWebhooksBackup.Items {
		if err := i.clientV3.Get(ctx, types.NamespacedName{Name: validatingWebhook.Name}, &admissionregistrationv1.ValidatingWebhookConfiguration{}, &runtimeclient.GetOptions{}); err != nil {
			if !apierrors.IsNotFound(err) {
				return err
			}
			klog.Infof("ValidatingWebhookConfiguration %s not found", validatingWebhook.Name)
			validatingWebhook.SetResourceVersion("")
			if err := i.clientV3.Create(ctx, &validatingWebhook, &runtimeclient.CreateOptions{}); err != nil {
				return err
			}
			klog.Infof("ValidatingWebhookConfiguration %s rollback", validatingWebhook.Name)
			validatingWebhooksRollbackCount += 1
		} else {
			klog.Infof("ValidatingWebhookConfiguration %s already exists", validatingWebhook.Name)
			validatingWebhooksSkipCount += 1
		}
	}
	for _, mutatingWebhook := range mutatingWebhooksBackup.Items {
		if err := i.clientV3.Get(ctx, types.NamespacedName{Name: mutatingWebhook.Name}, &admissionregistrationv1.MutatingWebhookConfiguration{}, &runtimeclient.GetOptions{}); err != nil {
			if !apierrors.IsNotFound(err) {
				return err
			}
			klog.Infof("MutatingWebhookConfiguration %s not found", mutatingWebhook.Name)
			mutatingWebhook.SetResourceVersion("")
			if err := i.clientV3.Create(ctx, &mutatingWebhook, &runtimeclient.CreateOptions{}); err != nil {
				return err
			}
			klog.Infof("MutatingWebhookConfiguration %s rollback", mutatingWebhook.Name)
			mutatingWebhooksRollbackCount += 1
		} else {
			klog.Infof("MutatingWebhookConfiguration %s already exists", mutatingWebhook.Name)
			mutatingWebhooksSkipCount += 1
		}
	}
	klog.Infof("Rollback %d ValidatingWebhookConfiguration", validatingWebhooksRollbackCount)
	klog.Infof("Skip %d ValidatingWebhookConfiguration", validatingWebhooksSkipCount)
	klog.Infof("Rollback %d MutatingWebhookConfiguration", mutatingWebhooksRollbackCount)
	klog.Infof("Skip %d MutatingWebhookConfiguration", mutatingWebhooksSkipCount)
	return nil
}

func (i *upgradeJob) deleteKubeSphereResource(ctx context.Context) error {
	var deployBackupCount = 0
	var deployDeleteCount = 0
	var deployBackup = appsv1.DeploymentList{
		Items: make([]appsv1.Deployment, 0),
	}
	if err := i.resourceStore.Load(kubeSphereSystemResourceBackupKey, &appsv1.DeploymentList{}); err != nil {
		if !errors.Is(err, storage.BackupKeyNotFound) {
			return err
		}
		deploys := appsv1.DeploymentList{}
		if err = i.clientV3.List(ctx, &deploys, &runtimeclient.ListOptions{
			Namespace: constants.KubeSphereNamespace,
		}); err != nil {
			return err
		}
		deployBackup.Items = deploys.Items
		if err = i.resourceStore.Save(kubeSphereSystemResourceBackupKey, &deploys); err != nil {
			return err
		}
		deployBackupCount += len(deployBackup.Items)
	}

	for _, deploy := range deployBackup.Items {
		// Delete ks-installer only
		if deploy.Name != "ks-installer" {
			continue
		}
		if len(deploy.GetFinalizers()) > 0 {
			deployCopy := deploy.DeepCopy()
			now := metav1.Now()
			deployCopy.SetDeletionTimestamp(&now)
			deployCopy.SetFinalizers(make([]string, 0))
			klog.Infof("Delete Deployment %s/%s", deploy.Namespace, deploy.Name)
			if err := i.clientV3.Patch(ctx, deployCopy, runtimeclient.MergeFrom(&deploy), &runtimeclient.PatchOptions{}); err != nil {
				return err
			}
		}
		if err := i.clientV3.Delete(ctx, &deploy, &runtimeclient.DeleteOptions{}); err != nil {
			return err
		}
		klog.Infof("Delete Deployment %s/%s", deploy.Namespace, deploy.Name)
		deployDeleteCount += 1
	}

	klog.Infof("Backup %d %s/Deployment", deployBackupCount, constants.KubeSphereNamespace)
	klog.Infof("Delete %d %s/Deployment", deployDeleteCount, constants.KubeSphereNamespace)
	return nil
}

func (i *upgradeJob) recoverKubeSphereResource(ctx context.Context) error {
	var deployRollbackCount = 0
	var deploySkipCount = 0
	deploys := appsv1.DeploymentList{}
	if err := i.resourceStore.Load(kubeSphereSystemResourceBackupKey, &deploys); err != nil {
		if errors.Is(err, storage.BackupKeyNotFound) {
			klog.Warningf("KubesphereSystemBackupKey %s not found", kubeSphereSystemResourceBackupKey)
			return nil
		}
		return err
	}
	for _, deploy := range deploys.Items {
		if err := i.clientV3.Get(ctx, types.NamespacedName{Name: deploy.Name, Namespace: deploy.Namespace}, &appsv1.Deployment{}, &runtimeclient.GetOptions{}); err != nil {
			if !apierrors.IsNotFound(err) {
				return err
			}
			klog.Infof("Deployment %s not found", deploy.Name)
			deploy.SetResourceVersion("")
			if err := i.clientV3.Create(ctx, &deploy, &runtimeclient.CreateOptions{}); err != nil {
				return err
			}
			klog.Infof("Deployment %s/%s rollback", deploy.Namespace, deploy.Name)
			deployRollbackCount += 1
		} else {
			klog.Infof("Deployment %s/%s already exists", deploy.Namespace, deploy.Name)
			deploySkipCount += 1
		}
	}
	klog.Infof("Rollback %d Deployment", deployRollbackCount)
	klog.Infof("Skip %d Deployment", deploySkipCount)
	return nil
}

func (i *upgradeJob) networkPrepareUpgrade(ctx context.Context) error {
	// backup workspace networkpolicy info
	if err := i.listResourceToStore(ctx, map[string]runtimeclient.ObjectList{
		"workspacetemplates.tenant.kubesphere.io.v1alpha2": &tenantv1alpha2.WorkspaceTemplateList{},
		"workspace.tenant.kubesphere.io.v1alpha1":          &tenantv1alpha1.WorkspaceList{},
	}); err != nil {
		return err
	}

	// label namespacenetworkpolicy info
	nsnp := &crdv1.CustomResourceDefinition{}
	err := i.clientV3.Get(ctx, types.NamespacedName{Name: nsNetworkPolicyCRD}, nsnp, &runtimeclient.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) || metaerrors.IsNoMatchError(err) {
			klog.Warning(err.Error())
		} else {
			return err
		}
	}
	if nsnp.Labels == nil {
		nsnp.Labels = map[string]string{}
	}
	nsnp.Labels["kubesphere.io/resource-served"] = "true"
	if nsnp.Name != "" {
		klog.Infof("label namespacenetworkpolicy crd kubesphere/resource-served")
		if err := i.clientV3.Update(ctx, nsnp, &runtimeclient.UpdateOptions{}); err != nil {
			return err
		}
	}

	// annotation workspace networkpolicy info
	wslist := &tenantv1alpha1.WorkspaceList{}
	if err := i.clientV3.List(ctx, wslist, &runtimeclient.ListOptions{}); err != nil && !metaerrors.IsNoMatchError(err) {
		return err
	}

	for _, ws := range wslist.Items {
		if ws.Spec.NetworkIsolation == nil || !*ws.Spec.NetworkIsolation {
			continue
		}

		clone := ws.DeepCopy()
		if clone.Annotations == nil {
			clone.Annotations = map[string]string{}
		}
		clone.Annotations[networkPolicyAnnotationKey] = "enabled"
		klog.Infof("annotate workspace[%s] network-isolate info", clone.Name)
		if err := i.clientV3.Update(ctx, clone, &runtimeclient.UpdateOptions{}); err != nil {
			return err
		}
	}

	return nil
}

func (i *upgradeJob) listResourceToStore(ctx context.Context, objs map[string]runtimeclient.ObjectList) error {
	for objKey, objValue := range objs {
		fileName := fmt.Sprintf("%s-%s", jobName, objKey)
		err := i.resourceStore.Load(fileName, objValue)
		if err != nil && errors.Is(err, storage.BackupKeyNotFound) {
			if err := i.clientV3.List(ctx, objValue, &runtimeclient.ListOptions{}); err != nil {
				if apierrors.IsNotFound(err) || metaerrors.IsNoMatchError(err) {
					klog.Warning(err.Error())
					continue
				} else {
					return err
				}
			}

			if err := i.resourceStore.Save(fileName, objValue); err != nil {
				return err
			}
		}
	}

	return nil
}

func (i *upgradeJob) removeNamespaceOwnerReference(ctx context.Context) error {
	namespaces := &corev1.NamespaceList{}
	if err := i.clientV3.List(ctx, namespaces, &runtimeclient.ListOptions{}); err != nil {
		return errors.Errorf("failed to list namespaces: %s", err)
	}
	for _, ns := range namespaces.Items {
		if !ns.DeletionTimestamp.IsZero() || len(ns.GetOwnerReferences()) == 0 {
			continue
		}
		ownerReferences := ns.GetOwnerReferences()
		for i := 0; i < len(ownerReferences); i++ {
			ownerReference := ownerReferences[i]
			if ownerReference.Kind == "Workspace" {
				ownerReferences = append(ownerReferences[:i], ownerReferences[i+1:]...)
			}
		}
		ns.SetOwnerReferences(ownerReferences)
		klog.Infof("remove owner reference from namespace %s", ns.Name)
		if err := i.clientV3.Update(ctx, &ns, &runtimeclient.UpdateOptions{}); err != nil {
			return errors.Errorf("failed to update namespace %s: %s", ns.Name, err)
		}
	}
	return nil
}

func (i *upgradeJob) patchWorkspaceTemplates(ctx context.Context) error {
	workspaceTemplates := &tenantv1beta1.WorkspaceTemplateList{}
	if err := i.clientV4.List(ctx, workspaceTemplates); err != nil {
		return errors.Wrapf(err, "failed to list workspace templates")
	} else {
		for _, workspaceTemplate := range workspaceTemplates.Items {
			if workspaceTemplate.Spec.Template.Spec.Manager != "" ||
				!workspaceTemplate.DeletionTimestamp.IsZero() {
				continue
			}
			item := &workspaceTemplate
			op, err := ctrl.CreateOrUpdate(ctx, i.clientV4, item, func() error {
				item.Spec.Template.Spec.Manager = "admin"
				return nil
			})
			if err != nil {
				return errors.Wrapf(err, "failed to patch workspace template %s", workspaceTemplate.Name)
			}
			klog.Infof("patch workspace template %s %s", workspaceTemplate.Name, op)
		}
	}
	return nil
}

func (i *upgradeJob) patchSystemWorkspaceTemplate(ctx context.Context) error {
	workspaceTemplateV3 := &tenantv1alpha2.WorkspaceTemplate{}
	if err := i.clientV3.Get(ctx, types.NamespacedName{Name: systemWorkspace}, workspaceTemplateV3); err != nil {
		if !metaerrors.IsNoMatchError(err) && !apierrors.IsNotFound(err) {
			return errors.Wrapf(err, "failed to get system workspace template")
		}
	} else {
		newWorkspaceTemplate := workspaceTemplateV3.DeepCopy()
		if !slices.Contains(newWorkspaceTemplate.Finalizers, workspaceTemplateProtection) {
			newWorkspaceTemplate.Finalizers = append(newWorkspaceTemplate.Finalizers, workspaceTemplateProtection)
			klog.Infof("patch system workspace template %s", systemWorkspace)
			if err := i.clientV3.Patch(ctx, newWorkspaceTemplate, runtimeclient.MergeFrom(workspaceTemplateV3)); err != nil {
				return errors.Wrapf(err, "failed to patch system workspace template")
			}
		}
	}

	workspaceTemplateV4 := &tenantv1beta1.WorkspaceTemplate{}
	if err := i.clientV4.Get(ctx, types.NamespacedName{Name: systemWorkspace}, workspaceTemplateV4); err != nil {
		if !metaerrors.IsNoMatchError(err) && !apierrors.IsNotFound(err) {
			return errors.Wrapf(err, "failed to get system workspace template")
		}
	} else {
		newWorkspaceTemplate := workspaceTemplateV4.DeepCopy()
		if !slices.Contains(newWorkspaceTemplate.Finalizers, workspaceTemplateProtection) {
			newWorkspaceTemplate.Finalizers = append(newWorkspaceTemplate.Finalizers, workspaceTemplateProtection)
			klog.Infof("patch system workspace template %s", systemWorkspace)
			if err := i.clientV4.Patch(ctx, newWorkspaceTemplate, runtimeclient.MergeFrom(workspaceTemplateV4)); err != nil {
				return errors.Wrapf(err, "failed to patch system workspace template")
			}
		}
	}
	return nil
}

func (i *upgradeJob) validateClusterRole(ctx context.Context) error {
	isHost, err := i.coreHelper.IsHostCluster(ctx)
	if err != nil {
		return errors.Wrapf(err, "failed to check if the cluster is host cluster")
	}
	systemWorkspaceTemplateExists, err := i.checkIfSystemWorkspaceTemplateExists(ctx)

	if err != nil {
		return errors.Wrapf(err, "failed to check if the system workspace template exists")
	}

	if !isHost && systemWorkspaceTemplateExists {
		return errors.New("system workspace template exists in managed cluster")
	}

	return nil
}

func (i *upgradeJob) checkIfSystemWorkspaceTemplateExists(ctx context.Context) (bool, error) {
	if err := i.clientV3.Get(ctx, types.NamespacedName{Name: systemWorkspace}, &tenantv1alpha2.WorkspaceTemplate{}); err != nil {
		if !apierrors.IsNotFound(err) {
			return false, errors.Wrapf(err, "failed to get system workspace template")
		}
	} else {
		return true, nil
	}

	if err := i.clientV4.Get(ctx, types.NamespacedName{Name: systemWorkspace}, &tenantv1beta1.WorkspaceTemplate{}); err != nil {
		if !apierrors.IsNotFound(err) {
			return false, errors.Wrapf(err, "failed to get system workspace template")
		}
	} else {
		return true, nil
	}
	return false, nil
}

func (i *upgradeJob) patchUpgradeJobPv(ctx context.Context) error {
	job := batchv1.Job{}
	if err := i.clientV3.Get(ctx, types.NamespacedName{
		Namespace: constants.KubeSphereNamespace,
		Name:      preUpgradeJobName,
	}, &job, &runtimeclient.GetOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if job.Spec.Template.Spec.Volumes == nil {
		klog.Infof("Patch upgrade job pv skipping %s", "field not configured")
		return nil
	}

	pvc := corev1.PersistentVolumeClaim{}
	for _, volume := range job.Spec.Template.Spec.Volumes {
		if volume.VolumeSource.PersistentVolumeClaim != nil {
			// Get the pvc associated with the job
			pvcName := volume.VolumeSource.PersistentVolumeClaim.ClaimName
			if err := i.clientV3.Get(ctx, types.NamespacedName{
				Namespace: constants.KubeSphereNamespace,
				Name:      pvcName,
			}, &pvc, &runtimeclient.GetOptions{}); err != nil {
				if apierrors.IsNotFound(err) {
					klog.Warningf("Patch upgrade job pvc %s/%s not found", constants.KubeSphereNamespace, pvcName)
					continue
				}
				return err
			}

			if pvc.Spec.VolumeName == "" {
				continue
			}

			// Get the pv associated with pvc
			pv := corev1.PersistentVolume{}
			if err := i.clientV3.Get(ctx, types.NamespacedName{
				Namespace: "",
				Name:      pvc.Spec.VolumeName,
			}, &pv, &runtimeclient.GetOptions{}); err != nil {
				if apierrors.IsNotFound(err) {
					klog.Warningf("Patch upgrade job pv %s not found", pvcName)
					continue
				}
				return err
			}

			// Patch pv reclaimPolicy
			if pv.Spec.PersistentVolumeReclaimPolicy != corev1.PersistentVolumeReclaimRetain {
				deployCopy := pv.DeepCopy()
				deployCopy.Spec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimRetain
				klog.Infof("Patch pv %s PersistentVolumeReclaimPolicy to %s", pvcName, corev1.PersistentVolumeReclaimRetain)
				if err := i.clientV3.Patch(ctx, deployCopy, runtimeclient.MergeFrom(&pv), &runtimeclient.PatchOptions{}); err != nil {
					klog.Warningf("Patch upgrade job pv %s failed", pvcName)
					return nil
				}
				klog.Infof("Patch pv %s PersistentVolumeReclaimPolicy to %s", pvcName, corev1.PersistentVolumeReclaimRetain)
			} else {
				klog.Infof("Pv %s PersistentVolumeReclaimPolicy is %s", pvcName, corev1.PersistentVolumeReclaimRetain)
			}
		}
	}
	return nil
}

func (i *upgradeJob) deleteSystemWorkspaceTemplateInMemberCluster(ctx context.Context) error {
	isHost, err := i.coreHelper.IsHostCluster(ctx)
	if err != nil {
		return errors.Wrapf(err, "failed to check if the cluster is host cluster")
	}

	if isHost {
		klog.Info("Skip delete system workspace template in host cluster")
		return nil
	}

	workspace := &tenantv1alpha1.Workspace{}
	if err := i.clientV3.Get(ctx, types.NamespacedName{Name: systemWorkspace}, workspace); err != nil {
		if apierrors.IsNotFound(err) || metaerrors.IsNoMatchError(err) {
			klog.Info("Skip delete system workspace template in member cluster, system workspace not found")
			return nil
		}
		return errors.Wrapf(err, "failed to get system workspace")
	}

	if len(workspace.OwnerReferences) > 0 {
		return errors.New("system workspace has unexpected owner reference")
	}

	systemWorkspaceTemplate := &tenantv1alpha2.WorkspaceTemplate{}
	if err := i.clientV3.Get(ctx, types.NamespacedName{Name: systemWorkspace}, systemWorkspaceTemplate); err != nil {
		if apierrors.IsNotFound(err) || metaerrors.IsNoMatchError(err) {
			return nil
		}
		return errors.Wrapf(err, "failed to get system workspace template")
	}

	systemWorkspaceTemplate.Finalizers = nil
	systemWorkspaceTemplate.OwnerReferences = nil
	klog.Infof("update system workspace template %s", systemWorkspaceTemplate.Name)
	if err := i.clientV3.Update(ctx, systemWorkspaceTemplate); err != nil {
		return errors.Wrapf(err, "failed to update system workspace template")
	}

	propagationPolicy := metav1.DeletePropagationOrphan
	if err := i.clientV3.Delete(ctx, systemWorkspaceTemplate, &runtimeclient.DeleteOptions{PropagationPolicy: &propagationPolicy}); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return errors.Wrapf(err, "failed to delete system workspace template")
	}

	return nil
}

func (i *upgradeJob) UpdateNamespaceLabels(ctx context.Context) error {
	var errs []error
	requirement, err := labels.NewRequirement("kubesphere.io/workspace", selection.Exists, nil)
	if err != nil {
		return err
	}
	selector := client.MatchingLabelsSelector{Selector: labels.NewSelector().Add(*requirement)}
	nsList := &corev1.NamespaceList{}
	err = i.clientV3.List(ctx, nsList, selector)
	if err != nil {
		return err
	}
	for _, item := range nsList.Items {
		if item.Labels["kubesphere.io/devopsproject"] != "" {
			continue
		}
		if err = addLabels(i.clientV3, ctx, &item, map[string]string{"kubesphere.io/managed": "true"}); err != nil {
			errs = append(errs, fmt.Errorf("failed to add labels to namespace %s, %s", item.Name, err))
		}
		klog.Info(fmt.Sprintf("add labels to namespace %s successfully", item.Name))
	}
	return errors2.NewAggregate(errs)
}

func addLabels(c client.Client, ctx context.Context, object client.Object, ls map[string]string) error {
	getLabels := object.GetLabels()
	if getLabels == nil {
		getLabels = make(map[string]string)
	}
	for k, v := range ls {
		getLabels[k] = v
	}
	object.SetLabels(getLabels)
	return c.Update(ctx, object)
}

// UpdateWorkspacePlacement default the workspacetemplate when is Single-cluster mode.
// Because workspacetemplate`s field `Placement` is empty at the single-cluster mode,
// and it will let the namespaces (controlled by workspace) be deleted.
func (i *upgradeJob) UpdateWorkspacePlacement(ctx context.Context) error {
	// ignore if is not single-cluster mode
	singleCluster, err := isSingleCluster(ctx, i.clientV3)
	if err != nil {
		return err
	}

	if !singleCluster {
		klog.Info("Skip update workspace template placement")
		return nil
	}

	templateList := &tenantv1alpha2.WorkspaceTemplateList{}
	err = i.clientV3.List(ctx, templateList)
	if err != nil {
		return err
	}

	hostClusterName := "host"
	if i.options != nil {
		if name, ok := i.options["hostClusterName"].(string); ok && name != "" {
			hostClusterName = name
		}
	}

	var errs []error
	for _, item := range templateList.Items {
		if item.Spec.Placement.ClusterSelector == nil && len(item.Spec.Placement.Clusters) == 0 {
			item.Spec.Placement.Clusters = []v1beta1.GenericClusterReference{{Name: hostClusterName}}
			klog.Infof("update workspace template %s placement to %s", item.Name, hostClusterName)
			if err := i.clientV3.Update(ctx, &item); err != nil {
				errs = append(errs, fmt.Errorf("failed to update workspace template %s, %s", item.Name, err))
				continue
			}
		}
	}

	return errors2.NewAggregate(errs)
}

func isSingleCluster(ctx context.Context, c client.Client) (bool, error) {
	configMap := corev1.ConfigMap{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: constants.KubeSphereNamespace, Name: constants.KubeSphereConfigName}, &configMap); err != nil {
		return false, err
	}
	value, ok := configMap.Data[constants.KubeSphereConfigMapDataKey]
	if !ok {
		return false, fmt.Errorf("failed to get configmap kubesphere.yaml value")
	}
	ksConfig := make(map[string]interface{})
	if err := yaml.Unmarshal([]byte(value), &ksConfig); err != nil {
		return false, fmt.Errorf("failed to unmarshal value from configmap. err: %s", err)
	}

	clusterRole, ok, err := unstructured.NestedString(ksConfig, "multicluster", "clusterRole")
	if err != nil {
		klog.Warningf("failed to get multicluster cluster role from configmap. err: %s", err)
	}
	if ok && clusterRole == "none" {
		return true, nil
	}
	return false, nil
}
