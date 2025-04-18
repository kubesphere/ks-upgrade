package alerting

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	"kubesphere.io/ks-upgrade/pkg/model"
	"kubesphere.io/ks-upgrade/pkg/model/core"
	"kubesphere.io/ks-upgrade/pkg/model/helper"
	"kubesphere.io/ks-upgrade/pkg/storage"
	"kubesphere.io/ks-upgrade/pkg/store"
	alertingv2beta1 "kubesphere.io/ks-upgrade/v3/api/alerting/v2beta1"
	v4clusterv1alpha1 "kubesphere.io/ks-upgrade/v4/api/cluster/v1alpha1"
	v4corev1alpha1 "kubesphere.io/ks-upgrade/v4/api/core/v1alpha1"
)

func init() {
	runtime.Must(executor.Register(&factory{}))
}

const (
	jobName = "whizard-alerting"
	jobDone = jobName + "-done"

	optionRerun      = "rerun"
	optionUpdateCrds = "updateCrds"
)

var (
	crdsv1GVR = apiextensionsv1.SchemeGroupVersion.WithResource("customresourcedefinitions")
	crdsv1    = []runtimeclient.Object{
		&apiextensionsv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: "globalrulegroups.alerting.kubesphere.io"}},
		&apiextensionsv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: "clusterrulegroups.alerting.kubesphere.io"}},
		&apiextensionsv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: "rulegroups.alerting.kubesphere.io"}},
	}

	validatingwebhookconfigurationsv1GVR = admissionregistrationv1.SchemeGroupVersion.WithResource("validatingwebhookconfigurations")
	validatingwebhookconfigurationsv1    = []runtimeclient.Object{
		&admissionregistrationv1.ValidatingWebhookConfiguration{ObjectMeta: metav1.ObjectMeta{Name: "rulegroups.alerting.kubesphere.io"}},
	}
	mutatingwebhookconfigurationsv1GVR = admissionregistrationv1.SchemeGroupVersion.WithResource("mutatingwebhookconfigurations")
	mutatingwebhookconfigurationsv1    = []runtimeclient.Object{
		&admissionregistrationv1.MutatingWebhookConfiguration{ObjectMeta: metav1.ObjectMeta{Name: "rulegroups.alerting.kubesphere.io"}},
	}

	globalrulegroupsv2beta1GVR  = alertingv2beta1.SchemeGroupVersion.WithResource("globalrulegroups")
	clusterrulegroupsv2beta1GVR = alertingv2beta1.SchemeGroupVersion.WithResource("clusterrulegroups")
	rulegroupsv2beta1GVR        = alertingv2beta1.SchemeGroupVersion.WithResource("rulegroups")
)

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
		dryRun:       false, // for test
	}, nil
}

type upgradeJob struct {
	options      executor.DynamicOptions
	extensionRef *model.ExtensionRef
	log          klog.Logger

	dryRun bool

	clientV3      runtimeclient.Client
	clientV4      runtimeclient.Client
	resourceStore store.ResourceStore
	coreHelper    core.Helper
}

func (i *upgradeJob) InjectClientV3(client runtimeclient.Client) {
	if i.dryRun {
		i.clientV3 = runtimeclient.NewDryRunClient(client)
	} else {
		i.clientV3 = client
	}
}

func (i *upgradeJob) InjectClientV4(client runtimeclient.Client) {
	if i.dryRun {
		i.clientV4 = runtimeclient.NewDryRunClient(client)
	} else {
		i.clientV4 = client
	}
}

func (i *upgradeJob) InjectResourceStore(store store.ResourceStore) {
	i.resourceStore = store
}

func (i *upgradeJob) InjectModelHelperFactory(factory helper.ModelHelperFactory) {
	i.coreHelper = factory.CoreHelper()
}

func (i *upgradeJob) PreUpgrade(ctx context.Context) error {
	// check if job needs re-run
	if i.options != nil {
		value := i.options[optionRerun]
		if fmt.Sprint(value) == "true" {
			i.log.Info("delete data", "key", jobDone)
			err := i.resourceStore.Delete(jobDone)
			if err != nil && !errors.Is(err, storage.BackupKeyNotFound) {
				return err
			}
		}
	}

	// check if job already done
	var date []byte
	date, err := i.resourceStore.LoadRaw(jobDone)
	if err != nil && !errors.Is(err, storage.BackupKeyNotFound) {
		return err
	}
	if string(date) != "" {
		i.log.Info("job is already done", "job", jobName, "date", date)
		return nil
	}

	// Backup crds and webhooks
	var gvrObjs = map[schema.GroupVersionResource][]runtimeclient.Object{
		crdsv1GVR:                            crdsv1,
		validatingwebhookconfigurationsv1GVR: validatingwebhookconfigurationsv1,
		mutatingwebhookconfigurationsv1GVR:   mutatingwebhookconfigurationsv1,
	}
	for gvr, objs := range gvrObjs {
		gvrName := strings.Join([]string{gvr.Group, gvr.Version, gvr.Resource}, ".")
		gvk, err := i.clientV3.RESTMapper().KindFor(gvr)
		if err != nil {
			return err
		}
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
			if err := i.clientV3.Get(ctx, namespacedName, obj, &runtimeclient.GetOptions{}); err != nil {
				if apierrors.IsNotFound(err) {
					log.Info("not found")
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

	cc, err := i.coreHelper.GetClusterConfiguration(ctx)
	if err != nil {
		return err
	}

	alertingEnabled, ok, err := unstructured.NestedBool(cc, "spec", "alerting", "enabled")
	if err != nil {
		return err
	}
	if !ok || !alertingEnabled { // Do nothing if alerting is not enabled
		return nil
	}

	// Just backup the rule groups, adaption to the extension of which are done inside the extension.
	selector, _ := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{{
			Operator: metav1.LabelSelectorOpDoesNotExist,
			Key:      "alerting.kubesphere.io/data_source",
		}},
	})
	gvrListMap := map[schema.GroupVersionResource]runtimeclient.ObjectList{
		globalrulegroupsv2beta1GVR:  &alertingv2beta1.GlobalRuleGroupList{},
		clusterrulegroupsv2beta1GVR: &alertingv2beta1.ClusterRuleGroupList{},
		rulegroupsv2beta1GVR:        &alertingv2beta1.RuleGroupList{},
	}
	for gvr, list := range gvrListMap {
		gvrString := strings.Join([]string{gvr.Group, gvr.Version, gvr.Resource}, ".")
		fileName := fmt.Sprintf("%s-%s", jobName, strings.ReplaceAll(gvrString, "/", "."))
		err = i.resourceStore.Load(fileName, list)
		if err == nil {
			continue
		}
		if !errors.Is(err, storage.BackupKeyNotFound) {
			return err
		}
		if err := i.clientV3.List(ctx, list, &runtimeclient.ListOptions{
			LabelSelector: selector,
		}); err != nil {
			return err
		}

		// set GVK into the resources to save
		list.GetObjectKind().SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("List"))
		gvk, err := i.clientV3.RESTMapper().KindFor(gvr)
		if err != nil {
			return err
		}
		switch glist := list.(type) {
		case *alertingv2beta1.GlobalRuleGroupList:
			for i := range glist.Items {
				glist.Items[i].GetObjectKind().SetGroupVersionKind(gvk)
			}
		case *alertingv2beta1.ClusterRuleGroupList:
			for i := range glist.Items {
				glist.Items[i].GetObjectKind().SetGroupVersionKind(gvk)
			}
		case *alertingv2beta1.RuleGroupList:
			for i := range glist.Items {
				glist.Items[i].GetObjectKind().SetGroupVersionKind(gvk)
			}
		default:
			continue // should not be here
		}

		err = i.resourceStore.Save(fileName, list)
		if err != nil {
			return err
		}
	}

	return nil
}

func (i *upgradeJob) PostUpgrade(ctx context.Context) error {
	// check if job needs re-run
	if i.options != nil {
		value := i.options[optionRerun]
		if fmt.Sprint(value) == "true" {
			i.log.Info("delete data", "key", jobDone)
			err := i.resourceStore.Delete(jobDone)
			if err != nil && !errors.Is(err, storage.BackupKeyNotFound) {
				return err
			}
		}
	}

	// check if job already done
	var date []byte
	date, err := i.resourceStore.LoadRaw(jobDone)
	if err != nil && !errors.Is(err, storage.BackupKeyNotFound) {
		return err
	}
	if string(date) != "" {
		i.log.Info("job is already done", "job", jobName, "date", date)
		return nil
	}

	// delete webhookconfigurations because the extension will install them
	var gvrObjs = map[schema.GroupVersionResource][]runtimeclient.Object{
		validatingwebhookconfigurationsv1GVR: validatingwebhookconfigurationsv1,
		mutatingwebhookconfigurationsv1GVR:   mutatingwebhookconfigurationsv1,
	}
	for gvr, objs := range gvrObjs {
		for _, obj := range objs {
			namespacedName := types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}
			log := i.log.WithValues("gvr", gvr.String(), "name", namespacedName.String())
			log.Info("delete")
			if err := i.clientV4.Delete(ctx, obj, &runtimeclient.DeleteOptions{}); err != nil {
				if apierrors.IsNotFound(err) {
					log.Info("not found")
					continue
				}
				return err
			}
		}
	}

	cc, err := i.coreHelper.GetClusterConfiguration(ctx)
	if err != nil {
		return err
	}

	// alertingEnabled, ok, err := unstructured.NestedBool(cc, "spec", "alerting", "enabled")
	// if err != nil {
	// 	return err
	// }
	// if !ok || !alertingEnabled { // Do nothing if alerting is not enabled
	// 	return nil
	// }

	isHost, err := i.coreHelper.IsHostCluster(ctx)
	if err != nil {
		return err
	}

	if i.options != nil {
		value := i.options[optionUpdateCrds]
		if fmt.Sprint(value) == "true" && i.extensionRef != nil {
			if err := i.coreHelper.ApplyCRDsFromExtensionRef(ctx, i.extensionRef,
				metav1.ApplyOptions{FieldManager: "kubectl", Force: true}, nil); err != nil {
				klog.Errorf("failed to apply crds: %s", err)
				return err
			}
		}
	}

	if isHost && i.extensionRef != nil { // Create InstallPlan only on host cluster
		i.log.Info("creating InstallPlan")

		extRef := *i.extensionRef
		whizardEnabled, _, err := unstructured.NestedBool(cc, "spec", "monitoring", "whizard", "enabled")
		if err != nil {
			return err
		}
		distributionMode := "Member"
		if whizardEnabled {
			distributionMode = "None"
		}
		extRef.Config, err = setYamlValues(extRef.Config, []pathValue{{
			path:  "global.rules.distributionMode",
			value: distributionMode,
		}})
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
				currentAlertingEnabled, _, err := unstructured.NestedBool(currentcc, "spec", "alerting", "enabled")
				if err != nil {
					return err
				}
				if currentAlertingEnabled {
					placement.Clusters = append(placement.Clusters, cluster.Name)
				} else {
					// Check status.alerting.status because it is enabled when whizard is enabled in KSE 3.5
					alertingStatus, _, err := unstructured.NestedString(currentcc, "status", "alerting", "status")
					if err != nil {
						return err
					}
					if alertingStatus == "enabled" {
						placement.Clusters = append(placement.Clusters, cluster.Name)
					}
				}
			}
			if len(placement.Clusters) > 0 { // create InstallPlan only if there are clusters with alerting enabled
				extRef.ClusterScheduling = &v4corev1alpha1.ClusterScheduling{Placement: &placement}
				if err := i.coreHelper.CreateInstallPlanFromExtensionRef(ctx, &extRef); err != nil && !apierrors.IsAlreadyExists(err) {
					return err
				}
			}
		} else {
			if err := i.coreHelper.CreateInstallPlanFromExtensionRef(ctx, &extRef); err != nil && !apierrors.IsAlreadyExists(err) {
				return err
			}
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

type pathValue struct {
	path, value string
}

func setYamlValues(yamlString string, pathValues []pathValue) (string, error) {
	jsonbytes, err := yaml.YAMLToJSON([]byte(yamlString))
	if err != nil {
		return yamlString, err
	}

	for _, v := range pathValues {
		result := gjson.GetBytes(jsonbytes, v.path)
		if !result.Exists() || result.Raw == "" {
			jsonbytes, err = sjson.SetBytes(jsonbytes, v.path, v.value)
			if err != nil {
				return yamlString, err
			}
		}
	}

	yamlbytes, err := yaml.JSONToYAML(jsonbytes)
	if err != nil {
		return yamlString, err
	}

	return string(yamlbytes), nil
}
