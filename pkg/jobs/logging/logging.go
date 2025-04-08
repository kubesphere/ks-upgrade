package notificationhistory

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tidwall/sjson"
	"helm.sh/helm/v3/pkg/storage/driver"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
	v4clusterv1alpha1 "kubesphere.io/ks-upgrade/v4/api/cluster/v1alpha1"
	v4corev1alpha1 "kubesphere.io/ks-upgrade/v4/api/core/v1alpha1"
	"sigs.k8s.io/yaml"

	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"
	"kubesphere.io/ks-upgrade/pkg/executor"
	"kubesphere.io/ks-upgrade/pkg/helm"
	"kubesphere.io/ks-upgrade/pkg/model"
	"kubesphere.io/ks-upgrade/pkg/model/core"
	"kubesphere.io/ks-upgrade/pkg/model/helper"
	"kubesphere.io/ks-upgrade/pkg/storage"
	"kubesphere.io/ks-upgrade/pkg/store"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func init() {
	runtime.Must(executor.Register(&factory{}))
}

const (
	jobName     = "whizard-logging"
	jobDone     = jobName + "-done"
	optionRerun = "rerun"
)

var _ executor.UpgradeJob = &upgradeJob{}
var _ executor.InjectClientV3 = &upgradeJob{}
var _ executor.InjectClientV4 = &upgradeJob{}
var _ executor.InjectResourceStore = &upgradeJob{}
var _ executor.InjectModelHelperFactory = &upgradeJob{}

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

func (i *upgradeJob) InjectHelmClient(client helm.HelmClient) {
	i.helmClient = client
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
		klog.Infof("failed to load data key: %s", jobDone)
		return err
	}
	if string(date) != "" {
		klog.Infof("job %s already done at %s", jobName, date)
		return nil
	}

	cc, err := i.coreHelper.GetClusterConfiguration(ctx)
	if err != nil {
		return err
	}

	loggingsidecarEnabled, ok, err := unstructured.NestedBool(cc, "spec", "logging", "logsidecar", "enabled")
	if err == nil && ok && loggingsidecarEnabled {
		if err = i.resourceStore.SaveRaw("logging_logsidecar_enabled", []byte("true")); err != nil {
			return err
		}
		_, err = i.helmClient.Uninstall("kubesphere-logging-system", "logsidecar-injector")
		if err != nil && !errors.Is(err, driver.ErrReleaseNotFound) {
			return err
		}
	}

	// create extension install plan
	klog.Infof("create install plan for extension %s", i.extensionRef.Name)
	isHost, err := i.coreHelper.IsHostCluster(ctx)
	if err != nil {
		return err
	}
	if isHost {
		err = i.createInstallPlan(ctx)
		if err != nil {
			return err
		}
	}

	// save job done time
	date = []byte(time.Now().UTC().String())
	klog.Infof("save data key: %s value: %s", jobDone, date)
	err = i.resourceStore.SaveRaw(jobDone, date)
	return err
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

func (i *upgradeJob) createInstallPlan(ctx context.Context) error {

	cc, err := i.coreHelper.GetClusterConfiguration(ctx)
	if err != nil {
		return err
	}
	var clusterScheduling []string
	if i.extensionRef != nil {
		if i.extensionRef.ClusterScheduling == nil {
			i.extensionRef.ClusterScheduling = &v4corev1alpha1.ClusterScheduling{
				Overrides: make(map[string]string),
				Placement: &v4corev1alpha1.Placement{},
			}
			var clusterList v4clusterv1alpha1.ClusterList
			err = i.coreHelper.ListCluster(ctx, &clusterList)
			if err != nil {
				return err
			}
			for _, cluster := range clusterList.Items {
				var currentcc map[string]interface{}
				var pathValues []pathValue
				if isHostCluster(cluster) {
					currentcc = cc
				} else {
					currentcc, err = getClusterConfiguration(ctx, cluster)
					if err != nil {
						return err
					}
				}
				loggingEnabled, _, err := unstructured.NestedBool(currentcc, "spec", "logging", "enabled")
				if err != nil {
					return err
				}
				loggingsidecarEnabled, _, err := unstructured.NestedBool(currentcc, "spec", "logging", "logsidecar", "enabled")
				if err != nil {
					return err
				}
				if loggingEnabled {
					clusterScheduling = append(clusterScheduling, cluster.Name)
				}
				if loggingsidecarEnabled {
					pathValues = append(pathValues, pathValue{path: "logsidecar-injector.enabled", value: true})
					overrideConfig := i.extensionRef.ClusterScheduling.Overrides[cluster.Name]
					if len(pathValues) > 0 {
						overrideConfig, err := setYamlValues(overrideConfig, pathValues)
						if err != nil {
							return err
						}
						i.extensionRef.ClusterScheduling.Overrides[cluster.Name] = overrideConfig
					}
				}
			}
			i.extensionRef.ClusterScheduling.Placement.Clusters = clusterScheduling
		}
		err = i.coreHelper.CreateInstallPlanFromExtensionRef(ctx, i.extensionRef)
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

type pathValue struct {
	path  string
	value interface{}
}

func setYamlValues(yamlString string, pathValues []pathValue) (string, error) {
	jsonbytes, err := yaml.YAMLToJSON([]byte(yamlString))
	if err != nil {
		return yamlString, err
	}

	for _, v := range pathValues {
		jsonbytes, err = sjson.SetBytes(jsonbytes, v.path, v.value)
		if err != nil {
			return yamlString, err
		}
	}

	yamlbytes, err := yaml.JSONToYAML(jsonbytes)
	if err != nil {
		return yamlString, err
	}

	return string(yamlbytes), nil
}
