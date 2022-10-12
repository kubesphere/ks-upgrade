package main

import (
	"flag"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"kubesphere.io/ks-upgrade/pkg/role"

	"log"
)

func main() {
	klog.InitFlags(flag.CommandLine)
	flag.Parse()
	k8sClient, err := newKubernetesClient()
	if err != nil {
		klog.Fatalln(err)
	}

	task := role.NewRoleMigrateTask(k8sClient)

	klog.Infof("starting upgrade: %T", task)
	if err := task.Run(); err != nil {
		log.Panicln(err)
	}
	klog.Infof("successfully upgraded': %T", task)

}

func newKubernetesClient() (kubernetes.Interface, error) {
	if config, err := rest.InClusterConfig(); err != nil {
		klog.Error(err)
		return nil, err
	} else {
		return kubernetes.NewForConfig(config)
	}
}
