package storage

import (
	"bytes"
	"errors"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
	"k8s.io/klog/v2"
)

type ObjectStorage struct {
	client Interface
}

func (s *ObjectStorage) Put(k string, object []byte) error {
	bf := bytes.NewBuffer(object)
	err := s.client.Upload(k, k, bf, len(object))
	err = handlerAwsErr(err)
	if errors.Is(err, BackupNotFound) {
		klog.Error(err)
		if err := s.client.CreateBucket(); err != nil {
			return err
		}
		err = s.client.Upload(k, k, bf, len(object))
	}
	return err
}

func (s *ObjectStorage) Get(k string) ([]byte, error) {
	data, err := s.client.Read(k)
	return data, handlerAwsErr(err)
}

func handlerAwsErr(err error) error {
	var awsErr awserr.Error
	if errors.As(err, &awsErr) {
		var reqErr awserr.RequestFailure
		if errors.As(awsErr, &reqErr) {
			if reqErr.StatusCode() == 404 {
				if reqErr.Code() == s3.ErrCodeNoSuchKey {
					klog.Error(reqErr)
					return BackupKeyNotFound
				} else if reqErr.Code() == s3.ErrCodeNoSuchBucket {
					klog.Error(reqErr)
					return BackupNotFound
				}
			}
		}
	}
	return err
}

func (s *ObjectStorage) Delete(k string) error {
	err := s.client.Delete(k)
	return handlerAwsErr(err)
}

func NewS3Storage(options *S3StorageOptions) (*ObjectStorage, error) {
	client, err := NewS3Client(options)
	if err != nil {
		return nil, err
	}
	return &ObjectStorage{
		client: client,
	}, err
}
