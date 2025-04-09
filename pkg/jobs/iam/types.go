package iam

import (
	"fmt"
	"golang.org/x/exp/slices"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	iamv1alpha2 "kubesphere.io/ks-upgrade/v3/api/iam/v1alpha2"
	tenantv1alpha1 "kubesphere.io/ks-upgrade/v3/api/tenant/v1alpha1"
	tenantv1alpha2 "kubesphere.io/ks-upgrade/v3/api/tenant/v1alpha2"
	iamv1beta1 "kubesphere.io/ks-upgrade/v4/api/iam/v1beta1"
	tenantv1beta1 "kubesphere.io/ks-upgrade/v4/api/tenant/v1beta1"
	"regexp"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ResourceUser                 = "users.iam.kubesphere.io"
	ResourceRoleBase             = "rolebases.iam.kubesphere.io"
	ResourceGlobalRole           = "globalroles.iam.kubesphere.io"
	ResourceGlobalRoleBinding    = "globalrolebindings.iam.kubesphere.io"
	ResourceWorkspaceRole        = "workspaceroles.iam.kubesphere.io"
	ResourceWorkspaceRoleBinding = "workspacerolebindings.iam.kubesphere.io"
	ResourceRole                 = "roles.rbac.authorization.kubernetes.io"
	ResourceRoleBinding          = "rolebindings.rbac.authorization.kubernetes.io"
	ResourceClusterRole          = "clusterroles.rbac.authorization.kubernetes.io"
	ResourceClusterRoleBinding   = "clusterrolebindings.rbac.authorization.kubernetes.io"
	ResourceLoginRecord          = "loginrecords.iam.kubesphere.io"
	ResourceGroup                = "groups.iam.kubesphere.io"
	ResourceGroupBinding         = "groupbindings.iam.kubesphere.io"
	ResourceWorkspace            = "workspaces.tenant.kubesphere.io"
	ResourceWorkspaceTemplate    = "workspacetemplates.tenant.kubesphere.io"

	KubeFedNamespace = "kube-federation-system"
)

var StoreOptions = []StoreOption{
	{
		Resource: ResourceUser,
		ListType: &iamv1alpha2.UserList{},
	},
	{
		Resource: ResourceRoleBase,
		ListType: &iamv1alpha2.RoleBaseList{},
	},
	{
		Resource: ResourceGlobalRole,
		ListType: &iamv1alpha2.GlobalRoleList{},
	},
	{
		Resource: ResourceGlobalRoleBinding,
		ListType: &iamv1alpha2.GlobalRoleBindingList{},
		FilterFunc: func(object client.Object) bool {
			binding := object.(*iamv1alpha2.GlobalRoleBinding)
			_, exist := binding.Labels["iam.kubesphere.io/user-ref"]
			return exist
		},
	},
	{
		Resource: ResourceWorkspaceRole,
		ListType: &iamv1alpha2.WorkspaceRoleList{},
	},
	{
		Resource: ResourceWorkspaceRoleBinding,
		ListType: &iamv1alpha2.WorkspaceRoleBindingList{},
	},
	{
		Resource: ResourceRole,
		ListType: &rbacv1.RoleList{},
		FilterFunc: func(object client.Object) bool {
			role := object.(*rbacv1.Role)
			_, exist := role.Annotations["kubesphere.io/creator"]
			if !exist {
				return false
			}
			builtinRole := []string{"admin", "viewer", "operator"}
			return !slices.Contains(builtinRole, role.GetName())
		},
	},
	{
		Resource: ResourceRoleBinding,
		ListType: &rbacv1.RoleBindingList{},
		FilterFunc: func(object client.Object) bool {
			binding := object.(*rbacv1.RoleBinding)
			_, exist := binding.Labels["iam.kubesphere.io/user-ref"]
			return exist
		},
	},
	{
		Resource: ResourceClusterRoleBinding,
		ListType: &rbacv1.ClusterRoleBindingList{},
		FilterFunc: func(list client.Object) bool {
			binding := list.(*rbacv1.ClusterRoleBinding)
			_, exist := binding.Labels["iam.kubesphere.io/user-ref"]
			return exist
		},
	},
	{
		Resource: ResourceLoginRecord,
		ListType: &iamv1alpha2.LoginRecordList{},
	},
	{
		Resource: ResourceGroup,
		ListType: &iamv1alpha2.GroupList{},
	},
	{
		Resource: ResourceGroupBinding,
		ListType: &iamv1alpha2.GroupBindingList{},
	},
	{
		Resource: ResourceWorkspace,
		ListType: &tenantv1alpha1.WorkspaceList{},
	},
	{
		Resource: ResourceWorkspaceTemplate,
		ListType: &tenantv1alpha2.WorkspaceTemplateList{},
	},
}

type UpgradeRbacFunc UpgradeFunc

type UpgradeFunc func(old client.Object) client.Object

var RoleUpgradeFunc = func(old client.Object) client.Object {
	oldRole := old.(*rbacv1.Role)
	// TODO fill with AggregationRoleTemplates
	annotation := oldRole.Annotations
	delete(annotation, "iam.kubesphere.io/aggregation-roles")
	return &iamv1beta1.Role{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:        oldRole.Name,
			Namespace:   oldRole.Namespace,
			Labels:      oldRole.Labels,
			Annotations: annotation,
		},
		AggregationRoleTemplates: nil,
		Rules:                    oldRole.Rules,
	}
}

var RoleBindingUpgradeFunc = func(old client.Object) client.Object {
	oldBinding := old.(*rbacv1.RoleBinding)
	newSubjects := []rbacv1.Subject{}
	username := ""
	for _, subject := range oldBinding.Subjects {
		if subject.Kind == "User" {
			username = subject.Name
			newSubject := rbacv1.Subject{
				Kind:      subject.Kind,
				APIGroup:  "iam.kubesphere.io",
				Name:      subject.Name,
				Namespace: subject.Namespace,
			}
			newSubjects = append(newSubjects, newSubject)
		}
	}

	if oldBinding.Labels == nil {
		oldBinding.Labels = make(map[string]string)
	}
	oldBinding.Labels["iam.kubesphere.io/role-ref"] = oldBinding.RoleRef.Name
	oldBinding.Labels["iam.kubesphere.io/user-ref"] = username

	return &iamv1beta1.RoleBinding{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:        oldBinding.Name,
			Namespace:   oldBinding.Namespace,
			Annotations: oldBinding.Annotations,
			Labels:      oldBinding.Labels,
		},
		Subjects: newSubjects,
		RoleRef: rbacv1.RoleRef{
			APIGroup: "iam.kubesphere.io",
			Kind:     oldBinding.RoleRef.Kind,
			Name:     oldBinding.RoleRef.Name,
		},
	}
}

var ClusterRoleUpgradeFunc = func(old client.Object) client.Object {
	oldRole := old.(*rbacv1.ClusterRole)
	annotation := oldRole.Annotations
	delete(annotation, "iam.kubesphere.io/aggregation-roles")
	// TODO fill with AggregationRoleTemplates
	return &iamv1beta1.ClusterRole{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:        oldRole.Name,
			Namespace:   oldRole.Namespace,
			Annotations: annotation,
			Labels:      oldRole.Labels,
		},
		AggregationRoleTemplates: nil,
		Rules:                    oldRole.Rules,
	}
}

var ClusterRoleBindingUpgradeFunc = func(old client.Object) client.Object {
	oldBinding := old.(*rbacv1.ClusterRoleBinding)
	newSubjects := []rbacv1.Subject{}
	username := ""
	for _, subject := range oldBinding.Subjects {
		if subject.Kind == "User" {
			username = subject.Name
			newSubject := rbacv1.Subject{
				Kind:      subject.Kind,
				APIGroup:  "iam.kubesphere.io",
				Name:      subject.Name,
				Namespace: subject.Namespace,
			}
			newSubjects = append(newSubjects, newSubject)
		}
	}

	if oldBinding.Labels == nil {
		oldBinding.Labels = make(map[string]string)
	}
	oldBinding.Labels["iam.kubesphere.io/role-ref"] = oldBinding.RoleRef.Name
	oldBinding.Labels["iam.kubesphere.io/user-ref"] = username

	return &iamv1beta1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:        oldBinding.Name,
			Annotations: oldBinding.Annotations,
			Labels:      oldBinding.Labels,
		},
		Subjects: newSubjects,
		RoleRef: rbacv1.RoleRef{
			APIGroup: "iam.kubesphere.io",
			Kind:     oldBinding.RoleRef.Kind,
			Name:     oldBinding.RoleRef.Name,
		},
	}
}

type DefaultCustomRoleOption struct {
	Resource    string
	ListType    client.ObjectList
	FilterFunc  FilterFunc
	DefaultFunc UpgradeFunc
}

var DefaultWorkspaceCustomRoleOption = DefaultCustomRoleOption{
	Resource: ResourceWorkspaceRole,
	ListType: &iamv1beta1.WorkspaceRoleList{},
	FilterFunc: func(object client.Object) bool {
		role := object.(*iamv1beta1.WorkspaceRole)
		workspaceName := ""
		if role.Labels != nil {
			workspaceName = role.Labels["kubesphere.io/workspace"]
		}
		pattern := fmt.Sprintf(`^%s-(admin|regular|self-provisioner|viewer)$`, workspaceName)
		regex := regexp.MustCompile(pattern)
		return !regex.MatchString(role.Name)
	},
	DefaultFunc: func(old client.Object) client.Object {
		role := old.(*iamv1beta1.WorkspaceRole)
		deepCopy := role.DeepCopy()
		deepCopy.Rules = []rbacv1.PolicyRule{}
		return deepCopy
	},
}

var DefaultNamespaceCustomRoleOption = DefaultCustomRoleOption{
	Resource: ResourceRole,
	ListType: &iamv1beta1.RoleList{},
	FilterFunc: func(object client.Object) bool {
		_, exist := object.GetAnnotations()["kubesphere.io/creator"]
		if !exist {
			return false
		}
		builtinRole := []string{"admin", "viewer", "operator"}
		return !slices.Contains(builtinRole, object.GetName())
	},
	DefaultFunc: func(old client.Object) client.Object {
		role := old.(*iamv1beta1.Role)
		deepCopy := role.DeepCopy()
		deepCopy.Rules = []rbacv1.PolicyRule{}
		return deepCopy
	},
}

var DefaultGlobalCustomRoleOption = DefaultCustomRoleOption{
	Resource: ResourceGlobalRole,
	ListType: &iamv1beta1.GlobalRoleList{},
	FilterFunc: func(object client.Object) bool {
		builtinRole := []string{"anonymous", "authenticated", "platform-admin", "platform-regular", "platform-self-provisioner", "pre-registration", "ks-console"}
		return !slices.Contains(builtinRole, object.GetName())
	},
	DefaultFunc: func(old client.Object) client.Object {
		role := old.(*iamv1beta1.GlobalRole)
		deepCopy := role.DeepCopy()
		deepCopy.Rules = []rbacv1.PolicyRule{}
		return deepCopy
	},
}

var UpgradeCRDFuncMap = map[string]UpgradeFunc{
	ResourceUser:                 UpgradeUserFunc,
	ResourceWorkspaceRole:        UpgradeWorkspaceRoleFunc,
	ResourceWorkspaceRoleBinding: UpgradeWorkspaceRoleBindingFunc,
	ResourceGlobalRole:           UpgradeGlobalRoleFunc,
	ResourceGlobalRoleBinding:    UpgradeGlobalRoleBindingFunc,
	ResourceLoginRecord:          UpgradeLoginRecordFunc,
	ResourceWorkspace:            UpgradeWorkspaceFunc,
	ResourceWorkspaceTemplate:    UpgradeWorkspaceTemplateFunc,

	ResourceRole:               RoleUpgradeFunc,
	ResourceRoleBinding:        RoleBindingUpgradeFunc,
	ResourceClusterRole:        ClusterRoleUpgradeFunc,
	ResourceClusterRoleBinding: ClusterRoleBindingUpgradeFunc,
}

var UpgradeUserFunc = UpgradeFunc(func(old client.Object) client.Object {
	user := old.(*iamv1alpha2.User)
	return &iamv1beta1.User{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:            user.Name,
			Annotations:     user.Annotations,
			Labels:          user.Labels,
			Finalizers:      user.Finalizers,
			ResourceVersion: user.ResourceVersion,
		},
		Spec: iamv1beta1.UserSpec{
			Email:             user.Spec.Email,
			Lang:              user.Spec.Lang,
			Description:       user.Spec.Description,
			DisplayName:       user.Spec.DisplayName,
			Groups:            user.Spec.Groups,
			EncryptedPassword: user.Spec.EncryptedPassword,
		},
		Status: iamv1beta1.UserStatus{
			State:              iamv1beta1.UserState(user.Status.State),
			Reason:             user.Status.Reason,
			LastTransitionTime: user.Status.LastTransitionTime,
			LastLoginTime:      user.Status.LastLoginTime,
		},
	}
})

var UpgradeWorkspaceRoleFunc = UpgradeFunc(func(old client.Object) client.Object {
	object := old.(*iamv1alpha2.WorkspaceRole)
	return &iamv1beta1.WorkspaceRole{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:            object.Name,
			Annotations:     object.Annotations,
			Labels:          object.Labels,
			Finalizers:      object.Finalizers,
			ResourceVersion: object.ResourceVersion,
		},
		AggregationRoleTemplates: nil,
		Rules:                    object.Rules,
	}
})

var UpgradeWorkspaceRoleBindingFunc = UpgradeFunc(func(old client.Object) client.Object {
	oldBinding := old.(*iamv1alpha2.WorkspaceRoleBinding)
	username := ""
	newSubjects := []rbacv1.Subject{}
	for _, subject := range oldBinding.Subjects {
		if subject.Kind == "User" {
			username = subject.Name

			newSubject := rbacv1.Subject{
				Kind:      subject.Kind,
				APIGroup:  "iam.kubesphere.io",
				Name:      subject.Name,
				Namespace: subject.Namespace,
			}
			newSubjects = append(newSubjects, newSubject)
		}
	}
	if oldBinding.Labels == nil {
		oldBinding.Labels = make(map[string]string)
	}
	if username != "" {
		oldBinding.Labels["iam.kubesphere.io/role-ref"] = oldBinding.RoleRef.Name
		oldBinding.Labels["iam.kubesphere.io/user-ref"] = username
	}
	return &iamv1beta1.WorkspaceRoleBinding{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:            oldBinding.Name,
			Annotations:     oldBinding.Annotations,
			Labels:          oldBinding.Labels,
			Finalizers:      oldBinding.Finalizers,
			ResourceVersion: oldBinding.ResourceVersion,
		},
		Subjects: newSubjects,
		RoleRef: rbacv1.RoleRef{
			APIGroup: "iam.kubesphere.io",
			Kind:     oldBinding.RoleRef.Kind,
			Name:     oldBinding.RoleRef.Name,
		},
	}
})

var UpgradeGlobalRoleFunc = UpgradeFunc(func(old client.Object) client.Object {
	object := old.(*iamv1alpha2.GlobalRole)
	return &iamv1beta1.GlobalRole{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:            object.Name,
			Annotations:     object.Annotations,
			Labels:          object.Labels,
			Finalizers:      object.Finalizers,
			ResourceVersion: object.ResourceVersion,
		},
		AggregationRoleTemplates: nil,
		Rules:                    object.Rules,
	}
})

var UpgradeGlobalRoleBindingFunc = UpgradeFunc(func(old client.Object) client.Object {
	oldBinding := old.(*iamv1alpha2.GlobalRoleBinding)
	username := ""
	newSubjects := []rbacv1.Subject{}
	for _, subject := range oldBinding.Subjects {
		if subject.Kind == "User" {
			username = subject.Name
			newSubject := rbacv1.Subject{
				Kind:      subject.Kind,
				APIGroup:  "iam.kubesphere.io",
				Name:      subject.Name,
				Namespace: subject.Namespace,
			}
			newSubjects = append(newSubjects, newSubject)
		}
	}
	if oldBinding.Labels == nil {
		oldBinding.Labels = make(map[string]string)
	}
	if username != "" {
		oldBinding.Labels["iam.kubesphere.io/role-ref"] = oldBinding.RoleRef.Name
		oldBinding.Labels["iam.kubesphere.io/user-ref"] = username
	}
	return &iamv1beta1.GlobalRoleBinding{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:            oldBinding.Name,
			Annotations:     oldBinding.Annotations,
			Labels:          oldBinding.Labels,
			Finalizers:      oldBinding.Finalizers,
			ResourceVersion: oldBinding.ResourceVersion,
		},
		Subjects: newSubjects,
		RoleRef: rbacv1.RoleRef{
			APIGroup: "iam.kubesphere.io",
			Kind:     oldBinding.RoleRef.Kind,
			Name:     oldBinding.RoleRef.Name,
		},
	}
})

var UpgradeLoginRecordFunc = UpgradeFunc(func(old client.Object) client.Object {
	object := old.(*iamv1alpha2.LoginRecord)
	return &iamv1beta1.LoginRecord{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:            object.Name,
			Annotations:     object.Annotations,
			Labels:          object.Labels,
			Finalizers:      object.Finalizers,
			ResourceVersion: object.ResourceVersion,
		},
		Spec: iamv1beta1.LoginRecordSpec{
			Type:      iamv1beta1.LoginType(object.Spec.Type),
			Provider:  object.Spec.Provider,
			SourceIP:  object.Spec.SourceIP,
			UserAgent: object.Spec.UserAgent,
			Success:   object.Spec.Success,
			Reason:    object.Spec.Reason,
		},
	}
})

var UpgradeWorkspaceFunc = UpgradeFunc(func(old client.Object) client.Object {
	object := old.(*tenantv1alpha1.Workspace)
	return &tenantv1beta1.Workspace{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:            object.Name,
			Annotations:     object.Annotations,
			Labels:          object.Labels,
			Finalizers:      object.Finalizers,
			ResourceVersion: object.ResourceVersion,
		},
		Spec: tenantv1beta1.WorkspaceSpec{
			Manager: object.Spec.Manager,
		},
		Status: tenantv1beta1.WorkspaceStatus{},
	}
})

var UpgradeWorkspaceTemplateFunc = UpgradeFunc(func(old client.Object) client.Object {
	object := old.(*tenantv1alpha2.WorkspaceTemplate)
	var clusters []tenantv1beta1.GenericClusterReference
	for _, c := range object.Spec.Placement.Clusters {
		clusters = append(clusters, tenantv1beta1.GenericClusterReference{Name: c.Name})
	}
	return &tenantv1beta1.WorkspaceTemplate{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:            object.Name,
			Annotations:     object.Annotations,
			Labels:          object.Labels,
			Finalizers:      object.Finalizers,
			ResourceVersion: object.ResourceVersion,
		},
		Spec: tenantv1beta1.WorkspaceTemplateSpec{
			Template: tenantv1beta1.Template{
				ObjectMeta: tenantv1beta1.ObjectMeta{
					Labels:      object.Spec.Template.ObjectMeta.Labels,
					Annotations: object.Spec.Template.ObjectMeta.Annotations,
				},
				Spec: tenantv1beta1.WorkspaceSpec{
					Manager: object.Spec.Template.Spec.Manager,
				},
			},
			Placement: tenantv1beta1.GenericPlacement{
				Clusters:        clusters,
				ClusterSelector: object.Spec.Placement.ClusterSelector,
			},
		},
	}
})
