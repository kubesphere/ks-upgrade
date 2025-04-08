package helm

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"

	"kubesphere.io/ks-upgrade/pkg/executor"
	"kubesphere.io/ks-upgrade/pkg/helm"
	"kubesphere.io/ks-upgrade/pkg/model"
)

func init() {
	runtime.Must(executor.Register(&factory{}))
}

const jobName = "sample"

var _ executor.UpgradeJob = &upgradeJob{}
var _ executor.InjectHelmClient = &upgradeJob{}

type factory struct {
}

func (f *factory) Name() string {
	return jobName
}

func (f *factory) Create(_ executor.DynamicOptions, _ *model.ExtensionRef) (executor.UpgradeJob, error) {
	return &upgradeJob{}, nil
}

type upgradeJob struct {
	helmClient helm.HelmClient
}

func (i *upgradeJob) InjectHelmClient(client helm.HelmClient) {
	i.helmClient = client
}

func (i *upgradeJob) PreUpgrade(ctx context.Context) error {
	releases, err := i.helmClient.ListReleases(metav1.NamespaceAll)
	if err != nil {
		return err
	}
	klog.Info(releases)
	return nil
}

func (i *upgradeJob) PostUpgrade(ctx context.Context) error {
	//release, err := i.helmClient.Uninstall(metav1.NamespaceDefault, "smaple")
	//klog.Info(release)
	return nil
}
