package gateway

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"kubesphere.io/ks-upgrade/pkg/constants"
	"strconv"
	"strings"
	"text/template"
	"time"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"gopkg.in/yaml.v3"
	helmrelease "helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	v1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apiext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metaerrors "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	pkgruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"kubesphere.io/ks-upgrade/pkg/executor"
	"kubesphere.io/ks-upgrade/pkg/helm"
	"kubesphere.io/ks-upgrade/pkg/model"
	"kubesphere.io/ks-upgrade/pkg/model/core"
	"kubesphere.io/ks-upgrade/pkg/model/helper"
	"kubesphere.io/ks-upgrade/pkg/storage"
	"kubesphere.io/ks-upgrade/pkg/store"
	gatewayv1alpha1 "kubesphere.io/ks-upgrade/v3/api/gateway/v1alpha1"
	gatewayv2alpha1 "kubesphere.io/ks-upgrade/v4/api/gateway/v2alpha1"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func init() {
	runtime.Must(executor.Register(&factory{}))
}

const (
	jobName = "gateway"
	jobDone = jobName + "-done"

	storeKeyPrefix                = "gateway-"
	gatewayCRStoreKey             = storeKeyPrefix + "gateway-cr-list"
	nginxCRStoreKey               = storeKeyPrefix + "nginx-cr-list"
	ingressStoreKey               = storeKeyPrefix + "ingress-list"
	gatewayConfigStoreKey         = storeKeyPrefix + "config"
	prometheusIngressRuleStoreKey = storeKeyPrefix + "prometheus-ingress-rules"

	appVersion               = "kubesphere-nginx-ingress-4.12.1"
	ksIngressNginxAppVersion = "1.12.1"
	kubesphereRouterPrefix   = "kubesphere-router-"
	workspaceGatewayPrefix   = kubesphereRouterPrefix + "workspace-"
	upgradeGatewaySuffix     = "-ingress"

	labelGatewayType   = "kubesphere.io/gateway-type"
	gatewayTypeCluster = "cluster"

	defaultPollImmediateInterval, defaultPollImmediateTimeout = time.Second, 5 * time.Minute

	serviceTypeNodePort     = "NodePort"
	portNameHttp            = "http"
	portNameHttps           = "https"
	annotationHttpNodePort  = "gateway.kubesphere.io/http-nodeport"
	annotationHttpsNodePort = "gateway.kubesphere.io/https-nodeport"
	newClusterGatewayName   = kubesphereRouterPrefix + "cluster"
	oldClusterGatewayName   = kubesphereRouterPrefix + "kubesphere-system"
)

var (
	KubeSphereNamespace              = "kubesphere-system"
	MonitoringNamespace              = "kubesphere-monitoring-system"
	KubeSphereConfigName             = "kubesphere-config"
	GatewayConfigBackupConfigMapName = "kubesphere-gateway-backup-config"
	KubeSphereConfigMapDataKey       = "kubesphere.yaml"

	nginxGVK       = schema.GroupVersionKind{Group: "gateway.kubesphere.io", Version: "v1alpha1", Kind: "Nginx"}
	gatewayCRDName = "gateways.gateway.kubesphere.io"
	nginxCRDName   = "nginxes.gateway.kubesphere.io"

	noMatchGVKErrorFunc = func(gvk schema.GroupVersionKind) string {
		return fmt.Sprintf(`no matches for kind "%s" in version "%s"`, gvk.Kind, gvk.GroupVersion().String())
	}
)

var _ executor.DynamicUpgrade = &upgradeJob{}
var _ executor.UpgradeJob = &upgradeJob{}
var _ executor.InjectClientV3 = &upgradeJob{}
var _ executor.InjectClientV4 = &upgradeJob{}
var _ executor.InjectResourceStore = &upgradeJob{}
var _ executor.InjectModelHelperFactory = &upgradeJob{}

type factory struct {
}

type gatewayOptions struct {
	GatewayName string `json:"gatewayName"`
	Rerun       bool   `json:"rerun"`
}

func (f *factory) Name() string {
	return jobName
}

func (f *factory) Create(options executor.DynamicOptions, extensionRef *model.ExtensionRef) (executor.UpgradeJob, error) {
	gatewayOptions := gatewayOptions{}
	if options != nil {
		optionsBytes, err := json.Marshal(options)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(optionsBytes, &gatewayOptions); err != nil {
			return nil, err
		}
	}

	return &upgradeJob{
		gatewayOptions: gatewayOptions,
		extensionRef:   extensionRef,
	}, nil
}

type upgradeJob struct {
	clientV3       runtimeclient.Client
	clientV4       runtimeclient.Client
	helmClient     helm.HelmClient
	coreHelper     core.Helper
	resourceStore  store.ResourceStore
	gatewayOptions gatewayOptions
	extensionRef   *model.ExtensionRef
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

func (i *upgradeJob) DynamicUpgrade(ctx context.Context) error {
	gatewayName := i.gatewayOptions.GatewayName
	if i.gatewayOptions.GatewayName == "" {
		return fmt.Errorf("You need to specify the gateway name to upgrade")
	}

	gatewayCRs := &gatewayv1alpha1.GatewayList{}
	if err := i.resourceStore.Load(gatewayCRStoreKey, gatewayCRs); err != nil {
		klog.Errorf("failed to load nginx cr list: %v", err)
		return err
	}

	var releaseName, releaseNamespace string
	for _, gateway := range gatewayCRs.Items {
		if gateway.GetName() == gatewayName {
			releaseName = gateway.GetName()
			releaseNamespace = gateway.GetNamespace()
			if gateway.Labels[labelGatewayType] == gatewayTypeCluster {
				releaseName = newClusterGatewayName
			}
			break
		}
	}

	_, err := i.helmClient.Uninstall(releaseNamespace, gatewayName+upgradeGatewaySuffix)
	if err != nil && !errors.Is(err, driver.ErrReleaseNotFound) {
		return err
	}

	if err := i.upgradeGateway(ctx, gatewayName); err != nil {
		return err
	}

	if err := i.waitForGatewayHelmReleaseDeployed(releaseName, releaseNamespace); err != nil {
		return fmt.Errorf("Error waiting for ingress-nginx helm release upgrade done: %v", err)
	}

	if err := i.upgradeIngress(ctx, gatewayName); err != nil {
		return err
	}

	return nil
}

func (i *upgradeJob) PreUpgrade(ctx context.Context) error {
	// check if job needs re-run
	if i.gatewayOptions.Rerun {
		klog.Infof("delete data for key %s", jobDone)
		if err := i.resourceStore.Delete(jobDone); err != nil && !errors.Is(err, storage.BackupKeyNotFound) {
			return err
		}
	}
	date, err := i.resourceStore.LoadRaw(jobDone)
	if err != nil && !errors.Is(err, storage.BackupKeyNotFound) {
		return err
	}
	if string(date) != "" {
		klog.Infof("job %s already done at %s", jobName, date)
		return nil
	}

	// backup gateway config to specify configmap
	if err := i.backupGatewayConfig2ConfigMap(ctx); err != nil {
		return err
	}

	if err := i.backupIngress(ctx); err != nil {
		return err
	}

	if err := i.backupGatewayCR(ctx); err != nil {
		return err
	}

	if err := i.backupNginxCR(ctx); err != nil {
		return err
	}

	if err := i.backupGatewayNginxV1CRD(ctx); err != nil {
		return err
	}

	return nil
}

func IgnoreNoMatchOrNotFound(err error) error {
	if err == nil {
		return nil
	}
	if metaerrors.IsNoMatchError(err) || k8serrors.IsNotFound(err) {
		klog.Warning(err.Error())
		return nil
	}
	return err
}

func (i *upgradeJob) PostUpgrade(ctx context.Context) error {
	// check if job needs re-run
	if i.gatewayOptions.Rerun {
		klog.Infof("delete data for key %s", jobDone)
		if err := i.resourceStore.Delete(jobDone); err != nil && !errors.Is(err, storage.BackupKeyNotFound) {
			return err
		}
	}
	date, err := i.resourceStore.LoadRaw(jobDone)
	if err != nil && !errors.Is(err, storage.BackupKeyNotFound) {
		return err
	}
	if string(date) != "" {
		klog.Infof("job %s already done at %s", jobName, date)
		return nil
	}

	if err := i.backupAndCleanPrometheusIngressRuleCR(ctx); err != nil {
		return err
	}

	if err := i.isHostAndCreateInstallPlan(ctx); err != nil {
		return err
	}

	if err := i.cleanGatewayV1CRDAndCR(ctx); err != nil {
		return err
	}
	if err := i.applyGatewayV2CRD(ctx); err != nil {
		return err
	}

	if err := i.waitForGatewayV2CRD(ctx); err != nil {
		klog.Fatalf("Error waiting for CRD: %v", err)
	}

	// save job done time
	date = []byte(time.Now().UTC().String())
	klog.Infof("save data key: %s value: %s", jobDone, date)
	return i.resourceStore.SaveRaw(jobDone, date)
}

func (i *upgradeJob) isHostAndCreateInstallPlan(ctx context.Context) error {
	isHostCluster, err := i.coreHelper.IsHostCluster(ctx)
	if err != nil {
		return err
	}
	if !isHostCluster {
		klog.Info("skip creating install plan for extension %s in member cluster", i.extensionRef.Name)
		return nil
	}

	// create install plan for extension
	klog.Infof("create install plan for extension %s", i.extensionRef.Name)
	if err := i.settingExtensionConfig(ctx, i.extensionRef); err != nil {
		klog.Errorf("failed to setting extension config: %v", err)
		return err
	}
	isEnabledFunc := func(cc map[string]any) (bool, error) {
		return true, nil
	}
	err = i.coreHelper.GenerateClusterScheduling(ctx, i.extensionRef, isEnabledFunc, nil)
	if err != nil {
		klog.Errorf("failed to generate cluster scheduling: %s", err)
		return err
	}
	err = i.coreHelper.CreateInstallPlanFromExtensionRef(ctx, i.extensionRef)
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		return err
	}
	klog.Infof("create install plan for extension %s done", i.extensionRef.Name)

	return nil
}

func (i *upgradeJob) waitForGatewayV2CRD(ctx context.Context) error {
	return wait.PollImmediate(defaultPollImmediateInterval, defaultPollImmediateTimeout, func() (bool, error) {
		gatewayV2List := &gatewayv2alpha1.GatewayList{}
		if err := i.clientV4.List(ctx, gatewayV2List); err != nil {
			klog.Infof("wait for gateway v2 crd ready: %v", err)
			return false, nil
		}
		return true, nil
	})
}

func (i *upgradeJob) waitForGatewayHelmReleaseDeployed(releaseName, releaseNamespace string) error {
	return wait.PollImmediate(defaultPollImmediateInterval, defaultPollImmediateTimeout, func() (bool, error) {
		release, err := i.helmClient.Status(releaseNamespace, releaseName)
		if err != nil {
			klog.Infof("wait for %s/%s ingress-nginx helm release upgrade done: %v", releaseNamespace, releaseName, err)
			return false, nil
		}

		if release.Chart.Metadata.AppVersion != ksIngressNginxAppVersion && release.Info.Status != helmrelease.StatusDeployed {
			klog.Infof("wait for ingress-nginx helm release upgrade done. release name: %s, appVersion: %s, status: %s",
				release.Name, release.Chart.Metadata.AppVersion, release.Info.Status)
			return false, nil
		}
		return true, nil
	})
}

// settingExtensionConfig set extension config for gateway, translate v1 configuration to v2.
func (i *upgradeJob) settingExtensionConfig(ctx context.Context, extension *model.ExtensionRef) error {
	oldGatewayConfig, err := i.getOldGatewayConfig(ctx)
	if err != nil {
		return err
	}

	namespace, ok, err := unstructured.NestedString(oldGatewayConfig, "namespace")
	if err != nil && !ok {
		klog.Warningf("get gateway namespace error. err:%v", err)
		return err
	}
	repository, ok, err := unstructured.NestedString(oldGatewayConfig, "repository")
	if err != nil && !ok {
		klog.Warningf("get gateway repository error. err:%v", err)
		return err
	}

	newGatewayConfig := map[string]interface{}{
		"backend": map[string]interface{}{
			"config": map[string]interface{}{
				"gateway": map[string]interface{}{
					"namespace": namespace,
					"valuesOverride": map[string]interface{}{
						"controller": map[string]interface{}{
							"image": map[string]interface{}{
								"repository": repository,
							},
						},
					},
				},
			},
			"upgrade": map[string]interface{}{
				"enabled": true,
			},
		},
	}

	var valuesBytes []byte
	if valuesBytes, err = yaml.Marshal(newGatewayConfig); err != nil {
		return err
	}
	extension.Config = string(valuesBytes)

	return nil
}

func (i *upgradeJob) backupIngress(ctx context.Context) error {
	list := &networkingv1.IngressList{}
	if err := i.clientV3.List(ctx, list); err != nil {
		if IgnoreNoMatchOrNotFound(err) != nil {
			klog.Errorf("failed to list %s: %v", ingressStoreKey, err)
			return err
		}
		return nil
	}
	return i.backupObjectList(ingressStoreKey, list)
}

func (i *upgradeJob) backupGatewayCR(ctx context.Context) error {
	list := &gatewayv1alpha1.GatewayList{}
	if err := i.clientV3.List(ctx, list); err != nil {
		if IgnoreNoMatchOrNotFound(err) != nil {
			klog.Errorf("failed to list %s: %v", gatewayCRStoreKey, err)
			return err
		}
		return nil
	}
	for index, gw := range list.Items {
		if gw.Spec.Service.Type == serviceTypeNodePort {
			service := &v1.Service{}
			err := i.clientV3.Get(ctx, types.NamespacedName{Name: gw.Name, Namespace: gw.Namespace}, service)
			if err != nil {
				return err
			}
			if service.Spec.Type != serviceTypeNodePort {
				continue
			}

			var httpNodePort, httpsNodePort string
			for _, port := range service.Spec.Ports {
				if port.Name == portNameHttp {
					httpNodePort = strconv.Itoa(int(port.NodePort))
				}
				if port.Name == portNameHttps {
					httpsNodePort = strconv.Itoa(int(port.NodePort))
				}
			}
			if gw.Spec.Service.Annotations == nil {
				gw.Spec.Service.Annotations = make(map[string]string)
			}
			gw.Spec.Service.Annotations[annotationHttpNodePort] = httpNodePort
			gw.Spec.Service.Annotations[annotationHttpsNodePort] = httpsNodePort
			list.Items[index] = gw
		}
	}
	return i.backupObjectList(gatewayCRStoreKey, list)
}

func (i *upgradeJob) backupNginxCR(ctx context.Context) error {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(nginxGVK)
	if err := i.clientV3.List(ctx, list); err != nil {
		if IgnoreNoMatchOrNotFound(err) != nil {
			klog.Errorf("failed to list %s: %v", nginxCRStoreKey, err)
			return err
		}
		return nil
	}
	return i.backupObjectList(nginxCRStoreKey, list)
}

func (i *upgradeJob) backupGatewayNginxV1CRD(ctx context.Context) error {
	for _, name := range []string{gatewayCRDName, nginxCRDName} {
		backupKey := fmt.Sprintf("%s%s", storeKeyPrefix, name)
		crd := &apiext.CustomResourceDefinition{}
		err := i.resourceStore.Load(backupKey, crd)
		if err != nil && errors.Is(err, storage.BackupKeyNotFound) {
			if err := i.clientV3.Get(ctx, types.NamespacedName{Name: name}, crd); err != nil {
				if IgnoreNoMatchOrNotFound(err) != nil {
					klog.Errorf("failed to get crd %s: %v", name, err)
					return err
				}
				return nil
			}

			if err := i.resourceStore.Save(backupKey, crd); err != nil {
				klog.Errorf("failed to save crd %s: %v", name, err)
				return err
			}
		}
	}

	return nil
}

func (i *upgradeJob) backupGatewayConfig2ConfigMap(ctx context.Context) error {
	cm := &v1.ConfigMap{}
	err := i.clientV3.Get(ctx, types.NamespacedName{Name: GatewayConfigBackupConfigMapName, Namespace: KubeSphereNamespace}, cm)
	if err != nil && k8serrors.IsNotFound(err) {
		ksConfigBytes, err := i.resourceStore.LoadRaw(core.KubeSphereConfigStoreKey)
		if err != nil {
			if errors.Is(err, storage.BackupKeyNotFound) {
				return errors.New("kubesphere-config not found in backup, please enable core upgrade job")
			}
			return err
		}

		ksConfig := make(map[string]interface{})
		if err := yaml.Unmarshal(ksConfigBytes, &ksConfig); err != nil {
			return fmt.Errorf("failed to unmarshal value from ksConfig. err: %s", err)
		}

		gatewayConfig, ok := ksConfig["gateway"]
		if !ok {
			return fmt.Errorf("failed to get gateway config from kubesphere.yaml")
		}

		yamlBytes, err := yaml.Marshal(gatewayConfig)
		if err != nil {
			return err
		}

		cm = &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      GatewayConfigBackupConfigMapName,
				Namespace: KubeSphereNamespace,
			},
			Data: map[string]string{
				KubeSphereConfigMapDataKey: string(yamlBytes),
			},
		}

		if err := i.clientV3.Create(ctx, cm); err != nil {
			return err
		}
		return nil
	}

	return err
}

func (i *upgradeJob) clearCRWithFinalizers(ctx context.Context, cr runtimeclient.Object) error {

	if err := i.clientV3.Get(ctx, runtimeclient.ObjectKeyFromObject(cr), cr); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		// if the CR is not found, it may be because last upgrade job has already deleted it.
		if strings.Contains(err.Error(), noMatchGVKErrorFunc(cr.GetObjectKind().GroupVersionKind())) {
			return nil
		}

		klog.Errorf("failed to get %s: %v", cr.GetObjectKind().GroupVersionKind().String(), err)
		return err
	}

	cr.SetFinalizers(nil)
	if err := i.clientV3.Update(ctx, cr); err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	if err := i.clientV3.Delete(ctx, cr); err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	return nil

}

func (i *upgradeJob) cleanGatewayV1CRDAndCR(ctx context.Context) error {
	// clean gateway v1 cr
	// because there is a finalizer that needs to be removed specifically, the controller that operates that finalizer has stopped.
	klog.Infof("start clean gateway v1 crd and cr")

	gatewayV1List := &gatewayv1alpha1.GatewayList{}
	if err := i.resourceStore.Load(gatewayCRStoreKey, gatewayV1List); err != nil {
		klog.Errorf("failed to load gateway v1 list: %v", err)
		return err
	}
	for _, gateway := range gatewayV1List.Items {
		if err := i.clearCRWithFinalizers(ctx, &gateway); err != nil {
			return err
		}
	}

	// clear gateway v1 crd
	crd := &apiext.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: gatewayCRDName}}
	if err := i.clientV4.Delete(ctx, crd); err != nil && !k8serrors.IsNotFound(err) {
		return err
	}
	err := wait.PollImmediate(defaultPollImmediateInterval, defaultPollImmediateTimeout, func() (bool, error) {
		if err := i.clientV3.List(ctx, &gatewayv1alpha1.GatewayList{}); err != nil {
			return true, nil
		}
		klog.Infof("wait for gateway v1 crd deleted")
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("failed to wait for gateway v1 crd deleted: %v", err)
	}
	klog.Infof("clean gateway v1 crd and cr done")

	return nil
}

func (i *upgradeJob) applyGatewayV2CRD(ctx context.Context) error {
	klog.Info("start apply gateway v2 crd")
	crd := &apiext.CustomResourceDefinition{}
	err := i.clientV4.Get(ctx, types.NamespacedName{Name: gatewayCRDName}, crd)
	if err != nil && k8serrors.IsNotFound(err) {
		opt := metav1.ApplyOptions{FieldManager: "kubectl"}
		if err := i.coreHelper.ApplyCRDsFromExtensionRef(ctx, i.extensionRef, opt, nil); err != nil {
			klog.Errorf("failed to ApplyCrdsFromExtesionRef %s", err)
			return err
		}
		klog.Info("apply gateway v2 crd success")
	}
	if err == nil {
		klog.Info("gateway v2 crd already exists")
	}
	klog.Info("apply gateway v2 crd done")

	return nil
}

func (i *upgradeJob) backupAndCleanPrometheusIngressRuleCR(ctx context.Context) error {
	prometheusIngressRule := &monitoringv1.PrometheusRule{ObjectMeta: metav1.ObjectMeta{Name: "prometheus-ingress-rules", Namespace: MonitoringNamespace}}

	err := i.resourceStore.Load(prometheusIngressRuleStoreKey, prometheusIngressRule)
	if err != nil && errors.Is(err, storage.BackupKeyNotFound) {
		objKey := runtimeclient.ObjectKey{Namespace: MonitoringNamespace, Name: "prometheus-ingress-rules"}
		if err := i.clientV3.Get(ctx, objKey, prometheusIngressRule); err != nil {
			if k8serrors.IsNotFound(err) {
				klog.Info("prometheus ingress rule cr not found, skip")
				return nil
			}
			klog.Errorf("failed to get %s: %v", objKey, err)
			return err
		}

		if err := i.resourceStore.Save(prometheusIngressRuleStoreKey, prometheusIngressRule); err != nil {
			klog.Errorf("failed to save %s: %v", prometheusIngressRuleStoreKey, err)
			return err
		}
	}

	klog.Infof("start clean prometheus ingress rule cr")
	if err := i.clientV3.Delete(ctx, prometheusIngressRule); err != nil && !k8serrors.IsNotFound(err) {
		return err
	}
	klog.Infof("clean prometheus ingress rule cr done")

	return nil
}

func (i *upgradeJob) backupObjectList(key string, list runtimeclient.ObjectList) error {
	err := i.resourceStore.Load(key, list)
	if err != nil && errors.Is(err, storage.BackupKeyNotFound) {
		if err := i.resourceStore.Save(key, list); err != nil {
			klog.Errorf("failed to save %s: %v", key, err)
			return err
		}
	}
	return nil
}

func (i *upgradeJob) getOldGatewayConfig(ctx context.Context) (oldGatewayConfig map[string]interface{}, err error) {
	cm := &v1.ConfigMap{}
	cmKey := types.NamespacedName{Name: GatewayConfigBackupConfigMapName, Namespace: KubeSphereNamespace}
	if err = i.clientV3.Get(ctx, cmKey, cm); err != nil {
		klog.Errorf("failed to get configmap %s/%s: %v", KubeSphereNamespace, GatewayConfigBackupConfigMapName, err)
		return nil, err
	}

	cmData, ok := cm.Data[KubeSphereConfigMapDataKey]
	if !ok {
		return nil, fmt.Errorf("failed to get gateway config from configmap %s/%s", KubeSphereNamespace, GatewayConfigBackupConfigMapName)
	}
	oldGatewayConfigBytes := []byte(cmData)
	if err := yaml.Unmarshal(oldGatewayConfigBytes, &oldGatewayConfig); err != nil {
		return nil, err
	}
	return oldGatewayConfig, err
}

func (i *upgradeJob) upgradeGateway(ctx context.Context, gatewayName string) error {
	klog.Infof("start upgrade gateway")

	gatewayV1List := &gatewayv1alpha1.GatewayList{}
	if err := i.resourceStore.Load(gatewayCRStoreKey, gatewayV1List); err != nil {
		klog.Errorf("failed to load gateway v1 list: %v", err)
		return err
	}

	oldGatewayConfig, err := i.getOldGatewayConfig(ctx)
	if err != nil {
		return err
	}
	repository, ok, err := unstructured.NestedString(oldGatewayConfig, "repository")
	if err != nil && !ok {
		klog.Warningf("get gateway repository error. err:%v", err)
		return err
	}

	isFindGateway := false
	for _, gatewayV1 := range gatewayV1List.Items {
		if gatewayName == gatewayV1.Name {
			gatewayV2 := &gatewayv2alpha1.Gateway{ObjectMeta: metav1.ObjectMeta{Name: gatewayV1.Name, Namespace: gatewayV1.Namespace}}
			err := i.convertGateway(ctx, &gatewayV1, gatewayV2, repository)
			if err != nil {
				klog.Errorf("failed to convert gateway %s/%s: %v", gatewayV1.Namespace, gatewayV1.Name, err)
				return err
			}
			err = i.clientV4.Create(ctx, gatewayV2)
			if err != nil {
				klog.Errorf("failed to create or update gateway %s: %v", gatewayV2.Name, err)
				return err
			}

			isFindGateway = true
			break
		}
	}

	if !isFindGateway {
		return fmt.Errorf("failed to find gateway %s", gatewayName)
	}
	klog.Infof("upgrade gateway done")

	return nil
}

func (i *upgradeJob) convertGateway(ctx context.Context, gatewayV1 *gatewayv1alpha1.Gateway, gatewayV2 *gatewayv2alpha1.Gateway, repository string) (err error) {
	gatewayV1.Spec.Controller.FullnameOverride = gatewayV1.Name
	gatewayV1.Spec.Controller.Repository = repository

	// check if the gateway is cluster gateway
	if gatewayV1.Labels[labelGatewayType] == gatewayTypeCluster {
		gatewayV1.Spec.Controller.FullnameOverride = newClusterGatewayName
		gatewayV2.Name = newClusterGatewayName
	}
	jsonBytes, err := templateHandler(&gatewayV1.Spec)
	if err != nil {
		return err
	}

	gatewayV2.Labels = gatewayV1.Labels

	if gatewayV2.Labels == nil {
		gatewayV2.Labels = make(map[string]string)
	}

	gatewayType := "project"
	if gatewayV1.Name == oldClusterGatewayName {
		gatewayType = "cluster"
	}

	gatewayV2.Labels[labelGatewayType] = gatewayType

	if gatewayType == "project" {
		ns := strings.ReplaceAll(gatewayV1.Name, kubesphereRouterPrefix, "")
		targetNs := &v1.Namespace{}
		if err := i.clientV4.Get(ctx, types.NamespacedName{Name: ns, Namespace: KubeSphereNamespace}, targetNs); err != nil {
			klog.Errorf("failed to get namespace %s: %v", ns, err)
			return err
		}
		if ws := targetNs.Labels[constants.WorkspaceLabelKey]; ws != "" {
			gatewayV2.Labels[constants.WorkspaceLabelKey] = ws
		}
	}

	gatewayV2.Annotations = gatewayV1.Annotations
	gatewayV2.Spec.AppVersion = appVersion
	gatewayV2.Spec.Values = pkgruntime.RawExtension{
		Raw: jsonBytes,
	}

	if strings.HasPrefix(gatewayV1.Name, workspaceGatewayPrefix) {
		gatewayV2.Labels["kubesphere.io/workspace"] = strings.TrimPrefix(gatewayV1.Name, workspaceGatewayPrefix)
	}

	return nil
}

func (i *upgradeJob) upgradeIngress(ctx context.Context, gatewayName string) error {
	klog.Infof("generate ingress for gateway")

	gatewayV1List := &gatewayv1alpha1.GatewayList{}
	if err := i.resourceStore.Load(gatewayCRStoreKey, gatewayV1List); err != nil {
		klog.Errorf("failed to load gateway v1 list: %v", err)
		return err
	}

	for _, gateway := range gatewayV1List.Items {
		if gatewayName == gateway.Name {
			ingressClassName := gateway.Name
			if gateway.Spec.Controller.Scope.Enabled {
				if gateway.Spec.Controller.Scope.Namespace != "" {
					i.upgradeProjectGatewayIngress(ctx, ingressClassName, gateway.Spec.Controller.Scope.Namespace)
				}
			} else {
				if gateway.Spec.Controller.Scope.NamespaceSelector != "" {
					i.upgradeWorkspaceGatewayIngress(ctx, ingressClassName, gateway.Spec.Controller.Scope.NamespaceSelector)
				} else {
					i.upgradeClusterGatewayIngress(ctx, ingressClassName)
				}
			}

			break
		}
	}
	klog.Infof("generate ingress for gateway done")

	return nil
}

func (i *upgradeJob) upgradeWorkspaceGatewayIngress(ctx context.Context, ingressClassName, namespaceSelector string) error {
	namespaceList := &v1.NamespaceList{}
	lbSelector, _ := labels.Parse(namespaceSelector)
	if err := i.clientV4.List(ctx, namespaceList, &runtimeclient.ListOptions{LabelSelector: lbSelector}); err != nil {
		klog.Errorf("failed to list namespace in workspace: %v", err)
		return err
	}

	for _, ns := range namespaceList.Items {
		ingressList := &networkingv1.IngressList{}
		if err := i.clientV4.List(ctx, ingressList, runtimeclient.InNamespace(ns.Name)); err != nil {
			klog.Errorf("failed to list ingress in namespace of workspace: %v", err)
			return err
		}

		i.updateIngress(ctx, ingressList, ingressClassName)
	}

	return nil
}

func (i *upgradeJob) upgradeClusterGatewayIngress(ctx context.Context, ingressClassName string) error {
	ingressList := &networkingv1.IngressList{}
	if err := i.clientV4.List(ctx, ingressList); err != nil {
		klog.Errorf("failed to list ingress in cluster: %v", err)
		return err
	}
	ingressClassName = newClusterGatewayName

	return i.updateIngress(ctx, ingressList, ingressClassName)
}

func (i *upgradeJob) upgradeProjectGatewayIngress(ctx context.Context, ingressClassName, namespace string) error {
	ingressList := &networkingv1.IngressList{}
	if err := i.clientV4.List(ctx, ingressList, runtimeclient.InNamespace(namespace)); err != nil {
		klog.Errorf("failed to list ingress in namespace %s: %v", namespace, err)
		return err
	}

	return i.updateIngress(ctx, ingressList, ingressClassName)
}

// updateIngress with ingressClassName for ingress without ingressClassName
func (i *upgradeJob) updateIngress(ctx context.Context, ingressList *networkingv1.IngressList, ingressClassName string) error {
	for _, ingress := range ingressList.Items {
		// only update oldIngress without ingressClassName
		if ingress.Spec.IngressClassName != nil {
			continue
		}

		oldIngress := ingress.DeepCopy()
		oldIngress.ResourceVersion = ""
		oldIngress.Spec.IngressClassName = &ingressClassName

		if err := i.clientV4.Update(ctx, oldIngress); err != nil {
			klog.Errorf("failed to create ingress: %v", err)
			return err
		}
	}

	return nil
}

//go:embed values.yaml
var fs embed.FS

func templateHandler(spec *gatewayv1alpha1.GatewaySpec) ([]byte, error) {
	tmplName := "values.yaml"

	tmpl, err := template.New(tmplName).Funcs(template.FuncMap{
		"toYaml":  toYaml,
		"nindent": nindent,
	}).ParseFS(fs, tmplName)
	if err != nil {
		klog.Errorf("failed to parse template: %v", err)
		return nil, err
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, tmplName, spec); err != nil {
		klog.Errorf("failed to execute template: %v", err)
		return nil, err
	}

	values := map[string]any{}
	if err := yaml.Unmarshal(buf.Bytes(), &values); err != nil {
		klog.Errorf("failed to unmarshal: %v", err)
		return nil, err
	}

	jsonBytes, err := json.Marshal(values)
	if err != nil {
		klog.Errorf("failed to marshal: %v", err)
		return nil, err
	}

	return jsonBytes, nil
}

func toYaml(data any) (string, error) {
	// There may be fields in data that do not need to be serialized (marked by json tags),
	// so they cannot be directly serialized into yaml. They need to be serialized into json first and finally into yaml.
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	var unmarshaledData any
	if err := json.Unmarshal(jsonBytes, &unmarshaledData); err != nil {
		return "", err
	}

	yamlBytes, err := yaml.Marshal(unmarshaledData)
	if err != nil {
		return "", err
	}

	ret := string(yamlBytes)
	if strings.Contains(ret, "null") {
		ret = strings.ReplaceAll(ret, "null", "{}")
	}
	return ret, nil
}

func nindent(indent int, value string) string {
	blank, line := " ", "\n"
	indentedValue := strings.ReplaceAll(value, line, line+strings.Repeat(blank, indent))
	return line + strings.Repeat(blank, indent) + indentedValue
}
