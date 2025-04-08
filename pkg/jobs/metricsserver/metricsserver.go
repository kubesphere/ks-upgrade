package metricsserver

import (
	"context"
	"errors"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	v4clusterv1alpha1 "kubesphere.io/ks-upgrade/v4/api/cluster/v1alpha1"
	v4corev1alpha1 "kubesphere.io/ks-upgrade/v4/api/core/v1alpha1"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"kubesphere.io/ks-upgrade/pkg/executor"
	"kubesphere.io/ks-upgrade/pkg/helm"
	"kubesphere.io/ks-upgrade/pkg/model"
	"kubesphere.io/ks-upgrade/pkg/model/core"
	"kubesphere.io/ks-upgrade/pkg/model/helper"
	"kubesphere.io/ks-upgrade/pkg/storage"
	"kubesphere.io/ks-upgrade/pkg/store"
)

func init() {
	runtime.Must(executor.Register(&factory{}))
}

const (
	jobName     = "metrics-server"
	jobDone     = jobName + "-done"
	optionRerun = "rerun"
)

type gvkObjects struct {
	gvk     metav1.GroupVersionKind
	objects []runtimeclient.Object
}

var metricsServerGVKObjects = []gvkObjects{
	{
		metav1.GroupVersionKind{Group: "apiregistration.k8s.io", Version: "v1", Kind: "APIService"},
		[]runtimeclient.Object{
			&apiregistrationv1.APIService{
				ObjectMeta: metav1.ObjectMeta{Name: "v1beta1.metrics.k8s.io"},
			},
		},
	},
	{
		metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "ServiceAccount"},
		[]runtimeclient.Object{
			&corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Name: "metrics-server", Namespace: "kube-system"},
			},
		},
	},
	{
		metav1.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"},
		[]runtimeclient.Object{
			&rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{Name: "system:aggregated-metrics-reader"},
			},
			&rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{Name: "system:metrics-server"},
			},
		},
	},
	{
		metav1.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRoleBinding"},
		[]runtimeclient.Object{
			&rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "metrics-server:system:auth-delegator"},
			},
			&rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "metrics-server:system:metrics-server"},
			},
			&rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "system:metrics-server"},
			},
		},
	},
	{
		metav1.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "RoleBinding"},
		[]runtimeclient.Object{
			&rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "metrics-server-auth-reader", Namespace: "kube-system"},
			},
		},
	},
	{
		metav1.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
		[]runtimeclient.Object{
			&appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "metrics-server", Namespace: "kube-system"},
			},
		},
	},
	{
		metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "Service"},
		[]runtimeclient.Object{

			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "metrics-server", Namespace: "kube-system"},
			},
		},
	},
}

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

	// before upgrade, check if the extension is installed
	cc, err := i.coreHelper.GetClusterConfiguration(ctx)
	if err != nil {
		return err
	}
	metricsServerEnabled, _, err := unstructured.NestedBool(cc, "spec", "metrics_server", "enabled")
	if err != nil {
		return err
	}
	if !metricsServerEnabled {
		return nil
	}

	// backup metrics-server runtime objects
	for _, gvkObject := range metricsServerGVKObjects {
		gvk := gvkObject.gvk
		for _, obj := range gvkObject.objects {
			err = i.clientV3.Get(ctx, runtimeclient.ObjectKeyFromObject(obj), obj)
			if err == nil {
				obj.GetObjectKind().SetGroupVersionKind(schema.GroupVersionKind(gvk))
				klog.Infof("%s %s detected in namespace %s", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName(), obj.GetNamespace())
				fileName := fmt.Sprintf("%s-%s-%s-%s.json", jobName, obj.GetObjectKind().GroupVersionKind().Kind, obj.GetNamespace(), obj.GetName())
				err = i.resourceStore.Save(fileName, obj)
				if err != nil {
					return err
				}

			} else if !k8serrors.IsNotFound(err) {
				return err
			}
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

	cc, err := i.coreHelper.GetClusterConfiguration(ctx)
	if err != nil {
		return err
	}
	metricsServerEnabled, _, err := unstructured.NestedBool(cc, "spec", "metrics_server", "enabled")
	if err != nil {
		return err
	}
	if metricsServerEnabled {
		klog.Infof("uninstall metrics-server")
		for _, gvkObjects := range metricsServerGVKObjects {
			for _, obj := range gvkObjects.objects {
				err = i.clientV4.Delete(ctx, obj)
				if err != nil && !k8serrors.IsNotFound(err) {
					return err
				}
			}
		}
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
	}

	// save job done time
	date = []byte(time.Now().UTC().String())
	klog.Infof("save data key: %s value: %s", jobDone, date)
	err = i.resourceStore.SaveRaw(jobDone, date)
	return err
}

func (i *upgradeJob) createInstallPlan(ctx context.Context) error {
	// create extension install plan
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
				metricsServerEnabled, _, err := unstructured.NestedBool(currentcc, "spec", "metrics_server", "enabled")
				if err != nil {
					return err
				}
				if metricsServerEnabled {
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
