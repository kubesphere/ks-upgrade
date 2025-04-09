package opensearch

import (
	"context"
	"errors"
	"fmt"
	v4clusterv1alpha1 "kubesphere.io/ks-upgrade/v4/api/cluster/v1alpha1"
	v4corev1alpha1 "kubesphere.io/ks-upgrade/v4/api/core/v1alpha1"
	"strconv"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
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
	"sigs.k8s.io/yaml"
)

func init() {
	runtime.Must(executor.Register(&factory{}))
}

const (
	jobName     = "opensearch"
	jobDone     = jobName + "-done"
	optionRerun = "rerun"

	opensearchEnabled          = "opensearch_enabled"
	opensearchCuratorEnabled   = "opensearch_curator_enabled"
	opensearchDashboardEnabled = "opensearch_dashboards_enabled"
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
			klog.Infof("Delete data for key %s", jobDone)
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
		klog.Infof("Job %s already done at %s", jobName, date)
		return nil
	}

	// check if host cluster,if not, skip upgrading
	isHost, err := i.coreHelper.IsHostCluster(ctx)
	if err != nil {
		return err
	}
	if !isHost {
		klog.Infof("Not the host cluster, skip upgrading")
		return nil
	}

	// check if opensearch already installed
	klog.Infof("Check if opensearch already installed")

	helmlist, err := i.helmClient.ListReleases("kubesphere-logging-system")
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}
	if helmlist == nil {
		klog.Infof("Opensearch not installed")
		return nil
	}

	master, data, curator, dashboard := false, false, false, false

	for _, release := range helmlist {
		switch release.Name {
		case "opensearch-master":
			master = true
		case "opensearch-data":
			data = true
		case "opensearch-logging-curator":
			curator = true
		case "opensearch-dashboard":
			dashboard = true
		}
	}

	if master == false || data == false {
		klog.Infof("Opensearch not installed")
		if err = i.resourceStore.SaveRaw(opensearchEnabled, []byte("false")); err != nil {
			return err
		}
		return nil
	}

	if err = i.resourceStore.SaveRaw(opensearchEnabled, []byte("true")); err != nil {
		return err
	}

	//If exist, remove old opensearch
	_, err = i.helmClient.Uninstall("kubesphere-logging-system", "opensearch-master")
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	_, err = i.helmClient.Uninstall("kubesphere-logging-system", "opensearch-data")
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	if curator != false {
		_, err = i.helmClient.Uninstall("kubesphere-logging-system", "opensearch-logging-curator")
		if err != nil && !k8serrors.IsNotFound(err) {
			return err
		}
		if err = i.resourceStore.SaveRaw(opensearchCuratorEnabled, []byte("true")); err != nil {
			return err
		}
	} else {
		if err = i.resourceStore.SaveRaw(opensearchCuratorEnabled, []byte("false")); err != nil {
			return err
		}
	}

	if dashboard != false {
		_, err = i.helmClient.Uninstall("kubesphere-logging-system", "opensearch-dashboard")
		if err != nil && !k8serrors.IsNotFound(err) {
			return err
		}
		if err = i.resourceStore.SaveRaw(opensearchDashboardEnabled, []byte("true")); err != nil {
			return err
		}
	} else {
		if err = i.resourceStore.SaveRaw(opensearchDashboardEnabled, []byte("false")); err != nil {
			return err
		}
	}
	return nil
}

func (i *upgradeJob) PostUpgrade(ctx context.Context) error {
	var err error

	// check if job needs re-run
	if i.options != nil {
		value := i.options[optionRerun]
		if fmt.Sprint(value) == "true" {
			klog.Infof("Delete data for key %s", jobDone)
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
		klog.Infof("Failed to load data key: %s", jobDone)
		return err
	}
	if string(date) != "" {
		klog.Infof("Job %s already done at %s", jobName, date)
		return nil
	}

	isHost, err := i.coreHelper.IsHostCluster(ctx)
	if err != nil {
		return err
	}
	if !isHost {
		klog.Infof("Not the host cluster, skip upgrading")
		return nil
	}

	opensearchEnable, err := i.resourceStore.LoadRaw(opensearchEnabled)
	if err != nil {
		return err
	}
	//Determine if opensearch was installed before, if not, skip it.
	if string(opensearchEnable) == "false" {
		klog.Infof("Opensearch not installed")
		return nil
	}

	var clusterScheduling []string
	if i.extensionRef != nil {
		if i.extensionRef.ClusterScheduling == nil {
			var clusterList v4clusterv1alpha1.ClusterList
			err = i.clientV4.List(ctx, &clusterList)
			if err != nil {
				return err
			}
			for _, cluster := range clusterList.Items {
				if isHostCluster(cluster) {
					clusterScheduling = append(clusterScheduling, cluster.Name)
				}
			}
			i.extensionRef.ClusterScheduling = &v4corev1alpha1.ClusterScheduling{
				Placement: &v4corev1alpha1.Placement{
					Clusters: clusterScheduling,
				},
			}
		}

		err = i.createConfig()
		if err != nil {
			return err
		}
		// create extension install plan
		klog.Infof("Create install plan for extension %s", i.extensionRef.Name)
		err = i.coreHelper.CreateInstallPlanFromExtensionRef(ctx, i.extensionRef)
		if err != nil && !k8serrors.IsAlreadyExists(err) {
			return err
		}
	}

	// save job done time
	date = []byte(time.Now().UTC().String())
	klog.Infof("Save data key: %s value: %s", jobDone, date)
	err = i.resourceStore.SaveRaw(jobDone, date)
	return err
}

func parseToBool(b []byte) (bool, error) {
	str := string(b)
	boolValue, err := strconv.ParseBool(str)
	if err != nil {
		fmt.Println("Error converting to bool:", err)
		return false, err
	}
	return boolValue, nil
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

func (i *upgradeJob) createConfig() error {
	curatorEnable, err := i.resourceStore.LoadRaw(opensearchCuratorEnabled)
	if err != nil {
		return err
	}
	dashboardEnable, err := i.resourceStore.LoadRaw(opensearchDashboardEnabled)
	if err != nil {
		return err
	}
	configMap := make(map[string]interface{})
	dashboardEnableBool, err := parseToBool(dashboardEnable)
	if err != nil {
		return err
	}
	curatorEnableBool, err := parseToBool(curatorEnable)
	if err != nil {
		return err
	}
	if i.extensionRef.Config != "" {
		err := yaml.Unmarshal([]byte(i.extensionRef.Config), &configMap)
		if err != nil {
			fmt.Println(" parsing YAML data error:", err)
			return err
		}
		_, ok := configMap["opensearch-dashboards"]
		if !ok {
			configMap["opensearch-dashboards"] = map[string]bool{"enabled": dashboardEnableBool}
		}
		_, curatorOK := configMap["opensearch-curator"]
		if !curatorOK {
			configMap["opensearch-curator"] = map[string]bool{"enabled": curatorEnableBool}
		}
	} else {
		configMap["opensearch-dashboards"] = map[string]bool{"enabled": dashboardEnableBool}
		configMap["opensearch-curator"] = map[string]bool{"enabled": curatorEnableBool}
	}

	var valuesBytes []byte
	if valuesBytes, err = yaml.Marshal(configMap); err != nil {
		return err
	}
	i.extensionRef.Config = string(valuesBytes)
	return nil
}
