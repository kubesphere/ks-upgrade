package workspacerolebindings

import (
	"encoding/json"
	"fmt"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"kubesphere.io/ks-upgrade/pkg/task"
	"strings"
)

type workspaceRoleBindingMigrateTask struct {
	k8sClient kubernetes.Interface
}

func NewWorkspaceRoleBindingMigrateTask(k8sClient kubernetes.Interface) task.UpgradeTask {
	return &workspaceRoleBindingMigrateTask{k8sClient: k8sClient}
}

func (t *workspaceRoleBindingMigrateTask) Run() error {

	workspaceRoleBindings := make([]WorkspaceRoleBinding, 0)

	clusterRoleBindings, err := t.k8sClient.RbacV1().ClusterRoleBindings().List(metav1.ListOptions{})

	if err != nil {
		klog.Error(err)
		return err
	}

	for _, clusterRoleBinding := range clusterRoleBindings.Items {
		workspace := clusterRoleBinding.Labels["kubesphere.io/workspace"]
		if workspace == "" {
			continue
		}
		workspaceRole := strings.TrimPrefix(clusterRoleBinding.RoleRef.Name, fmt.Sprintf("workspace:%s:", workspace))
		if workspaceRole == "" {
			continue
		}
		if workspaceRole == "regular" {
			workspaceRole = "self-provisioner"
		}
		workspaceRole = fmt.Sprintf("%s-%s", workspace, workspaceRole)
		for _, subject := range clusterRoleBinding.Subjects {
			if subject.Kind == "User" {
				workspaceRoleBinding := WorkspaceRoleBinding{
					TypeMeta: metav1.TypeMeta{
						Kind:       "WorkspaceRoleBinding",
						APIVersion: "iam.kubesphere.io/v1alpha2",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf("%s-%s", subject.Name, workspaceRole),
						Labels: map[string]string{
							"iam.kubesphere.io/user-ref": subject.Name,
							"kubesphere.io/workspace":    workspace,
						},
					},
					Subjects: []rbacv1.Subject{{Name: subject.Name, Kind: "User", APIGroup: "rbac.authorization.k8s.io"}},
					RoleRef: rbacv1.RoleRef{
						APIGroup: "iam.kubesphere.io",
						Kind:     "WorkspaceRole",
						Name:     workspaceRole,
					},
				}
				workspaceRoleBindings = append(workspaceRoleBindings, workspaceRoleBinding)
			}
		}
	}

	cli := t.k8sClient.(*kubernetes.Clientset)
	for _, workspaceRoleBinding := range workspaceRoleBindings {
		outputData, _ := json.Marshal(workspaceRoleBinding)
		klog.Infof("migrate workspaceRoleBinding: %s: %s", workspaceRoleBinding.Name, string(outputData))
		err := cli.RESTClient().
			Post().
			AbsPath(fmt.Sprintf("/apis/iam.kubesphere.io/v1alpha2/workspacerolebindings")).
			Body(outputData).
			Do().Error()

		if err != nil && !errors.IsAlreadyExists(err) {
			klog.Error(err)
			return err
		}
	}

	// delete legacy workspace role binding after upgrade
	if err := t.k8sClient.RbacV1().ClusterRoleBindings().
		DeleteCollection(metav1.NewDeleteOptions(0), metav1.ListOptions{LabelSelector: "kubesphere.io/workspace"}); err != nil {
		klog.Error(err)
		return err
	}

	return nil
}
