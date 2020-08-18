package users

import (
	"encoding/json"
	"fmt"
	"github.com/go-ldap/ldap"
	"golang.org/x/crypto/bcrypt"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"kubesphere.io/ks-upgrade/pkg/task"
	"net/mail"
	"time"
)

const (
	ldapManagerDN                  = "cn=admin,dc=kubesphere,dc=io"
	ldapManagerPassword            = "admin"
	userSearchBase                 = "ou=Users,dc=kubesphere,dc=io"
	ldapAttributeObjectClass       = "objectClass"
	ldapAttributeCommonName        = "cn"
	ldapAttributeSerialNumber      = "sn"
	ldapAttributeGlobalIDNumber    = "gidNumber"
	ldapAttributeHomeDirectory     = "homeDirectory"
	ldapAttributeUserID            = "uid"
	ldapAttributeUserIDNumber      = "uidNumber"
	ldapAttributeMail              = "mail"
	ldapAttributeUserPassword      = "userPassword"
	ldapAttributePreferredLanguage = "preferredLanguage"
	ldapAttributeDescription       = "description"
	ldapAttributeCreateTimestamp   = "createTimestamp"
	ldapAttributeOrganizationUnit  = "ou"
	userFilter                     = "(&(objectClass=inetOrgPerson))"

	// ldap create timestamp attribute layout
	ldapAttributeCreateTimestampLayout = "20060102150405Z"

	initialPassword = "P@88w0rd"
)

type userMigrateTask struct {
	k8sClient  kubernetes.Interface
	ldapClient ldap.Client
}

func NewUserMigrateTask(k8sClient kubernetes.Interface, ldapClient ldap.Client) task.UpgradeTask {

	return &userMigrateTask{k8sClient: k8sClient, ldapClient: ldapClient}
}

func (t *userMigrateTask) Run() error {

	if err := t.ldapClient.Bind(ldapManagerDN, ldapManagerPassword); err != nil {
		klog.Error(err)
		return err
	}

	users := make([]User, 0)
	pageControl := ldap.NewControlPaging(1000)
	for {
		userSearchRequest := ldap.NewSearchRequest(
			userSearchBase,
			ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
			userFilter,
			[]string{ldapAttributeUserID, ldapAttributeMail, ldapAttributeDescription, ldapAttributePreferredLanguage, ldapAttributeUserPassword, ldapAttributeCreateTimestamp},
			[]ldap.Control{pageControl},
		)

		response, err := t.ldapClient.Search(userSearchRequest)
		if err != nil {
			klog.Error(err)
			return err
		}
		for _, entry := range response.Entries {
			uid := entry.GetAttributeValue(ldapAttributeUserID)
			email := entry.GetAttributeValue(ldapAttributeMail)
			description := entry.GetAttributeValue(ldapAttributeDescription)
			lang := entry.GetAttributeValue(ldapAttributePreferredLanguage)
			createTimestamp, _ := time.Parse(ldapAttributeCreateTimestampLayout, entry.GetAttributeValue(ldapAttributeCreateTimestamp))
			pwd := entry.GetAttributeValue(ldapAttributeUserPassword)
			if pwd == "" {
				pwd = initialPassword
			}
			if _, err := mail.ParseAddress(email); err != nil {
				email = ""
			}
			encryptedPassword, _ := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.DefaultCost)
			user := User{
				TypeMeta: metav1.TypeMeta{
					Kind:       "User",
					APIVersion: "iam.kubesphere.io/v1alpha2",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:              uid,
					CreationTimestamp: metav1.Time{Time: createTimestamp},
					Annotations: map[string]string{
						"kubesphere.io/description":            description,
						"iam.kubesphere.io/password-encrypted": "true",
					},
				},
				Spec: UserSpec{
					Email:             email,
					Lang:              lang,
					EncryptedPassword: string(encryptedPassword),
				},
				Status: UserStatus{
					State:              "Active",
					LastTransitionTime: &metav1.Time{Time: time.Now()},
				},
			}
			users = append(users, user)
		}

		updatedControl := ldap.FindControl(response.Controls, ldap.ControlTypePaging)
		if ctrl, ok := updatedControl.(*ldap.ControlPaging); ctrl != nil && ok && len(ctrl.Cookie) != 0 {
			pageControl.SetCookie(ctrl.Cookie)
			continue
		}
		break
	}

	for _, user := range users {
		// deprecated account
		if user.Name == "sonarqube" {
			continue
		}
		klog.Infof("migrate users: %s", user.Name)
		old, err := t.getUser(user.Name)
		if err != nil {
			if errors.IsNotFound(err) {
				err = t.createUser(&user)
				if err != nil {
					klog.Error(err)
				}
				continue
			}
			klog.Error(err)
			return err
		}
		user.ResourceVersion = old.ResourceVersion
		err = t.updateUser(&user)
		if err != nil {
			klog.Error(err)
			return err
		}
	}
	return nil
}

func (t *userMigrateTask) getUser(username string) (*User, error) {
	cli := t.k8sClient.(*kubernetes.Clientset)
	data, err := cli.RESTClient().
		Get().
		AbsPath(fmt.Sprintf("/apis/iam.kubesphere.io/v1alpha2/users/%s", username)).
		DoRaw()
	if err != nil {
		return nil, err
	}
	var old User
	err = json.Unmarshal(data, &old)
	if err != nil {
		klog.Error(err)
		return nil, err
	}
	return &old, nil
}

func (t *userMigrateTask) updateUser(user *User) error {
	cli := t.k8sClient.(*kubernetes.Clientset)
	outputData, _ := json.Marshal(user)
	err := cli.RESTClient().
		Put().
		AbsPath(fmt.Sprintf("/apis/iam.kubesphere.io/v1alpha2/users/%s", user.Name)).
		Body(outputData).
		Do().Error()
	if err != nil {
		klog.Error(err)
		return err
	}
	return nil
}

func (t *userMigrateTask) createUser(user *User) error {
	cli := t.k8sClient.(*kubernetes.Clientset)
	outputData, _ := json.Marshal(user)
	err := cli.RESTClient().
		Post().
		AbsPath(fmt.Sprintf("/apis/iam.kubesphere.io/v1alpha2/users")).
		Body(outputData).
		Do().Error()
	if err != nil {
		klog.Error(err)
		return err
	}
	return nil
}
