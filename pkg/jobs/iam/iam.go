package iam

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	errors2 "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/watch"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/klog/v2"
	"kubesphere.io/ks-upgrade/pkg/constants"
	"kubesphere.io/ks-upgrade/pkg/executor"
	iamhelper "kubesphere.io/ks-upgrade/pkg/jobs/iam/helper"
	"kubesphere.io/ks-upgrade/pkg/model"
	"kubesphere.io/ks-upgrade/pkg/model/core"
	"kubesphere.io/ks-upgrade/pkg/model/helper"
	"kubesphere.io/ks-upgrade/pkg/storage"
	"kubesphere.io/ks-upgrade/pkg/store"
	iamv1alpha2 "kubesphere.io/ks-upgrade/v3/api/iam/v1alpha2"
	tenantv1alpha2 "kubesphere.io/ks-upgrade/v3/api/tenant/v1alpha2"
	corev1alpha1 "kubesphere.io/ks-upgrade/v4/api/core/v1alpha1"
	iamv1beta1 "kubesphere.io/ks-upgrade/v4/api/iam/v1beta1"
	tenantv1beta1 "kubesphere.io/ks-upgrade/v4/api/tenant/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"
)

func init() {
	runtime.Must(executor.Register(&factory{}))
}

const jobName = "iam"

var _ executor.UpgradeJob = &upgradeJob{}
var _ executor.PrepareUpgrade = &upgradeJob{}
var _ executor.InjectClientV3 = &upgradeJob{}
var _ executor.InjectClientV4 = &upgradeJob{}
var _ executor.InjectResourceStore = &upgradeJob{}

var resources = []string{
	ResourceUser,
	ResourceRoleBase,
	ResourceWorkspaceRole,
	ResourceWorkspaceRoleBinding,
	ResourceGlobalRole,
	ResourceGlobalRoleBinding,
	ResourceLoginRecord,
	ResourceGroup,
	ResourceGroupBinding,
	ResourceWorkspace,
	ResourceWorkspaceTemplate,
}

type factory struct {
}

func (f *factory) Name() string {
	return jobName
}

func (f *factory) Create(options executor.DynamicOptions, extensionRef *model.ExtensionRef) (executor.UpgradeJob, error) {
	return &upgradeJob{
		extensionRef: extensionRef,
		options:      options,
		logger:       klog.NewKlogr().WithName("IAM-Job"),
	}, nil
}

type upgradeJob struct {
	clientV3      client.Client
	clientV4      client.Client
	resourceStore store.ResourceStore
	coreHelper    core.Helper
	extensionRef  *model.ExtensionRef
	options       executor.DynamicOptions
	objectHelper  iamhelper.ObjectHelper
	logger        logr.Logger
}

type FilterFunc func(object client.Object) bool

func (j *upgradeJob) InjectClientV3(client client.Client) {
	j.clientV3 = client
}

func (j *upgradeJob) InjectClientV4(client client.Client) {
	j.clientV4 = client
}

func (j *upgradeJob) InjectModelHelperFactory(factory helper.ModelHelperFactory) {
	j.coreHelper = factory.CoreHelper()
}

func (j *upgradeJob) InjectResourceStore(store store.ResourceStore) {
	j.resourceStore = store
}

type StoreOption struct {
	Resource   string
	ListType   client.ObjectList
	FilterFunc FilterFunc
}

func (j *upgradeJob) PreUpgrade(ctx context.Context) error {
	status, err := j.GetUpgradeStatus()
	if err != nil {
		return err
	}
	if status.Status == UpgradeStatusSuccess {
		j.logger.Info("iam has been upgraded successfully, skip job")
		return nil
	}

	j.objectHelper, err = iamhelper.NewObjectHelper(ctx, j.clientV3, j.clientV4, resources)
	if err != nil {
		return err
	}

	j.objectHelper.Register(
		ResourceRole,
		iamhelper.NewReference("v1", &rbacv1.Role{}, &rbacv1.RoleList{}), nil)
	j.objectHelper.Register(
		ResourceRoleBinding,
		iamhelper.NewReference("v1", &rbacv1.RoleBinding{}, &rbacv1.RoleBindingList{}), nil)
	j.objectHelper.Register(
		ResourceClusterRole,
		iamhelper.NewReference("v1", &rbacv1.ClusterRole{}, &rbacv1.ClusterRoleList{}), nil)
	j.objectHelper.Register(
		ResourceClusterRoleBinding,
		iamhelper.NewReference("v1", &rbacv1.ClusterRoleBinding{}, &rbacv1.ClusterRoleBindingList{}), nil)

	err = j.StoreObject(ctx, StoreOptions)
	if err != nil {
		return err
	}

	j.logger.Info("start to migrate data")
	err = j.DataMigration(ctx)
	if err != nil {
		return err
	}

	j.logger.Info("start to remove deprecated roletemplates")
	err = j.DeleteRoleTemplates(ctx)
	if err != nil {
		return err
	}

	j.logger.Info("start to remove legacy clusterroles and roles")
	err = j.DeleteLegacyRoles(ctx)
	if err != nil {
		return err
	}

	j.logger.Info("start to remove federated iam resources")
	err = j.RemoveKubeFedResources(ctx)
	if err != nil {
		return err
	}

	j.logger.Info("start to upgrade identity providers")
	err = j.MigrateIdentityProviders(ctx)
	if err != nil {
		return err
	}
	return nil
}

func (j *upgradeJob) PrepareUpgrade(ctx context.Context) error {
	status, err := j.GetUpgradeStatus()
	if err != nil {
		return err
	}
	if status.Status == UpgradeStatusSuccess {
		j.logger.Info("iam has been upgraded successfully, skip job")
		return nil
	}

	if err := j.fixHelmManagedResourceAnnotations(ctx); err != nil {
		return errors.Errorf("failed to fix helm managed resource annotation: %s", err)
	}
	if err := j.fixConflictResources(ctx); err != nil {
		return errors.Errorf("failed to fix conflict resources: %s", err)
	}
	return nil
}

func (j *upgradeJob) PostUpgrade(ctx context.Context) error {
	status, err := j.GetUpgradeStatus()
	if err != nil {
		return err
	}
	if status.Status == UpgradeStatusSuccess {
		j.logger.Info("iam has been upgraded successfully, skip job")
		return nil
	}

	j.objectHelper, err = iamhelper.NewObjectHelper(ctx, j.clientV3, j.clientV4, resources)
	if err != nil {
		return err
	}
	j.objectHelper.Register(
		ResourceRole,
		iamhelper.NewReference("v1", &rbacv1.Role{}, &rbacv1.RoleList{}), nil)
	j.objectHelper.Register(
		ResourceRoleBinding,
		iamhelper.NewReference("v1", &rbacv1.RoleBinding{}, &rbacv1.RoleBindingList{}), nil)
	j.objectHelper.Register(
		ResourceClusterRole,
		iamhelper.NewReference("v1", &rbacv1.ClusterRole{}, &rbacv1.ClusterRoleList{}), nil)
	j.objectHelper.Register(
		ResourceClusterRoleBinding,
		iamhelper.NewReference("v1", &rbacv1.ClusterRoleBinding{}, &rbacv1.ClusterRoleBindingList{}), nil)

	j.logger.Info("start to default custom roles")
	err = j.DefaultCustomRoles(ctx)
	if err != nil {
		return err
	}

	j.logger.Info("start to update user`s globalrole annotation")
	err = j.UpdateUserGlobalRoleAnnotation(ctx)
	if err != nil {
		return errors.Errorf("failed to update user global role annotation: %s", err)
	}

	j.logger.Info("start to delete deprecated crds")
	err = j.DeleteDeprecatedCrds(ctx)
	if err != nil {
		return err
	}

	return j.SetUpgradeStatus(UpgradeStatusSuccess)
}

func (j *upgradeJob) DeleteRoleTemplates(ctx context.Context) error {
	oldRoleTemplates := []string{ResourceRole, ResourceClusterRole,
		ResourceWorkspaceRole, ResourceGlobalRole}
	for _, resource := range oldRoleTemplates {
		list := j.objectHelper.Object(resource).V3().ListType()
		err := j.DeleteRoleTemplate(ctx, resource, list)
		if err != nil {
			return err
		}
		j.logger.Info("delete roletemplates successfully", "resource", resource)
	}
	return nil
}

func (j *upgradeJob) DataMigration(ctx context.Context) error {
	for _, option := range StoreOptions {
		oh := j.objectHelper.Object(option.Resource)
		//v4 version of resource is empty means the resource is not crd, is a K8s builtin resource
		if oh.V4().Version() != "" {
			err := updateCRDStoreVersion(j.clientV4, ctx, option.Resource, oh.V4().Version())
			if err != nil {
				return err
			}
		}
		list := oh.V3().ListType()
		err := j.LoadRecentObject(fmt.Sprintf("%s-%s", jobName, option.Resource), list)
		if err != nil {
			return err
		}
		items, err := meta.ExtractList(list)
		if err != nil {
			return err
		}
		upFunc := UpgradeCRDFuncMap[option.Resource]
		if upFunc == nil {
			continue
		}

		for _, item := range items {
			upgraded := upFunc(item.(client.Object))
			deepCopy := upgraded.DeepCopyObject().(client.Object)
			err = j.clientV4.Get(ctx, types.NamespacedName{Namespace: deepCopy.GetNamespace(), Name: deepCopy.GetName()}, deepCopy)
			if err != nil && !apierrors.IsNotFound(err) {
				return err
			}
			// skip upgrade if the resource is not crd and upgraded object is exist
			if err == nil && oh.V4().Version() == "" {
				continue
			}
			err := objectMigration(j.clientV4, ctx, upgraded)
			if err != nil {
				return err
			}
		}

		j.logger.Info(fmt.Sprintf("upgrade resource %s version %s to %s successfully", option.Resource, oh.V3().Version(), oh.V4().Version()))
	}
	return nil
}

func (j *upgradeJob) StoreObject(ctx context.Context, option []StoreOption) error {
	for _, objOpt := range option {
		resource := objOpt.Resource
		list := objOpt.ListType

		if err := j.clientV3.List(ctx, list); err != nil {
			return err
		}

		filterFunc := objOpt.FilterFunc
		if filterFunc != nil {
			filteredList := objOpt.ListType.DeepCopyObject()
			extractList, err := meta.ExtractList(list)
			if err != nil {
				return err
			}
			var filteredItems []apiruntime.Object
			for _, item := range extractList {
				if filterFunc(item.(client.Object)) {
					filteredItems = append(filteredItems, item)
				}
			}
			err = meta.SetList(filteredList, filteredItems)
			if err != nil {
				return err
			}
			list = filteredList.(client.ObjectList)
		}
		err := j.storeObjectWithTimestamp(fmt.Sprintf("%s-%s", jobName, resource), list)
		if err != nil {
			return err
		}
	}
	return nil
}

func (j *upgradeJob) storeObjectWithTimestamp(key string, list client.ObjectList) error {
	datas := []timestampedData{}
	b, err := j.resourceStore.LoadRaw(key)
	if err != nil && !errors.Is(err, storage.BackupKeyNotFound) {
		return err
	}

	if len(b) != 0 {
		err = json.Unmarshal(b, &datas)
		if err != nil {
			return err
		}
	}

	listRaw, err := json.Marshal(list)
	if err != nil {
		return err
	}

	datas = append(datas, timestampedData{Data: listRaw, Timestamp: time.Now().Unix()})
	marshal, err := json.Marshal(datas)
	if err != nil {
		return err
	}
	return j.resourceStore.SaveRaw(key, marshal)
}

func (j *upgradeJob) LoadRecentObject(key string, list client.ObjectList) error {
	datas := []timestampedData{}
	b, err := j.resourceStore.LoadRaw(key)
	if err != nil {
		return err
	}
	err = json.Unmarshal(b, &datas)
	if err != nil {
		return err
	}

	var (
		recent      int64
		recentIndex int
	)
	for i, data := range datas {
		if data.Timestamp > recent {
			recentIndex = i
		}
	}

	return json.Unmarshal(datas[recentIndex].Data, list)
}

type timestampedData struct {
	Data      []byte `json:"list"`
	Timestamp int64  `json:"timestamp"`
}

func (j *upgradeJob) UpdateUserGlobalRoleAnnotation(ctx context.Context) error {
	list := &iamv1beta1.UserList{}
	err := j.clientV4.List(ctx, list)
	if err != nil {
		return err
	}

	var errList []error
	for _, user := range list.Items {
		if user.Annotations == nil {
			user.Annotations = make(map[string]string)
		}
		globalrole := user.Annotations["iam.kubesphere.io/globalrole"]
		if globalrole == "" {
			rolelist := &iamv1beta1.GlobalRoleBindingList{}
			err := j.clientV4.List(ctx, rolelist, client.MatchingLabels{"iam.kubesphere.io/user-ref": user.Name})
			if err != nil {
				errList = append(errList, fmt.Errorf("failed to list globalrolebinding of user %s", user.Name))
				continue
			}
			if len(rolelist.Items) == 0 {
				continue
			}
			globalRoleAnnotation := rolelist.Items[0].RoleRef.Name
			user.Annotations["iam.kubesphere.io/globalrole"] = globalRoleAnnotation
			err = j.clientV4.Update(ctx, &user)
			if err != nil {
				errList = append(errList, fmt.Errorf("failed to update globalrolebinding of user %s", user.Name))
				continue
			}
			j.logger.Info("add globalrole annotation to user successfully", "user", user.Name, "globalrole", globalRoleAnnotation)
		}
	}
	return errors2.NewAggregate(errList)
}

func (j *upgradeJob) DeleteDeprecatedCrds(ctx context.Context) error {
	deprecatedCrds := []string{ResourceRoleBase}
	for _, crd := range deprecatedCrds {
		multiVersion := j.objectHelper.Object(crd)
		if multiVersion.V3().Type() == nil {
			j.logger.Info("skip delete deprecated crd: not found", "crd", crd)
			continue
		}
		list := multiVersion.V3().ListType()

		definition := &apiextensionsv1.CustomResourceDefinition{}
		err := j.clientV4.Get(ctx, client.ObjectKey{Name: crd}, definition)
		if err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("failed to get crd %s, %s", crd, err)
		}

		// delete objects
		err = j.clientV3.List(ctx, list)
		if err != nil {
			return fmt.Errorf("failed to list resource: %s, %s", crd, err)
		}
		items, err := meta.ExtractList(list)
		var errList []error
		for _, item := range items {
			object := item.(client.Object)
			err := deleteObjectWithFinalizer(j.clientV3, ctx, object)
			if err != nil {
				errList = append(errList, fmt.Errorf("failed to delete object: %s, %s", object.GetName(), err))
			} else {
				j.logger.Info("delete resource object successfully", "resource", crd, "object", object.GetName())
			}
		}
		if len(errList) != 0 {
			return errors2.NewAggregate(errList)
		}

		// delete crd
		err = j.clientV4.Delete(ctx, definition)
		if err != nil {
			errList = append(errList, fmt.Errorf("failed to delete crd %s, %s", crd, err))
		}
		j.logger.Info("delete crd successfully", "crd", crd)
	}

	return nil
}

func (j *upgradeJob) DeleteRoleTemplate(ctx context.Context, resources string, listType client.ObjectList) error {
	var errList []error

	requirement, err := labels.NewRequirement("iam.kubesphere.io/role-template", selection.Exists, nil)
	if err != nil {
		return err
	}
	err = j.clientV3.List(ctx, listType, client.InNamespace(""), client.MatchingLabelsSelector{Selector: labels.NewSelector().Add(*requirement)})
	if err != nil {
		return err
	}

	items, err := meta.ExtractList(listType)
	if err != nil {
		return err
	}

	for _, item := range items {
		object := item.(client.Object)
		err := deleteObjectWithFinalizer(j.clientV3, ctx, object)
		if err != nil {
			errList = append(errList, fmt.Errorf("failed to delete resource, %s", err))
		} else {
			j.logger.Info("delete role template successfully", "resource", resources, "role template", object.GetName(), "namespace", object.GetNamespace())
		}
	}

	return errors2.NewAggregate(errList)
}

func isNamespaceTerminating(err error) bool {
	statusError := err.(*apierrors.StatusError)
	details := statusError.ErrStatus.Details
	if details == nil {
		return false
	}
	causes := details.Causes
	if len(causes) == 0 {
		return false
	}

	for _, cause := range causes {
		if cause.Type == corev1.NamespaceTerminatingCause {
			return true
		}
	}
	return false
}

func (j *upgradeJob) DefaultCustomRoles(ctx context.Context) error {
	var DefaultCustomRoleOptions = []DefaultCustomRoleOption{
		DefaultNamespaceCustomRoleOption, DefaultWorkspaceCustomRoleOption, DefaultGlobalCustomRoleOption,
	}

	for _, value := range DefaultCustomRoleOptions {
		var errList []error

		filterFunc := value.FilterFunc
		defaultFunc := value.DefaultFunc

		list := value.ListType.DeepCopyObject().(client.ObjectList)
		err := j.clientV4.List(ctx, list)
		if err != nil {
			return err
		}
		items, err := meta.ExtractList(list)
		if err != nil {
			return err
		}

		for _, item := range items {
			role := item.(client.Object)
			if !filterFunc(role) {
				continue
			}
			err := j.clientV4.Update(ctx, defaultFunc(role))
			if err != nil {
				errList = append(errList, fmt.Errorf("failed to update resource %s, name %s, %s", value.Resource, role.GetName(), err))
				continue
			}
			j.logger.Info("update custom role successfully",
				"resource", value.Resource, "name", role.GetName(), "namespace", role.GetNamespace())

		}
		if len(errList) != 0 {
			return errors2.NewAggregate(errList)
		}

	}
	return nil
}

func (j *upgradeJob) defaultCustomRoles(ctx context.Context, resource string, list client.ObjectList, filterFunc FilterFunc, updateFunc UpgradeFunc) error {
	err := j.clientV4.List(ctx, list)
	if err != nil {
		return err
	}
	items, err := meta.ExtractList(list)
	if err != nil {
		return err
	}
	var errList []error
	for _, item := range items {
		role := item.(client.Object)
		if filterFunc(role) {
			j.logger.Info("update custom role", "name", role.GetName())
			err := j.clientV4.Update(ctx, updateFunc(role))
			if err != nil {
				errList = append(errList, fmt.Errorf("failed to update resource %s, name %s, %s", resource, role.GetName(), err))
				continue
			}
		}
	}
	return errors2.NewAggregate(errList)
}

func (j *upgradeJob) fixHelmManagedResourceAnnotations(ctx context.Context) error {
	resources := []client.Object{
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "kubesphere-system",
				Name:      "kubesphere-config",
			},
		},
		&iamv1alpha2.GlobalRole{
			ObjectMeta: metav1.ObjectMeta{Name: "anonymous"},
		},
		&iamv1alpha2.GlobalRole{
			ObjectMeta: metav1.ObjectMeta{Name: "authenticated"},
		},
		&iamv1alpha2.GlobalRole{
			ObjectMeta: metav1.ObjectMeta{Name: "platform-admin"},
		},
		&iamv1alpha2.GlobalRole{
			ObjectMeta: metav1.ObjectMeta{Name: "platform-regular"},
		},
		&iamv1alpha2.GlobalRole{
			ObjectMeta: metav1.ObjectMeta{Name: "platform-self-provisioner"},
		},
		&iamv1alpha2.GlobalRole{
			ObjectMeta: metav1.ObjectMeta{Name: "pre-registration"},
		},
		&iamv1alpha2.GlobalRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "admin"},
		},
		&iamv1alpha2.GlobalRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "anonymous"},
		},
		&iamv1alpha2.GlobalRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "authenticated"},
		},
		&iamv1alpha2.GlobalRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "pre-registration"},
		},
		&tenantv1alpha2.WorkspaceTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "system-workspace"},
		},
	}

	for _, obj := range resources {
		key := types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}
		if err := j.clientV3.Get(ctx, key, obj); err != nil {
			if apierrors.IsNotFound(err) {
				j.logger.Info("resource %s not found", key)
				continue
			}
			return err
		}
		annotations := obj.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}
		annotations["meta.helm.sh/release-name"] = "ks-core"
		annotations["meta.helm.sh/release-namespace"] = "kubesphere-system"
		obj.SetAnnotations(annotations)

		labels := obj.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}
		labels["app.kubernetes.io/managed-by"] = "Helm"
		obj.SetLabels(labels)

		if err := j.clientV3.Update(ctx, obj); err != nil {
			return err
		}

		j.logger.Info(fmt.Sprintf("fix helm managed resource metadata for %s", key.String()))
	}
	return nil
}

func (j *upgradeJob) fixConflictResources(ctx context.Context) error {
	roleBinding := &iamv1alpha2.GlobalRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "admin"},
	}
	op, err := controllerutil.CreateOrUpdate(ctx, j.clientV3, roleBinding, func() error {
		if roleBinding.Labels == nil {
			roleBinding.Labels = make(map[string]string)
		}
		roleBinding.Labels["iam.kubesphere.io/user-ref"] = "admin"
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to fix conflict resources %s %s", roleBinding.Name, err)
	}

	j.logger.Info("fix conflict resources", "globalrolebinding", roleBinding.Name, "operation", op)

	secretDriver := driver.NewSecrets(j)

	releases, err := secretDriver.List(func(release *release.Release) bool {
		return release.Name == "ks-core"
	})

	if err != nil {
		return err
	}

	if len(releases) == 0 {
		return fmt.Errorf("failed to find release ks-core")
	}

	latestRelease := releases[0]
	for _, rel := range releases {
		if rel.Info.Status == release.StatusDeployed && rel.Version > latestRelease.Version {
			latestRelease = rel
		}
	}

	resources := []client.Object{
		&corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "kubesphere-system",
				Name:      "kubesphere-config",
			},
		},
		&iamv1beta1.GlobalRole{
			TypeMeta: metav1.TypeMeta{
				Kind:       "GlobalRole",
				APIVersion: "iam.kubesphere.io/v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{Name: "anonymous"},
		},
		&iamv1beta1.GlobalRole{
			TypeMeta: metav1.TypeMeta{
				Kind:       "GlobalRole",
				APIVersion: "iam.kubesphere.io/v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{Name: "authenticated"},
		},
		&iamv1beta1.GlobalRole{
			TypeMeta: metav1.TypeMeta{
				Kind:       "GlobalRole",
				APIVersion: "iam.kubesphere.io/v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{Name: "platform-admin"},
		},
		&iamv1beta1.GlobalRole{
			TypeMeta: metav1.TypeMeta{
				Kind:       "GlobalRole",
				APIVersion: "iam.kubesphere.io/v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{Name: "platform-regular"},
		},
		&iamv1beta1.GlobalRole{
			TypeMeta: metav1.TypeMeta{
				Kind:       "GlobalRole",
				APIVersion: "iam.kubesphere.io/v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{Name: "platform-self-provisioner"},
		},
		&iamv1beta1.GlobalRole{
			TypeMeta: metav1.TypeMeta{
				Kind:       "GlobalRole",
				APIVersion: "iam.kubesphere.io/v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{Name: "pre-registration"},
		},
		&iamv1beta1.GlobalRoleBinding{
			TypeMeta: metav1.TypeMeta{
				Kind:       "GlobalRoleBinding",
				APIVersion: "iam.kubesphere.io/v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{Name: "admin"},
		},
		&iamv1beta1.GlobalRoleBinding{
			TypeMeta: metav1.TypeMeta{
				Kind:       "GlobalRoleBinding",
				APIVersion: "iam.kubesphere.io/v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{Name: "anonymous"},
		},
		&iamv1beta1.GlobalRoleBinding{
			TypeMeta: metav1.TypeMeta{
				Kind:       "GlobalRoleBinding",
				APIVersion: "iam.kubesphere.io/v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{Name: "authenticated"},
		},
		&iamv1beta1.GlobalRoleBinding{
			TypeMeta: metav1.TypeMeta{
				Kind:       "GlobalRoleBinding",
				APIVersion: "iam.kubesphere.io/v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{Name: "pre-registration"},
		},
		&tenantv1beta1.WorkspaceTemplate{
			TypeMeta: metav1.TypeMeta{
				Kind:       "WorkspaceTemplate",
				APIVersion: "tenant.kubesphere.io/v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{Name: "system-workspace"},
		},
		&corev1alpha1.Repository{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Repository",
				APIVersion: "kubesphere.io/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{Name: "extensions-museum"},
		},
	}

	buf := bytes.NewBufferString(latestRelease.Manifest)
	for _, resource := range resources {
		if !strings.Contains(latestRelease.Manifest, fmt.Sprintf("name: %s", resource.GetName())) {
			data, _ := yaml.Marshal(resource)
			buf.WriteString("---\n")
			buf.Write(data)
		}
	}

	newManifest := buf.String()

	if len(newManifest) > len(latestRelease.Manifest) {
		latestRelease.Manifest = newManifest
		j.logger.Info("update ks-core release", "newManifest", latestRelease.Manifest)
		key := fmt.Sprintf("sh.helm.release.v1.ks-core.v%d", latestRelease.Version)
		if err := secretDriver.Update(key, latestRelease); err != nil {
			return fmt.Errorf("failed to update release %s %s", key, err)
		}
		klog.Infof("update release %s/%v", latestRelease.Namespace, latestRelease.Name)
	}

	return nil
}

func (j *upgradeJob) Create(ctx context.Context, secret *corev1.Secret, opts metav1.CreateOptions) (*corev1.Secret, error) {
	return nil, nil
}

func (j *upgradeJob) Update(ctx context.Context, secret *corev1.Secret, opts metav1.UpdateOptions) (*corev1.Secret, error) {
	secret.Namespace = core.KubeSphereNamespace
	err := j.clientV3.Update(ctx, secret)
	return secret, err
}

func (j *upgradeJob) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return nil
}

func (j *upgradeJob) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	return nil
}

func (j *upgradeJob) Get(ctx context.Context, name string, opts metav1.GetOptions) (*corev1.Secret, error) {
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name}}
	err := j.clientV3.Get(ctx, types.NamespacedName{Namespace: core.KubeSphereNamespace, Name: name}, secret)
	return secret, err
}

func (j *upgradeJob) List(ctx context.Context, opts metav1.ListOptions) (*corev1.SecretList, error) {
	secrets := &corev1.SecretList{}
	err := j.clientV3.List(ctx, secrets, client.InNamespace(core.KubeSphereNamespace))
	return secrets, err
}

func (j *upgradeJob) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return nil, nil
}

func (j *upgradeJob) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *corev1.Secret, err error) {
	return nil, nil
}

func (j *upgradeJob) Apply(ctx context.Context, secret *applycorev1.SecretApplyConfiguration, opts metav1.ApplyOptions) (result *corev1.Secret, err error) {
	return nil, nil
}

func (j *upgradeJob) DeleteLegacyRoles(ctx context.Context) error {
	list := &rbacv1.RoleList{}
	err := j.clientV3.List(ctx, list)
	if err != nil {
		return err
	}
	for i, item := range list.Items {
		if item.Annotations == nil || item.Annotations["kubesphere.io/creator"] == "" {
			continue
		}
		err = deleteObjectWithFinalizer(j.clientV3, ctx, &list.Items[i])
		if err != nil {
			return err
		}
		j.logger.Info("delete legacy role successfully", "role", item.Name, "namespace", item.Namespace)
	}
	return nil
}

func (j *upgradeJob) MigrateIdentityProviders(ctx context.Context) error {
	cm := &corev1.ConfigMap{}
	err := j.clientV3.Get(ctx, types.NamespacedName{Namespace: constants.KubeSphereNamespace, Name: constants.KubeSphereConfigName}, cm)
	if err != nil {
		return err
	}
	data, exist := cm.Data["kubesphere.yaml"]
	if !exist {
		return nil
	}
	ksConfig := &KSConfig{}
	err = yaml.Unmarshal([]byte(data), ksConfig)
	if err != nil {
		return err
	}
	providers := ksConfig.Authentication.OAuthOptions.IdentityProviders
	for _, provider := range providers {
		secret, err := identityProviderSecret(provider)
		if err != nil {
			return err
		}
		secretGet := secret.DeepCopy()
		_, err = controllerutil.CreateOrUpdate(ctx, j.clientV3, secretGet, func() error {
			secretGet.Type = secret.Type
			secretGet.Labels = secret.Labels
			secretGet.StringData = secret.StringData
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func identityProviderSecret(provider IdentityProvider) (*corev1.Secret, error) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("kubesphere-identityprovider.%s", strings.ToLower(provider.Name)),
			Namespace: constants.KubeSphereNamespace,
			Labels: map[string]string{
				"config.kubesphere.io/type": "identityprovider",
			},
		},
		StringData: map[string]string{},
		Type:       "config.kubesphere.io/identityprovider",
	}
	marshal, err := yaml.Marshal(provider)
	if err != nil {
		return nil, err
	}
	secret.StringData["configuration.yaml"] = string(marshal)
	return secret, nil
}

type IdentityProvider struct {
	Name          string                 `json:"name" yaml:"name"`
	MappingMethod string                 `json:"mappingMethod" yaml:"mappingMethod"`
	Type          string                 `json:"type" yaml:"type"`
	Provider      map[string]interface{} `json:"provider" yaml:"provider"`
}

type OAuthOptions struct {
	IdentityProviders []IdentityProvider `yaml:"identityProviders"`
}

type Authentication struct {
	OAuthOptions OAuthOptions `yaml:"oauthOptions"`
}

type KSConfig struct {
	Authentication Authentication `yaml:"authentication"`
}
