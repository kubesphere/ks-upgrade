package helm

import (
	"bytes"
	"fmt"

	"helm.sh/helm/v3/pkg/chart/loader"
)

func GetFileFromChart(chartData []byte, targetFileName string) ([]byte, error) {
	chartReader := bytes.NewBuffer(chartData)

	files, err := loader.LoadArchiveFiles(chartReader)
	if err != nil {
		return nil, err
	}

	for _, file := range files {
		if file.Name == targetFileName {
			return file.Data, nil
		}
	}

	return nil, fmt.Errorf("file %s not found in chart", targetFileName)
}
