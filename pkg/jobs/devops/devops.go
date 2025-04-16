package devops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/imdario/mergo"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"golang.org/x/exp/slices"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	v3apicluster "kubesphere.io/ks-upgrade/v3/api/cluster/v1alpha1"
	devopsv1alpha3 "kubesphere.io/ks-upgrade/v3/api/devops/v1alpha3"
	v4apicore "kubesphere.io/ks-upgrade/v4/api/core/v1alpha1"
	v4corev1alpha1 "kubesphere.io/ks-upgrade/v4/api/core/v1alpha1"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

func init() {
	runtime.Must(executor.Register(&factory{}))
}

const (
	JobName    = "devops"
	JobDoneKey = "upgrade-devops-done"

	SysNs    = "kubesphere-devops-system"
	WorkerNs = "kubesphere-devops-worker"
	ArgoNs   = "argocd"

	AgentCM            = "ks-devops-agent"
	CascCM             = "jenkins-casc-config"
	ConfigMapBackupFmt = "%s-backup"

	ApiServerWorkload  = "devops-apiserver"
	ControllerWorkload = "devops-controller"
	JenkinsWorkload    = "devops-jenkins"

	ExtApiserverRepKey      = "extension.apiserver.replicas"
	ExtApiserverResourceKey = "extension.apiserver.resources"

	ManagedKey   = "devops.kubesphere.io/managed"
	ManagedTrue  = "true"
	WorkspaceKey = "kubesphere.io/workspace"
)

var EnabledPaths = []string{"spec", "devops", "enabled"}

var _ executor.UpgradeJob = &upgradeJob{}
var _ executor.InjectClientV3 = &upgradeJob{}
var _ executor.InjectClientV4 = &upgradeJob{}
var _ executor.InjectHelmClient = &upgradeJob{}
var _ executor.InjectResourceStore = &upgradeJob{}

type factory struct{}

func (f *factory) Name() string {
	return JobName
}

func (f *factory) Create(_ executor.DynamicOptions, extensionRef *model.ExtensionRef) (executor.UpgradeJob, error) {
	job := &upgradeJob{
		extensionRef:  extensionRef,
		isHostCluster: false,
	}
	if job.extensionRef.Name == "" {
		job.extensionRef.Name = "devops"
	}
	if job.extensionRef.ClusterScheduling == nil {
		job.extensionRef.ClusterScheduling = new(v4apicore.ClusterScheduling)
	}
	if job.extensionRef.ClusterScheduling.Overrides == nil {
		job.extensionRef.ClusterScheduling.Overrides = map[string]string{}
	}
	if job.extensionRef.ClusterScheduling.Placement == nil {
		job.extensionRef.ClusterScheduling.Placement = &v4apicore.Placement{}
	}
	return job, nil
}

type upgradeJob struct {
	clientV3      runtimeclient.Client
	clientV4      runtimeclient.Client
	helmClient    helm.HelmClient
	resourceStore store.ResourceStore
	coreHelper    core.Helper
	extensionRef  *model.ExtensionRef

	isHostCluster bool

	// config and overrides from the cluster 3.x for PostUpgrade
	// enabledClusters the clusters 3.x that enabled DevOps
	inClusterConfig    *Config
	inClusterOverrides map[string]*Override
	enabledClusters    []string
}

func (j *upgradeJob) InjectClientV3(client runtimeclient.Client) {
	j.clientV3 = client
}

func (j *upgradeJob) InjectClientV4(client runtimeclient.Client) {
	j.clientV4 = client
}

func (j *upgradeJob) InjectModelHelperFactory(factory helper.ModelHelperFactory) {
	j.coreHelper = factory.CoreHelper()
}

func (j *upgradeJob) InjectResourceStore(store store.ResourceStore) {
	j.resourceStore = store
}

func (j *upgradeJob) InjectHelmClient(client helm.HelmClient) {
	j.helmClient = client
}

func (j *upgradeJob) PreUpgrade(ctx context.Context) (err error) {
	klog.Info("DevOps PreUpgrade..")
	// check: if upgrade devops job is done, ignore
	var done bool
	if done, err = j.jobDone(); done || err != nil {
		return
	}

	// 1. check if current cluster enabled devops
	var enabled bool
	if enabled, err = j.coreHelper.PluginEnabled(ctx, nil, EnabledPaths...); err != nil {
		klog.Errorf("check DevOps enabled error: %+v", err)
		return
	}
	if !enabled {
		klog.Warning("the DevOps is not enabled in current cluster, ignore pre-upgrade")
		return
	}

	// 2. backup devops resources for all clusters on host cluster
	if j.isHostCluster, err = j.coreHelper.IsHostCluster(ctx); err != nil {
		klog.Errorf("check clusterRole error: %+v", err)
		return
	}
	if j.isHostCluster {
		clusters := &v3apicluster.ClusterList{}
		if err = j.coreHelper.ListCluster(ctx, clusters); err != nil {
			return
		}
		if len(clusters.Items) == 0 {
			// backup with not multiCluster
			klog.Info("[devops] there is no Cluster found, backup resources of current cluster..")
			if enabled, err = j.coreHelper.PluginEnabled(ctx, nil, EnabledPaths...); err != nil {
				return
			}
			if enabled {
				var bak *backup
				if bak, err = newBackup("host", nil, j.clientV3, j.coreHelper, j.resourceStore); err != nil {
					return
				}
				if err = bak.backupClusterResources(ctx); err != nil {
					return
				}
			} else {
				klog.Warning("the DevOps not enable, ignore backup resource")
			}
		} else {
			var bak *backup
			for _, cluster := range clusters.Items {
				klog.Infof("back resources of the cluster %s ..", cluster.Name)
				if enabled, err = j.coreHelper.PluginEnabled(ctx, cluster.Spec.Connection.KubeConfig, EnabledPaths...); err != nil {
					return
				}
				if !enabled {
					klog.Warningf("the DevOps not enable in cluster %s, ignore backup resource", cluster.Name)
					continue
				}
				if bak, err = newBackup(cluster.Name, cluster.Spec.Connection.KubeConfig, nil, j.coreHelper, j.resourceStore); err != nil {
					return
				}
				if err = bak.backupClusterResources(ctx); err != nil {
					return
				}
			}
		}
	} else {
		klog.Warning("the clusterRole is not host, ignore to backup resource.")
	}

	// 3. backup configmaps(ks-devops-agent/jenkins-casc-config) in current cluster to restore them after install
	var bak *backup
	if bak, err = newBackup("current", nil, j.clientV3, j.coreHelper, j.resourceStore); err != nil {
		return
	}
	klog.Infof("save configmaps %s and %s of devops..", CascCM, AgentCM)
	agentCm := &corev1.ConfigMap{}
	if err = bak.backupObj(ctx, WorkerNs, AgentCM, AgentCM, agentCm); err != nil {
		return
	}
	if err = bak.backupConfigmapToCluster(ctx, *agentCm); err != nil {
		return
	}
	cascCm := &corev1.ConfigMap{}
	if err = bak.backupObj(ctx, SysNs, CascCM, CascCM, cascCm); err != nil {
		return
	}
	if err = bak.backupConfigmapToCluster(ctx, *cascCm); err != nil {
		return
	}

	// 4. update pv Reclaim to Retain in current cluster
	klog.Info("update PersistentVolumeReclaimPolicy of devops-jenkins pv to Retain if not..")
	var pvc *corev1.PersistentVolumeClaim
	if pvc, err = bak.backupJenkinsPvc(ctx, "current"); err != nil {
		klog.Errorf("backup of devops-jenkins pvc in current cluster error: %+v", err)
		return
	}
	pvName := pvc.Spec.VolumeName
	pvObjKey := types.NamespacedName{Name: pvName}
	pv := &corev1.PersistentVolume{}
	if err = j.clientV3.Get(ctx, pvObjKey, pv); err != nil {
		return
	}
	if pv.Spec.PersistentVolumeReclaimPolicy != corev1.PersistentVolumeReclaimRetain {
		pv.Spec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimRetain
		if err = j.clientV3.Update(ctx, pv); err != nil {
			return
		}
		// check if pv Reclaim updated successfully
		pv = &corev1.PersistentVolume{}
		if err = j.clientV3.Get(ctx, pvObjKey, pv); err != nil {
			return
		}
		if pv.Spec.PersistentVolumeReclaimPolicy != corev1.PersistentVolumeReclaimRetain {
			err = errors.New("failed to update PersistentVolumeReclaimPolicy of devops-jenkins PV to Retain")
			return
		}
	}

	// 5. delete devops and argocd release in current cluster
	if err = j.uninstall(ctx); err != nil {
		klog.Errorf("uninstall devops release error: %+v", err)
		return
	}

	// 6. re-create pvc and bind to the old pv in current cluster to re-use in 4.x..
	// if the old pvc is managed by devops release, will be deleted when uninstall devops and re-create here;
	// otherwise, the pvc already exist after uninstall devops and ignore create it.
	err = j.createNewPvc(ctx, pvc)
	return
}

func (j *upgradeJob) createNewPvc(ctx context.Context, pvc *corev1.PersistentVolumeClaim) (err error) {
	pvName := pvc.Spec.VolumeName
	klog.Infof("re-create pvc and bind to the old pv %s ..", pvName)
	newPvcObjKey := types.NamespacedName{Namespace: pvc.Namespace, Name: pvc.Name}
	newPvc := &corev1.PersistentVolumeClaim{}
	// check if new pvc exist(legacy pvc ks-jenkins or already created)
	if err = j.clientV3.Get(ctx, newPvcObjKey, newPvc); err == nil {
		klog.Warningf("the pvc %s of devops-jenkins exist, ignore create it", pvc.Name)
		return
	}
	if !apierrors.IsNotFound(err) {
		return
	}

	// create new pvc
	newPvc.Name = pvc.Name
	newPvc.Namespace = pvc.Namespace
	newPvc.Spec = pvc.Spec
	newPvc.Labels = map[string]string{
		"app":                          JenkinsWorkload,
		"app.kubernetes.io/managed-by": "ks-upgrade",
	}
	if err = j.clientV3.Create(ctx, newPvc); err != nil {
		klog.Errorf("create new pvc %s error: %+v", newPvc.Name, err)
		return
	}
	// bind to pv
	pv := &corev1.PersistentVolume{}
	if err = j.clientV3.Get(ctx, types.NamespacedName{Name: pvName}, pv); err != nil {
		klog.Errorf("get pv %s error: %+v", pvName, err)
		return
	}
	if pv.Spec.ClaimRef != nil {
		pv.Spec.ClaimRef.Name = pvc.Name
		pv.Spec.ClaimRef.Namespace = pvc.Namespace
		pv.Spec.ClaimRef.ResourceVersion = ""
		pv.Spec.ClaimRef.UID = ""
		if err = j.clientV3.Update(ctx, pv); err != nil {
			klog.Errorf("update ClaimRef of pv %s error: %+v", pvName, err)
			return
		}
	}
	// waiting the new pvc bond to the old pv
	newPvcObjKey.Name = newPvc.Name
	timeCount := 0
	timeout := 60 * 5
	for {
		if err = j.clientV3.Get(ctx, newPvcObjKey, newPvc); err != nil {
			klog.Errorf("check if new pvc bound to pv error: %+v", err)
			return
		}
		if newPvc.Status.Phase == "Bound" {
			klog.Infof("the new pvc bound to the old pv")
			break
		}
		if timeCount >= timeout {
			err = errors.New("waiting to new pvc bind to the old pv timeout")
			return
		}

		time.Sleep(3 * time.Second)
		timeCount += 3
	}
	return
}

func (j *upgradeJob) jobDone() (bool, error) {
	var err error
	if _, err = j.resourceStore.LoadRaw(JobDoneKey); err == nil {
		klog.Info("the upgrade devops job is done, ignore")
		return true, nil
	}
	if errors.Is(err, storage.BackupKeyNotFound) {
		return false, nil
	}
	klog.Errorf("get JobDone info from resourceStore error: %+v", err)
	return false, err
}

func (j *upgradeJob) uninstall(ctx context.Context) (err error) {
	klog.Info("uninstall devops and argocd releases..")
	timeout := time.Minute * 20
	var rel *release.Release
	if rel, err = j.helmClient.GetRelease(SysNs, SysRelease); err == nil {
		klog.V(4).Infof("chart: %s, label: %+v, appVersion: %s", rel.Chart.Name(), rel.Labels, rel.Chart.AppVersion())
		// check if devops release already upgraded
		if strings.HasPrefix(rel.Chart.Name(), "devops") {
			klog.Infof("the devops release already is 4.x, ignore")
		} else {
			// When uninstalling devops, it will automatically delete the kubesphere-devops-worker namespace.
			// Therefore, some processing is done on the PVCs under that namespace.
			if err = j.updateWorkerNsPVCPolicy(ctx); err != nil {
				klog.Errorf("update %s's PVCPolicy error: %+v", WorkerNs, err)
				return
			}

			// remove finalizers: finalizers.kubesphere.io/namespaces of namespace kubesphere-devops-worker
			klog.Infof("remove finalizers of namespace %s ..", WorkerNs)
			workerNs := &corev1.Namespace{}
			if err = j.clientV3.Get(ctx, types.NamespacedName{Name: WorkerNs}, workerNs); err == nil {
				if len(workerNs.Finalizers) > 0 {
					workerNs.SetFinalizers(nil)
					err = j.clientV3.Update(ctx, workerNs)
				}
			}
			if err != nil && !apierrors.IsNotFound(err) {
				klog.Errorf("remove namespace %s finalizers error: %+v", WorkerNs, err)
				return
			}

			_, err = j.helmClient.Uninstall(SysNs, SysRelease, true, timeout)
		}
	}
	if err != nil && !errors.Is(err, driver.ErrReleaseNotFound) {
		klog.Errorf("uninstall DevOps release error: %+v", err)
		return
	}
	if _, err = j.helmClient.GetRelease(ArgoNs, ArgoCDRelease); err == nil {
		_, err = j.helmClient.Uninstall(ArgoNs, ArgoCDRelease, true, timeout)
	}
	if errors.Is(err, driver.ErrReleaseNotFound) {
		err = nil
	}
	return
}

func (j *upgradeJob) updateWorkerNsPVCPolicy(ctx context.Context) error {
	pvcList := &corev1.PersistentVolumeClaimList{}
	if err := j.clientV3.List(ctx, pvcList, runtimeclient.InNamespace(WorkerNs)); err != nil {
		return err
	}

	for _, pvc := range pvcList.Items {
		pvObjKey := types.NamespacedName{Name: pvc.Spec.VolumeName}
		pv := &corev1.PersistentVolume{}
		if err := j.clientV3.Get(ctx, pvObjKey, pv); err != nil {
			if apierrors.IsNotFound(err) {
				klog.Warningf("the pv %s not found", pvObjKey.Name)
				continue
			}
			return err
		}

		if pv.Spec.PersistentVolumeReclaimPolicy != corev1.PersistentVolumeReclaimRetain {
			pv.Spec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimRetain
			if err := j.clientV3.Update(ctx, pv); err != nil {
				return err
			}
		}
	}

	return nil
}

func (j *upgradeJob) PostUpgrade(ctx context.Context) (err error) {
	klog.Info("## devops PostUpgrade ..")
	// 1. check: if upgrade devops job is done, ignore
	var done bool
	if done, err = j.jobDone(); done || err != nil {
		return
	}

	// 3. parse j.inClusterConfig and j.inClusterOverrides from 3.x if isHostCluster
	if j.isHostCluster, err = j.coreHelper.IsHostCluster(ctx); err != nil {
		return
	}

	if j.isHostCluster {
		j.inClusterConfig = new(Config)
		j.inClusterOverrides = map[string]*Override{}
		j.enabledClusters = []string{}

		clusters := &v3apicluster.ClusterList{}
		if err = j.coreHelper.ListCluster(ctx, clusters); err != nil {
			return
		}
		var needInstall bool
		for _, cluster := range clusters.Items {
			if needInstall, err = j.needInstall(ctx, cluster); err != nil {
				return
			}
			if !needInstall {
				continue
			}
			j.enabledClusters = append(j.enabledClusters, cluster.Name)

			// 3.1 setup devops configurations into j.inClusterOverrides from cluster
			if err = j.parseSystemValues(cluster); err != nil {
				klog.Errorf("[%s]parsed system values error: %+v", cluster.Name, err)
				return
			}

			// 3.2 setup argocd configurations into j.inClusterOverrides from cluster
			if err = j.parseArgocdValues(cluster); err != nil {
				if !errors.Is(err, storage.BackupKeyNotFound) {
					klog.Errorf("[%s]parsed argocd values error: %+v", cluster.Name, err)
					return
				}
				klog.Warningf("[%s]the argocd release not exist in storage, ignore parse argocd values", cluster.Name)
			}

			// 3.3 setup jenkins configurations(resources/persistence/smtp/sonarqube) into j.inClusterOverrides from cluster
			if err = j.parseJenkinsValues(cluster); err != nil {
				klog.Errorf("[%s]parsed jenkins values: %+v", cluster.Name, err)
				return
			}
			if klog.V(4).Enabled() {
				overridesYaml, _ := yaml.Marshal(j.inClusterOverrides)
				klog.Infof("[%s]parsed jenkins values, overrides: \n%s", cluster.Name, overridesYaml)
			}
		}

		// 4. install DevOps by InstallPlan
		if err = j.install(ctx); err != nil {
			klog.Errorf("install devops error: %+v", err)
			return
		}
	}

	// check if DevOps enabled in current cluster
	var enabled bool
	if enabled, err = j.coreHelper.PluginEnabled(ctx, nil, EnabledPaths...); err != nil {
		klog.Errorf("check DevOps enabled error: %+v", err)
		return
	}
	if !enabled {
		klog.Warning("the DevOps is not enabled in current cluster, ignore")
		return
	}

	// 5. update labels of DevOpsProjects namespace
	klog.Infof("update labels of DevOpsProjects namespace ..")
	projects := &devopsv1alpha3.DevOpsProjectList{}
	if err = j.clientV4.List(ctx, projects); err != nil {
		klog.Errorf("list devops-projects error: %+v", err)
		return
	}
	for _, proj := range projects.Items {
		projNs := &corev1.Namespace{}
		if err = j.clientV4.Get(ctx, types.NamespacedName{Name: proj.Name}, projNs); err != nil {
			klog.Errorf("get namespace of devops-project %s error: %+v", proj.Name, err)
			return
		}
		_, managedByDevOpsExtension := projNs.Labels[ManagedKey]
		_, managedByWorkspace := projNs.Labels[WorkspaceKey]
		if !managedByDevOpsExtension || !managedByWorkspace {
			klog.Infof("update namespace %s", projNs.Name)
			projNs.Labels[ManagedKey] = ManagedTrue
			projNs.Labels[WorkspaceKey] = proj.Labels[WorkspaceKey]
			if err = j.clientV4.Update(ctx, projNs); err != nil {
				return
			}
		}
	}

	// 6. job done
	var extensionRefBytes []byte
	if extensionRefBytes, err = yaml.Marshal(j.extensionRef); err != nil {
		klog.Errorf("marshal j.extensionRef to bytes error: %+v", err)
		return
	}
	err = j.resourceStore.SaveRaw(JobDoneKey, extensionRefBytes)
	return
}

func (j *upgradeJob) needInstall(ctx context.Context, cluster v3apicluster.Cluster) (bool, error) {
	// if the placement.clusters has been specified and not contained the cluster, then ignore install
	if j.extensionRef.ClusterScheduling != nil &&
		j.extensionRef.ClusterScheduling.Placement != nil &&
		len(j.extensionRef.ClusterScheduling.Placement.Clusters) > 0 {
		if !slices.Contains(j.extensionRef.ClusterScheduling.Placement.Clusters, cluster.Name) {
			klog.Infof("ParseConfig: the cluster %s not in placements, ignore", cluster.Name)
			return false, nil
		}
	}

	// the DevOps not enabled in the cluster
	enabled, err := j.coreHelper.PluginEnabled(ctx, cluster.Spec.Connection.KubeConfig, EnabledPaths...)
	if err != nil {
		return false, err
	}
	if !enabled {
		klog.Infof("ParseConfig: the DevOps in cluster %s not enabled, ignore", cluster.Name)
	}
	return enabled, nil
}

func (j *upgradeJob) parseSystemValues(cluster v3apicluster.Cluster) (err error) {
	klog.Infof("## [%s]setup resources of workloads in kubesphere-devops-system ..", cluster.Name)
	deployments := &appv1.DeploymentList{}
	if err = j.resourceStore.Load(fmt.Sprintf(DeploymentsFmt, cluster.Name, SysNs), deployments); err != nil {
		return
	}
	var override *Override
	var ok bool
	for _, deployment := range deployments.Items {
		switch deployment.Name {
		case ApiServerWorkload:
			if override, ok = j.inClusterOverrides[cluster.Name]; !ok {
				override = new(Override)
			}
			if override.Agent == nil {
				override.Agent = new(AgentOptions)
			}
			override.Agent.SetApiserver(deployment)
			j.inClusterOverrides[cluster.Name] = override

			if isHostCluster(cluster) {
				if j.inClusterConfig.Extension == nil {
					j.inClusterConfig.Extension = &ExtensionOptions{}
				}
				j.inClusterConfig.Extension.Apiserver = new(Workload)
				j.inClusterConfig.Extension.Apiserver.FromDeployment(deployment)
			}
		case ControllerWorkload:
			if override, ok = j.inClusterOverrides[cluster.Name]; !ok {
				override = new(Override)
			}
			if override.Agent == nil {
				override.Agent = new(AgentOptions)
			}
			override.Agent.SetController(deployment)
			j.inClusterOverrides[cluster.Name] = override
		}
	}
	return
}

func (j *upgradeJob) parseArgocdValues(cluster v3apicluster.Cluster) (err error) {
	klog.Infof("## [%s]setup resources of workloads in argocd ..", cluster.Name)
	var argocdValues []byte
	if argocdValues, err = j.resourceStore.LoadRaw(fmt.Sprintf(HelmValuesFmt, cluster.Name, ArgoNs, ArgoCDRelease)); err != nil {
		return
	}
	argocd := &ArgoOptions{}
	if err = json.Unmarshal(argocdValues, argocd); err != nil {
		return
	}
	argocd.cleanImageNil()

	if argocd.Dex.Resources == nil || argocd.Dex.Resources.Limits == nil {
		argocd.Dex.Resources = &ResourceRequirements{
			Limits: &Resource{
				Memory: "1024Mi",
				Cpu:    "500m",
			},
		}
	} else {
		// update resources.limit of argocd.Dex for starting failed
		if strings.HasSuffix(argocd.Dex.Resources.Limits.Memory, "Mi") {
			var dexMem int
			if dexMem, err = strconv.Atoi(strings.TrimSuffix(argocd.Dex.Resources.Limits.Memory, "Mi")); err != nil {
				return
			}
			if dexMem < 1024 {
				argocd.Dex.Resources.Limits.Memory = "1024Mi"
			}
		}
		if strings.HasSuffix(argocd.Dex.Resources.Limits.Cpu, "m") {
			var dexCpu int
			if dexCpu, err = strconv.Atoi(strings.TrimSuffix(argocd.Dex.Resources.Limits.Cpu, "m")); err != nil {
				return
			}
			if dexCpu < 500 {
				argocd.Dex.Resources.Limits.Cpu = "500m"
			}
		}
	}

	j.inClusterOverrides[cluster.Name].Agent.Argocd = argocd
	return
}

func (j *upgradeJob) parseJenkinsValues(cluster v3apicluster.Cluster) (err error) {
	klog.Infof("## [%s]parse devops-jenkins values..", cluster.Name)
	jenkinsConfig := new(JenkinsOptions)

	// 1. setup persistence: re-use the exist pv/pvc by new devops-jenkins
	klog.Infof("setup persistence: re-use the exist pv/pvc by new devops-jenkins")
	pv := &corev1.PersistentVolume{}
	if err = j.resourceStore.Load(fmt.Sprintf(PvFmt, cluster.Name, JenkinsWorkload), pv); err != nil {
		return
	}

	jenkinsConfig.Persistence = &Persistence{ExistingClaim: pv.Spec.ClaimRef.Name}

	// 2. setup jenkins master resources and envs
	klog.Info("setup jenkins master ..")
	deployments := &appv1.DeploymentList{}
	if err = j.resourceStore.Load(fmt.Sprintf(DeploymentsFmt, cluster.Name, SysNs), deployments); err != nil {
		return
	}
	for _, deployment := range deployments.Items {
		if deployment.Name == JenkinsWorkload {
			klog.Infof("setup resource / smtp / sonarqube from old devops-jenkins ..")
			master := new(JenkinsMaster)
			// setup resource
			container := deployment.Spec.Template.Spec.Containers[0]
			master.Workload = &Workload{
				Resources: new(ResourceRequirements),
			}
			master.Workload.Resources.Parse(container.Resources)
			// setup smtp and sonarqube
			master.Smtp = new(Smtp)
			for _, env := range container.Env {
				switch env.Name {
				case "EMAIL_SMTP_HOST":
					master.Smtp.Host = env.Value
				case "EMAIL_SMTP_PORT":
					master.Smtp.Port = env.Value
				case "EMAIL_USE_SSL":
					master.Smtp.UseSSL, _ = strconv.ParseBool(env.Value)
				case "EMAIL_FROM_NAME":
					master.Smtp.FromName = env.Value
				case "EMAIL_FROM_ADDR":
					master.Smtp.FromAddr = env.Value
				case "EMAIL_FROM_PASS":
					master.Smtp.FromPass = env.Value
				case "SONAR_SERVER_URL":
					if master.Sonarqube == nil {
						master.Sonarqube = new(Sonarqube)
					}
					master.Sonarqube.ServerUrl = env.Value
				case "SONAR_AUTH_TOKEN":
					if master.Sonarqube == nil {
						master.Sonarqube = new(Sonarqube)
					}
					master.Sonarqube.AuthToken = env.Value
				}
			}
			jenkinsConfig.Master = master
			break
		}
	}
	if jenkinsConfig.Master == nil {
		return errors.New("failed to setup devops-jenkins master")
	}

	secret := &corev1.Secret{}
	_ = j.resourceStore.Load(fmt.Sprintf(SecretDevOpsJenkins, cluster.Name), secret)
	if jenkinsAdminPassword, ok := secret.Data["jenkins-admin-password"]; ok && len(jenkinsAdminPassword) > 0 {
		jenkinsConfig.Master.AdminPassword = string(jenkinsAdminPassword)
	}

	// 3. setup ServiceType of devops-jenkins
	var oldValues, oldValuesJson []byte
	if oldValues, err = j.resourceStore.LoadRaw(fmt.Sprintf(HelmValuesFmt, cluster.Name, SysNs, SysRelease)); err != nil {
		return
	}
	if oldValuesJson, err = yaml.YAMLToJSON(oldValues); err != nil {
		return
	}
	serviceType := gjson.GetBytes(oldValuesJson, "jenkins.Master.ServiceType")
	if serviceType.Exists() {
		jenkinsConfig.Master.ServiceType = serviceType.String()
	}

	j.inClusterOverrides[cluster.Name].Agent.Jenkins = jenkinsConfig
	return
}

func (j *upgradeJob) install(ctx context.Context) (err error) {
	// check if there is placement.cluster or enabled devops cluster
	if len(j.enabledClusters) == 0 && len(j.extensionRef.ClusterScheduling.Placement.Clusters) == 0 {
		klog.Warning("there is no placement.cluster and cluster enabled DevOps, ignore create InstallPlan")
		return
	}

	// 1. merge apiserver.replica/resources in j.inClusterConfig into j.extensionRef.config from config.yaml
	klog.Infof("setup apiserver.replicas/resources to j.extensionRef.Config ...")
	var customConfigJson []byte
	if customConfigJson, err = yaml.YAMLToJSON([]byte(j.extensionRef.Config)); err != nil {
		return
	}
	var replicaResult, resourceResult gjson.Result
	if replicaResult = gjson.GetBytes(customConfigJson, ExtApiserverRepKey); !replicaResult.Exists() {
		if customConfigJson, err = sjson.SetBytes(customConfigJson, ExtApiserverRepKey, j.inClusterConfig.Extension.Apiserver.Replicas); err != nil {
			return err
		}
	}
	if resourceResult = gjson.GetBytes(customConfigJson, ExtApiserverResourceKey); !resourceResult.Exists() {
		if customConfigJson, err = sjson.SetBytes(customConfigJson, ExtApiserverResourceKey, j.inClusterConfig.Extension.Apiserver.Resources); err != nil {
			return err
		}
	}
	if !replicaResult.Exists() || !resourceResult.Exists() {
		var customConfigYaml []byte
		if customConfigYaml, err = yaml.JSONToYAML(customConfigJson); err != nil {
			return err
		}
		j.extensionRef.Config = string(customConfigYaml)
	}

	// 2. update extensionRef.clusterScheduling
	klog.Infof("merge customOverrides and inClusterOverrides ...")
	var customOverride *Override
	var overrideBytes []byte
	for clusterName, override := range j.inClusterOverrides {
		if _, ok := j.extensionRef.ClusterScheduling.Overrides[clusterName]; ok {
			if err = yaml.Unmarshal([]byte(j.extensionRef.ClusterScheduling.Overrides[clusterName]), customOverride); err != nil {
				return
			}
			klog.V(4).Infof("customOverride: %+v, inClusterOverride: %+v", customOverride, override)
			// merge customOverride into override in 3.x, means the customOverride in config.yaml has highest priority
			if err = mergo.Merge(override, customOverride, mergo.WithOverride); err != nil {
				return
			}
		}
		if overrideBytes, err = yaml.Marshal(override); err != nil {
			return
		}
		j.extensionRef.ClusterScheduling.Overrides[clusterName] = string(overrideBytes)
	}
	// if the placement.clusters in config.yaml exist, use it,
	// otherwise set placement.clusters that enabled devops in 3.x
	if len(j.extensionRef.ClusterScheduling.Placement.Clusters) == 0 {
		j.extensionRef.ClusterScheduling.Placement.Clusters = j.enabledClusters
	}

	klog.Info("create DevOps InstallPlan..")
	if klog.V(4).Enabled() {
		extensionRefBytes, _ := yaml.Marshal(j.extensionRef)
		klog.V(4).Infof("extensionRef: \n%s", extensionRefBytes)
	}

	watchFuncs := func() (context.Context, time.Duration, time.Duration, wait.ConditionWithContextFunc) {
		return ctx, time.Second * 2, time.Minute * 10, func(ctx context.Context) (done bool, err error) {
			ext := v4corev1alpha1.InstallPlan{}
			if err := j.clientV4.Get(ctx, types.NamespacedName{Name: j.extensionRef.Name}, &ext, &runtimeclient.GetOptions{}); err != nil {
				return false, fmt.Errorf("get InstallPlan error: %w", err)
			}
			klog.Infof("CreateInstallPlanFromExtensionRef object, %v", ext)
			if ext.Status.State == v4corev1alpha1.StateDeployed {
				return true, nil
			}
			return false, nil
		}
	}

	return j.coreHelper.CreateInstallPlanFromExtensionRef(ctx, j.extensionRef, watchFuncs)
}

func isHostCluster(cluster v3apicluster.Cluster) bool {
	if _, ok := cluster.Labels[v3apicluster.HostCluster]; ok {
		return true
	}
	return false
}
