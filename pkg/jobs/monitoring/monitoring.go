package monitoring

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"helm.sh/helm/v3/pkg/storage/driver"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

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
	jobName = "whizard-monitoring"
	jobDone = jobName + "-done"

	optionRerun      = "rerun"
	optionUpdateCrds = "updateCrds"

	MonitoringNamespace = "kubesphere-monitoring-system"

	KubeSystemNamespace = "kube-system"
)

var (
	prometheusesv1GVR = monitoringv1.SchemeGroupVersion.WithResource("prometheuses")
	prometheusesv1    = []runtimeclient.Object{
		&monitoringv1.Prometheus{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "k8s"}},
	}

	alertmanagersv1GVR = monitoringv1.SchemeGroupVersion.WithResource("alertmanagers")
	alertmanagersv1    = []runtimeclient.Object{
		&monitoringv1.Alertmanager{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "main"}},
	}

	thanosrulersv1GVR = monitoringv1.SchemeGroupVersion.WithResource("thanosrulers")
	thanosrulersv1    = []runtimeclient.Object{
		&monitoringv1.ThanosRuler{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "kubesphere"}},
	}

	servicemonitorsv1GVR = monitoringv1.SchemeGroupVersion.WithResource("servicemonitors")
	servicemonitorsv1    = []runtimeclient.Object{
		&monitoringv1.ServiceMonitor{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "alertmanager-main"}},
		&monitoringv1.ServiceMonitor{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "coredns"}},
		&monitoringv1.ServiceMonitor{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "etcd"}},
		&monitoringv1.ServiceMonitor{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "ks-apiserver"}},
		&monitoringv1.ServiceMonitor{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "ks-controller-manager"}},
		&monitoringv1.ServiceMonitor{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "kube-apiserver"}},
		&monitoringv1.ServiceMonitor{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "kube-controller-manager"}},
		&monitoringv1.ServiceMonitor{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "kube-scheduler"}},
		&monitoringv1.ServiceMonitor{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "kube-state-metrics"}},
		&monitoringv1.ServiceMonitor{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "kubelet"}},
		&monitoringv1.ServiceMonitor{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "node-exporter"}},
		&monitoringv1.ServiceMonitor{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "prometheus-k8s"}},
		&monitoringv1.ServiceMonitor{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "prometheus-operator"}},
		&monitoringv1.ServiceMonitor{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "thanos-ruler-service"}},
	}

	prometheusrulesv1GVR = monitoringv1.SchemeGroupVersion.WithResource("prometheusrules")
	prometheusrulesv1    = []runtimeclient.Object{
		&monitoringv1.PrometheusRule{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "alertmanager-main-rules"}},
		&monitoringv1.PrometheusRule{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "kube-prometheus-rules"}},
		&monitoringv1.PrometheusRule{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "kube-state-metrics-rules"}},
		&monitoringv1.PrometheusRule{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "node-exporter-rules"}},
		&monitoringv1.PrometheusRule{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "prometheus-k8s-etcd-rules"}},
		&monitoringv1.PrometheusRule{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "prometheus-k8s-prometheus-rules"}},
		&monitoringv1.PrometheusRule{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "prometheus-k8s-rules"}},
		&monitoringv1.PrometheusRule{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "prometheus-operator-rules"}},
		&monitoringv1.PrometheusRule{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "thanos-ruler-kubesphere-rules"}},
	}

	configmapsGVR = corev1.SchemeGroupVersion.WithResource("configmaps")
	configmaps    = []runtimeclient.Object{
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "grafana-dashboard-alertmanager-overview"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "grafana-dashboard-apiserver"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "grafana-dashboard-cluster-total"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "grafana-dashboard-controller-manager"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "grafana-dashboard-k8s-resources-cluster"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "grafana-dashboard-k8s-resources-namespace"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "grafana-dashboard-k8s-resources-node"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "grafana-dashboard-k8s-resources-pod"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "grafana-dashboard-k8s-resources-workload"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "grafana-dashboard-k8s-resources-workloads-namespace"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "grafana-dashboard-kubelet"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "grafana-dashboard-namespace-by-pod"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "grafana-dashboard-namespace-by-workload"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "grafana-dashboard-node-cluster-rsrc-use"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "grafana-dashboard-node-rsrc-use"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "grafana-dashboard-nodes"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "grafana-dashboard-persistentvolumesusage"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "grafana-dashboard-pod-total"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "grafana-dashboard-prometheus-remote-write"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "grafana-dashboard-prometheus"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "grafana-dashboard-proxy"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "grafana-dashboard-scheduler"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "grafana-dashboard-workload-total"}},
	}

	secretsGVR = corev1.SchemeGroupVersion.WithResource("secrets")
	secrets    = []runtimeclient.Object{
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "alertmanager-main"}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "grafana-config"}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "grafana-datasources"}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "prometheus-k8s-tls"}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "prometheus-operator"}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "thanos-ruler-kubesphere"}},
	}

	clusterrolesGVR = rbacv1.SchemeGroupVersion.WithResource("clusterroles")
	clusterroles    = []runtimeclient.Object{
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "kubesphere-kube-state-metrics"}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "kubesphere-prometheus-operator"}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "kubesphere-node-exporter"}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "kubesphere-prometheus-k8s"}},
	}

	clusterrolebindingsGVR = rbacv1.SchemeGroupVersion.WithResource("clusterrolebindings")
	clusterrolebindings    = []runtimeclient.Object{
		&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "kubesphere-kube-state-metrics"}},
		&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "kubesphere-prometheus-operator"}},
		&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "kubesphere-node-exporter"}},
		&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "kubesphere-prometheus-k8s"}},
	}

	serviceaccountsGVR = corev1.SchemeGroupVersion.WithResource("serviceaccounts")
	serviceaccounts    = []runtimeclient.Object{
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "alertmanager-main"}},
		// &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "ks-blackbox-prometheus-blackbox-exporter"}}, // ignoring the chart resources
		// &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "ks-process-prometheus-process-exporter"}}, // ignoring the chart resources
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "kube-state-metrics"}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "node-exporter"}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "prometheus-k8s"}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "prometheus-operator"}},
	}

	servicesGVR = corev1.SchemeGroupVersion.WithResource("services")
	services    = []runtimeclient.Object{
		// in monitoring namespace
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "alertmanager-main"}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "grafana"}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "kube-state-metrics"}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "node-exporter"}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "prometheus-k8s"}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "prometheus-operator"}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "whizard-agent-proxy"}},
		// in kube-system namespace
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: KubeSystemNamespace, Name: "kube-controller-manager-svc"}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: KubeSystemNamespace, Name: "kube-scheduler-svc"}},
	}

	daemonsetsGVR = corev1.SchemeGroupVersion.WithResource("daemonsets")
	daemonsets    = []runtimeclient.Object{
		&appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "node-exporter"}},
	}

	statefulsetsGVR = corev1.SchemeGroupVersion.WithResource("statefulsets")
	statefulsets    = []runtimeclient.Object{}

	deploymentsGVR = corev1.SchemeGroupVersion.WithResource("deployments")
	deployments    = []runtimeclient.Object{
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "kube-state-metrics"}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "prometheus-operator"}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "grafana"}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "whizard-agent-proxy"}},
	}

	poddisruptionbudgetsv1GVR = policyv1.SchemeGroupVersion.WithResource("poddisruptionbudgets")
	poddisruptionbudgetsv1    = []runtimeclient.Object{
		&policyv1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "alertmanager-main"}},
		&policyv1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "prometheus-k8s"}},
		&policyv1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "thanos-ruler-kubesphere"}},
	}
	poddisruptionbudgetsv1beta1GVR = policyv1beta1.SchemeGroupVersion.WithResource("poddisruptionbudgets")
	poddisruptionbudgetsv1beta1    = []runtimeclient.Object{
		&policyv1beta1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "alertmanager-main"}},
		&policyv1beta1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "prometheus-k8s"}},
		&policyv1beta1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Namespace: MonitoringNamespace, Name: "thanos-ruler-kubesphere"}},
	}
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
		log:          klog.NewKlogr().WithName(jobName),
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

	log klog.Logger
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

	// check if job needs re-run
	if i.options != nil {
		value := i.options[optionRerun]
		if fmt.Sprint(value) == "true" {
			i.log.Info("delete job data", "key", jobDone)
			err := i.resourceStore.Delete(jobDone)
			if err != nil && !errors.Is(err, storage.BackupKeyNotFound) {
				return err
			}
		}
	}

	// check if job already done
	date, err := i.resourceStore.LoadRaw(jobDone)
	if err != nil && !errors.Is(err, storage.BackupKeyNotFound) {
		return err
	}
	if string(date) != "" {
		i.log.Info("job is already done", "date", date)
		return nil
	}

	// check if monitoring components already installed in ks-installer
	cc, err := i.coreHelper.GetClusterConfiguration(ctx)
	if err != nil {
		return err
	}

	isHost, err := i.coreHelper.IsHostCluster(ctx)
	if err != nil {
		return err
	}

	backupHelmRelease := func(namespace, name string) error {
		fileName := fmt.Sprintf("%s.helm.release-%s.%s", jobName, namespace, name)
		_, err = i.resourceStore.LoadRaw(fileName)
		if err == nil { // The backup has been done
			return nil
		} else if !errors.Is(err, storage.BackupKeyNotFound) {
			return err
		}
		// backup
		release, err := i.helmClient.Status(namespace, name)
		if err == nil {
			releaseBytes, err := json.Marshal(release)
			if err != nil {
				return err
			}
			if err := i.resourceStore.SaveRaw(fileName, releaseBytes); err != nil {
				return err
			}
			return nil
		} else if err == driver.ErrReleaseNotFound { // ignore release not found
			return nil
		}
		return err
	}
	// backup process exporter
	processExporterEnabled, _, err := unstructured.NestedBool(cc, "spec", "monitoring", "process_exporter", "enabled")
	if err != nil {
		return err
	}
	if processExporterEnabled {
		err := backupHelmRelease(MonitoringNamespace, "ks-process")
		if err != nil {
			return err
		}
	}
	// backup calico exporter
	calicoExporterEnabled, _, err := unstructured.NestedBool(cc, "spec", "monitoring", "calico_exporter", "enabled")
	if err != nil {
		return err
	}
	if calicoExporterEnabled {
		err := backupHelmRelease(MonitoringNamespace, "ks-calico")
		if err != nil {
			return err
		}
	}
	// backup blackbox exporter
	// blackBoxExporterEnabled, _, err := unstructured.NestedBool(cc, "spec", "monitoring", "blackbox_exporter", "enabled")
	// if err != nil {
	// 	return err
	// }
	// if blackBoxExporterEnabled {
	// 	err := backupHelmRelease(MonitoringNamespace, "ks-blackbox")
	// 	if err != nil {
	// 		return err
	// 	}
	// }
	// backup whizard
	whizardEnabled, _, err := unstructured.NestedBool(cc, "spec", "monitoring", "whizard", "enabled")
	if err != nil {
		return err
	}
	if whizardEnabled && isHost {
		err := backupHelmRelease(MonitoringNamespace, "whizard")
		if err != nil {
			return err
		}
	}
	// TODO backup gpu-exporter

	// backup prometheus stack
	type gvrObjects struct {
		gvr  schema.GroupVersionResource
		objs []runtimeclient.Object
	}
	var gvrObjectsArray = []gvrObjects{
		{gvr: thanosrulersv1GVR, objs: thanosrulersv1},
		{gvr: alertmanagersv1GVR, objs: alertmanagersv1},
		{gvr: prometheusesv1GVR, objs: prometheusesv1},
		{gvr: daemonsetsGVR, objs: daemonsets},
		{gvr: statefulsetsGVR, objs: statefulsets},
		{gvr: deploymentsGVR, objs: deployments},
		{gvr: servicesGVR, objs: services},
		{gvr: configmapsGVR, objs: configmaps},
		{gvr: secretsGVR, objs: secrets},
		{gvr: prometheusrulesv1GVR, objs: prometheusrulesv1},
		{gvr: servicemonitorsv1GVR, objs: servicemonitorsv1},
		{gvr: clusterrolebindingsGVR, objs: clusterrolebindings},
		{gvr: clusterrolesGVR, objs: clusterroles},
		{gvr: serviceaccountsGVR, objs: serviceaccounts},
		{gvr: poddisruptionbudgetsv1GVR, objs: poddisruptionbudgetsv1},
		{gvr: poddisruptionbudgetsv1beta1GVR, objs: poddisruptionbudgetsv1beta1},
	}
	for _, gvrObjects := range gvrObjectsArray {
		gvr, objs := gvrObjects.gvr, gvrObjects.objs
		gvk, err := i.clientV3.RESTMapper().KindFor(gvr)
		if err != nil {
			if !meta.IsNoMatchError(err) {
				return err
			}
		}
		gvrName := strings.Join([]string{gvr.Group, gvr.Version, gvr.Resource}, ".")
		for _, obj := range objs {
			namespacedName := types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}
			log := i.log.WithValues("gvr", gvr.String(), "name", namespacedName.String())
			fileName := fmt.Sprintf("%s-%s-%s", jobName, gvrName, strings.ReplaceAll(namespacedName.String(), "/", "."))
			err := i.resourceStore.Load(fileName, obj)
			if err == nil {
				continue
			}
			if !errors.Is(err, storage.BackupKeyNotFound) {
				return err
			}
			// backup the objects
			if err := i.clientV3.Get(ctx, namespacedName, obj, &runtimeclient.GetOptions{}); err != nil {
				if apierrors.IsNotFound(err) {
					log.Info("not found")
					continue
				}
				if meta.IsNoMatchError(err) {
					log.Info("no match")
					continue
				}
				return err
			}
			// set GVK into the resources to save
			obj.GetObjectKind().SetGroupVersionKind(gvk)
			if err := i.resourceStore.Save(fileName, obj); err != nil {
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

	isHost, err := i.coreHelper.IsHostCluster(ctx)
	if err != nil {
		return err
	}

	uninstallHelmRelease := func(namespace, name string) error {
		_, err := i.helmClient.Uninstall(namespace, name)
		if errors.Is(err, driver.ErrReleaseNotFound) {
			return nil
		}
		return err
	}
	// uninstall process exporter
	processExporterEnabled, _, err := unstructured.NestedBool(cc, "spec", "monitoring", "process_exporter", "enabled")
	if err != nil {
		return err
	}
	if processExporterEnabled {
		err := uninstallHelmRelease(MonitoringNamespace, "ks-process")
		if err != nil {
			return err
		}
	}
	// uninstall calico exporter
	calicoExporterEnabled, _, err := unstructured.NestedBool(cc, "spec", "monitoring", "calico_exporter", "enabled")
	if err != nil {
		return err
	}
	if calicoExporterEnabled {
		err := uninstallHelmRelease(MonitoringNamespace, "ks-calico")
		if err != nil {
			return err
		}
	}
	// uninstall blackbox exporter
	// blackBoxExporterEnabled, _, err := unstructured.NestedBool(cc, "spec", "monitoring", "blackbox_exporter", "enabled")
	// if err != nil {
	// 	return err
	// }
	// if blackBoxExporterEnabled {
	// 	err := uninstallHelmRelease(MonitoringNamespace, "ks-blackbox")
	// 	if err != nil {
	// 		return err
	// 	}
	// }
	// uninstall whizard
	whizardEnabled, _, err := unstructured.NestedBool(cc, "spec", "monitoring", "whizard", "enabled")
	if err != nil {
		return err
	}
	if whizardEnabled && isHost {
		err := uninstallHelmRelease(MonitoringNamespace, "whizard")
		if err != nil {
			return err
		}
	}
	// TODO uninstall gpu-exporter

	// uninstall prometheus stack
	type gvrObjects struct {
		gvr  schema.GroupVersionResource
		objs []runtimeclient.Object
	}
	var gvrObjectsArray = []gvrObjects{
		// The deletion sequence may be to be optimized
		{gvr: thanosrulersv1GVR, objs: thanosrulersv1},
		{gvr: alertmanagersv1GVR, objs: alertmanagersv1},
		{gvr: prometheusesv1GVR, objs: prometheusesv1},
		{gvr: daemonsetsGVR, objs: daemonsets},
		{gvr: statefulsetsGVR, objs: statefulsets},
		{gvr: deploymentsGVR, objs: deployments},
		{gvr: servicesGVR, objs: services},
		{gvr: configmapsGVR, objs: configmaps},
		{gvr: secretsGVR, objs: secrets},
		{gvr: prometheusrulesv1GVR, objs: prometheusrulesv1},
		{gvr: servicemonitorsv1GVR, objs: servicemonitorsv1},
		{gvr: clusterrolebindingsGVR, objs: clusterrolebindings},
		{gvr: clusterrolesGVR, objs: clusterroles},
		{gvr: serviceaccountsGVR, objs: serviceaccounts},
		{gvr: poddisruptionbudgetsv1GVR, objs: poddisruptionbudgetsv1},
		{gvr: poddisruptionbudgetsv1beta1GVR, objs: poddisruptionbudgetsv1beta1},
	}
	for _, gvrObjects := range gvrObjectsArray {
		gvr, objs := gvrObjects.gvr, gvrObjects.objs
		for _, obj := range objs {
			namespacedName := types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}
			log := i.log.WithValues("gvr", gvr.String(), "name", namespacedName.String())
			log.Info("delete")
			if err := i.clientV3.Delete(ctx, obj); err != nil {
				if apierrors.IsNotFound(err) {
					log.Info("not found")
					continue
				}
				if meta.IsNoMatchError(err) {
					log.Info("no match")
					continue
				}
				return err
			}
		}
	}

	if i.options != nil {
		value := i.options[optionUpdateCrds]
		if fmt.Sprint(value) == "true" && i.extensionRef != nil {
			if err := i.coreHelper.ApplyCRDsFromExtensionRef(ctx, i.extensionRef,
				metav1.ApplyOptions{FieldManager: "kubectl", Force: true},
				func(crd apiextensionsv1.CustomResourceDefinition) bool {
					// If not in host cluster, ignore the whizard crds.
					return !isHost && crd.Spec.Group == "monitoring.whizard.io"
				}); err != nil {
				klog.Errorf("failed to apply crds: %s", err)
				return err
			}
		}
	}

	if isHost && i.extensionRef != nil { // create extension install plan
		i.log.Info("creating InstallPlan")

		extRef := *i.extensionRef

		transPaths := append(whizardTransPaths[:], stackTransPaths...)
		extRef.Config, err = SetExtensionValues(extRef.Config, cc, transPaths)
		if err != nil {
			return err
		}

		if extRef.ClusterScheduling == nil { // Will be generated by cc config in every cluster.

			var clusterList v4clusterv1alpha1.ClusterList
			err = i.coreHelper.ListCluster(ctx, &clusterList)
			if err != nil {
				return err
			}
			var placement v4corev1alpha1.Placement
			var overrides = make(map[string]string)
			var hasSetWhizardGatewayUrl bool
			for _, cluster := range clusterList.Items {
				placement.Clusters = append(placement.Clusters, cluster.Name)

				var currentcc map[string]interface{}
				if isHostCluster(cluster) {
					currentcc = cc
				} else {
					currentcc, err = getClusterConfiguration(ctx, cluster)
					if err != nil {
						return err
					}
				}

				transPaths := stackTransPaths[:]
				currentEtcdEnabled, _, err := unstructured.NestedBool(currentcc, "spec", "etcd", "monitoring")
				if err != nil {
					return err
				}
				if currentEtcdEnabled {
					transPaths = append(transPaths, etcdTransPaths...)
				}

				override, err := SetOverrideValues(overrides[cluster.Name], currentcc, transPaths, extRef.Config)
				if err != nil {
					return err
				}
				overrides[cluster.Name] = override

				// Set whizard gatewayUrl into extRef.Config, because it is same in every member cluster
				if whizardEnabled && !hasSetWhizardGatewayUrl && !isHostCluster(cluster) {
					extRef.Config, err = SetExtensionValues(extRef.Config, currentcc, []TransPath{{
						toPath:   "whizard-agent-proxy.config.gatewayUrl",
						fromPath: "spec.monitoring.whizard.client.gatewayUrl",
					}})
					if err != nil {
						return err
					}
					hasSetWhizardGatewayUrl = true
				}
			}
			extRef.ClusterScheduling = &v4corev1alpha1.ClusterScheduling{Placement: &placement, Overrides: overrides}
		}

		if err = i.coreHelper.CreateInstallPlanFromExtensionRef(ctx, &extRef); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
	}

	if err := i.coreHelper.SyncHelmChart(ctx, i.extensionRef); err != nil {
		return err
	}

	// save job done time
	date = []byte(time.Now().UTC().String())
	klog.Infof("save data key: %s value: %s", jobDone, date)
	err = i.resourceStore.SaveRaw(jobDone, date)
	return err
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

var whizardTransPaths = []TransPath{
	{
		toPath:         "whizardMonitoringHelper.enabled",
		fromPath:       "spec.monitoring.whizard.enabled",
		valueConverter: CreateValueConverterWithDefault(false),
	},
	{
		toPath:         "whizard-monitoring-helper.whizardHelper.enabled",
		fromPath:       "spec.monitoring.whizard.enabled",
		valueConverter: CreateValueConverterWithDefault(false),
	},
	{
		toPath:         "whizard.enabled",
		fromPath:       "spec.monitoring.whizard.enabled",
		valueConverter: CreateValueConverterWithDefault(false),
	},
	{
		toPath:         "whizard.service.compactorTemplateSpec.dataVolume.persistentVolumeClaim.spec.resources.requests.storage",
		fromPath:       "spec.monitoring.whizard.server.volumeSize",
		valueConverter: CreateValueConverterWithDefault("20Gi"),
	},
	{
		toPath:         "whizard.service.ingesterTemplateSpec.dataVolume.persistentVolumeClaim.spec.resources.requests.storage",
		fromPath:       "spec.monitoring.whizard.server.volumeSize",
		valueConverter: CreateValueConverterWithDefault("20Gi"),
	},
	{
		toPath:         "whizard.service.storeTemplateSpec.dataVolume.persistentVolumeClaim.spec.resources.requests.storage",
		fromPath:       "spec.monitoring.whizard.server.volumeSize",
		valueConverter: CreateValueConverterWithDefault("20Gi"),
	},
	{
		toPath:         "whizardAgentProxy.enabled",
		fromPath:       "spec.monitoring.whizard.enabled",
		valueConverter: CreateValueConverterWithDefault(false),
	},
	{
		toPath:         "kube-prometheus-stack.prometheus.agentMode",
		fromPath:       "spec.monitoring.whizard.enabled",
		valueConverter: CreateValueConverterWithDefault(false),
	},
}

var stackTransPaths = []TransPath{
	{
		fromPath: "spec.monitoring.prometheus.resources",
		toPath:   "kube-prometheus-stack.prometheus.prometheusSpec.resources",
		valueConverter: CreateValueConverterWithDefault(map[string]interface{}{
			"requests": map[string]interface{}{
				"cpu":    "200m",
				"memory": "400Mi",
			},
			"limits": map[string]interface{}{
				"cpu":    "4",
				"memory": "16Gi",
			},
		}),
	}, {
		fromPath:       "spec.monitoring.prometheus.volumeSize",
		toPath:         "kube-prometheus-stack.prometheus.prometheusSpec.storageSpec.volumeClaimTemplate.spec.resources.requests.storage",
		valueConverter: CreateValueConverterWithDefault("20Gi"),
	},
	{
		fromPath:       "spec.monitoring.process_exporter.enabled",
		toPath:         "kube-prometheus-stack.prometheus-node-exporter.ProcessExporter.enabled",
		valueConverter: CreateValueConverterWithDefault(false),
	}, {
		fromPath:       "spec.monitoring.calico_exporter.enabled",
		toPath:         "kube-prometheus-stack.prometheus-node-exporter.CalicoExporter.enabled",
		valueConverter: CreateValueConverterWithDefault(false),
	},
}

var etcdTransPaths = []TransPath{
	{
		toPath:         "whizard-monitoring-helper.etcdMonitoringHelper.enabled",
		fromPath:       "spec.etcd.monitoring",
		valueConverter: CreateValueConverterWithDefault(false),
	}, {
		toPath:         "kube-prometheus-stack.kubeEtcd.enabled",
		fromPath:       "spec.etcd.monitoring",
		valueConverter: CreateValueConverterWithDefault(false),
	}, {
		toPath:   "kube-prometheus-stack.kubeEtcd.endpoints",
		fromPath: "spec.etcd.endpointIps",
		valueConverter: func(i interface{}) (interface{}, error) {
			if i != nil {
				s, ok := i.(string)
				if ok {
					return strings.Split(strings.TrimSpace(s), ","), nil
				}
			}
			return []string{}, nil
		},
	}, {
		toPath:   "kube-prometheus-stack.prometheus.prometheusSpec.secrets",
		fromPath: "spec.etcd.tlsEnable",
		valueConverter: func(fromValue interface{}) (interface{}, error) {

			if fromValue != nil {
				b, ok := fromValue.(bool)
				if ok && b {
					return []string{"kube-etcd-client-certs"}, nil
				}
			}
			return []string{}, nil
		},
	},
}

type ValueConverter func(fromValue interface{}) (interface{}, error)

func CreateValueConverterWithDefault(defaultValue interface{}) ValueConverter {
	return func(fromValue interface{}) (interface{}, error) {
		if fromValue == nil {
			return defaultValue, nil
		}
		return fromValue, nil
	}
}

type TransPath struct {
	toPath         string
	fromPath       string
	valueConverter ValueConverter
}

func SetExtensionValues(toExtensionValues string, fromCc map[string]interface{}, transPaths []TransPath) (string, error) {
	valuesBytes, err := yaml.YAMLToJSON([]byte(toExtensionValues))
	if err != nil {
		return "", err
	}
	for _, tp := range transPaths {
		v, _, err := unstructured.NestedFieldCopy(fromCc, strings.Split(tp.fromPath, ".")...)
		if err != nil {
			return "", err
		}
		if tp.valueConverter != nil {
			v, err = tp.valueConverter(v)
			if err != nil {
				return "", err
			}
		}
		if v == nil {
			continue
		}
		result := gjson.GetBytes(valuesBytes, tp.toPath)
		if !result.Exists() || result.Raw == "" {
			valuesBytes, err = sjson.SetBytes(valuesBytes, tp.toPath, v)
			if err != nil {
				return "", err
			}
		}
	}

	yamlBytes, err := yaml.JSONToYAML(valuesBytes)
	if err != nil {
		return "", err
	}

	return string(yamlBytes), nil
}

func SetOverrideValues(toOverrideValues string, fromCc map[string]interface{}, transPaths []TransPath, referExtensionValues string) (string, error) {
	overrideBytes, err := yaml.YAMLToJSON([]byte(toOverrideValues))
	if err != nil {
		return "", err
	}
	extensionValuesBytes, err := yaml.YAMLToJSON([]byte(referExtensionValues))
	if err != nil {
		return "", err
	}

	for _, tp := range transPaths {
		v, _, err := unstructured.NestedFieldCopy(fromCc, strings.Split(tp.fromPath, ".")...)
		if err != nil {
			return "", err
		}
		if tp.valueConverter != nil {
			v, err = tp.valueConverter(v)
			if err != nil {
				return "", err
			}
		}
		// Only when the value is different from that in extension values, it will be set.
		var gv interface{}
		valuesResult := gjson.GetBytes(extensionValuesBytes, tp.toPath)
		if valuesResult.Exists() {
			gv = valuesResult.Value()
		}

		result := gjson.GetBytes(overrideBytes, tp.toPath)
		if !result.Exists() || result.Raw == "" {
			if !reflect.DeepEqual(v, gv) {
				overrideBytes, err = sjson.SetBytes(overrideBytes, tp.toPath, v)
				if err != nil {
					return "", err
				}
			}
		}
	}

	yamlBytes, err := yaml.JSONToYAML(overrideBytes)
	if err != nil {
		return "", err
	}

	return string(yamlBytes), nil
}
