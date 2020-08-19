package rolebindings

import (
	"encoding/json"
	"fmt"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"kubesphere.io/ks-upgrade/pkg/task"
)

type roleBindingMigrateTask struct {
	k8sClient kubernetes.Interface
}

func NewRoleBindingMigrateTask(k8sClient kubernetes.Interface) task.UpgradeTask {
	return &roleBindingMigrateTask{k8sClient: k8sClient}
}

func (t *roleBindingMigrateTask) Run() error {

	roleBindings := make([]rbacv1.RoleBinding, 0)

	oldRoleBindings, err := t.k8sClient.RbacV1().RoleBindings("").List(metav1.ListOptions{})
	if err != nil {
		klog.Error(err)
		return err
	}

	migrateMapping := map[string]string{
		"admin":    "admin",
		"operator": "operator",
		"viewer":   "viewer",
	}

	for _, oldRoleBinding := range oldRoleBindings.Items {
		// delete <workspace>-admin <workspace>-viewer role binding after upgrade
		if oldRoleBinding.Name == "admin" || oldRoleBinding.Name == "viewer" {
			if err := t.deleteRoleBinding(&oldRoleBinding); err != nil {
				klog.Error(err)
				return err
			}
			continue
		}
		role := migrateMapping[oldRoleBinding.RoleRef.Name]
		if role == "" {
			continue
		}
		for _, subject := range oldRoleBinding.Subjects {
			if subject.Kind == "User" {
				roleBinding := rbacv1.RoleBinding{
					TypeMeta: metav1.TypeMeta{
						Kind:       "RoleBinding",
						APIVersion: "rbac.authorization.k8s.io/v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("%s-%s", subject.Name, role),
						Namespace: oldRoleBinding.Namespace,
						Labels: map[string]string{
							"iam.kubesphere.io/user-ref": subject.Name,
						},
					},
					Subjects: []rbacv1.Subject{{Name: subject.Name, Kind: "User", APIGroup: "rbac.authorization.k8s.io"}},
					RoleRef: rbacv1.RoleRef{
						APIGroup: "rbac.authorization.k8s.io",
						Kind:     "Role",
						Name:     role,
					},
				}
				roleBindings = append(roleBindings, roleBinding)
			}
		}
	}

	for _, roleBinding := range roleBindings {
		outputData, _ := json.Marshal(roleBinding)
		klog.Infof("migrate roleBinding: namespace:%s, %s: %s", roleBinding.Namespace, roleBinding.Name, string(outputData))

		if err := t.deleteRoleBinding(&roleBinding); err != nil {
			klog.Error(err)
			return err
		}

		if err := t.createRoleBinding(&roleBinding); err != nil {
			klog.Error(err)
			return err
		}
	}

	return nil
}

func (t *roleBindingMigrateTask) deleteRoleBinding(roleBinding *rbacv1.RoleBinding) error {
	err := t.k8sClient.RbacV1().RoleBindings(roleBinding.Namespace).Delete(roleBinding.Name, metav1.NewDeleteOptions(0))
	if err != nil && !errors.IsNotFound(err) {
		klog.Error(err)
		return err
	}
	return nil
}

func (t *roleBindingMigrateTask) createRoleBinding(roleBinding *rbacv1.RoleBinding) error {
	_, err := t.k8sClient.RbacV1().RoleBindings(roleBinding.Namespace).Create(roleBinding)
	if err != nil && !errors.IsAlreadyExists(err) {
		klog.Error(err)
		return err
	}
	return nil
}
