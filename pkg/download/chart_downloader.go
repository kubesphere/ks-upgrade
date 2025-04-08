package download

import (
	"bytes"
	"net/url"
)

type Downloader interface {
	Get(uri string) (*bytes.Buffer, error)
	Provides(uri string) bool
}

type ChartDownloader struct {
	defaultDownloader Downloader
	downloader        []Downloader
}

func NewChartDownloader() (*ChartDownloader, error) {
	httpDownloader, err := NewHttpDownloader(&HttpDownloaderOptions{InsecureSkipVerify: true})
	if err != nil {
		return nil, err
	}
	return &ChartDownloader{
		defaultDownloader: httpDownloader,
		downloader: []Downloader{
			httpDownloader,
		},
	}, nil
}

func (c *ChartDownloader) Download(uri string) (*bytes.Buffer, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "" {
		u.Scheme = "file"
	}
	for _, downloader := range c.downloader {
		if downloader.Provides(u.Scheme) {
			return downloader.Get(uri)
		}
	}
	return c.defaultDownloader.Get(uri)
}
