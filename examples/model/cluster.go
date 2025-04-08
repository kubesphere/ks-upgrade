package model

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	restconfig "sigs.k8s.io/controller-runtime/pkg/client/config"

	"kubesphere.io/ks-upgrade/pkg/download"
	"kubesphere.io/ks-upgrade/pkg/model/core"
	schemev3 "kubesphere.io/ks-upgrade/v3/scheme"
	schemev4 "kubesphere.io/ks-upgrade/v4/scheme"
)

func createCoreHelper() (core.Helper, error) {
	restConfig, err := restconfig.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get rest config: %s", err)
	}

	runtimeClientV3, err := runtimeclient.New(restConfig, runtimeclient.Options{
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

	runtimeClientV4, err := runtimeclient.New(restConfig, runtimeclient.Options{
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

	coreHelper := core.NewCoreHelper(runtimeClientV3, runtimeClientV4, dynamicClient, chartDownloader)
	return coreHelper, nil
}
