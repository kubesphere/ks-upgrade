package model

import (
	"errors"
	"k8s.io/klog/v2"
	restconfig "sigs.k8s.io/controller-runtime/pkg/client/config"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"kubesphere.io/ks-upgrade/pkg/storage"
	"kubesphere.io/ks-upgrade/pkg/store"
)

var (
	kubeConfigBackupKey = "factory.kubeconfig"
)

type ConfigFactory interface {
	GetRestConfig() (*rest.Config, error)
}

var _ ConfigFactory = &configFactory{}

type configFactory struct {
	resourceStore store.ResourceStore
}

func NewConfigFactory(resourceStore store.ResourceStore) ConfigFactory {
	return &configFactory{resourceStore: resourceStore}
}

func (c *configFactory) GetRestConfig() (*rest.Config, error) {
	var configBytes []byte
	var clientConfig *clientcmdapi.Config
	var restConfig *rest.Config
	var err error
	configBytes, err = c.resourceStore.LoadRaw(kubeConfigBackupKey)
	if err != nil {
		if !errors.Is(err, storage.BackupKeyNotFound) {
			return nil, err
		}
		klog.Warningf("[Factory] Failed to load kubeconfig cache because of cache %s not found", kubeConfigBackupKey)
		restConfig, err = restconfig.GetConfig()
		if err != nil {
			return nil, err
		}
		if err = rest.LoadTLSFiles(restConfig); err != nil {
			return nil, err
		}
		clientConfig = CreateKubeConfigFileForRestConfig(restConfig)

		configBytes, err = clientcmd.Write(*clientConfig)
		if err != nil {
			return nil, err
		}
		if err = c.resourceStore.SaveRaw(kubeConfigBackupKey, configBytes); err != nil {
			return nil, err
		}
	} else {
		restConfig, err = clientcmd.RESTConfigFromKubeConfig(configBytes)
		if err != nil {
			klog.Warningf("[Factory] Failed to get configuration file from cache %s", err)
			restConfig, err = restconfig.GetConfig()
			if err != nil {
				klog.Warningf("[Factory] Dailed to load default restconfig %s", err)
				return nil, err
			}
			return restConfig, nil
		}
	}
	return restConfig, nil
}

func CreateKubeConfigFileForRestConfig(restConfig *rest.Config) *clientcmdapi.Config {
	clusters := make(map[string]*clientcmdapi.Cluster)
	clusters["kubernetes"] = &clientcmdapi.Cluster{
		Server:                   restConfig.Host,
		CertificateAuthorityData: restConfig.CAData,
	}
	contexts := make(map[string]*clientcmdapi.Context)
	contexts["kubernetes-admin@kubernetes"] = &clientcmdapi.Context{
		Cluster:  "kubernetes",
		AuthInfo: "kubernetes-admin",
	}
	authinfos := make(map[string]*clientcmdapi.AuthInfo)
	authinfos["kubernetes-admin"] = &clientcmdapi.AuthInfo{
		ClientCertificateData: restConfig.CertData,
		ClientKeyData:         restConfig.KeyData,
		Token:                 restConfig.BearerToken,
	}
	return &clientcmdapi.Config{
		Kind:           "Config",
		APIVersion:     "v1",
		Clusters:       clusters,
		Contexts:       contexts,
		CurrentContext: "kubernetes-admin@kubernetes",
		AuthInfos:      authinfos,
	}
}
