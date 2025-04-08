package notification

import (
	"context"
	"errors"
	"fmt"
	metaerrors "k8s.io/apimachinery/pkg/api/meta"
	"kubesphere.io/ks-upgrade/v4/api/core/v1alpha1"
	"sync"
	"time"

	notificationv2beta2 "github.com/kubesphere/notification-manager/apis/v2beta2"
	"github.com/tidwall/sjson"
	v12 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"
	"kubesphere.io/ks-upgrade/pkg/executor"
	"kubesphere.io/ks-upgrade/pkg/helm"
	"kubesphere.io/ks-upgrade/pkg/model"
	"kubesphere.io/ks-upgrade/pkg/model/core"
	"kubesphere.io/ks-upgrade/pkg/model/helper"
	"kubesphere.io/ks-upgrade/pkg/storage"
	"kubesphere.io/ks-upgrade/pkg/store"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func init() {
	runtime.Must(executor.Register(&factory{}))
}

const (
	jobName                      = "whizard-notification"
	jobDone                      = jobName + "-done"
	optionRerun                  = "rerun"
	NotificationConfigResource   = "notificationConfigResource"
	NotificationReceiverResource = "notificationReceiverResource"
	NotificationSilenceResource  = "notificationSilenceResource"
	NotificationRouterResource   = "notificationRouterResource"
	OldNamespace                 = "kubesphere-monitoring-federated"
	NewNamespcae                 = "kubesphere-monitoring-system"
	OldSecretResource            = "OldSecretResource"
	OldConfigmapResource         = "OldConfigmapResource"
	ConfigCrd                    = "configs.notification.kubesphere.io"
	NotificationManagerCrd       = "notificationmanagers.notification.kubesphere.io"
	ReceiverCrd                  = "receivers.notification.kubesphere.io"
	RouterCrd                    = "routers.notification.kubesphere.io"
	SilenceCrd                   = "silences.notification.kubesphere.io"
)

var _ executor.UpgradeJob = &upgradeJob{}
var _ executor.InjectClientV3 = &upgradeJob{}
var _ executor.InjectClientV4 = &upgradeJob{}
var _ executor.InjectResourceStore = &upgradeJob{}
var _ executor.InjectModelHelperFactory = &upgradeJob{}

var retryCount = 180

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
	helmClient    helm.HelmClient
	clientV3      runtimeclient.Client
	clientV4      runtimeclient.Client
	coreHelper    core.Helper
	resourceStore store.ResourceStore
	options       executor.DynamicOptions
	extensionRef  *model.ExtensionRef
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

func (i *upgradeJob) PreUpgrade(ctx context.Context) error {
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

	isHost, err := i.coreHelper.IsHostCluster(ctx)
	if err != nil {
		return err
	}

	// backup notification resources
	err = i.backupResource(ctx)
	if err != nil {
		return err
	}

	if isHost {

		// copy secret and configmap
		err = i.copySecretAndConfigmap(ctx)
		if err != nil {
			return err
		}

	}

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
		klog.Infof("failed to load data key: %s", jobDone)
		return err
	}
	if string(date) != "" {
		klog.Infof("job %s already done at %s", jobName, date)
		return nil
	}

	klog.Info("uninstall notification-manager")
	_, err = i.helmClient.GetRelease("kubesphere-monitoring-system", "notification-manager")
	if err == nil {
		// remove notification-manager helm charts
		_, err = i.helmClient.Uninstall("kubesphere-monitoring-system", "notification-manager")
		if err != nil {
			return err
		}
	}

	err = i.clientV3.Delete(ctx, &v12.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "notification-adapter",
			Namespace: "kubesphere-monitoring-system",
		},
	})
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	isHost, err := i.coreHelper.IsHostCluster(ctx)
	if err != nil {
		return err
	}
	if isHost {

		installPlan := v1alpha1.InstallPlan{}
		err = i.clientV4.Get(ctx, types.NamespacedName{
			Name: i.extensionRef.Name,
		}, &installPlan)
		// check if install plan already exists
		if k8serrors.IsNotFound(err) {
			// remove crd
			err = i.uninstallCrd(ctx)
			if err != nil {
				return err
			}
			i.waitForCrdsDeleted(ctx)
			klog.Info("notification crds deleted")
		}

		// create extension install plan
		klog.Infof("create install plan for extension %s", i.extensionRef.Name)
		err = i.createInstallPlan(ctx)
		if err != nil && !k8serrors.IsAlreadyExists(err) {
			return err
		}

		var notificationManagerDeployment v12.Deployment
		if i.options != nil {
			if v, ok := i.options["retryCount"]; ok {
				if times, ok := v.(int); !ok {
					klog.Errorf("failed to convert retryCount to int")
				} else {
					retryCount = times
				}
			}
		}
		for j := 0; j < retryCount; j++ {
			err = i.clientV3.Get(ctx, types.NamespacedName{
				Namespace: "kubesphere-monitoring-system",
				Name:      "notification-manager-operator",
			}, &notificationManagerDeployment)
			if err != nil && !k8serrors.IsNotFound(err) {
				return err
			}
			if k8serrors.IsNotFound(err) {
				klog.Infof("waiting for notification-manager to be installed")
				time.Sleep(10 * time.Second)
				continue
			}
			if notificationManagerDeployment.Status.ReadyReplicas == *notificationManagerDeployment.Spec.Replicas {
				time.Sleep(10 * time.Second)
				klog.Infof("notification-manager is installed")
				break
			} else {
				klog.Infof("waiting for notification-manager to be installed")
				time.Sleep(10 * time.Second)
			}
		}

		// create old resource
		err = i.createOldResource(ctx)
		if err != nil {
			return err
		}
	}

	// save job done time
	date = []byte(time.Now().UTC().String())
	klog.Infof("save data key: %s value: %s", jobDone, date)
	err = i.resourceStore.SaveRaw(jobDone, date)
	return err
}

func (i *upgradeJob) backupResource(ctx context.Context) error {

	klog.Info("Backup notification resources...")
	err := i.clientV3.Get(ctx, types.NamespacedName{Name: NotificationManagerCrd}, &v1.CustomResourceDefinition{}, &runtimeclient.GetOptions{})
	if err == nil {
		// backup notificationManagerList
		notificationManagerList := notificationv2beta2.NotificationManagerList{}
		err = i.clientV3.List(ctx, &notificationManagerList)
		if err != nil && !k8serrors.IsNotFound(err) && !metaerrors.IsNoMatchError(err) {
			klog.Errorf("failed to list notificationManager, err: %v", err)
			return err
		}
		if len(notificationManagerList.Items) > 0 {
			err = i.resourceStore.Save(NotificationConfigResource, &notificationManagerList)
			if err != nil {
				return err
			}
		}

	}

	err = i.clientV3.Get(ctx, types.NamespacedName{Name: ConfigCrd}, &v1.CustomResourceDefinition{}, &runtimeclient.GetOptions{})
	if err == nil {
		// backup config
		configList := notificationv2beta2.ConfigList{}
		err = i.clientV3.List(ctx, &configList)
		if err != nil && !k8serrors.IsNotFound(err) && !metaerrors.IsNoMatchError(err) {
			klog.Errorf("failed to list config, err: %v", err)
			return err
		}
		if len(configList.Items) > 0 {
			err = i.resourceStore.Save(NotificationConfigResource, &configList)
			if err != nil {
				return err
			}
		}

	}

	err = i.clientV3.Get(ctx, types.NamespacedName{Name: ReceiverCrd}, &v1.CustomResourceDefinition{}, &runtimeclient.GetOptions{})
	if err == nil {
		// backup receiver
		receiverList := notificationv2beta2.ReceiverList{}
		err = i.clientV3.List(ctx, &receiverList)
		if err != nil && !k8serrors.IsNotFound(err) && !metaerrors.IsNoMatchError(err) {
			klog.Errorf("failed to list receiver, err: %v", err)
			return err
		}
		if len(receiverList.Items) > 0 {
			err = i.resourceStore.Save(NotificationReceiverResource, &receiverList)
			if err != nil {
				return err
			}
		}
	}

	err = i.clientV3.Get(ctx, types.NamespacedName{Name: RouterCrd}, &v1.CustomResourceDefinition{}, &runtimeclient.GetOptions{})
	if err == nil {
		// backup router
		routerList := notificationv2beta2.RouterList{}
		err = i.clientV3.List(ctx, &routerList)
		if err != nil && !k8serrors.IsNotFound(err) && !metaerrors.IsNoMatchError(err) {
			klog.Errorf("failed to list router, err: %v", err)
			return err
		}
		if len(routerList.Items) > 0 {
			err = i.resourceStore.Save(NotificationRouterResource, &routerList)
			if err != nil {
				return err
			}
		}
	}

	err = i.clientV3.Get(ctx, types.NamespacedName{Name: SilenceCrd}, &v1.CustomResourceDefinition{}, &runtimeclient.GetOptions{})
	if err == nil {
		// backup silence
		silenceList := notificationv2beta2.SilenceList{}
		err = i.clientV3.List(ctx, &silenceList)
		if err != nil && !k8serrors.IsNotFound(err) && !metaerrors.IsNoMatchError(err) {
			klog.Errorf("failed to list silence, err: %v", err)
			return err
		}
		if len(silenceList.Items) > 0 {
			err = i.resourceStore.Save(NotificationSilenceResource, &silenceList)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (i *upgradeJob) waitForCrdsDeleted(ctx context.Context) {
	waitGroup := sync.WaitGroup{}
	crdNames := []string{ConfigCrd, ReceiverCrd, RouterCrd, SilenceCrd, NotificationManagerCrd}

	for _, crdName := range crdNames {
		waitGroup.Add(1)
		go func(name string) {
			defer waitGroup.Done()

			for {
				err := i.clientV3.Get(ctx, types.NamespacedName{Name: name}, &v1.CustomResourceDefinition{})
				if k8serrors.IsNotFound(err) {
					return // CRD deleted, exit the goroutine
				}

				klog.Infof("waiting for CRD %s to be deleted", name)
				time.Sleep(5 * time.Second)
			}
		}(crdName)
	}

	waitGroup.Wait()
}

func (i *upgradeJob) uninstallCrd(ctx context.Context) error {
	// remove crd resources
	configCrd := v1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "configs.notification.kubesphere.io",
		},
	}
	err := i.clientV3.Delete(ctx, &configCrd)
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	notificationManagerCrd := v1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "notificationmanagers.notification.kubesphere.io",
		},
	}
	err = i.clientV3.Delete(ctx, &notificationManagerCrd)
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	receiverCrd := v1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "receivers.notification.kubesphere.io",
		},
	}
	err = i.clientV3.Delete(ctx, &receiverCrd)
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	routerCrd := v1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "routers.notification.kubesphere.io",
		},
	}
	err = i.clientV3.Delete(ctx, &routerCrd)
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	silenceCrd := v1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "silences.notification.kubesphere.io",
		},
	}
	err = i.clientV3.Delete(ctx, &silenceCrd)
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	err = i.clientV3.Delete(ctx, &v1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "federatednotificationsilences.types.kubefed.io",
		},
	})
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	err = i.clientV3.Delete(ctx, &v1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "federatednotificationrouters.types.kubefed.io",
		},
	})
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	err = i.clientV3.Delete(ctx, &v1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "federatednotificationconfigs.types.kubefed.io",
		},
	})
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	err = i.clientV3.Delete(ctx, &v1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "federatednotificationreceivers.types.kubefed.io",
		},
	})
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	err = i.clientV3.Delete(ctx, &v1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "federatednotificationmanagers.types.kubefed.io",
		},
	})
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	return nil
}

func (i *upgradeJob) createOldResource(ctx context.Context) error {

	klog.Info("Creating backup data for config, receiver, router, silence")
	configList := &notificationv2beta2.ConfigList{}
	err := i.resourceStore.Load(NotificationConfigResource, configList)
	if err != nil && !errors.Is(err, storage.BackupKeyNotFound) {
		return err
	}
	for _, item := range configList.Items {
		klog.Infof("create config %s", item.Name)
		item.SetResourceVersion("")
		err = i.clientV3.Create(ctx, &item)
		if err != nil && !k8serrors.IsAlreadyExists(err) {
			return err
		}
	}

	routerList := &notificationv2beta2.RouterList{}
	err = i.resourceStore.Load(NotificationRouterResource, routerList)
	if err != nil && !errors.Is(err, storage.BackupKeyNotFound) {
		return err
	}
	for _, item := range routerList.Items {
		klog.Infof("create router %s", item.Name)
		item.SetResourceVersion("")
		err = i.clientV3.Create(ctx, &item)
		if err != nil && !k8serrors.IsAlreadyExists(err) {
			return err
		}
	}

	silenceList := &notificationv2beta2.SilenceList{}
	err = i.resourceStore.Load(NotificationSilenceResource, silenceList)
	if err != nil && !errors.Is(err, storage.BackupKeyNotFound) {
		return err
	}
	for _, item := range silenceList.Items {
		klog.Infof("create silence %s", item.Name)
		item.SetResourceVersion("")
		err = i.clientV3.Create(ctx, &item)
		if err != nil && !k8serrors.IsAlreadyExists(err) {
			return err
		}
	}

	receiverList := &notificationv2beta2.ReceiverList{}
	err = i.resourceStore.Load(NotificationReceiverResource, receiverList)
	if err != nil && !errors.Is(err, storage.BackupKeyNotFound) {
		return err
	}
	for _, item := range receiverList.Items {
		klog.Infof("create receiver %s", item.Name)
		item.SetResourceVersion("")
		err = i.clientV3.Create(ctx, &item)
		if err != nil && !k8serrors.IsAlreadyExists(err) {
			return err
		}
	}

	return nil
}

func (i *upgradeJob) copySecretAndConfigmap(ctx context.Context) error {
	secretList := &corev1.SecretList{}
	err := i.clientV3.List(ctx, secretList, &runtimeclient.ListOptions{
		Namespace: OldNamespace,
	})
	if err != nil {
		return err
	}

	for _, item := range secretList.Items {
		item.SetResourceVersion("")
		item.SetNamespace(NewNamespcae)
		err = i.clientV3.Create(ctx, &item)
		if err != nil && !k8serrors.IsAlreadyExists(err) {
			return err
		}
	}
	configmapList := &corev1.ConfigMapList{}
	err = i.clientV3.List(ctx, configmapList, &runtimeclient.ListOptions{
		Namespace: OldNamespace,
	})
	if err != nil {
		return err
	}

	for _, item := range configmapList.Items {
		item.SetResourceVersion("")
		item.SetNamespace(NewNamespcae)
		err = i.clientV3.Create(ctx, &item)
		if err != nil && !k8serrors.IsAlreadyExists(err) {
			return err
		}
	}

	var adapter v12.Deployment
	err = i.clientV3.Get(ctx, types.NamespacedName{
		Name:      "notification-adapter",
		Namespace: "kubesphere-monitoring-system",
	}, &adapter, &runtimeclient.GetOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}
	err = i.resourceStore.Save("notification-adapter", &adapter)
	if err != nil {
		return err
	}

	return nil
}

func (i *upgradeJob) createInstallPlan(ctx context.Context) error {

	cc, err := i.coreHelper.GetClusterConfiguration(ctx)
	if err != nil {
		return err
	}
	if i.extensionRef != nil {
		if i.extensionRef.ClusterScheduling == nil {
			notificationHistoryEnabled, _, err := unstructured.NestedBool(cc, "spec", "notification", "history", "enabled")
			if err != nil {
				return err
			}
			if !notificationHistoryEnabled && i.extensionRef.Config == "" {
				m := make(map[string]interface{})
				m["notification-history"] = map[string]interface{}{
					"enabled": false,
				}
				marshal, err := yaml.Marshal(m)
				if err != nil {
					return err
				}
				i.extensionRef.Config = string(marshal)
			}
		}
	}
	err = i.coreHelper.CreateInstallPlanFromExtensionRef(ctx, i.extensionRef)
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		return err
	}

	return nil

}

type pathValue struct {
	path  string
	value interface{}
}

func setYamlValues(yamlString string, pathValues []pathValue) (string, error) {
	jsonbytes, err := yaml.YAMLToJSON([]byte(yamlString))
	if err != nil {
		return yamlString, err
	}

	for _, v := range pathValues {
		jsonbytes, err = sjson.SetBytes(jsonbytes, v.path, v.value)
		if err != nil {
			return yamlString, err
		}
	}

	yamlbytes, err := yaml.JSONToYAML(jsonbytes)
	if err != nil {
		return yamlString, err
	}

	return string(yamlbytes), nil
}
