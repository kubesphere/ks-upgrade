package installplan_create

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	restconfig "sigs.k8s.io/controller-runtime/pkg/client/config"

	"kubesphere.io/ks-upgrade/pkg/download"
	"kubesphere.io/ks-upgrade/pkg/model"
	"kubesphere.io/ks-upgrade/pkg/model/core"
	schemev3 "kubesphere.io/ks-upgrade/v3/scheme"
	schemev4 "kubesphere.io/ks-upgrade/v4/scheme"
)

func createClient() (runtimeclient.Client, runtimeclient.Client, *dynamic.DynamicClient, error) {
	restConfig, err := restconfig.GetConfig()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get rest config: %s", err)
	}

	runtimeClientV3, err := runtimeclient.New(restConfig, runtimeclient.Options{
		Scheme: runtime.NewScheme(),
	})

	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create runtime client v3: %s", err)
	}

	if err := scheme.AddToScheme(runtimeClientV3.Scheme()); err != nil {
		return nil, nil, nil, err
	}

	if err := schemev3.AddToScheme(runtimeClientV3.Scheme()); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to register scheme v3: %s", err)
	}

	runtimeClientV4, err := runtimeclient.New(restConfig, runtimeclient.Options{
		Scheme: runtime.NewScheme(),
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create runtime client v3: %s", err)
	}

	if err := scheme.AddToScheme(runtimeClientV4.Scheme()); err != nil {
		return nil, nil, nil, err
	}

	if err = schemev4.AddToScheme(runtimeClientV4.Scheme()); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to register scheme v3: %s", err)
	}
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create dynamic client: %s", err)
	}
	return runtimeClientV3, runtimeClientV4, dynamicClient, nil
}

func createInstallPlanFromExtensionRef() error {
	clientV3, clientV4, dynamicClient, err := createClient()
	if err != nil {
		return err
	}
	ctx := context.Background()

	extensionRef := model.ExtensionRef{
		Name:    "sample-extension",
		Version: "0.0.1",
	}

	chartDownloader, err := download.NewChartDownloader()
	if err != nil {
		return fmt.Errorf("failed to create chartDownloader client: %s", err)
	}
	coreHelper := core.NewCoreHelper(clientV3, clientV4, dynamicClient, chartDownloader)
	return coreHelper.CreateInstallPlanFromExtensionRef(ctx, &extensionRef)
}
