package helm

import (
	"context"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/kube"
	"helm.sh/helm/v3/pkg/registry"
	"helm.sh/helm/v3/pkg/release"
	relutil "helm.sh/helm/v3/pkg/releaseutil"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"
	"k8s.io/klog/v2"
)

type HelmClient interface {
	ListReleases(namespace string) ([]*release.Release, error)
	GetRelease(namespace, name string) (*release.Release, error)
	Uninstall(namespace string, name string, options ...interface{}) (*release.UninstallReleaseResponse, error)
	Install(ctx context.Context, namespace string, name string, path string, vals map[string]interface{}) (*release.Release, error)
	Upgrade(ctx context.Context, namespace string, name string, path string, vals map[string]interface{}) (*release.Release, error)
	Status(namespace string, name string) (*release.Release, error)
	History(namespace string, name string) ([]*release.Release, error)
	LastReleaseDeployed(namespace string, name string) (*release.Release, error)
}

type client struct {
	kubeClient *kube.Client
	lazyClient *lazyClient
	cfg        *action.Configuration
}

func (c *client) ListReleases(namespace string) ([]*release.Release, error) {
	c.lazyClient.namespace = namespace
	c.kubeClient.Namespace = namespace
	listAction := action.NewList(c.cfg)
	return listAction.Run()
}

func (c *client) GetRelease(namespace, name string) (*release.Release, error) {
	c.lazyClient.namespace = namespace
	c.kubeClient.Namespace = namespace
	getAction := action.NewGet(c.cfg)
	return getAction.Run(name)
}

// Uninstall uninstall helm release, options are option parameters in Wait and timeout.
func (c *client) Uninstall(namespace string, name string, options ...interface{}) (*release.UninstallReleaseResponse, error) {
	c.lazyClient.namespace = namespace
	c.kubeClient.Namespace = namespace
	uninstallAction := action.NewUninstall(c.cfg)
	for _, opt := range options {
		switch opt.(type) {
		case bool:
			uninstallAction.Wait = opt.(bool)
		case time.Duration:
			uninstallAction.Timeout = opt.(time.Duration)
		}
	}
	return uninstallAction.Run(name)
}

func (c *client) Install(ctx context.Context, namespace string, name string, path string, vals map[string]interface{}) (*release.Release, error) {
	c.lazyClient.namespace = namespace
	c.kubeClient.Namespace = namespace
	installAction := action.NewInstall(c.cfg)
	chart, err := loader.Load(path)
	if err != nil {
		return nil, err
	}
	installAction.Namespace = namespace
	installAction.ReleaseName = name
	return installAction.RunWithContext(ctx, chart, vals)
}

func (c *client) Upgrade(ctx context.Context, namespace string, name string, path string, vals map[string]interface{}) (*release.Release, error) {
	c.lazyClient.namespace = namespace
	c.kubeClient.Namespace = namespace
	upgradeAction := action.NewUpgrade(c.cfg)
	chart, err := loader.Load(path)
	if err != nil {
		return nil, err
	}
	upgradeAction.Namespace = namespace
	return upgradeAction.RunWithContext(ctx, name, chart, vals)
}

func (c *client) Status(namespace string, name string) (*release.Release, error) {
	c.lazyClient.namespace = namespace
	c.kubeClient.Namespace = namespace
	statusAction := action.NewStatus(c.cfg)
	return statusAction.Run(name)
}

func (c *client) History(namespace string, name string) ([]*release.Release, error) {
	c.lazyClient.namespace = namespace
	c.kubeClient.Namespace = namespace
	statusAction := action.NewHistory(c.cfg)
	return statusAction.Run(name)
}

func (c *client) LastReleaseDeployed(namespace string, name string) (*release.Release, error) {
	c.lazyClient.namespace = namespace
	c.kubeClient.Namespace = namespace

	h, err := c.History(namespace, name)
	if err != nil {
		return nil, err
	}
	if len(h) == 0 {
		return nil, driver.ErrReleaseNotFound
	}

	relutil.Reverse(h, relutil.SortByRevision)
	for _, rd := range h {
		if rd.Info.Status == release.StatusDeployed {
			return rd, nil
		}
	}

	return nil, driver.NewErrNoDeployedReleases(name)
}

func NewClient(namespace string) (HelmClient, error) {
	registryClient, err := registry.NewClient()
	if err != nil {
		return nil, err
	}

	kubeClient := kube.New(NewClusterRESTClientGetter([]byte{}, ""))
	lazyClient := &lazyClient{
		namespace: namespace,
		clientFn:  kubeClient.Factory.KubernetesClientSet,
	}

	cfg := action.Configuration{
		Releases:       storage.Init(driver.NewSecrets(newSecretClient(lazyClient))),
		KubeClient:     kubeClient,
		Capabilities:   chartutil.DefaultCapabilities,
		RegistryClient: registryClient,
		Log: func(s string, i ...interface{}) {
			klog.Infof(s, i)
		},
	}

	return &client{
		kubeClient: kubeClient,
		lazyClient: lazyClient,
		cfg:        &cfg,
	}, nil
}

func NewClientWithConf(kubeconfig []byte, namespace string) (HelmClient, error) {
	getter := NewClusterRESTClientGetter(kubeconfig, namespace)

	kubeClient := kube.New(getter)

	lazyClient := &lazyClient{
		namespace: namespace,
		clientFn:  kubeClient.Factory.KubernetesClientSet,
	}

	registryClient, err := registry.NewClient()
	if err != nil {
		return nil, err
	}

	cfg := action.Configuration{
		Releases:       storage.Init(driver.NewSecrets(newSecretClient(lazyClient))),
		KubeClient:     kubeClient,
		Capabilities:   chartutil.DefaultCapabilities,
		RegistryClient: registryClient,
		Log: func(s string, i ...interface{}) {
			klog.Infof(s, i)
		},
	}

	return &client{
		kubeClient: kubeClient,
		lazyClient: lazyClient,
		cfg:        &cfg,
	}, nil
}
