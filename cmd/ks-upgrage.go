package main

import (
	"flag"
	"github.com/go-ldap/ldap"
	"github.com/go-redis/redis"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"kubesphere.io/ks-upgrade/pkg/globalrolebindings"
	"kubesphere.io/ks-upgrade/pkg/rolebindings"
	"kubesphere.io/ks-upgrade/pkg/task"
	"kubesphere.io/ks-upgrade/pkg/users"
	"kubesphere.io/ks-upgrade/pkg/workspacerolebindings"
	"kubesphere.io/ks-upgrade/pkg/workspaces"
	"log"
)

const (
	LDAPHost  = "openldap.kubesphere-system.svc:389"
	RedisHost = "redis.kubesphere-system.svc:6379"
)

func main() {
	klog.InitFlags(flag.CommandLine)
	flag.Parse()
	k8sClient, err := newKubernetesClient()
	if err != nil {
		klog.Fatalln(err)
	}
	ldapClient, err := newLdapClient()
	if err != nil {
		klog.Fatalln(err)
	}
	defer ldapClient.Close()
	redisClient, err := newRedisClient()
	if err != nil {
		klog.Fatalln(err)
	}
	defer redisClient.Close()
	tasks := make([]task.UpgradeTask, 0)
	tasks = append(tasks, users.NewUserMigrateTask(k8sClient, ldapClient, redisClient))
	tasks = append(tasks, globalrolebindings.NewGlobalRoleBindingMigrateTask(k8sClient))
	tasks = append(tasks, workspaces.NewWorkspaceMigrateTask(k8sClient))
	tasks = append(tasks, workspacerolebindings.NewWorkspaceRoleBindingMigrateTask(k8sClient))
	tasks = append(tasks, rolebindings.NewRoleBindingMigrateTask(k8sClient))

	for _, task := range tasks {
		klog.Infof("starting upgrade: %T", task)
		if err := task.Run(); err != nil {
			log.Panicln(err)
		}
		klog.Infof("successfully upgraded': %T", task)
	}
}

func newLdapClient() (ldap.Client, error) {
	client, err := ldap.Dial("tcp", LDAPHost)
	if err != nil {
		klog.Error(err)
		return nil, err
	}
	return client, nil
}

func newKubernetesClient() (kubernetes.Interface, error) {
	if config, err := rest.InClusterConfig(); err != nil {
		klog.Error(err)
		return nil, err
	} else {
		return kubernetes.NewForConfig(config)
	}
}

func newRedisClient() (*redis.Client, error) {
	redisClient := redis.NewClient(&redis.Options{Addr: RedisHost})

	if err := redisClient.Ping().Err(); err != nil {
		redisClient.Close()
		return nil, err
	}
	return redisClient, nil
}
