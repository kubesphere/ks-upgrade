package role

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"

	"kubesphere.io/ks-upgrade/pkg/globalrolebindings"
	"kubesphere.io/ks-upgrade/pkg/task"
)

const (
	roleTypeGlobalRole    = "globalroles"
	roleTypeWorkspaceRole = "workspaceroles"
	roleTypeRole          = "roles"

	iamPath  = "/apis/iam.kubesphere.io/v1alpha2"
	rbacPath = "/apis/rbac.authorization.k8s.io/v1"
)

var deleteGlobalRoleList = []string{
	"users-manager",
	"workspaces-manager",
}

var deprecatedRoleTemplateList = map[string][]string{
	roleTypeGlobalRole: {
		"role-template-manage-users",
		"role-template-manage-roles",
		"role-template-manage-workspaces",
	},

	roleTypeWorkspaceRole: {
		"role-template-manage-members",
		"role-template-manage-roles",
		"role-template-manage-groups",
	},

	roleTypeRole: {
		"role-template-manage-members",
		"role-template-manage-roles",
	},
}

var builtinRolesList = map[string][]string{
	roleTypeGlobalRole: {
		"platform-admin", "platform-regular", "platform-self-provisioner", "anonymous", "authenticated", "pre-registration",
	},
	roleTypeWorkspaceRole: {
		"admin", "regular", "self-provisioner", "viewer",
	},

	roleTypeRole: {
		"admin", "operator", "viewer",
	},
}

type roleMigrateTask struct {
	clientSet  *kubernetes.Clientset
	reCreators []ReCreator
}

func NewRoleMigrateTask(k8sClient kubernetes.Interface) task.UpgradeTask {
	clientset := k8sClient.(*kubernetes.Clientset)
	r := &roleMigrateTask{clientSet: clientset, reCreators: make([]ReCreator, 0)}

	r.reCreators = append(r.reCreators,
		newGlobalCustomRoleReCreator(k8sClient, deprecatedRoleTemplateList[roleTypeGlobalRole], builtinRolesList[roleTypeGlobalRole]),
		newWorkspaceCustomRoleReCreator(k8sClient, deprecatedRoleTemplateList[roleTypeWorkspaceRole], builtinRolesList[roleTypeWorkspaceRole]),
		newCustomRoleReCreator(k8sClient, deprecatedRoleTemplateList[roleTypeRole], builtinRolesList[roleTypeRole]),
	)

	return r
}

func (t *roleMigrateTask) Run() error {
	// delete the deprecated global roles
	for _, globalRole := range deleteGlobalRoleList {
		err := t.deleteGlobalRole(globalRole)
		if err != nil {
			klog.Error(err)
			return err
		}
	}

	// recreate the roles is including deprecated role templates
	for _, reCreator := range t.reCreators {
		if err := reCreator.Recreate(); err != nil {
			klog.Error(err)
			return err
		}
	}

	return nil
}

func (t *roleMigrateTask) deleteGlobalRole(name string) error {
	path := fmt.Sprintf("%s/%s", iamPath, roleTypeGlobalRole)

	err := deleteRole(t.clientSet, path, name)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Infof(fmt.Sprintf("Global Role %s is not existing, skipping it.", name))
		} else {
			return err
		}
	}
	return nil
}

type ReCreator interface {
	Recreate() error
}

type globalCustomRoleReCreator struct {
	client                  *kubernetes.Clientset
	deprecatedRoleTemplates []string
	builtinRoles            []string
}

func newGlobalCustomRoleReCreator(client kubernetes.Interface, deprecatedRoleTemplates []string, builtinRole []string) ReCreator {
	return &globalCustomRoleReCreator{
		client:                  client.(*kubernetes.Clientset),
		deprecatedRoleTemplates: deprecatedRoleTemplates,
		builtinRoles:            builtinRole,
	}
}

func (g *globalCustomRoleReCreator) Recreate() error {
	path := fmt.Sprintf("%s/%s", iamPath, roleTypeGlobalRole)

	globalRoleList := &GlobalRoleList{}
	err := listRole(g.client, path, "", globalRoleList)
	if err != nil {
		return err
	}
	for _, globalrole := range globalRoleList.Items {
		if isValidCustomRole(globalrole.ObjectMeta, g.builtinRoles) {
			oldAggregateRoles, err := getAggregationRoles(globalrole.ObjectMeta)
			if err != nil {
				klog.Warning("get aggregation roles of %s failed, %s", globalrole.Name, err.Error())
				continue
			}

			trimmed, aggregateRoles := trimRoleTemplates(g.deprecatedRoleTemplates, oldAggregateRoles)
			if trimmed {
				marshal, err := json.Marshal(aggregateRoles)
				if err != nil {
					return err
				}

				// Creating a new custom role excluding the deprecated role templates.
				newGlobalRole := &globalrolebindings.GlobalRole{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "iam.kubesphere.io/v1alpha2",
						Kind:       "GlobalRole",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: globalrole.Name,
						Annotations: map[string]string{
							"iam.kubesphere.io/aggregation-roles": string(marshal),
							"kubesphere.io/creator":               globalrole.Annotations["kubesphere.io/creator"],
						},
					},
					Rules: make([]v1.PolicyRule, 0),
				}

				for _, a := range aggregateRoles {
					roleTemplate := &globalrolebindings.GlobalRole{}
					err := listRole(g.client, path, a, roleTemplate)
					if err != nil {
						if errors.IsNotFound(err) {
							klog.Warning(err)
						} else {
							return err
						}
					}
					newGlobalRole.Rules = append(newGlobalRole.Rules, roleTemplate.Rules...)
				}

				klog.Infof("recreate global role %s with aggregating role: %s", newGlobalRole.Name, string(marshal))
				// delete the old custom role
				if err := deleteRole(g.client, path, globalrole.Name); err != nil {
					return err
				}

				if err := createRole(g.client, path, newGlobalRole.Name, newGlobalRole); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

type workspaceCustomRoleReCreator struct {
	client                  *kubernetes.Clientset
	deprecatedRoleTemplates []string
	builtinRoles            []string
}

func newWorkspaceCustomRoleReCreator(client kubernetes.Interface, deprecatedRoleTemplates, builtinRoles []string) ReCreator {
	return &workspaceCustomRoleReCreator{
		client:                  client.(*kubernetes.Clientset),
		deprecatedRoleTemplates: deprecatedRoleTemplates,
		builtinRoles:            builtinRoles,
	}
}

func (w *workspaceCustomRoleReCreator) Recreate() error {
	path := fmt.Sprintf("%s/%s", iamPath, roleTypeWorkspaceRole)

	workspaceRoleList := &WorkspaceRoleList{}
	err := listRole(w.client, path, "", workspaceRoleList)
	if err != nil {
		return err
	}
	for _, workspaceRole := range workspaceRoleList.Items {
		// Just check the custom role

		if workspaceRole.Labels["iam.kubesphere.io/role-template"] == "" &&
			!suffixInSliceString(workspaceRole.Name, w.builtinRoles) &&
			workspaceRole.Annotations["iam.kubesphere.io/aggregation-roles"] != "" {

			oldAggregateRoles, err := getAggregationRoles(workspaceRole.ObjectMeta)
			if err != nil {
				klog.Warning("get aggregation roles of %s failed, %s", workspaceRole.Name, err.Error())
				continue
			}

			trimmed, aggregateRoles := trimRoleTemplates(w.deprecatedRoleTemplates, oldAggregateRoles)
			if trimmed {
				marshal, err := json.Marshal(aggregateRoles)
				if err != nil {
					return err
				}

				// Creating a new custom role excluding the deprecated role templates.
				newWorkspaceRole := &WorkspaceRole{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "iam.kubesphere.io/v1alpha2",
						Kind:       "WorkspaceRole",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: workspaceRole.Name,
						Labels: map[string]string{
							"kubesphere.io/workspace": workspaceRole.Labels["kubesphere.io/workspace"],
						},
						Annotations: map[string]string{
							"iam.kubesphere.io/aggregation-roles": string(marshal),
							"kubesphere.io/creator":               workspaceRole.Annotations["kubesphere.io/creator"],
						},
					},
					Rules: make([]v1.PolicyRule, 0),
				}

				for _, a := range aggregateRoles {
					roleTemplate := &WorkspaceRole{}
					err := listRole(w.client, path, a, roleTemplate)
					if err != nil {
						if errors.IsNotFound(err) {
							klog.Warning(err)
						} else {
							return err
						}
					}
					newWorkspaceRole.Rules = append(newWorkspaceRole.Rules, roleTemplate.Rules...)
				}

				klog.Infof("recreate workspace role %s with aggregating role: %s", newWorkspaceRole.Name, string(marshal))
				// delete the old custom role
				if err := deleteRole(w.client, path, workspaceRole.Name); err != nil {
					return err
				}
				if err := createRole(w.client, path, newWorkspaceRole.Name, newWorkspaceRole); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

type customRoleReCreator struct {
	client                  *kubernetes.Clientset
	deprecatedRoleTemplates []string
	builtinRoles            []string
}

func newCustomRoleReCreator(client kubernetes.Interface, deprecatedRoleTemplates, builtinRoles []string) ReCreator {
	return &customRoleReCreator{
		client:                  client.(*kubernetes.Clientset),
		deprecatedRoleTemplates: deprecatedRoleTemplates,
		builtinRoles:            builtinRoles,
	}
}

func (w *customRoleReCreator) Recreate() error {
	path := fmt.Sprintf("%s/%s", rbacPath, roleTypeRole)

	roleList := &v1.RoleList{}
	err := listRole(w.client, path, "", roleList)
	if err != nil {
		return err
	}
	for _, role := range roleList.Items {
		// Confirm the role isn`t builtinRole or role template
		if isValidCustomRole(role.ObjectMeta, w.builtinRoles) {

			oldAggregateRoles, err := getAggregationRoles(role.ObjectMeta)
			if err != nil {
				klog.Warning("get aggregation roles of %s failed, %s", role.Name, err.Error())
				continue
			}

			pathWithNs := fmt.Sprintf("%s/namespaces/%s/%s", rbacPath, role.Namespace, roleTypeRole)
			hasTrimmed, aggregateRoles := trimRoleTemplates(w.deprecatedRoleTemplates, oldAggregateRoles)
			if hasTrimmed {
				marshal, err := json.Marshal(aggregateRoles)
				if err != nil {
					return err
				}

				// Delete old role and creating a new role with the same name as old one but excluding the deprecated role templates.
				newRole := &v1.Role{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "rbac.authorization.k8s.io/v1",
						Kind:       "Role",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      role.Name,
						Namespace: role.Namespace,
						Annotations: map[string]string{
							"iam.kubesphere.io/aggregation-roles": string(marshal),
							"kubesphere.io/creator":               role.Annotations["kubesphere.io/creator"],
						},
					},
					Rules: make([]v1.PolicyRule, 0),
				}

				for _, a := range aggregateRoles {
					roleTemplate := &v1.Role{}
					err := listRole(w.client, pathWithNs, a, roleTemplate)
					if err != nil {
						if errors.IsNotFound(err) {
							klog.Warning(err)
						} else {
							return err
						}
					}
					newRole.Rules = append(newRole.Rules, roleTemplate.Rules...)
				}

				klog.Infof("recreate role %s with aggregating role: %s", newRole.Name, string(marshal))
				if err := deleteRole(w.client, pathWithNs, role.Name); err != nil {
					return err
				}
				if err := createRole(w.client, pathWithNs, newRole.Name, newRole); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func deleteRole(clientSet *kubernetes.Clientset, path, name string) error {
	_, err := clientSet.RESTClient().Delete().AbsPath(fmt.Sprintf("%s/%s", path, name)).DoRaw()
	if err != nil {
		return err
	}

	klog.Infof("deleted role %s", name)
	return nil
}

func createRole(clientSet *kubernetes.Clientset, path, name string, body interface{}) error {
	marshal, err := json.Marshal(body)
	if err != nil {
		return err
	}
	_, err = clientSet.RESTClient().Post().AbsPath(path).Body(marshal).DoRaw()
	if err != nil {
		return err
	}
	klog.Infof("created role %s", name)

	return nil
}

func listRole(clientSet *kubernetes.Clientset, path, name string, output interface{}) error {
	raw, err := clientSet.RESTClient().Get().AbsPath(fmt.Sprintf("%s/%s", path, name)).DoRaw()
	if err != nil {
		return err
	}
	err = json.Unmarshal(raw, output)

	if err != nil {
		return err
	}

	return nil
}

func inSliceString(e string, slice []string) bool {
	for _, s := range slice {
		if s == e {
			return true
		}
	}
	return false
}

func suffixInSliceString(e string, slice []string) bool {
	for _, s := range slice {
		if strings.HasSuffix(e, s) {
			return true
		}
	}
	return false
}

func trimRoleTemplates(deprecatedRoleTemplates, target []string) (trimmed bool, aggregateRoles []string) {
	newAggregateRoles := make([]string, 0)

	for _, t := range target {
		if !inSliceString(t, deprecatedRoleTemplates) {
			newAggregateRoles = append(newAggregateRoles, t)
		}
	}

	if len(newAggregateRoles) != len(target) {
		return true, newAggregateRoles
	}

	return false, nil
}

// Just confirm the role isn`t builtinRole or role template etc.
func isValidCustomRole(meta metav1.ObjectMeta, builtinRoles []string) bool {
	if meta.Labels["iam.kubesphere.io/role-template"] == "" &&
		!inSliceString(meta.Name, builtinRoles) &&
		meta.Annotations["iam.kubesphere.io/aggregation-roles"] != "" {
		return true
	}
	return false
}

func getAggregationRoles(meta metav1.ObjectMeta) ([]string, error) {
	roles := make([]string, 0)
	err := json.Unmarshal([]byte(meta.Annotations["iam.kubesphere.io/aggregation-roles"]), roles)
	if err != nil {
		return nil, err
	}
	return roles, nil
}
