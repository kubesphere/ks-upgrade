package core

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	schemev4 "kubesphere.io/ks-upgrade/v4/scheme"
	"mime"
	"net/http"
	"path"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"strings"
	"time"

	"gopkg.in/yaml.v2"
	"helm.sh/helm/v3/pkg/chart/loader"
	v1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	yamlutils "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"kubesphere.io/ks-upgrade/pkg/download"
	"kubesphere.io/ks-upgrade/pkg/helm"
	"kubesphere.io/ks-upgrade/pkg/model"
	v3clusterv1alpha1 "kubesphere.io/ks-upgrade/v3/api/cluster/v1alpha1"
	schemev3 "kubesphere.io/ks-upgrade/v3/scheme"
	v4clusterv1alpha1 "kubesphere.io/ks-upgrade/v4/api/cluster/v1alpha1"
	"kubesphere.io/ks-upgrade/v4/api/core/v1alpha1"
	corev1alpha1 "kubesphere.io/ks-upgrade/v4/api/core/v1alpha1"
)

var SchemeNotSupported = errors.New("unsupported scheme")
var (
	KubeSphereNamespace        = "kubesphere-system"
	KubeSphereConfigName       = "kubesphere-config"
	KubeSphereConfigMapDataKey = "kubesphere.yaml"
	KubeSphereConfigStoreKey   = "kubesphere-config"

	ClusterConfigurationGVR = schema.GroupVersionResource{Group: "installer.kubesphere.io", Version: "v1alpha1", Resource: "clusterconfigurations"}
	KubeSphereInstallerName = "ks-installer"

	ExtensionConfigMapKey        = "chart.tgz"
	ExtensionConfigMapNameFormat = "extension-%s-%s-chart"
	ExtensionURLFmt              = "https://extensions-museum.kubesphere-system.svc/charts/%s-%s.tgz"
)

type Helper interface {
	CreateInstallPlanFromExtensionRef(ctx context.Context, extensionRef *model.ExtensionRef, watchFuncs ...WatchFunc) error
	ApplyCRDsFromExtensionRef(ctx context.Context, extensionRef *model.ExtensionRef,
		options metav1.ApplyOptions, ignore func(apiextensionsv1.CustomResourceDefinition) bool) error
	ListCluster(ctx context.Context, clusterList runtime.Object) error
	IsHostCluster(ctx context.Context) (bool, error)
	GetClusterConfiguration(ctx context.Context) (map[string]interface{}, error)
	GetChartDownloader() *download.ChartDownloader
	SyncHelmChart(ctx context.Context, extensionRef *model.ExtensionRef) error
	GenerateClusterScheduling(ctx context.Context, extensionRef *model.ExtensionRef, isEnabledFunc func(cc map[string]any) (bool, error),
		convertConfigFunc func(cc map[string]any) (map[string]any, error)) error
	GetSpecifiedClusterConfiguration(ctx context.Context, cluster v4clusterv1alpha1.Cluster) (map[string]interface{}, error)
	NewRuntimeClientV3(kubeconfig []byte) (runtimeclient.Client, error)
	NewRuntimeClientV4(kubeconfig []byte) (runtimeclient.Client, error)
	PluginEnabled(ctx context.Context, kubeconfig []byte, paths ...string) (bool, error)
}

func NewCoreHelper(clientV3 runtimeclient.Client, clientV4 runtimeclient.Client, dynamicClient *dynamic.DynamicClient, chartDownloader *download.ChartDownloader) Helper {
	return &coreHelper{
		clientV3:        clientV3,
		clientV4:        clientV4,
		dynamicClient:   dynamicClient,
		chartDownloader: chartDownloader,
	}
}

type coreHelper struct {
	dynamicClient   *dynamic.DynamicClient
	clientV3        runtimeclient.Client
	clientV4        runtimeclient.Client
	chartDownloader *download.ChartDownloader
}

type WatchFunc func() (ctx context.Context, interval time.Duration, timeout time.Duration, condition wait.ConditionWithContextFunc)

func (c *coreHelper) CreateInstallPlanFromExtensionRef(ctx context.Context, extensionRef *model.ExtensionRef, watchFuncs ...WatchFunc) error {
	if isHostCluster, err := c.IsHostCluster(ctx); !isHostCluster {
		klog.Errorf("failed to check if it is host cluster: %s", err)
		return err
	}

	// Check if extensionVersion was created.
	extensionVersionName := fmt.Sprintf("%s-%s", extensionRef.Name, extensionRef.Version)
	extensionVersion := corev1alpha1.ExtensionVersion{}
	if err := c.clientV4.Get(ctx, types.NamespacedName{Name: extensionVersionName}, &extensionVersion, &runtimeclient.GetOptions{}); err != nil {
		klog.Errorf("failed to get extensionVersion %s: %s", extensionVersionName, err)
		return err
	}

	// Check if installPlan was created.
	installPlan := corev1alpha1.InstallPlan{}
	if err := c.clientV4.Get(ctx, types.NamespacedName{Name: extensionRef.Name}, &installPlan, &runtimeclient.GetOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("failed to get installPlan %s: %s", extensionRef.Name, err)
			return err
		}
		installPlan.SetName(extensionRef.Name)
		installPlan.Spec.Enabled = true
		installPlan.Spec.Extension.Name = extensionRef.Name
		installPlan.Spec.Extension.Version = extensionRef.Version
		installPlan.Spec.UpgradeStrategy = corev1alpha1.Manual
		installPlan.Spec.Config = extensionRef.Config
		if extensionRef.ClusterScheduling != nil {
			installPlan.Spec.ClusterScheduling = extensionRef.ClusterScheduling
		}
		if err := c.clientV4.Create(ctx, &installPlan, &runtimeclient.CreateOptions{}); err != nil {
			klog.Errorf("failed to create installPlan %s: %s", extensionRef.Name, err)
			return err
		}
	}
	if len(watchFuncs) > 0 {
		for _, watchFunc := range watchFuncs {
			if err := wait.PollImmediateWithContext(watchFunc()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *coreHelper) ListCluster(ctx context.Context, clusterList runtime.Object) error {
	switch v := clusterList.(type) {
	case *v3clusterv1alpha1.ClusterList:
		return c.clientV3.List(ctx, v, &runtimeclient.ListOptions{})
	case *v4clusterv1alpha1.ClusterList:
		return c.clientV4.List(ctx, v, &runtimeclient.ListOptions{})
	}
	return SchemeNotSupported
}

func (c *coreHelper) IsHostCluster(ctx context.Context) (bool, error) {
	configMap := v1.ConfigMap{}
	if err := c.clientV4.Get(ctx, types.NamespacedName{Namespace: KubeSphereNamespace, Name: KubeSphereConfigName}, &configMap, &runtimeclient.GetOptions{}); err != nil {
		return false, err
	}
	value, ok := configMap.Data[KubeSphereConfigMapDataKey]
	if !ok {
		return false, fmt.Errorf("failed to get configmap kubesphere.yaml value")
	}
	ksConfig := make(map[string]interface{})
	if err := yaml.Unmarshal([]byte(value), &ksConfig); err != nil {
		return false, fmt.Errorf("failed to unmarshal value from configmap. err: %s", err)
	}
	if multiClusterConf, ok := ksConfig["multicluster"]; ok {
		if multiClusterConfInner, ok := multiClusterConf.(map[interface{}]interface{}); ok {
			if clusterRole, ok := multiClusterConfInner["clusterRole"]; ok {
				if clusterRoleInner, ok := clusterRole.(string); ok && (clusterRoleInner == "host" || clusterRoleInner == "none") {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

func (c *coreHelper) GetClusterConfiguration(ctx context.Context) (map[string]interface{}, error) {

	cc, err := c.dynamicClient.Resource(ClusterConfigurationGVR).Namespace(KubeSphereNamespace).Get(ctx, KubeSphereInstallerName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return cc.Object, nil
}

func (c *coreHelper) GetChartDownloader() *download.ChartDownloader {
	return c.chartDownloader
}

// The apply behavior here is like kubectl server side apply but does not handle managed fields migration.
// Refer to https://github.com/kubernetes/kubernetes/blob/release-1.27/staging/src/k8s.io/kubectl/pkg/cmd/apply/apply.go#L563
func (c *coreHelper) ApplyCRDsFromExtensionRef(ctx context.Context, extensionRef *model.ExtensionRef,
	options metav1.ApplyOptions, ignore func(apiextensionsv1.CustomResourceDefinition) bool) error {

	var chartBuf *bytes.Buffer
	var err error
	chartURL := fmt.Sprintf("https://extensions-museum.kubesphere-system.svc/charts/%s-%s.tgz", extensionRef.Name, extensionRef.Version)
	chartBuf, err = c.chartDownloader.Download(chartURL)
	if err != nil {
		klog.V(2).Info("failed to download chart, try to get from configmap")
		cm := v1.ConfigMap{}
		cm.Name = fmt.Sprintf(ExtensionConfigMapNameFormat, extensionRef.Name, extensionRef.Version)
		err = c.clientV4.Get(ctx, types.NamespacedName{Name: cm.Name, Namespace: KubeSphereNamespace}, &cm)
		if err != nil {
			return fmt.Errorf("failed to get configmap %s/%s, err: %s", cm.Namespace, cm.Name, err)
		}
		chartBytes, ok := cm.BinaryData[ExtensionConfigMapKey]
		if !ok {
			return fmt.Errorf("failed to get chart data from configmap %s", cm.Name)
		}
		chartBuf = bytes.NewBuffer(chartBytes)
	}

	chart, err := loader.LoadArchive(chartBuf)
	if err != nil {
		return fmt.Errorf("failed to load chart data: %v", err)
	}
	crdClient := c.dynamicClient.Resource(apiextensionsv1.SchemeGroupVersion.WithResource("customresourcedefinitions"))
	codecs := serializer.NewCodecFactory(c.clientV4.Scheme())
	for _, chartCRD := range chart.CRDObjects() {
		obj, _, err := codecs.UniversalDeserializer().Decode(chartCRD.File.Data, nil, nil)
		if err != nil {
			return fmt.Errorf("failed to decode chart crd: %v", err)
		}
		crd, ok := obj.(*apiextensionsv1.CustomResourceDefinition)
		if !ok {
			continue
		}
		if ignore != nil && ignore(*crd) {
			continue
		}
		uns, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		if err != nil {
			return err
		}
		if _, err := crdClient.Apply(ctx, crd.Name, &unstructured.Unstructured{Object: uns}, options); err != nil {
			return err
		}
	}
	return nil
}

// GenerateClusterScheduling generates cluster scheduling configuration based on the specified conditions.
// The isEnabledFunc is used to determine whether the component is enabled in the cluster configuration.
// The convertConfigFunc is used to convert the old configuration into a new configuration as an override.
// If the convertConfigFunc is nil, the cluster overrides is nil.
func (c *coreHelper) GenerateClusterScheduling(ctx context.Context, extensionRef *model.ExtensionRef,
	isEnabledFunc func(cc map[string]any) (bool, error), convertConfigFunc func(cc map[string]any) (map[string]any, error)) error {

	isHost, err := c.IsHostCluster(ctx)
	if err != nil {
		return err
	}
	if !isHost {
		klog.Infof("upgrade running on member cluster, skip generate clusterScheduling")
		return nil
	}

	if extensionRef.ClusterScheduling == nil {
		extensionRef.ClusterScheduling = &v1alpha1.ClusterScheduling{}
	}
	if extensionRef.ClusterScheduling.Placement == nil {
		extensionRef.ClusterScheduling.Placement = &v1alpha1.Placement{}
	}
	if len(extensionRef.ClusterScheduling.Placement.Clusters) == 0 && extensionRef.ClusterScheduling.Placement.ClusterSelector == nil {
		klog.Infof("generate clusterScheduling since it is not specified")

		var clusterNames []string
		overrides := map[string]string{}
		clusterList := v4clusterv1alpha1.ClusterList{}
		if err := c.ListCluster(ctx, &clusterList); err != nil {
			return err
		}

		computeClusterScheduling := func(cc map[string]any, clusterName string) error {
			enabled, err := isEnabledFunc(cc)
			if err != nil {
				klog.Errorf("get %s cluster cc config enabled err %s", clusterName, err)
				return err
			}
			if enabled {
				klog.Infof("cluster: %s enabled, add to clusterScheduling", clusterName)
				clusterNames = append(clusterNames, clusterName)
			}

			if convertConfigFunc != nil {
				newValuesMap, err := convertConfigFunc(cc)
				if err != nil {
					klog.Errorf("%s cluster config convert err %s", clusterName, err)
					return err
				}
				if newValuesMap != nil {
					yamlStr, err := c.Map2YamlString(newValuesMap)
					if err != nil {
						klog.Errorf("%s cluster config convert to yaml err %s", clusterName, err)
						return err
					}
					overrides[clusterName] = yamlStr
				}
			}
			return nil
		}

		// multi-cluster mode
		for _, cluster := range clusterList.Items {
			cc, err := c.GetSpecifiedClusterConfiguration(ctx, cluster)
			if err != nil {
				klog.Errorf("get cluster %s cc config err %s", cluster.Name, err)
				return err
			}
			computeClusterScheduling(cc, cluster.Name)
		}
		// single-cluster mode
		if len(clusterList.Items) == 0 {
			cc, err := c.GetClusterConfiguration(ctx)
			clusterName := "host"
			if err != nil {
				klog.Errorf("get cluster %s cc config err %s", clusterName, err)
				return err
			}
			computeClusterScheduling(cc, clusterName)
		}

		if len(clusterNames) == 0 {
			klog.Infof("no cluster enabled %s, skip generate clusterScheduling", extensionRef.Name)
			return nil
		}

		extensionRef.ClusterScheduling.Placement.Clusters = clusterNames
		extensionRef.ClusterScheduling.Overrides = overrides
	}
	return nil
}

func (c *coreHelper) Map2YamlString(config map[string]any) (yamlString string, err error) {
	var valuesBytes []byte
	if valuesBytes, err = yaml.Marshal(config); err != nil {
		return yamlString, err
	}
	return string(valuesBytes), nil
}

func (c *coreHelper) GetSpecifiedClusterConfiguration(ctx context.Context, cluster v4clusterv1alpha1.Cluster) (map[string]interface{}, error) {
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(cluster.Spec.Connection.KubeConfig)
	if err != nil {
		return nil, err
	}
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %s", err)
	}
	unstructed, err := dynamicClient.Resource(ClusterConfigurationGVR).Namespace(KubeSphereNamespace).Get(ctx, KubeSphereInstallerName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return unstructed.Object, nil
}

func (c *coreHelper) NewRuntimeClientV3(kubeconfig []byte) (runtimeclient.Client, error) {
	clientConfig, err := clientcmd.NewClientConfigFromBytes(kubeconfig)
	if err != nil {
		return nil, err
	}
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, err
	}

	v3Schema := runtime.NewScheme()
	if err = schemev3.AddToScheme(v3Schema); err != nil {
		return nil, fmt.Errorf("failed to register scheme v3: %s", err)
	}

	return runtimeclient.New(restConfig, runtimeclient.Options{
		Scheme: v3Schema,
	})
}

func (c *coreHelper) NewRuntimeClientV4(kubeconfig []byte) (runtimeclient.Client, error) {
	clientConfig, err := clientcmd.NewClientConfigFromBytes(kubeconfig)
	if err != nil {
		return nil, err
	}
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, err
	}
	v4Schema := runtime.NewScheme()
	if err = schemev4.AddToScheme(v4Schema); err != nil {
		return nil, fmt.Errorf("failed to register scheme v4: %s", err)
	}
	return runtimeclient.New(restConfig, runtimeclient.Options{
		Scheme: v4Schema,
	})
}

func (c *coreHelper) PluginEnabled(ctx context.Context, kubeconfig []byte, paths ...string) (bool, error) {
	var client *dynamic.DynamicClient
	if kubeconfig != nil {
		clientConfig, err := clientcmd.NewClientConfigFromBytes(kubeconfig)
		if err != nil {
			return false, err
		}
		restConfig, err := clientConfig.ClientConfig()
		if err != nil {
			return false, err
		}
		if client, err = dynamic.NewForConfig(restConfig); err != nil {
			return false, err
		}
	}
	if client == nil {
		client = c.dynamicClient
	}

	cc, err := client.Resource(ClusterConfigurationGVR).Namespace(KubeSphereNamespace).Get(ctx, KubeSphereInstallerName, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	enabled, _, err := unstructured.NestedBool(cc.Object, paths...)
	return enabled, err
}

// LoadExtensionSpec loads the extension specification from a chart file.
func LoadExtensionSpec(chartBuf []byte) (*corev1alpha1.ExtensionVersionSpec, error) {
	// Get extension.yaml from chart
	extensionBuf, err := helm.GetFileFromChart(chartBuf, "extension.yaml")
	if err != nil {
		return nil, fmt.Errorf("failed to get extension.yaml from chart: %v", err)
	}

	// Check if the file content is empty
	if len(extensionBuf) == 0 {
		return nil, fmt.Errorf("extension.yaml is empty")
	}

	// Parse extension spec
	var spec corev1alpha1.ExtensionVersionSpec
	if err = yamlutils.NewYAMLOrJSONDecoder(bytes.NewReader(extensionBuf), 1024).Decode(&spec); err != nil {
		return nil, fmt.Errorf("failed to parse extension.yaml: %v", err)
	}
	if spec.Icon != "" && !strings.HasPrefix(spec.Icon, "http://") &&
		!strings.HasPrefix(spec.Icon, "https://") &&
		!strings.HasPrefix(spec.Icon, "data:image") {

		iconPath := spec.Icon
		if strings.HasPrefix(spec.Icon, "./") {
			iconPath = strings.TrimPrefix(spec.Icon, "./")
		}
		if iconBuf, err := helm.GetFileFromChart(chartBuf, iconPath); err != nil {
			return nil, fmt.Errorf("failed to get extension.yaml from chart: %v", err)
		} else {
			var base64Encoding string
			mimeType := mime.TypeByExtension(path.Ext(iconPath))
			if mimeType == "" {
				mimeType = http.DetectContentType(iconBuf)
			}
			base64Encoding += "data:" + mimeType + ";base64,"
			base64Encoding += base64.StdEncoding.EncodeToString(iconBuf)
			spec.Icon = base64Encoding
		}
	}
	return &spec, nil
}

func (c *coreHelper) SyncHelmChart(ctx context.Context, extensionRef *model.ExtensionRef) error {
	isHostCluster, err := c.IsHostCluster(ctx)
	if err != nil {
		return err
	}
	if !isHostCluster {
		klog.Infof("skip sync helm chart to member cluster")
		return nil
	}

	chartURL := fmt.Sprintf(ExtensionURLFmt, extensionRef.Name, extensionRef.Version)
	chartData, err := c.chartDownloader.Download(chartURL)
	if err != nil {
		klog.Errorf("failed to download chart %s: %s", chartURL, err)
		return err
	}

	clusters := &v4clusterv1alpha1.ClusterList{}
	if err := c.ListCluster(ctx, clusters); err != nil {
		return err
	}
	for _, cluster := range clusters.Items {
		clusterClient, err := c.NewRuntimeClientV4(cluster.Spec.Connection.KubeConfig)
		if err != nil {
			return err
		}
		configMap := v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf(ExtensionConfigMapNameFormat, extensionRef.Name, extensionRef.Version),
				Namespace: KubeSphereNamespace,
			},
		}
		op, err := controllerruntime.CreateOrUpdate(ctx, clusterClient, &configMap, func() error {
			configMap.BinaryData = map[string][]byte{ExtensionConfigMapKey: chartData.Bytes()}
			return nil
		})
		klog.Infof("op: %s, cluster: %s, configMap: %s", op, cluster.Name, configMap.Name)
	}

	return nil
}
