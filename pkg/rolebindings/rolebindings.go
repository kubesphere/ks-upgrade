package rolebindings

import (
	"encoding/json"
	"fmt"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"kubesphere.io/ks-upgrade/pkg/rbac"
	"kubesphere.io/ks-upgrade/pkg/task"
	"time"
)

type roleBindingMigrateTask struct {
	k8sClient kubernetes.Interface
}

func NewRoleBindingMigrateTask(k8sClient kubernetes.Interface) task.UpgradeTask {
	return &roleBindingMigrateTask{k8sClient: k8sClient}
}

func (t *roleBindingMigrateTask) Run() error {

	roleBindings := make([]rbacv1.RoleBinding, 0)
	newRoles := make([]rbacv1.Role, 0)

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
		roleRef := migrateMapping[oldRoleBinding.RoleRef.Name]
		if roleRef == "" {
			role, err := t.k8sClient.RbacV1().Roles(oldRoleBinding.Namespace).Get(oldRoleBinding.RoleRef.Name, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					klog.Warningf("invalid role binding found: %s.%s", oldRoleBinding.Namespace, oldRoleBinding.Name)
					continue
				}
				klog.Error(err)
				return err
			}
			if role.Annotations["kubesphere.io/creator"] == "" {
				continue
			}
			var newRole rbacv1.Role
			for {
				newRole, err = t.newRole(role)
				if err == nil {
					break
				} else if !errors.IsNotFound(err) {
					klog.Error(err)
					return err
				}
				time.Sleep(time.Second * 3)
			}
			newRoles = append(newRoles, newRole)
			roleRef = newRole.Name
		}
		for _, subject := range oldRoleBinding.Subjects {
			if subject.Kind == "User" {
				roleBinding := rbacv1.RoleBinding{
					TypeMeta: metav1.TypeMeta{
						Kind:       "RoleBinding",
						APIVersion: "rbac.authorization.k8s.io/v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("%s-%s", subject.Name, roleRef),
						Namespace: oldRoleBinding.Namespace,
						Labels: map[string]string{
							"iam.kubesphere.io/user-ref": subject.Name,
						},
					},
					Subjects: []rbacv1.Subject{{Name: subject.Name, Kind: "User", APIGroup: "rbac.authorization.k8s.io"}},
					RoleRef: rbacv1.RoleRef{
						APIGroup: "rbac.authorization.k8s.io",
						Kind:     "Role",
						Name:     roleRef,
					},
				}
				roleBindings = append(roleBindings, roleBinding)
			}
		}
	}

	for _, role := range newRoles {
		outputData, _ := json.Marshal(role)
		klog.Infof("migrate role: namespace:%s, %s: %s", role.Namespace, role.Name, string(outputData))
		if _, err := t.k8sClient.RbacV1().Roles(role.Namespace).Update(&role); err != nil {
			klog.Error(err)
			return err
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

func (t *roleBindingMigrateTask) newRole(oldRole *rbacv1.Role) (rbacv1.Role, error) {
	aggregationRoles := make([]string, 0)
	policyRules := make([]rbacv1.PolicyRule, 0)
	for role, policyRule := range customRoleMapping {
		if rbac.RulesMatchesRequired(oldRole.Rules, policyRule) {
			roleTemplate, err := t.k8sClient.RbacV1().Roles(oldRole.Namespace).Get(role, metav1.GetOptions{})
			if err != nil {
				klog.Error(err)
				return rbacv1.Role{}, err
			}
			policyRules = append(policyRules, roleTemplate.Rules...)
			aggregationRoles = append(aggregationRoles, role)
		}
	}
	roles, _ := json.Marshal(aggregationRoles)
	return rbacv1.Role{
		TypeMeta: metav1.TypeMeta{
			Kind:       "GlobalRole",
			APIVersion: "iam.kubesphere.io/v1alpha2",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      oldRole.Name,
			Namespace: oldRole.Namespace,
			Annotations: map[string]string{
				"iam.kubesphere.io/aggregation-roles": string(roles),
				"kubesphere.io/creator":               oldRole.Annotations["kubesphere.io/creator"],
				"kubesphere.io/description":           oldRole.Annotations["kubesphere.io/description"],
			},
			ResourceVersion: oldRole.ResourceVersion,
		},
		Rules: policyRules,
	}, nil
}

var customRoleMapping = map[string][]rbacv1.PolicyRule{
	"role-template-view-app-workloads": {
		{
			Verbs:     []string{"get", "list"},
			APIGroups: []string{"apps"},
			Resources: []string{"deployments"},
		},
		{
			Verbs:     []string{"get", "list"},
			APIGroups: []string{"apps"},
			Resources: []string{"statefulsets"},
		},
		{
			Verbs:     []string{"get", "list"},
			APIGroups: []string{"apps"},
			Resources: []string{"daemonsets"},
		},
		{
			Verbs:     []string{"get", "list"},
			APIGroups: []string{"batch"},
			Resources: []string{"jobs"},
		},
		{
			Verbs:     []string{"get", "list"},
			APIGroups: []string{"batch"},
			Resources: []string{"cronjobs"},
		},
		{
			Verbs:     []string{"get", "list"},
			APIGroups: []string{""},
			Resources: []string{"pods"},
		},
		{
			Verbs:     []string{"get", "list"},
			APIGroups: []string{""},
			Resources: []string{"services"},
		},
		{
			Verbs:     []string{"get", "list"},
			APIGroups: []string{""},
			Resources: []string{"secrets"},
		},
		{
			Verbs:     []string{"get", "list"},
			APIGroups: []string{""},
			Resources: []string{"configmaps"},
		},
		{
			Verbs:     []string{"get", "list"},
			APIGroups: []string{""},
			Resources: []string{"persistentvolumeclaims"},
		},
	},
	"role-template-manage-app-workloads": {
		{
			Verbs:     []string{"delete"},
			APIGroups: []string{"apps"},
			Resources: []string{"deployments"},
		},
		{
			Verbs:     []string{"delete"},
			APIGroups: []string{"apps"},
			Resources: []string{"statefulsets"},
		},
		{
			Verbs:     []string{"delete"},
			APIGroups: []string{"apps"},
			Resources: []string{"daemonsets"},
		},
		{
			Verbs:     []string{"delete"},
			APIGroups: []string{"batch"},
			Resources: []string{"jobs"},
		},
		{
			Verbs:     []string{"delete"},
			APIGroups: []string{"batch"},
			Resources: []string{"cronjobs"},
		},
		{
			Verbs:     []string{"delete"},
			APIGroups: []string{""},
			Resources: []string{"pods"},
		},
		{
			Verbs:     []string{"delete"},
			APIGroups: []string{""},
			Resources: []string{"services"},
		},
		{
			Verbs:     []string{"delete"},
			APIGroups: []string{""},
			Resources: []string{"secrets"},
		},
		{
			Verbs:     []string{"delete"},
			APIGroups: []string{""},
			Resources: []string{"configmaps"},
		},
		{
			Verbs:     []string{"delete"},
			APIGroups: []string{""},
			Resources: []string{"persistentvolumeclaims"},
		},
	},
	"role-template-view-configmaps": {
		{
			Verbs:     []string{"get", "list"},
			APIGroups: []string{""},
			Resources: []string{"configmaps"},
		},
	},
	"role-template-manage-configmaps": {
		{
			Verbs:     []string{"delete"},
			APIGroups: []string{""},
			Resources: []string{"configmaps"},
		},
	},
	"role-template-view-secrets": {
		{
			Verbs:     []string{"get", "list"},
			APIGroups: []string{""},
			Resources: []string{"secrets"},
		},
	},
	"role-template-manage-secrets": {
		{
			Verbs:     []string{"delete"},
			APIGroups: []string{""},
			Resources: []string{"secrets"},
		},
	},
	"role-template-view-volumes": {
		{
			Verbs:     []string{"get", "list"},
			APIGroups: []string{""},
			Resources: []string{"persistentvolumeclaims"},
		},
	},
	"role-template-manage-volumes": {
		{
			Verbs:     []string{"delete"},
			APIGroups: []string{""},
			Resources: []string{"persistentvolumeclaims"},
		},
	},
	"role-template-view-members": {
		{
			Verbs:     []string{"get", "list"},
			APIGroups: []string{"rbac.authorization.k8s.io"},
			Resources: []string{"rolebindings"},
		},
	},
	"role-template-manage-members": {
		{
			Verbs:     []string{"delete"},
			APIGroups: []string{"rbac.authorization.k8s.io"},
			Resources: []string{"rolebindings"},
		},
	},
	"role-template-view-roles": {
		{
			Verbs:     []string{"get", "list"},
			APIGroups: []string{"rbac.authorization.k8s.io"},
			Resources: []string{"roles"},
		},
	},
	"role-template-manage-roles": {
		{
			Verbs:     []string{"delete"},
			APIGroups: []string{"rbac.authorization.k8s.io"},
			Resources: []string{"roles"},
		},
	},
	"role-template-manage-project-settings": {
		{
			Verbs:     []string{"delete"},
			APIGroups: []string{""},
			Resources: []string{"namespaces"},
		},
	},
}
