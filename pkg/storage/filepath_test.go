package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFileStorage(t *testing.T) {
	s, err := NewLocalFileStorage(&LocalFileStorageOptions{StoragePath: "../../bin/"})
	assert.Equal(t, err, nil)
	_, err = s.Get("iam")
	assert.Equal(t, err, BackupKeyNotFound)

	err = s.Put("iam", []byte("test"))
	assert.Equal(t, err, nil)

	err = s.Delete("iam")
	assert.Equal(t, err, nil)

	err = s.Delete("iam-not-found")
	assert.Equal(t, err, BackupKeyNotFound)
}
