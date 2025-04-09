package network

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	webhookv1 "k8s.io/api/admissionregistration/v1"
	netv1 "k8s.io/api/networking/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metaerrors "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"kubesphere.io/ks-upgrade/pkg/executor"
	"kubesphere.io/ks-upgrade/pkg/model"
	"kubesphere.io/ks-upgrade/pkg/model/core"
	"kubesphere.io/ks-upgrade/pkg/model/helper"
	"kubesphere.io/ks-upgrade/pkg/storage"
	"kubesphere.io/ks-upgrade/pkg/store"
	calicov3 "kubesphere.io/ks-upgrade/v3/api/network/calicov3"
	networkv1alpha1 "kubesphere.io/ks-upgrade/v3/api/network/v1alpha1"
	tenantv1alpha2 "kubesphere.io/ks-upgrade/v3/api/tenant/v1alpha2"
	v4clusterv1alpha1 "kubesphere.io/ks-upgrade/v4/api/cluster/v1alpha1"
	v4corev1alpha1 "kubesphere.io/ks-upgrade/v4/api/core/v1alpha1"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

func init() {
	utilruntime.Must(executor.Register(&factory{}))
}

const (
	jobName     = "network"
	jobDone     = jobName + "-done"
	optionRerun = "rerun"
	webhookName = "network.kubesphere.io"

	egress       = "egress"
	ingress      = "ingress"
	inside       = "inside"
	outside      = "outside"
	trafficLabel = "kubesphere.io/policy-traffic"
	typeLabel    = "kubesphere.io/policy-type"

	apiVersion    = "network.kubesphere.io/v1alpha1"
	ippoolCRD     = "ippools.network.kubesphere.io"
	ipamblockCRD  = "ipamblocks.network.kubesphere.io"
	ipamhandleCRD = "ipamhandles.network.kubesphere.io"
)

var _ executor.UpgradeJob = &upgradeJob{}
var _ executor.InjectClientV3 = &upgradeJob{}
var _ executor.InjectClientV4 = &upgradeJob{}
var _ executor.InjectResourceStore = &upgradeJob{}
var _ executor.InjectModelHelperFactory = &upgradeJob{}

type factory struct{}
type typeName struct {
	typeKey types.NamespacedName
	Name    string
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
	// backup ippool
	if err := i.backupIPPoolResource(ctx); err != nil {
		return err
	}

	// backup namespace networkpolicy
	return i.backupNetworkPolicyResource(ctx)
}

func (i *upgradeJob) backupIPPoolResource(ctx context.Context) error {
	// get webhook
	getResources := map[typeName]runtimeclient.Object{
		{
			typeKey: types.NamespacedName{Name: webhookName},
			Name:    "validatingwebhookconfiguration.admissionregistration.k8s.io.v1",
		}: &webhookv1.ValidatingWebhookConfiguration{},
		{
			typeKey: types.NamespacedName{Name: ippoolCRD},
			Name:    fmt.Sprintf("%s-crd.apiextensions.k8s.io.v1", ippoolCRD),
		}: &apiextensionsv1.CustomResourceDefinition{},
		{
			typeKey: types.NamespacedName{Name: ipamblockCRD},
			Name:    fmt.Sprintf("%s-crd.apiextensions.k8s.io.v1", ipamblockCRD),
		}: &apiextensionsv1.CustomResourceDefinition{},
		{
			typeKey: types.NamespacedName{Name: ipamhandleCRD},
			Name:    fmt.Sprintf("%s-crd.apiextensions.k8s.io.v1", ipamhandleCRD),
		}: &apiextensionsv1.CustomResourceDefinition{},
	}

	if err := i.getResourceToStore(ctx, getResources); err != nil {
		return err
	}

	// list ippool resource
	resources := map[string]runtimeclient.ObjectList{
		"ippools.network.kubesphere.io.v1alpha1": &networkv1alpha1.IPPoolList{},
		"ippools.crd.projectcalico.org.v1":       &calicov3.IPPoolList{},
	}

	return i.listResourceToStore(ctx, resources)
}

func (i *upgradeJob) backupNetworkPolicyResource(ctx context.Context) error {
	resources := map[string]runtimeclient.ObjectList{
		"networkpolicies.networking.k8s.io.v1":                    &netv1.NetworkPolicyList{},
		"workspacetemplates.tenant.kubesphere.io.v1alpha2":        &tenantv1alpha2.WorkspaceTemplateList{},
		"namespacenetworkpolicies.network.kubesphere.io.v1alpha1": &networkv1alpha1.NamespaceNetworkPolicyList{},
	}

	return i.listResourceToStore(ctx, resources)
}

func (i *upgradeJob) getResourceToStore(ctx context.Context, objs map[typeName]runtimeclient.Object) error {
	for objKey, objValue := range objs {
		fileName := fmt.Sprintf("%s-%s", jobName, objKey.Name)
		err := i.resourceStore.Load(fileName, objValue)
		if err != nil && errors.Is(err, storage.BackupKeyNotFound) {
			if err := i.clientV3.Get(ctx, objKey.typeKey, objValue, &runtimeclient.GetOptions{}); err != nil {
				if k8serrors.IsNotFound(err) || metaerrors.IsNoMatchError(err) {
					klog.Warning(err.Error())
					continue
				} else {
					return err
				}
			}

			if err := i.resourceStore.Save(fileName, objValue); err != nil {
				return err
			}
		}
	}

	return nil
}

func (i *upgradeJob) listResourceToStore(ctx context.Context, objs map[string]runtimeclient.ObjectList) error {
	for objKey, objValue := range objs {
		fileName := fmt.Sprintf("%s-%s", jobName, objKey)
		err := i.resourceStore.Load(fileName, objValue)
		if err != nil && errors.Is(err, storage.BackupKeyNotFound) {
			if err := i.clientV3.List(ctx, objValue, &runtimeclient.ListOptions{}); err != nil {
				if k8serrors.IsNotFound(err) || metaerrors.IsNoMatchError(err) {
					klog.Warning(err.Error())
					continue
				} else {
					return err
				}
			}

			if err := i.resourceStore.Save(fileName, objValue); err != nil {
				return err
			}
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
		return err
	}
	if string(date) != "" {
		klog.Infof("job %s already done at %s", jobName, date)
		return nil
	}

	if err := i.postUpgrade(ctx); err != nil {
		return err
	}

	// save job done time
	date = []byte(time.Now().UTC().String())
	klog.Infof("save data key: %s value: %s", jobDone, date)
	return i.resourceStore.SaveRaw(jobDone, date)
}

func (i *upgradeJob) postUpgrade(ctx context.Context) error {
	// check if network extension enabled
	enable, err := i.isNetworkEnable(ctx)
	if err != nil {
		return err
	}
	if enable {
		if err := i.upgradeNetworkData(ctx); err != nil {
			return err
		}
	}

	// host cluster create InstallPlan
	isHost, err := i.coreHelper.IsHostCluster(ctx)
	if err != nil {
		klog.Errorf("[network] check host cluster err %s", err)
		return err
	}
	if !isHost {
		klog.Infof("[network] upgrade running on member cluster, skip creating InstallPlan")
		return nil
	}

	klog.Infof("[network] host cluster, begin to generateClusterScheduling clusterScheduling")
	if i.extensionRef.ClusterScheduling == nil {
		i.extensionRef.ClusterScheduling = &v4corev1alpha1.ClusterScheduling{}
	}
	if i.extensionRef.ClusterScheduling.Placement == nil {
		i.extensionRef.ClusterScheduling.Placement = &v4corev1alpha1.Placement{}
	}

	if err := i.generateConfigForClusters(ctx); err != nil {
		return err
	}

	klog.Infof("create install plan for extension %s", i.extensionRef.Name)
	err = i.coreHelper.CreateInstallPlanFromExtensionRef(ctx, i.extensionRef)
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		return err
	}

	return nil
}

// generateConfigForClusters generates cluster config for network extension
func (i *upgradeJob) generateConfigForClusters(ctx context.Context) error {
	var clusterScheduling []string
	clusterList, err := i.getClusterList(ctx)
	if err != nil {
		klog.Errorf("[network] error list cluster %s", err)
		return err
	}
	networkPolicyEnabled, ippoolEnabled := false, false
	for _, cluster := range clusterList.Items {
		cc, err := getClusterConfiguration(ctx, cluster)
		if err != nil {
			klog.Errorf("[network] get cluster %s cc config err %s", cluster.Name, err)
			return err
		}

		npEabled, poolEnabled, err := i.isEnable(cc)
		if err != nil {
			return err
		}
		if !networkPolicyEnabled && npEabled {
			networkPolicyEnabled = true
		}

		if !ippoolEnabled && poolEnabled {
			ippoolEnabled = true
		}

		klog.Infof("[network] cluster %s network: [ippool: %v][networkpolicy: %v]", cluster.Name, ippoolEnabled, networkPolicyEnabled)
		if npEabled || poolEnabled {
			klog.Infof("[network] cluster: %s network enabled, add to clusterScheduling", cluster.Name)
			clusterScheduling = append(clusterScheduling, cluster.Name)
		}
	}

	if err := i.setExtensionConfig(networkPolicyEnabled, ippoolEnabled); err != nil {
		return err
	}

	if len(i.extensionRef.ClusterScheduling.Placement.Clusters) == 0 && i.extensionRef.ClusterScheduling.Placement.ClusterSelector == nil {
		klog.Infof("compute clusters placement since it is not specified")
		i.extensionRef.ClusterScheduling = &v4corev1alpha1.ClusterScheduling{
			Placement: &v4corev1alpha1.Placement{Clusters: clusterScheduling},
		}
	}

	klog.Infof("[network] %d clusters network enabled, continue post upgrade", len(i.extensionRef.ClusterScheduling.Placement.Clusters))
	return nil
}

func getClusterConfiguration(ctx context.Context, cluster v4clusterv1alpha1.Cluster) (map[string]interface{}, error) {
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(cluster.Spec.Connection.KubeConfig)
	if err != nil {
		return nil, err
	}
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %s", err)
	}
	unstructed, err := dynamicClient.Resource(core.ClusterConfigurationGVR).Namespace(core.KubeSphereNamespace).Get(ctx, core.KubeSphereInstallerName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return unstructed.Object, nil
}

func (i *upgradeJob) getClusterList(ctx context.Context) (v4clusterv1alpha1.ClusterList, error) {
	var clusterList v4clusterv1alpha1.ClusterList
	for {
		err := i.coreHelper.ListCluster(ctx, &clusterList)
		if err != nil {
			klog.Errorf("[network] list cluster err %s", err)
			return v4clusterv1alpha1.ClusterList{}, err
		}
		if len(clusterList.Items) == 0 {
			klog.Infof("[network] no cluster found, retry in 5 seconds")
			time.Sleep(5 * time.Second)
			continue
		}
		klog.Infof("[network] find %d cluster", len(clusterList.Items))
		return clusterList, nil
	}
}

func (i *upgradeJob) setExtensionConfig(networkPolicyEnabled, ippoolEnabled bool) error {
	globalConfig := map[string]interface{}{
		"global": map[string]interface{}{
			"ippool": map[string]interface{}{
				"enable":  ippoolEnabled,
				"type":    "calico",
				"webhook": true,
			},
			"networkpolicy": map[string]interface{}{
				"enabled": networkPolicyEnabled,
			},
		},
	}

	valuesBytes, err := yaml.Marshal(globalConfig)
	if err != nil {
		return err
	}
	i.extensionRef.Config = string(valuesBytes)

	return nil
}

func (i *upgradeJob) isEnable(cc map[string]interface{}) (bool, bool, error) {
	networkPolicyEnabled, _, err := unstructured.NestedBool(cc, "spec", "network", "networkpolicy", "enabled")
	if err != nil {
		return false, false, err
	}

	ippoolEnabled := false
	ippoolType, _, err := unstructured.NestedString(cc, "spec", "network", "ippool", "type")
	if err != nil {
		return false, false, err
	}
	if ippoolType == "calico" {
		ippoolEnabled = true
	}

	return networkPolicyEnabled, ippoolEnabled, nil
}

func (i *upgradeJob) isNetworkEnable(ctx context.Context) (bool, error) {
	cc, err := i.coreHelper.GetClusterConfiguration(ctx)
	if err != nil {
		klog.Errorf("get cluster configuration error. err:%s", err.Error())
		return false, err
	}

	networkPolicyEnabled, ippoolEnabled, err := i.isEnable(cc)
	if err != nil {
		return false, err
	}

	return networkPolicyEnabled || ippoolEnabled, nil
}

func (i *upgradeJob) upgradeNetworkData(ctx context.Context) error {
	if err := i.upgradeIPPoolData(ctx); err != nil {
		return err
	}

	return i.upgradeNetworkPolicyData(ctx)
}

func (i *upgradeJob) upgradeIPPoolData(ctx context.Context) error {
	// update calico ippool
	calicoIPPool := make(map[string]*calicov3.IPPool)
	cippools := &calicov3.IPPoolList{}
	if err := i.clientV3.List(ctx, cippools, &runtimeclient.ListOptions{}); err != nil && !metaerrors.IsNoMatchError(err) {
		return err
	}

	for _, ippool := range cippools.Items {
		if ippool.OwnerReferences != nil {
			for i, v := range ippool.OwnerReferences {
				if v.APIVersion == apiVersion {
					if i == 0 {
						ippool.OwnerReferences = ippool.OwnerReferences[1:]
					} else if i == len(ippool.OwnerReferences)-1 {
						ippool.OwnerReferences = ippool.OwnerReferences[:len(ippool.OwnerReferences)-1]
					} else {
						ippool.OwnerReferences = append(ippool.OwnerReferences[:i], ippool.OwnerReferences[i+1:]...)
					}

					break
				}
			}

			if len(ippool.OwnerReferences) == 0 {
				ippool.OwnerReferences = nil
			}
		}

		calicoIPPool[ippool.Name] = ippool.DeepCopy()
	}

	// clean ippool webhook
	webhook := &webhookv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: webhookName,
		},
	}
	klog.Infof("delete ValidatingWebhookConfiguration %s", webhook.Name)
	err := i.clientV4.Delete(ctx, webhook)
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	// clean ippool finalizers
	kippools := &networkv1alpha1.IPPoolList{}
	if err := i.clientV3.List(ctx, kippools, &runtimeclient.ListOptions{}); err != nil && !metaerrors.IsNoMatchError(err) {
		return err
	}

	for _, ippool := range kippools.Items {
		// merge annotation
		cpool, exist := calicoIPPool[ippool.Name]
		if exist && ippool.Annotations != nil {
			if cpool.Annotations == nil {
				cpool.Annotations = make(map[string]string)
			}

			for k, v := range ippool.Annotations {
				cpool.Annotations[k] = v
			}
		}

		clone := ippool.DeepCopy()
		if clone.Finalizers != nil {
			clone.Finalizers = nil

			klog.Infof("update ippool %s", clone.Name)
			if err := i.clientV3.Update(ctx, clone, &runtimeclient.UpdateOptions{}); err != nil {
				return err
			}
		}
	}

	// update calico ippool
	for _, ippool := range calicoIPPool {
		klog.Infof("update calico ippool %s", ippool.Name)
		if err := i.clientV3.Update(ctx, ippool, &runtimeclient.UpdateOptions{}); err != nil {
			return err
		}
	}

	// clean ippool crds
	wantDeleteCRDs := []string{ippoolCRD, ipamblockCRD, ipamhandleCRD}
	for _, name := range wantDeleteCRDs {
		crd := &apiextensionsv1.CustomResourceDefinition{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		}
		klog.Infof("delete CustomResourceDefinition %s", crd.Name)
		err := i.clientV4.Delete(ctx, crd)
		if err != nil && !k8serrors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

func (i *upgradeJob) upgradeNetworkPolicyData(ctx context.Context) error {
	// update namespacenetworkpolicy
	nsnps := &networkv1alpha1.NamespaceNetworkPolicyList{}
	if err := i.clientV3.List(ctx, nsnps, &runtimeclient.ListOptions{}); err != nil && !metaerrors.IsNoMatchError(err) {
		return err
	}

	for _, nsnp := range nsnps.Items {
		clone := nsnp.DeepCopy()
		if clone.Labels == nil {
			clone.Labels = make(map[string]string)
		}

		policyType := ""
		policytraffic := ""
		if len(clone.Spec.Egress) > 0 {
			policyType = egress
			if len(clone.Spec.Egress[0].To) > 0 {
				if clone.Spec.Egress[0].To[0].IPBlock != nil {
					policytraffic = outside
				}
				if clone.Spec.Egress[0].To[0].NamespaceSelector != nil || clone.Spec.Egress[0].To[0].ServiceSelector != nil {
					policytraffic = inside
				}
			}
		}

		if len(clone.Spec.Ingress) > 0 {
			policyType = ingress
			if len(clone.Spec.Ingress[0].From) > 0 {
				if clone.Spec.Ingress[0].From[0].IPBlock != nil {
					policytraffic = outside
				}
				if clone.Spec.Ingress[0].From[0].NamespaceSelector != nil || clone.Spec.Ingress[0].From[0].ServiceSelector != nil {
					policytraffic = inside
				}
			}
		}

		clone.Labels[typeLabel] = policyType
		clone.Labels[trafficLabel] = policytraffic
		if !reflect.DeepEqual(clone.Labels, nsnp.Labels) {
			klog.Infof("update NamespaceNetworkPolicy %s", clone.Name)
			if err := i.clientV3.Update(ctx, clone, &runtimeclient.UpdateOptions{}); err != nil {
				return err
			}
		}
	}

	return nil
}
