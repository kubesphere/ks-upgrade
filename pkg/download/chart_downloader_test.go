package download

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChartDownloaderDownload(t *testing.T) {
	chartDownloader, err := NewChartDownloader()
	assert.Equal(t, err, nil)
	_, err = chartDownloader.Download("https://charts.kubesphere.io/test/ks-core-0.6.12.tgz")
	assert.Equal(t, err, nil)
}
