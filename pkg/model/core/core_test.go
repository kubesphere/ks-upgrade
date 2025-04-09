package core

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	restconfig "sigs.k8s.io/controller-runtime/pkg/client/config"

	"kubesphere.io/ks-upgrade/pkg/download"
	"kubesphere.io/ks-upgrade/pkg/model"
	v3clusterv1alpha1 "kubesphere.io/ks-upgrade/v3/api/cluster/v1alpha1"
	schemev3 "kubesphere.io/ks-upgrade/v3/scheme"
	v4clusterv1alpha1 "kubesphere.io/ks-upgrade/v4/api/cluster/v1alpha1"
	corev1alpha1 "kubesphere.io/ks-upgrade/v4/api/core/v1alpha1"
	schemev4 "kubesphere.io/ks-upgrade/v4/scheme"
)

var (
	runtimeClientV3 runtimeclient.Client
	runtimeClientV4 runtimeclient.Client
)

func createCoreHelper() (Helper, error) {
	restConfig, err := restconfig.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get rest config: %s", err)
	}

	runtimeClientV3, err = runtimeclient.New(restConfig, runtimeclient.Options{
		Scheme: runtime.NewScheme(),
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create runtime client v3: %s", err)
	}

	if err := scheme.AddToScheme(runtimeClientV3.Scheme()); err != nil {
		return nil, err
	}

	if err := schemev3.AddToScheme(runtimeClientV3.Scheme()); err != nil {
		return nil, fmt.Errorf("failed to register scheme v3: %s", err)
	}

	runtimeClientV4, err = runtimeclient.New(restConfig, runtimeclient.Options{
		Scheme: runtime.NewScheme(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create runtime client v3: %s", err)
	}

	if err := scheme.AddToScheme(runtimeClientV4.Scheme()); err != nil {
		return nil, err
	}

	if err = schemev4.AddToScheme(runtimeClientV4.Scheme()); err != nil {
		return nil, fmt.Errorf("failed to register scheme v3: %s", err)
	}
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %s", err)
	}

	chartDownloader, err := download.NewChartDownloader()
	if err != nil {
		return nil, fmt.Errorf("failed to create chartDownloader client: %s", err)
	}
	coreHelper := NewCoreHelper(runtimeClientV3, runtimeClientV4, dynamicClient, chartDownloader)
	return coreHelper, nil
}

func TestCoreHelper_CreateInstallPlanFromExtensionRef(t *testing.T) {
	coreHelper, err := createCoreHelper()
	assert.Equal(t, err, nil)

	extensionRef := model.ExtensionRef{
		Name:    "sample-extension",
		Version: "0.0.1",
	}

	resultChan := make(chan struct{}, 1)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*5)
	defer cancel()

	watchFuncs := []WatchFunc{
		func() (context.Context, time.Duration, time.Duration, wait.ConditionWithContextFunc) {
			return ctx, time.Millisecond * 500, time.Minute * 2, func(ctx context.Context) (done bool, err error) {
				installPlan := corev1alpha1.InstallPlan{}
				if err := runtimeClientV4.Get(ctx, types.NamespacedName{Name: extensionRef.Name}, &installPlan, &runtimeclient.GetOptions{}); err != nil {
					return false, err
				}
				if installPlan.Status.State == corev1alpha1.StateInstalled || installPlan.Status.State == corev1alpha1.StateUpgraded {
					return true, nil
				}
				return false, nil
			}
		},
	}

	err = coreHelper.CreateInstallPlanFromExtensionRef(ctx, &extensionRef, watchFuncs...)
	select {
	case <-resultChan:
		cancel()
		break
	case <-ctx.Done():
		break
	}
	close(resultChan)
	assert.Equal(t, err, nil)
}

func TestCoreHelper_ListClusterV3(t *testing.T) {
	coreHelper, err := createCoreHelper()
	assert.Equal(t, err, nil)

	v3ClusterList := v3clusterv1alpha1.ClusterList{}
	err = coreHelper.ListCluster(context.Background(), &v3ClusterList)
	assert.Equal(t, err, nil)
	t.Log(v3ClusterList)
}

func TestCoreHelper_ListClusterV4(t *testing.T) {
	coreHelper, err := createCoreHelper()
	assert.Equal(t, err, nil)

	v4ClusterList := v4clusterv1alpha1.ClusterList{}
	err = coreHelper.ListCluster(context.Background(), &v4ClusterList)
	assert.Equal(t, err, nil)
	t.Log(v4ClusterList)
}

func TestCoreHelper_GetClusterConfiguration(t *testing.T) {
	coreHelper, err := createCoreHelper()
	assert.Equal(t, err, nil)

	ctx := context.Background()
	cc, err := coreHelper.GetClusterConfiguration(ctx)
	assert.Equal(t, err, nil)

	name, ok, err := unstructured.NestedString(cc, "metadata", "name")
	assert.Equal(t, err, nil)
	assert.Equal(t, ok, true)
	assert.Equal(t, name, KubeSphereInstallerName)
}

func TestCoreHelper_ApplyCrdsFromExtesionRef(t *testing.T) {
	cHelper, err := createCoreHelper()
	assert.Equal(t, err, nil)
	downloader, err := download.NewChartDownloader()
	assert.Equal(t, err, nil)
	cHelper.(*coreHelper).chartDownloader = downloader
	ctx := context.Background()
	extensionRef := model.ExtensionRef{
		Name:    "whizard-alerting",
		Version: "0.1.9-rc.2",
	}
	// just dry run
	err = cHelper.ApplyCRDsFromExtensionRef(ctx, &extensionRef,
		metav1.ApplyOptions{
			DryRun:       []string{metav1.DryRunAll},
			FieldManager: "kubectl",
		}, nil)
	assert.Equal(t, err, nil)
}

func TestCoreHelper_GenerateClusterScheduling(t *testing.T) {
	coreHelper, err := createCoreHelper()
	assert.Equal(t, err, nil)

	ctx := context.Background()
	extensionRef := &model.ExtensionRef{
		Name:    "springcloud",
		Version: "0.1.0",
	}
	isEnabledFunc := func(cc map[string]any) (bool, error) {
		enabled, _, err := unstructured.NestedBool(cc, "spec", "springcloud", "enabled")
		return enabled, err
	}
	convertConfigFunc := func(cc map[string]any) (map[string]any, error) {
		oldValuesMap, _, err := unstructured.NestedMap(cc, "spec", "springcloud", "nacos")
		if err != nil {
			return nil, err
		}
		newValuesMap := map[string]any{
			"backend": map[string]any{
				"nacos": oldValuesMap,
			},
		}
		return newValuesMap, nil
	}

	err = coreHelper.GenerateClusterScheduling(ctx, extensionRef, isEnabledFunc, convertConfigFunc)
	assert.Equal(t, err, nil)
	t.Log(extensionRef)
}
