package store

import (
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kubesphere.io/ks-upgrade/pkg/storage"
	iamv1beta1 "kubesphere.io/ks-upgrade/v4/api/iam/v1beta1"
	"testing"
)

func TestStoreWithFileStorage(t *testing.T) {
	s, err := storage.NewLocalFileStorage(&storage.LocalFileStorageOptions{StoragePath: "../../bin"})
	assert.Equal(t, err, nil)
	store := NewKubernetesResourceStore(s)
	err = store.SaveRaw("file1", []byte("file1"))
	assert.Equal(t, err, nil)
	data, err := store.LoadRaw("file1")
	assert.Equal(t, err, nil)
	assert.Equal(t, data, []byte("file1"))

	user := iamv1beta1.User{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: "admin",
		},
		Spec:   iamv1beta1.UserSpec{},
		Status: iamv1beta1.UserStatus{},
	}
	err = store.Save("admin", &user)
	assert.Equal(t, err, nil)

	user2 := iamv1beta1.User{}
	err = store.Load("admin", &user2)
	assert.Equal(t, user, user2)
}

func TestStoreWithS3Storage(t *testing.T) {
	s, err := storage.NewS3Storage(&storage.S3StorageOptions{
		Endpoint:        "127.0.0.1:9000",
		Region:          "wh",
		DisableSSL:      true,
		ForcePathStyle:  true,
		AccessKeyID:     "admin",
		SecretAccessKey: "P@88w0rd",
		SessionToken:    "",
		Bucket:          "ks-upgrade",
	})
	assert.Equal(t, err, nil)
	store := NewKubernetesResourceStore(s)
	err = store.SaveRaw("file1", []byte("file1"))
	assert.Equal(t, err, nil)
	data, err := store.LoadRaw("file1")
	assert.Equal(t, err, nil)
	assert.Equal(t, data, []byte("file1"))

	user := iamv1beta1.User{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: "admin",
		},
		Spec:   iamv1beta1.UserSpec{},
		Status: iamv1beta1.UserStatus{},
	}
	err = store.Save("admin", &user)
	assert.Equal(t, err, nil)

	user2 := iamv1beta1.User{}
	err = store.Load("admin", &user2)
	assert.Equal(t, user, user2)
}
