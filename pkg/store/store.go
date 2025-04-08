package store

import (
	"bytes"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/json"

	"k8s.io/apimachinery/pkg/util/yaml"
	"kubesphere.io/ks-upgrade/pkg/storage"
)

type ResourceStore interface {
	Load(key string, object runtime.Object) error
	Save(key string, object runtime.Object) error
	Delete(key string) error
	LoadRaw(key string) ([]byte, error)
	SaveRaw(key string, raw []byte) error
}

type KubernetesResourceStore struct {
	Storage storage.Storage
}

func (k *KubernetesResourceStore) Load(key string, object runtime.Object) error {
	resourceRaw, err := k.Storage.Get(key)
	if err != nil {
		return err
	}
	return yaml.NewYAMLOrJSONDecoder(bytes.NewBuffer(resourceRaw), 1024).Decode(object)
}

func (k *KubernetesResourceStore) Save(key string, object runtime.Object) error {
	resourceRaw, err := json.Marshal(object)
	if err != nil {
		return err
	}
	return k.Storage.Put(key, resourceRaw)
}

func (k *KubernetesResourceStore) Delete(key string) error {
	return k.Storage.Delete(key)
}

func (k *KubernetesResourceStore) LoadRaw(key string) ([]byte, error) {
	return k.Storage.Get(key)
}

func (k *KubernetesResourceStore) SaveRaw(key string, raw []byte) error {
	return k.Storage.Put(key, raw)
}

func NewKubernetesResourceStore(storage storage.Storage) ResourceStore {
	return &KubernetesResourceStore{
		Storage: storage,
	}
}
