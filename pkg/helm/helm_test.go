package helm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestClient_ListReleases(t *testing.T) {
	client, err := NewClient("")
	assert.Equal(t, err, nil)
	releases, err := client.ListReleases("minio")
	assert.Equal(t, err, nil)
	t.Log(releases)
}

func TestClient_Installed(t *testing.T) {
	client, err := NewClient("")
	assert.Equal(t, err, nil)
	release, err := client.Install(context.Background(), metav1.NamespaceDefault, "sample-redis", "../../bin/redis-17.0.1.tgz", nil)
	assert.Equal(t, err, nil)
	t.Log(release)
}

func TestClient_Upgrade(t *testing.T) {
	client, err := NewClient("")
	assert.Equal(t, err, nil)
	release, err := client.Upgrade(context.Background(), metav1.NamespaceDefault, "sample-redis", "../../bin/redis-18.4.0.tgz", nil)
	assert.Equal(t, err, nil)
	t.Log(release)
}

func TestClient_Uninstalled(t *testing.T) {
	client, err := NewClient("")
	assert.Equal(t, err, nil)
	release, err := client.Uninstall(metav1.NamespaceDefault, "sample-redis")
	assert.Equal(t, err, nil)
	t.Log(release)
}

func TestClient_Status(t *testing.T) {
	client, err := NewClient("")
	assert.Equal(t, err, nil)
	release, err := client.Status(metav1.NamespaceDefault, "sample-redis")
	assert.Equal(t, err, nil)
	t.Log(release)
}

func TestClient_History(t *testing.T) {
	client, err := NewClient("")
	assert.Equal(t, err, nil)
	release, err := client.History("kubesphere-system", "ks-core")
	assert.Equal(t, err, nil)
	t.Log(release)
}

func TestClient_LastReleaseDeployed(t *testing.T) {
	client, err := NewClient("")
	assert.Equal(t, err, nil)
	release, err := client.LastReleaseDeployed("kubesphere-system", "ks-core")
	assert.Equal(t, err, nil)
	t.Log(release)
}
