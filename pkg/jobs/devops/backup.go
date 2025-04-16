package devops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"kubesphere.io/ks-upgrade/pkg/helm"
	"kubesphere.io/ks-upgrade/pkg/model/core"
	"kubesphere.io/ks-upgrade/pkg/storage"
	"kubesphere.io/ks-upgrade/pkg/store"
	v3apidevops "kubesphere.io/ks-upgrade/v3/api/devops/v1alpha3"
)

const (
	SysRelease    = "devops"
	ArgoCDRelease = "devops"

	PvcName       = "devops-jenkins"
	LegacyPvcName = "ks-jenkins"

	HelmReleaseFmt      = "%s-%s-%s-release"
	HelmValuesFmt       = "%s-%s-%s-values"
	PvFmt               = "%s-%s-pv"
	PvcFmt              = "%s-%s-pvc"
	DeploymentsFmt      = "%s-%s-deployments"
	ConfigmapsFmt       = "%s-%s-configmaps"
	SecretDevOpsJenkins = "%s-secret-devops-jenkins"
	ProjectsFmt         = "%s-devops-projects"
	PipelinesFmt        = "%s-devops-pipelines"
	PipelineRunsFmt     = "%s-devops-pipelineruns"
	GitReposFmt         = "%s-devops-gitrepos"
	CredListFmt         = "%s-devops-secrets"
)

type backup struct {
	cluster string

	client     runtimeclient.Client
	helmClient helm.HelmClient

	coreHelper    core.Helper
	resourceStore store.ResourceStore
}

func newBackup(cluster string, kubeconfig []byte, client runtimeclient.Client,
	helper core.Helper, store store.ResourceStore) (*backup, error) {
	bak := &backup{
		cluster:       cluster,
		client:        client,
		coreHelper:    helper,
		resourceStore: store,
	}

	var err error
	if kubeconfig == nil {
		if bak.helmClient, err = helm.NewClient(""); err != nil {
			klog.Errorf("new helm client error: %+v", err)
			return nil, err
		}
	} else {
		if bak.client, err = helper.NewRuntimeClientV3(kubeconfig); err != nil {
			klog.Errorf("new clientV3 with kubeconfig: %s error: %+v", kubeconfig, err)
			return nil, err
		}

		if bak.helmClient, err = helm.NewClientWithConf(kubeconfig, ""); err != nil {
			klog.Errorf("new helm client with kubeconfig: %s error: %+v", kubeconfig, err)
			return nil, err
		}
	}
	return bak, nil
}

func (b *backup) backupClusterResources(ctx context.Context) (err error) {
	klog.Infof("backup resources for cluster: %s", b.cluster)

	// 1. save: devops release and values
	klog.Info("save helm release devops and values..")
	if err = b.backupHelmRelease(SysNs, SysRelease); err != nil {
		return
	}

	// 2. save deployment: apiserver / controller / jenkins
	klog.Info("save deployment: devops-apiserver/devops-controller/devops-jenkins..")
	var key string
	deployments := &appv1.DeploymentList{}
	key = fmt.Sprintf(DeploymentsFmt, b.cluster, SysNs)
	if err = b.backupList(ctx, SysNs, key, deployments); err != nil {
		return
	}

	// 3. save configmaps in kubesphere-devops-system: devops-config devops-jenkins jenkins-agent-config jenkins-casc-config
	klog.Info("save configmaps of devops..")
	configmaps := &corev1.ConfigMapList{}
	key = fmt.Sprintf(ConfigmapsFmt, b.cluster, SysNs)
	if err = b.backupList(ctx, SysNs, key, configmaps); err != nil {
		return
	}

	// 4. save configmaps in kubesphere-devops-worker: ks-devops-agent
	configmaps = &corev1.ConfigMapList{}
	key = fmt.Sprintf(ConfigmapsFmt, b.cluster, WorkerNs)
	if err = b.backupList(ctx, WorkerNs, key, configmaps); err != nil {
		return
	}

	// 5. save pv and pvc(ks-jenkins or devops-jenkins)
	klog.Info("save devops-jenkins pv and pvc..")
	if _, err = b.backupJenkinsPvc(ctx, b.cluster); err != nil {
		return
	}

	// 5. save pv and pvc(ks-jenkins or devops-jenkins)
	klog.Info("save devops-jenkins secret")
	if err = b.backupObj(ctx, SysNs, "devops-jenkins", fmt.Sprintf(SecretDevOpsJenkins, b.cluster), &corev1.Secret{}); err != nil {
		return
	}

	// 6. save devopsProjects / pipelines / pipelineRuns..
	klog.Info("save resources of DevOpsProjects..")
	if err = b.backupDevopsCRs(ctx, b.cluster); err != nil {
		return
	}

	// 7. save argocd release and values
	klog.Info("save helm release argocd and values..")
	if err = b.backupHelmRelease(ArgoNs, ArgoCDRelease); err != nil {
		if errors.Is(err, driver.ErrReleaseNotFound) {
			klog.Warning("the argocd release not exist, ignore backup resource for that")
			err = nil
		}
		return
	}

	// 8. save configmaps in argocd: devops-config devops-jenkins jenkins-agent-config jenkins-casc-config
	klog.Info("save configmaps of argocd..")
	argoConfigmaps := &corev1.ConfigMapList{}
	key = fmt.Sprintf(ConfigmapsFmt, b.cluster, ArgoNs)
	err = b.backupList(ctx, ArgoNs, key, argoConfigmaps)
	return
}

func (b *backup) backupHelmRelease(relNamespace, relName string) (err error) {
	relKey := fmt.Sprintf(HelmReleaseFmt, b.cluster, relNamespace, relName)
	valueKey := fmt.Sprintf(HelmValuesFmt, b.cluster, relNamespace, relName)
	if _, err = b.resourceStore.LoadRaw(valueKey); err == nil {
		klog.Infof("the release %s already backup, ignore", relKey)
		return
	}

	var rel *release.Release
	var relBytes, valuesBytes []byte
	if rel, err = b.helmClient.GetRelease(relNamespace, relName); err != nil {
		return
	}
	if relBytes, err = json.Marshal(rel); err != nil {
		return
	}
	if err = b.resourceStore.SaveRaw(relKey, relBytes); err != nil {
		return
	}
	if valuesBytes, err = json.Marshal(rel.Config); err != nil {
		return
	}
	err = b.resourceStore.SaveRaw(valueKey, valuesBytes)
	return
}

func (b *backup) backupObj(ctx context.Context, namespace, name, backupKey string, obj runtimeclient.Object) (err error) {
	if err = b.resourceStore.Load(backupKey, obj); err != nil {
		if !errors.Is(err, storage.BackupKeyNotFound) {
			return err
		}
		if err = b.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, obj); err != nil {
			return
		}
		err = b.resourceStore.Save(backupKey, obj)
		return
	}
	klog.Infof("the resource with key %s already exist, ignore backup", backupKey)
	return
}

// backup all configmaps to k8s namespace kubesphere-devops-system in-case
// that kubesphere-devops-worker would be delete with uninstall release devops
func (b *backup) backupConfigmapToCluster(ctx context.Context, cm corev1.ConfigMap) (err error) {
	backupCm := &corev1.ConfigMap{}
	backupName := fmt.Sprintf(ConfigMapBackupFmt, cm.Name)
	if err = b.client.Get(ctx, types.NamespacedName{Namespace: SysNs, Name: backupName}, backupCm); err != nil {
		if apierrors.IsNotFound(err) {
			backupCm.Name = backupName
			backupCm.Namespace = SysNs
			backupCm.Data = cm.Data
			err = b.client.Create(ctx, backupCm)
		}
		return
	}
	klog.Infof("the backup configmap %s already exist, ignore", backupName)
	return
}

func (b *backup) backupList(ctx context.Context, namespace, backupKey string, objs runtimeclient.ObjectList) (err error) {
	if err = b.resourceStore.Load(backupKey, objs); err != nil {
		if !errors.Is(err, storage.BackupKeyNotFound) {
			return err
		}
		if err = b.client.List(ctx, objs, runtimeclient.InNamespace(namespace)); err != nil {
			return
		}
		err = b.resourceStore.Save(backupKey, objs)
		return
	}
	klog.Infof("the resources with key %s already exist, ignore backup", backupKey)
	return
}

func (b *backup) backupJenkinsPvc(ctx context.Context, clusterName string) (*corev1.PersistentVolumeClaim, error) {
	// 1. backup pvc of devops-jenkins
	var err error
	pvcs := &corev1.PersistentVolumeClaimList{}
	backupPvcKey := fmt.Sprintf(PvcFmt, clusterName, JenkinsWorkload)
	klog.Infof("backup pvc(devops-jenkins) in namespace %s", SysNs)
	if err = b.backupList(ctx, SysNs, backupPvcKey, pvcs); err != nil {
		return nil, err
	}

	// 2. backup pv of devops-jenkins
	pv := &corev1.PersistentVolume{}
	backupPvKey := fmt.Sprintf(PvFmt, clusterName, JenkinsWorkload)
	// get pvName from pvc(legacy pvc ks-jenkins or pvc devops-jenkins)
	var pvc *corev1.PersistentVolumeClaim
	for _, item := range pvcs.Items {
		if item.Name == PvcName || item.Name == LegacyPvcName {
			pvc = &item
			break
		}
	}
	if pvc == nil {
		return nil, errors.New("not found pvc of devops-jenkins")
	}
	klog.V(4).Infof("the devops-jenkins pv name is %s", pvc.Spec.VolumeName)
	err = b.backupObj(ctx, "", pvc.Spec.VolumeName, backupPvKey, pv)
	return pvc, err
}

func (b *backup) backupDevopsCRs(ctx context.Context, clusterName string) (err error) {
	projects := &v3apidevops.DevOpsProjectList{}
	key := fmt.Sprintf(ProjectsFmt, clusterName)
	if err = b.backupList(ctx, "", key, projects); err != nil {
		return
	}

	pipelines := &v3apidevops.PipelineList{}
	key = fmt.Sprintf(PipelinesFmt, clusterName)
	if err = b.backupList(ctx, "", key, pipelines); err != nil {
		return
	}

	pipelineRuns := &v3apidevops.PipelineRunList{}
	key = fmt.Sprintf(PipelineRunsFmt, clusterName)
	if err = b.backupList(ctx, "", key, pipelineRuns); err != nil {
		return
	}

	gitRepos := &v3apidevops.GitRepositoryList{}
	key = fmt.Sprintf(GitReposFmt, clusterName)
	if err = b.backupList(ctx, "", key, gitRepos); err != nil {
		return
	}

	credentials := &corev1.SecretList{}
	key = fmt.Sprintf(CredListFmt, clusterName)
	if err = b.resourceStore.Load(key, credentials); err == nil {
		klog.Infof("the DevOps credentials(%s) already backup, ignore", key)
		return
	}
	if !errors.Is(err, storage.BackupKeyNotFound) {
		return
	}
	for _, project := range projects.Items {
		secrets := &corev1.SecretList{}
		if err = b.client.List(ctx, secrets, runtimeclient.InNamespace(project.Namespace)); err != nil {
			return
		}
		if len(secrets.Items) > 0 {
			credentials.Items = append(credentials.Items, secrets.Items...)
		}
	}
	err = b.resourceStore.Save(key, credentials)
	return
}
