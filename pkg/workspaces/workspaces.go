package workspaces

import (
	"encoding/json"
	"fmt"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"kubesphere.io/ks-upgrade/pkg/task"
)

type workspaceMigrateTask struct {
	k8sClient kubernetes.Interface
}

func NewWorkspaceMigrateTask(k8sClient kubernetes.Interface) task.UpgradeTask {
	return &workspaceMigrateTask{k8sClient: k8sClient}
}

func (t *workspaceMigrateTask) Run() error {
	hostCluster, err := t.getHostCluster()
	if err != nil {
		klog.Error(err)
		return err
	}
	cli := t.k8sClient.(*kubernetes.Clientset)
	workspaces := make([]WorkspaceTemplate, 0)
	data, err := cli.RESTClient().Get().AbsPath("/apis/tenant.kubesphere.io/v1alpha1/workspaces").DoRaw()
	if err != nil {
		klog.Error(err)
		return err
	}
	var workspaceList WorkspaceList
	json.Unmarshal(data, &workspaceList)
	for _, workspace := range workspaceList.Items {
		workspaceTemplate := WorkspaceTemplate{
			TypeMeta: metav1.TypeMeta{
				Kind:       "WorkspaceTemplate",
				APIVersion: "tenant.kubesphere.io/v1alpha2",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        workspace.Name,
				Annotations: workspace.Annotations,
			},
			Spec: FederatedWorkspaceSpec{
				Template: Workspace{
					ObjectMeta: metav1.ObjectMeta{
						Name:        workspace.Name,
						Labels:      workspace.Labels,
						Annotations: workspace.Annotations,
					},
					Spec: workspace.Spec,
				},
				Placement: GenericPlacementFields{},
			},
		}

		if hostCluster != "" {
			workspaceTemplate.Spec.Placement.Clusters = []GenericClusterReference{{Name: hostCluster}}
		}

		if workspace.Name == "system-workspace" {
			workspaceTemplate.Spec.Placement = GenericPlacementFields{ClusterSelector: metav1.SetAsLabelSelector(labels.Set{})}
		}

		workspaces = append(workspaces, workspaceTemplate)
	}

	for _, workspace := range workspaces {
		outputData, _ := json.Marshal(workspace)
		klog.Infof("migrate workspace: %s: %s", workspace.Name, string(outputData))
		err := cli.RESTClient().
			Post().
			AbsPath(fmt.Sprintf("/apis/tenant.kubesphere.io/v1alpha2/workspacetemplates")).
			Body(outputData).
			Do().Error()

		if err != nil && !errors.IsAlreadyExists(err) {
			klog.Error(err)
			return err
		}
	}

	return nil
}

func (t *workspaceMigrateTask) getHostCluster() (string, error) {
	cli := t.k8sClient.(*kubernetes.Clientset)
	data, err := cli.RESTClient().Get().AbsPath("/apis/cluster.kubesphere.io/v1alpha1/clusters").Param("labelSelector", "cluster-role.kubesphere.io/host").DoRaw()
	if err != nil && !errors.IsNotFound(err) {
		klog.Error(err)
		return "", err
	}
	var list metav1.List
	if err = json.Unmarshal(data, &list); err == nil && len(list.Items) > 0 {
		var item struct {
			Metadata metav1.ObjectMeta `json:"metadata"`
		}
		if err = json.Unmarshal(list.Items[0].Raw, &item); err == nil {
			return item.Metadata.Name, nil
		}
	}
	return "", nil
}
