package vector

import (
	"context"
	"errors"
	"fmt"
	metaerrors "k8s.io/apimachinery/pkg/api/meta"
	"time"

	v12 "k8s.io/api/apps/v1"
	v13 "k8s.io/api/core/v1"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
	v4clusterv1alpha1 "kubesphere.io/ks-upgrade/v4/api/cluster/v1alpha1"
	v4corev1alpha1 "kubesphere.io/ks-upgrade/v4/api/core/v1alpha1"

	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"
	"kubesphere.io/fluentbit-operator/api/fluentbitoperator/v1alpha2"
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
	jobName                 = "vector"
	jobDone                 = jobName + "-done"
	optionRerun             = "rerun"
	FilterResource          = "logging-filter"
	ParserResource          = "logging-parser"
	OutputResource          = "logging-output"
	InputResource           = "logging-input"
	FluentBitResource       = "logging-fluentbit"
	FluentBitConfigResource = "logging-fluentbitconfig"

	FilterCrdName          = "filters.logging.kubesphere.io"
	ParseCrdName           = "parsers.logging.kubesphere.io"
	OutputCrdName          = "outputs.logging.kubesphere.io"
	InputCrdName           = "inputs.logging.kubesphere.io"
	FluentBitCrdName       = "fluentbits.logging.kubesphere.io"
	FluentBitConfigCrdName = "fluentbitconfigs.logging.kubesphere.io"
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

	// backup fluentbit cr
	klog.Info("backup fluentbit crd...")
	err = i.backupResource(ctx)
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
		klog.Infof("failed to load data key: %s", jobDone)
		return err
	}
	if string(date) != "" {
		klog.Infof("job %s already done at %s", jobName, date)
		return nil
	}

	err = i.uninstallCrd(ctx)
	if err != nil {
		return err
	}

	for j := 0; j < 20; j++ {
		err = i.clientV3.Get(ctx, types.NamespacedName{
			Namespace: "kubesphere-logging-system",
			Name:      "fluent-bit",
		}, &v12.DaemonSet{})
		if err != nil && !k8serrors.IsNotFound(err) {
			return err
		}
		if k8serrors.IsNotFound(err) {
			break
		}
		klog.Info("Wait for the operator to delete fluent-bit")
		time.Sleep(time.Second * 10)
	}

	err = i.clientV3.Delete(ctx, &v12.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fluentbit-operator",
			Namespace: "kubesphere-logging-system",
		},
	}, &runtimeclient.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	isHost, err := i.coreHelper.IsHostCluster(ctx)
	if err != nil {
		return err
	}
	if isHost {
		err = i.createInstallPlan(ctx)
		if err != nil {
			return err
		}

		for j := 0; j < 10; j++ {
			err = i.clientV3.Get(ctx, types.NamespacedName{Name: "vector-sinks", Namespace: "kubesphere-logging-system"}, &v13.Secret{}, &runtimeclient.GetOptions{})
			if err != nil && !k8serrors.IsNotFound(err) {
				return err
			}
			if k8serrors.IsNotFound(err) {
				time.Sleep(5 * time.Second)
				continue
			}
			break
		}

	}

	// save job done time
	date = []byte(time.Now().UTC().String())
	klog.Infof("save data key: %s value: %s", jobDone, date)
	err = i.resourceStore.SaveRaw(jobDone, date)
	return err
}

func (i *upgradeJob) backupResource(ctx context.Context) error {

	err := i.clientV3.Get(ctx, types.NamespacedName{Name: FilterCrdName}, &v1.CustomResourceDefinition{}, &runtimeclient.GetOptions{})
	if err == nil {
		// backup filter
		klog.Info("backup filter...")
		filterList := &v1alpha2.FilterList{}
		err = i.clientV3.List(ctx, filterList)
		if err != nil && (!k8serrors.IsNotFound(err) || !metaerrors.IsNoMatchError(err)) {
			klog.Errorf("failed to list filter, err: %v", err)
			return err
		}
		if len(filterList.Items) > 0 {
			klog.Info("backup filter")
			err = i.resourceStore.Save(FilterResource, filterList)
			if err != nil {
				return err
			}
		}
	}

	err = i.clientV3.Get(ctx, types.NamespacedName{Name: ParseCrdName}, &v1.CustomResourceDefinition{})
	if err == nil {
		// backup parser
		klog.Info("backup parser...")
		parserList := &v1alpha2.ParserList{}
		err = i.clientV3.List(ctx, parserList)
		if err != nil && !k8serrors.IsNotFound(err) && !metaerrors.IsNoMatchError(err) {
			klog.Errorf("failed to list parser, err: %v", err)
			return err
		}
		if len(parserList.Items) > 0 {
			klog.Info("backup parser")
			err = i.resourceStore.Save(ParserResource, parserList)
			if err != nil {
				return err
			}
		}
	}

	err = i.clientV3.Get(ctx, types.NamespacedName{Name: InputCrdName}, &v1.CustomResourceDefinition{})
	if err == nil {
		// backup input
		klog.Info("backup input...")
		inputList := &v1alpha2.InputList{}
		err = i.clientV3.List(ctx, inputList)
		if err != nil && !k8serrors.IsNotFound(err) && !metaerrors.IsNoMatchError(err) {
			klog.Errorf("failed to list input, err: %v", err)
			return err
		}
		if len(inputList.Items) > 0 {
			klog.Info("backup input")
			err = i.resourceStore.Save(InputResource, inputList)
			if err != nil {
				return err
			}
		}
	}

	err = i.clientV3.Get(ctx, types.NamespacedName{Name: OutputCrdName}, &v1.CustomResourceDefinition{}, &runtimeclient.GetOptions{})
	if err == nil {
		// backup output
		klog.Info("backup output...")
		outputList := &v1alpha2.OutputList{}
		err = i.clientV3.List(ctx, outputList)
		if err != nil && !k8serrors.IsNotFound(err) && !metaerrors.IsNoMatchError(err) {
			klog.Errorf("failed to list output, err: %v", err)
			return err
		}
		if len(outputList.Items) > 0 {
			klog.Info("backup output")
			err = i.resourceStore.Save(OutputResource, outputList)
			if err != nil {
				return err
			}
		}
	}

	err = i.clientV3.Get(ctx, types.NamespacedName{Name: FluentBitCrdName}, &v1.CustomResourceDefinition{}, &runtimeclient.GetOptions{})
	if err == nil {
		// backup fluentbit
		fluentbitList := &v1alpha2.FluentBitList{}
		err = i.clientV3.List(ctx, fluentbitList)
		if err != nil && !k8serrors.IsNotFound(err) && !metaerrors.IsNoMatchError(err) {
			klog.Errorf("failed to list fluentbit, err: %v", err)
			return err
		}
		if len(fluentbitList.Items) > 0 {
			klog.Info("backup fluentbit...")
			err = i.resourceStore.Save(FluentBitResource, fluentbitList)
			if err != nil {
				return err
			}
		}
	}

	err = i.clientV3.Get(ctx, types.NamespacedName{Name: FluentBitConfigCrdName}, &v1.CustomResourceDefinition{}, &runtimeclient.GetOptions{})
	if err == nil {
		// backup fluentbitconfig
		klog.Info("backup fluentbitconfig...")
		fluentbitConfigList := &v1alpha2.FluentBitConfigList{}
		err = i.clientV3.List(ctx, fluentbitConfigList)
		if err != nil && !k8serrors.IsNotFound(err) && !metaerrors.IsNoMatchError(err) {
			klog.Errorf("failed to list fluentbitconfig, err: %v", err)
			return err
		}
		if len(fluentbitConfigList.Items) > 0 {
			klog.Info("backup fluentbitconfig")
			err = i.resourceStore.Save(FluentBitConfigResource, fluentbitConfigList)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (i *upgradeJob) removeResource(ctx context.Context) error {
	klog.Info("delete fluent-bit-config...")
	err := i.clientV3.Delete(ctx, &v1alpha2.FluentBitConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fluent-bit-config",
			Namespace: "kubesphere-logging-system",
		},
	}, &runtimeclient.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	klog.Info("delete fluent-bit...")
	err = i.clientV3.Delete(ctx, &v1alpha2.FluentBit{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fluent-bit",
			Namespace: "kubesphere-logging-system",
		},
	}, &runtimeclient.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}
	return nil
}

func (i *upgradeJob) uninstallCrd(ctx context.Context) error {
	var crds = []string{ParseCrdName, FilterCrdName, InputCrdName, OutputCrdName, FluentBitCrdName, FluentBitConfigCrdName}
	for _, crd := range crds {
		klog.Infof("delete crd %s", crd)
		err := i.clientV3.Delete(ctx, &v1.CustomResourceDefinition{
			ObjectMeta: metav1.ObjectMeta{
				Name: crd,
			},
		}, &runtimeclient.DeleteOptions{})
		if err != nil && !k8serrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (i *upgradeJob) createInstallPlan(ctx context.Context) error {
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
				if loggingEnabled {
					clusterScheduling = append(clusterScheduling, cluster.Name)
				}
			}
			i.extensionRef.ClusterScheduling = &v4corev1alpha1.ClusterScheduling{
				Placement: &v4corev1alpha1.Placement{
					Clusters: clusterScheduling,
				},
			}
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
