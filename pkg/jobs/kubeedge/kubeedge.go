package kubeedge

import (
	"errors"
	"fmt"
	"sigs.k8s.io/yaml"
	"time"

	"golang.org/x/net/context"
	"helm.sh/helm/v3/pkg/storage/driver"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"kubesphere.io/ks-upgrade/pkg/executor"
	"kubesphere.io/ks-upgrade/pkg/helm"
	"kubesphere.io/ks-upgrade/pkg/model"
	"kubesphere.io/ks-upgrade/pkg/model/core"
	"kubesphere.io/ks-upgrade/pkg/model/helper"
	"kubesphere.io/ks-upgrade/pkg/storage"
	"kubesphere.io/ks-upgrade/pkg/store"
	v4clusterv1alpha1 "kubesphere.io/ks-upgrade/v4/api/cluster/v1alpha1"
	v4corev1alpha1 "kubesphere.io/ks-upgrade/v4/api/core/v1alpha1"
)

func init() {
	runtime.Must(executor.Register(&factory{}))
}

const (
	jobName     = "kubeedge"
	Namespace   = "kubeedge"
	jobDone     = jobName + "-done"
	optionRerun = "rerun"
)

var _ executor.UpgradeJob = &upgradeJob{}

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
	helmClient    helm.HelmClient
	clientV3      runtimeclient.Client
	clientV4      runtimeclient.Client
	coreHelper    core.Helper
	resourceStore store.ResourceStore
	options       executor.DynamicOptions
	extensionRef  *model.ExtensionRef
}

func (i *upgradeJob) PreUpgrade(ctx context.Context) error {
	var err error

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

	// check if kubeedge components already installed in ks-installer
	cc, err := i.coreHelper.GetClusterConfiguration(ctx)
	if err != nil {
		return err
	}

	if !i.kubeEdgeEnabled(cc) {
		klog.Info("kubeedge is not enabled.")
		return nil
	}

	// get kubeedge advertiseAddress
	//advertiseAddress, ok, err := unstructured.NestedStringSlice(cc, "spec", "edgeruntime", "kubeedge", "cloudCore", "cloudHub", "advertiseAddress")
	//if err != nil || !ok || len(advertiseAddress) == 0 {
	//	return fmt.Errorf("kubeedge advertiseAddress is invalid.")
	//}

	secretNames := []string{"tokensecret", "cloudcoresecret", "casecret", "cloudcore"}
	configmapNames := []string{"tunnelport", "cloudcore"}
	configmapsKey := fmt.Sprintf("%s-configmap-list", jobName)
	secretsKey := fmt.Sprintf("%s-secret-list", jobName)
	allConfigMaps := &corev1.ConfigMapList{}
	allSecrets := &corev1.SecretList{}

	err = i.resourceStore.Load(configmapsKey, allConfigMaps) // <----- load resource form store
	if err != nil && errors.Is(err, storage.BackupKeyNotFound) {
		err := i.clientV3.List(ctx, allConfigMaps, runtimeclient.InNamespace(Namespace))
		if err != nil {
			return err
		}

		newConfigMapItems := []corev1.ConfigMap{}
		for _, item := range allConfigMaps.Items {
			for _, name := range configmapNames {
				if item.Name == name {
					newConfigMapItems = append(newConfigMapItems, item)
				}
			}
		}
		allConfigMaps.Items = newConfigMapItems
		err = i.resourceStore.Save(configmapsKey, allConfigMaps)
		if err != nil {
			return err
		}
	}

	err = i.resourceStore.Load(secretsKey, allSecrets) // <----- load resource form store
	if err != nil && errors.Is(err, storage.BackupKeyNotFound) {
		err = i.clientV3.List(ctx, allSecrets, runtimeclient.InNamespace(Namespace))
		if err != nil {
			return err
		}

		newSecretItems := []corev1.Secret{}
		for _, item := range allSecrets.Items {
			for _, name := range secretNames {
				if item.Name == name {
					newSecretItems = append(newSecretItems, item)
				}
			}
		}
		allSecrets.Items = newSecretItems
		err = i.resourceStore.Save(secretsKey, allSecrets)
		if err != nil {
			return err
		}
	}

	_, err = i.helmClient.Uninstall("kubeedge", "cloudcore")
	if errors.Is(err, driver.ErrReleaseNotFound) {
		klog.Info("helm: kubeedge release is not found.")
		err = nil
	}

	if err != nil {
		return err
	}

	return nil
}

func (i *upgradeJob) PostUpgrade(ctx context.Context) error {
	var err error

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

	// check if kubeedge components already installed in ks-installer
	cc, err := i.coreHelper.GetClusterConfiguration(ctx)
	if err != nil {
		return err
	}

	if !i.kubeEdgeEnabled(cc) {
		klog.Info("kubeedge is not enabled.")
		return nil
	}

	watchFuncs := func() (context.Context, time.Duration, time.Duration, wait.ConditionWithContextFunc) {
		return ctx, time.Second * 2, time.Minute * 10, func(ctx context.Context) (done bool, err error) {
			ext := v4corev1alpha1.InstallPlan{}
			if err := i.clientV4.Get(ctx, types.NamespacedName{Name: i.extensionRef.Name}, &ext, &runtimeclient.GetOptions{}); err != nil {
				return false, err
			}
			klog.Infof("CreateInstallPlanFromExtensionRef object, %v", ext)
			if ext.Status.State == v4corev1alpha1.StateDeployed {
				// restore configmap secret, overwrite the existing one
				allConfigMaps := &corev1.ConfigMapList{}
				allSecrets := &corev1.SecretList{}
				configmapsKey := fmt.Sprintf("%s-configmap-list", jobName)
				secretsKey := fmt.Sprintf("%s-secret-list", jobName)
				err = i.resourceStore.Load(configmapsKey, allConfigMaps)
				if err != nil {
					return false, err
				}
				err = i.resourceStore.Load(secretsKey, allSecrets)
				if err != nil {
					return false, err
				}
				for _, configmap := range allConfigMaps.Items {
					curConfigMap := &corev1.ConfigMap{}
					err = i.clientV4.Get(ctx, runtimeclient.ObjectKey{Name: configmap.Name, Namespace: configmap.Namespace}, curConfigMap)
					if err != nil {
						return false, err
					}
					curConfigMap.Data = configmap.Data
					err = i.clientV4.Update(ctx, curConfigMap)
					if err != nil {
						return false, err
					}
				}

				for _, secret := range allSecrets.Items {
					curSecret := &corev1.Secret{}
					err = i.clientV4.Get(ctx, runtimeclient.ObjectKey{Name: secret.Name, Namespace: secret.Namespace}, curSecret)
					if err != nil {
						return false, err
					}
					curSecret.Data = secret.Data
					err = i.clientV4.Update(ctx, curSecret)
					if err != nil {
						return false, err
					}
				}

				return true, nil
			}
			return false, nil
		}
	}
	// create extension install plan
	klog.Infof("create install plan for extension %s", i.extensionRef.Name)
	//err = i.coreHelper.CreateInstallPlanFromExtensionRef(ctx, i.extensionRef, watchFuns)
	//if err != nil && !k8serrors.IsAlreadyExists(err) {
	//	return err
	//}
	err = i.createInstallPlan(ctx, watchFuncs)
	if err != nil {
		klog.Infof("create kubeedge install plan error, %s", err)
		return err
	}

	// save job done time
	date = []byte(time.Now().UTC().String())
	klog.Infof("save data key: %s value: %s", jobDone, date)
	err = i.resourceStore.SaveRaw(jobDone, date)
	return nil
}

func (i *upgradeJob) kubeEdgeEnabled(cc map[string]interface{}) bool {
	edgeRuntimeEnabled, ok, err := unstructured.NestedBool(cc, "spec", "edgeruntime", "enabled")
	if err != nil || !ok || !edgeRuntimeEnabled {
		klog.Info("edgeruntime is not enabled.")
		return false
	}
	kubeEdgeEnabled, ok, err := unstructured.NestedBool(cc, "spec", "edgeruntime", "kubeedge", "enabled")
	if err != nil || !ok || !kubeEdgeEnabled {
		klog.Info("kubeedge is not enabled.")
		return false
	}
	return true
}

//------ clusterconfigurations.installer.kubesphere.io ------
//	edgeruntime:
//	  enabled: true
//	  kubeedge:
//	    cloudCore:
//	      cloudHub:
//	        advertiseAddress:
//	          - 172.18.0.2
//	      service:
//	        cloudhubNodePort: '30000'
//	        cloudhubQuicNodePort: '30001'
//	        cloudhubHttpsNodePort: '30002'
//	        cloudstreamNodePort: '30003'
//	        tunnelNodePort: '30004'
//	    enabled: true
//	    iptables-manager:
//	      enabled: true
//	      mode: external
//
//------ .extensionRef.Config ------
//global:
//	imageRegistry: docker.io
//	imagePullSecrets: []
//	tag:
//kubeedge:
//	enabled: true
//cloudcore:
//	cloudCore:
//	  modules:
//	    cloudHub:
//	      advertiseAddress:
//	        - "172.31.73.33"
//	  service:
//	    cloudhubNodePort: "30000"
//	    cloudhubQuicNodePort: "30001"
//	    cloudhubHttpsNodePort: "30002"
//	    cloudstreamNodePort: "30003"
//	    tunnelNodePort: "30004"

func (i *upgradeJob) getKubeEdgeInstallPlanConfig(cc map[string]interface{}) map[string]interface{} {
	config := map[string]interface{}{}
	edgeRuntimeEnabled, ok, err := unstructured.NestedBool(cc, "spec", "edgeruntime", "enabled")
	if err != nil || !ok || !edgeRuntimeEnabled {
		klog.Info("edgeruntime is not enabled.")
		return config
	}

	// kubeedge.enabled
	kubeEdgeEnabled, ok, err := unstructured.NestedBool(cc, "spec", "edgeruntime", "kubeedge", "enabled")
	if err != nil || !ok || !kubeEdgeEnabled {
		klog.Info("kubeedge is not enabled.")
		kubeEdgeEnabled = false
	}
	unstructured.SetNestedField(config, kubeEdgeEnabled, "kubeedge", "enabled")

	// cloudcore.cloudCore.modules.cloudHub
	cloudHubConfig, ok, err := unstructured.NestedMap(cc, "spec", "edgeruntime", "kubeedge", "cloudCore", "cloudHub")
	if err != nil || !ok {
		klog.Info("edgeruntime.kubeedge.cloudCore.cloudHub is not found.")
		cloudHubConfig = map[string]interface{}{}
	}
	unstructured.SetNestedMap(config, cloudHubConfig, "cloudcore", "cloudCore", "modules", "cloudHub")

	// cloudcore.cloudCore.service
	serviceConfig, ok, err := unstructured.NestedMap(cc, "spec", "edgeruntime", "kubeedge", "cloudCore", "service")
	if err != nil || !ok {
		klog.Info("edgeruntime.kubeedge.cloudCore.cloudHub is not found.")
		serviceConfig = map[string]interface{}{}
	}
	unstructured.SetNestedMap(config, serviceConfig, "cloudcore", "cloudCore", "service")

	return config
}

func (i *upgradeJob) createInstallPlan(ctx context.Context, watchFuncs ...core.WatchFunc) error {
	cc, err := i.coreHelper.GetClusterConfiguration(ctx)
	if err != nil {
		return err
	}
	var clusterScheduling []string
	if i.extensionRef != nil {
		if i.extensionRef.ClusterScheduling == nil {
			var clusterList v4clusterv1alpha1.ClusterList
			err = i.coreHelper.ListCluster(ctx, &clusterList)
			if err != nil {
				return err
			}
			for _, cluster := range clusterList.Items {
				var currentcc map[string]interface{}
				if !isHostCluster(cluster) {
					continue
				}
				currentcc = cc

				// upgrade will creat agent on the cluster which is in the clusterScheduling array
				//if i.kubeEdgeEnabled(currentcc) {
				//	clusterScheduling = append(clusterScheduling, cluster.Name)
				//}

				hostConfig := i.getKubeEdgeInstallPlanConfig(currentcc)
				marshal, err := yaml.Marshal(hostConfig)
				if err != nil {
					return err
				}
				i.extensionRef.Config = string(marshal)
			}
			i.extensionRef.ClusterScheduling = &v4corev1alpha1.ClusterScheduling{
				Placement: &v4corev1alpha1.Placement{
					Clusters: clusterScheduling,
				},
			}
		}

		err = i.coreHelper.CreateInstallPlanFromExtensionRef(ctx, i.extensionRef, watchFuncs...)
		if err != nil && !k8serrors.IsAlreadyExists(err) {
			return err
		}
	}
	return nil
}

func isHostCluster(cluster v4clusterv1alpha1.Cluster) bool {
	if cluster.Labels == nil {
		return false
	}
	if _, ok := cluster.Labels[v4clusterv1alpha1.HostCluster]; ok {
		return true
	}
	return false
}

func getClusterConfiguration(ctx context.Context, cluster v4clusterv1alpha1.Cluster) (map[string]interface{}, error) {
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(cluster.Spec.Connection.KubeConfig)
	if err != nil {
		return nil, err
	}
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %s", err)
	}
	unstructed, err := dynamicClient.Resource(core.ClusterConfigurationGVR).Namespace(core.KubeSphereNamespace).Get(ctx, core.KubeSphereInstallerName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return unstructed.Object, nil
}

var _ executor.InjectClientV3 = &upgradeJob{}
var _ executor.InjectClientV4 = &upgradeJob{}

func (i *upgradeJob) InjectClientV3(client runtimeclient.Client) {
	i.clientV3 = client
}

func (i *upgradeJob) InjectClientV4(client runtimeclient.Client) {
	i.clientV4 = client
}

var _ executor.InjectResourceStore = &upgradeJob{}

func (i *upgradeJob) InjectResourceStore(store store.ResourceStore) {
	i.resourceStore = store
}

func (i *upgradeJob) InjectHelmClient(client helm.HelmClient) {
	i.helmClient = client
}

func (i *upgradeJob) InjectModelHelperFactory(factory helper.ModelHelperFactory) {
	i.coreHelper = factory.CoreHelper()
}
