package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
	constants4 "kubesphere.io/ks-upgrade/pkg/constants"
	"kubesphere.io/ks-upgrade/pkg/storage"
	"kubesphere.io/ks-upgrade/v3/api/application/v1alpha1"
	"kubesphere.io/ks-upgrade/v3/api/constants"
	v2 "kubesphere.io/ks-upgrade/v4/api/application/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (i *upgradeJob) s3Pre(ctx context.Context) error {

	secret := corev1.Secret{}
	err := i.clientV3.Get(ctx, client.ObjectKey{Namespace: constants4.KubeSphereNamespace, Name: "minio"}, &secret)
	if err != nil {
		return err
	}

	svc := corev1.Service{}
	err = i.clientV3.Get(ctx, client.ObjectKey{Namespace: constants4.KubeSphereNamespace, Name: "minio"}, &svc)
	if err != nil && k8serrors.IsNotFound(err) {
		klog.Infof("[Application] minio service not found, skip download chart from minio")
		return nil
	}
	if err != nil {
		klog.Errorf("[Application] Failed to get minio service: %v", err)
		return err
	}

	AK := string(secret.Data["accesskey"])
	SK := string(secret.Data["secretkey"])

	config := aws.Config{
		Endpoint:         aws.String(i.s3EndPoint),
		DisableSSL:       aws.Bool(true),
		S3ForcePathStyle: aws.Bool(true),
		Region:           aws.String("us-east-1"),
		Credentials:      credentials.NewStaticCredentials(AK, SK, ""),
	}

	s, err := session.NewSession(&config)
	if err != nil {
		klog.Errorf("[Application] Failed to create session: %v", err)
		return err
	}

	appVerList := v1alpha1.HelmApplicationVersionList{}
	err = i.clientV3.List(ctx, &appVerList)
	if err != nil {
		klog.Errorf("[Application] Failed to list helm application version: %v", err)
		return err
	}

	for _, v := range appVerList.Items {
		workspace := v.GetLabels()[constants.WorkspaceLabelKey]
		name := fmt.Sprintf("%s/%s", workspace, v.Name)
		get, err := download(s, name, "app-store")
		if err != nil {
			klog.Warningf("[Application] Failed to download chart from minio: %v", err)
			continue
		}

		err = i.resourceStore.SaveRaw(v.Name, get)
		if err != nil {
			klog.Errorf("[Application] Failed to save helm chart: %v", err)
			return err
		}
	}
	klog.Infof("[Application] %d Successfully download chart from minio", len(appVerList.Items))

	return err
}

type S3Config struct {
	S3 S3 `json:"s3"`
}
type S3 struct {
	Endpoint        string `json:"endpoint"`
	Region          string `json:"region"`
	DisableSSL      bool   `json:"disableSSL"`
	ForcePathStyle  bool   `json:"forcePathStyle"`
	AccessKeyID     string `json:"accessKeyID"`
	SecretAccessKey string `json:"secretAccessKey"`
	Bucket          string `json:"bucket"`
}

func (i *upgradeJob) s3After(ctx context.Context) error {
	svc := corev1.Service{}
	err := i.clientV3.Get(ctx, client.ObjectKey{Namespace: constants4.KubeSphereNamespace, Name: "minio"}, &svc)
	if err != nil && k8serrors.IsNotFound(err) {
		klog.Infof("[Application] minio service not found, skip s3After from minio")
		return nil
	}
	if err != nil {
		klog.Errorf("[Application] Failed to get minio service: %v", err)
		return err
	}

	appVerList := v2.ApplicationVersionList{}
	opts := client.ListOptions{
		LabelSelector: labels.Set{v2.RepoIDLabelKey: v2.UploadRepoKey}.AsSelector(),
	}
	err = i.clientV4.List(ctx, &appVerList, &opts)
	if err != nil {
		klog.Errorf("[Application] Failed to list application version: %v", err)
		return err
	}
	conf, err := i.getConfig(ctx)
	if err != nil {
		klog.Errorf("[Application] Failed to get kubesphere config: %v", err)
		return err
	}
	if conf.S3.Endpoint != "" {
		klog.Infof("[Application] Save application to oss")
		return i.saveToOss(conf, appVerList)
	}
	klog.Infof("[Application] ks4.1 oss config not set, Save app to configmap")
	return i.saveToCm(ctx, appVerList)
}

func (i *upgradeJob) saveToOss(config S3Config, appVerList v2.ApplicationVersionList) error {

	opt := Options{
		Endpoint:        config.S3.Endpoint,
		Region:          config.S3.Region,
		AccessKeyID:     config.S3.AccessKeyID,
		SecretAccessKey: config.S3.SecretAccessKey,
		Bucket:          config.S3.Bucket,
	}

	c, err := getAwsSession(opt)
	if err != nil {
		klog.Errorf("Failed to create session: %v", err)
		return err
	}

	for _, appVersion := range appVerList.Items {
		data, err := i.resourceStore.LoadRaw(appVersion.Name)
		if !errors.Is(err, storage.BackupKeyNotFound) {
			klog.Warningf("[Application] %s not found", appVersion.Name)
			continue
		}
		err = Upload(appVersion.Name, opt.Bucket, bytes.NewBuffer(data), len(data), c)
		if err != nil {
			klog.Errorf("[Application] Failed to upload application version: %v", err)
			return err
		}
	}
	return nil
}

func (i *upgradeJob) getConfig(ctx context.Context) (config S3Config, err error) {

	cm := corev1.ConfigMap{}
	key := client.ObjectKey{Namespace: constants4.KubeSphereNamespace, Name: "kubesphere-config"}
	err = i.clientV4.Get(ctx, key, &cm)
	if err != nil {
		klog.Errorf("[Application] Failed to get kubesphere config: %v", err)
		return config, err
	}

	err = yaml.Unmarshal([]byte(cm.Data["kubesphere.yaml"]), &config)
	if err != nil {
		klog.Errorf("[Application] Failed to unmarshal kubesphere config: %v", err)
		return config, err
	}
	js, _ := json.Marshal(config)
	klog.Infof("S3 config: %s", string(js))

	return config, err
}

func (i *upgradeJob) saveToCm(ctx context.Context, appVerList v2.ApplicationVersionList) error {
	for _, appVersion := range appVerList.Items {
		cm := &corev1.ConfigMap{}
		cm.Name = appVersion.Name
		cm.Namespace = v2.ApplicationNamespace
		data, err := i.resourceStore.LoadRaw(appVersion.Name)
		if err != nil {
			klog.Warningf("[Application] Failed to load application version: %s %v", appVersion.Name, err)
			continue
		}
		cm.BinaryData = map[string][]byte{v2.BinaryKey: data}
		err = i.clientV4.Create(ctx, cm)
		if err != nil && !k8serrors.IsAlreadyExists(err) {
			return err
		}
	}
	klog.Infof("[Application] %d Successfully save application to configmap", len(appVerList.Items))
	return nil
}
