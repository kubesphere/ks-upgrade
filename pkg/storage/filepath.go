package storage

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"

	"k8s.io/klog/v2"
)

type LocalFileStorageOptions struct {
	StoragePath string `json:"path"`
}

type LocalFileStorage struct {
	Path string
}

func (f *LocalFileStorage) Put(k string, object []byte) error {
	file, err := os.OpenFile(path.Join(f.Path, k), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		return err
	}
	if _, err := file.Write(object); err != nil {
		return err
	}
	klog.Infof("[Storage] Put object to %s", k)
	return nil
}

func (f *LocalFileStorage) Get(k string) ([]byte, error) {
	file, err := os.Open(path.Join(f.Path, k))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, BackupKeyNotFound
		}
		return nil, err
	}
	buffer := bytes.NewBuffer([]byte{})
	if _, err := buffer.ReadFrom(file); err != nil {
		return nil, err
	}
	klog.Infof("[Storage] Get object from %s", k)
	return buffer.Bytes(), nil
}

func (f *LocalFileStorage) Delete(k string) error {
	target := path.Join(f.Path, k)
	err := os.Remove(target)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return BackupKeyNotFound
		}
		return err
	}

	klog.Infof("[Storage] Delete object %s from %s", k, target)
	return nil
}

func NewLocalFileStorage(options *LocalFileStorageOptions) (*LocalFileStorage, error) {
	if _, err := os.Stat(options.StoragePath); os.IsNotExist(err) {
		if err := os.MkdirAll(options.StoragePath, 0666); err != nil {
			return nil, errors.New(fmt.Sprintf("[Storage] Create LocalFileStorage failed: %s", err))
		}
		klog.Infof("[Storage] Created LocalFileStorage dir %s", options.StoragePath)
	} else {
		klog.Infof("[Storage] LocalFileStorage File directory %s already exists", options.StoragePath)
	}
	return &LocalFileStorage{
		Path: options.StoragePath,
	}, nil
}
