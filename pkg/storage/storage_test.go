package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestS3Storage(t *testing.T) {
	s, err := NewS3Storage(&S3StorageOptions{
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
	_, err = s.Get("iam")
	assert.Equal(t, err, BackupKeyNotFound)
}

func TestS3StorageBucketNotFound(t *testing.T) {
	s, err := NewS3Storage(&S3StorageOptions{
		Endpoint:        "127.0.0.1:9000",
		Region:          "wh",
		DisableSSL:      true,
		ForcePathStyle:  true,
		AccessKeyID:     "admin",
		SecretAccessKey: "P@88w0rd",
		SessionToken:    "",
		Bucket:          "bucket01",
	})
	assert.Equal(t, err, nil)
	err = s.Put("iam", []byte("test"))
	assert.Equal(t, err, nil)
}

func TestS3StorageDelete(t *testing.T) {
	s, err := NewS3Storage(&S3StorageOptions{
		Endpoint:        "127.0.0.1:9000",
		Region:          "wh",
		DisableSSL:      true,
		ForcePathStyle:  true,
		AccessKeyID:     "admin",
		SecretAccessKey: "P@88w0rd",
		SessionToken:    "",
		Bucket:          "bucket01",
	})
	assert.Equal(t, err, nil)
	err = s.Put("bucket01-key01", []byte("test"))
	assert.Equal(t, err, nil)
	err = s.Delete("bucket01-key01")
	assert.Equal(t, err, nil)
}
