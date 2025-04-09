package servicemesh

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	// jaegerv1 "github.com/jaegertracing/jaeger-operator/apis/v1"
	webhookv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metaerrors "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
	"kubesphere.io/ks-upgrade/pkg/executor"
	"kubesphere.io/ks-upgrade/pkg/model"
	"kubesphere.io/ks-upgrade/pkg/model/core"
	"kubesphere.io/ks-upgrade/pkg/model/helper"
	"kubesphere.io/ks-upgrade/pkg/storage"
	"kubesphere.io/ks-upgrade/pkg/store"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	restconfig "sigs.k8s.io/controller-runtime/pkg/client/config"
)

func init() {
	runtime.Must(executor.Register(&factory{}))
}

const (
	jobName     = "servicemesh"
	jobDone     = jobName + "-done"
	optionRerun = "rerun"
	istioSystem = "istio-system"
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
	clientV3      runtimeclient.Client
	clientV4      runtimeclient.Client
	coreHelper    core.Helper
	resourceStore store.ResourceStore
	options       executor.DynamicOptions
	extensionRef  *model.ExtensionRef
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

	// cleanup old data
	if err := i.clean(ctx); err != nil {
		return err
	}

	// create extension install plan
	if err := i.isHostAndCreateInstallPlan(ctx); err != nil && !k8serrors.IsAlreadyExists(err) {
		return err
	}

	// save job done time
	date = []byte(time.Now().UTC().String())
	klog.Infof("save data key: %s value: %s", jobDone, date)
	err = i.resourceStore.SaveRaw(jobDone, date)
	return err
}

type resrouceObject struct {
	nsName types.NamespacedName
	obj    runtimeclient.Object
}

func (i *upgradeJob) clean(ctx context.Context) error {
	cleanfun := func(objs []resrouceObject) error {
		for _, o := range objs {
			fileName := fmt.Sprintf("%s-%s-%s", jobName, o.nsName.Name, reflect.TypeOf(o.obj).Elem().Name())
			err := i.resourceStore.Load(fileName, o.obj)
			if err != nil && errors.Is(err, storage.BackupKeyNotFound) {
				if err := i.clientV3.Get(ctx, o.nsName, o.obj); err != nil {
					if k8serrors.IsNotFound(err) || metaerrors.IsNoMatchError(err) {
						klog.Warning(err.Error())
						continue
					} else {
						return err
					}
				}

				if err := i.resourceStore.Save(fileName, o.obj); err != nil {
					return err
				}

				if err := i.clientV3.Delete(ctx, o.obj); err != nil && !k8serrors.IsNotFound(err) {
					return err
				}
			}
		}
		return nil
	}

	cc, err := i.coreHelper.GetClusterConfiguration(ctx)
	if err != nil {
		return err
	}

	enabled, _, err := unstructured.NestedBool(cc, "spec", "servicemesh", "enabled")
	if !enabled || err != nil {
		return err
	}

	resources := []resrouceObject{
		{nsName: types.NamespacedName{Name: "jaeger-query", Namespace: istioSystem}, obj: &appsv1.Deployment{}},
		{nsName: types.NamespacedName{Name: "jaeger-collector", Namespace: istioSystem}, obj: &appsv1.Deployment{}},
		{nsName: types.NamespacedName{Name: "jaeger-operator", Namespace: istioSystem}, obj: &appsv1.Deployment{}},
		{nsName: types.NamespacedName{Name: "kiali-operator", Namespace: istioSystem}, obj: &appsv1.Deployment{}},
		{nsName: types.NamespacedName{Name: "istiod-1-14-6", Namespace: istioSystem}, obj: &appsv1.Deployment{}},
		{nsName: types.NamespacedName{Name: "kiali", Namespace: istioSystem}, obj: &appsv1.Deployment{}},
		{nsName: types.NamespacedName{Name: "istio-ingressgateway", Namespace: istioSystem}, obj: &appsv1.Deployment{}},
		{nsName: types.NamespacedName{Name: "istio-reader-clusterrole-1-14-6-istio-system"}, obj: &rbacv1.ClusterRole{}},
		{nsName: types.NamespacedName{Name: "istio-reader-istio-system"}, obj: &rbacv1.ClusterRole{}},
		{nsName: types.NamespacedName{Name: "istiod-clusterrole-1-14-6-istio-system"}, obj: &rbacv1.ClusterRole{}},
		{nsName: types.NamespacedName{Name: "istiod-gateway-controller-1-14-6-istio-system"}, obj: &rbacv1.ClusterRole{}},
		{nsName: types.NamespacedName{Name: "istiod-istio-system"}, obj: &rbacv1.ClusterRole{}},
		{nsName: types.NamespacedName{Name: "kiali"}, obj: &rbacv1.ClusterRole{}},
		{nsName: types.NamespacedName{Name: "kiali-operator"}, obj: &rbacv1.ClusterRole{}},
		{nsName: types.NamespacedName{Name: "istio-reader-clusterrole-1-14-6-istio-system"}, obj: &rbacv1.ClusterRoleBinding{}},
		{nsName: types.NamespacedName{Name: "istio-reader-istio-system"}, obj: &rbacv1.ClusterRoleBinding{}},
		{nsName: types.NamespacedName{Name: "istiod-clusterrole-1-14-6-istio-system"}, obj: &rbacv1.ClusterRoleBinding{}},
		{nsName: types.NamespacedName{Name: "istiod-gateway-controller-1-14-6-istio-system"}, obj: &rbacv1.ClusterRoleBinding{}},
		{nsName: types.NamespacedName{Name: "istiod-istio-system"}, obj: &rbacv1.ClusterRoleBinding{}},
		{nsName: types.NamespacedName{Name: "kiali"}, obj: &rbacv1.ClusterRoleBinding{}},
		{nsName: types.NamespacedName{Name: "kiali-operator"}, obj: &rbacv1.ClusterRoleBinding{}},
		{nsName: types.NamespacedName{Name: "istio-validator-1-14-6-istio-system"}, obj: &webhookv1.ValidatingWebhookConfiguration{}},
		{nsName: types.NamespacedName{Name: "istiod-default-validator"}, obj: &webhookv1.ValidatingWebhookConfiguration{}},
		{nsName: types.NamespacedName{Name: "istio-revision-tag-default"}, obj: &webhookv1.MutatingWebhookConfiguration{}},
		{nsName: types.NamespacedName{Name: "istio-sidecar-injector-1-14-6"}, obj: &webhookv1.MutatingWebhookConfiguration{}},
	}
	if err := cleanfun(resources); err != nil {
		return err
	}

	// clean finalizer - delete cr and crd
	cleanFinalizer := map[string]schema.GroupVersionResource{
		"jaeger": {
			Group:    "jaegertracing.io",
			Version:  "v1",
			Resource: "jaegers",
		},
		"kiali": {
			Group:    "kiali.io",
			Version:  "v1alpha1",
			Resource: "kialis",
		},
	}

	restConfig, err := restconfig.GetConfig()
	if err != nil {
		klog.Errorf("[servicemesh] failed to get rest config: %s", err)
		return err
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		klog.Errorf("[servicemesh] failed to create dynamic client: %s", err)
		return fmt.Errorf("failed to create dynamic client: %s", err)
	}

	for name, resource := range cleanFinalizer {
		unstructured := &unstructured.Unstructured{}
		fileName := fmt.Sprintf("%s-%s", jobName, name)
		err := i.resourceStore.Load(fileName, unstructured)
		if err != nil && errors.Is(err, storage.BackupKeyNotFound) {
			unstructured, err = dynamicClient.Resource(resource).Namespace(istioSystem).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				if k8serrors.IsNotFound(err) || metaerrors.IsNoMatchError(err) {
					klog.Warningf("[servicemesh] get resource %s/%s/%s error: %s", resource.String(), istioSystem, name, err)
					continue
				}
				return err
			}

			if err := i.resourceStore.Save(fileName, unstructured); err != nil {
				return err
			}
			unstructured.SetFinalizers(nil)
			_, err = dynamicClient.Resource(resource).Namespace(istioSystem).Update(ctx, unstructured, metav1.UpdateOptions{})
			if err != nil && !k8serrors.IsNotFound(err) {
				return err
			}

			err = dynamicClient.Resource(resource).Namespace(istioSystem).Delete(ctx, name, metav1.DeleteOptions{})
			if err != nil && !k8serrors.IsNotFound(err) {
				return err
			}
		}
	}

	// delete crd
	cleanResources := []resrouceObject{
		{nsName: types.NamespacedName{Name: "kialis.kiali.io"}, obj: &apiextensionsv1.CustomResourceDefinition{}},
		{nsName: types.NamespacedName{Name: "jaegers.jaegertracing.io"}, obj: &apiextensionsv1.CustomResourceDefinition{}},
	}

	if err := cleanfun(cleanResources); err != nil {
		return err
	}
	return nil
}

func (i *upgradeJob) isHostAndCreateInstallPlan(ctx context.Context) error {
	isHostCluster, err := i.coreHelper.IsHostCluster(ctx)
	if err != nil {
		return err
	}
	if !isHostCluster {
		klog.Infof("skip creating install plan for extension %s in non-host cluster", i.extensionRef.Name)
		return nil
	}
	klog.Infof("create install plan for extension %s", i.extensionRef.Name)

	isEnabledFunc := func(cc map[string]any) (bool, error) {
		enabled, _, err := unstructured.NestedBool(cc, "spec", "servicemesh", "enabled")
		return enabled, err
	}
	err = i.coreHelper.GenerateClusterScheduling(ctx, i.extensionRef, isEnabledFunc, nil)
	if err != nil {
		klog.Errorf("failed to generate cluster scheduling: %s", err)
		return err
	}

	return i.coreHelper.CreateInstallPlanFromExtensionRef(ctx, i.extensionRef)
}
