package helm

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestGetFileFromChartBytes(t *testing.T) {
	// Get the absolute path to the test data directory
	_, currentFile, _, _ := runtime.Caller(0)
	testDataDir := filepath.Join(filepath.Dir(currentFile), "testdata")
	testChartPath := filepath.Join(testDataDir, "test-chart.tgz")

	// Read the test chart file
	chartBuf, err := os.ReadFile(testChartPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("skipping test: " + err.Error())
		}
		t.Fatalf("failed to read test chart file: %v", err)
	}

	// Decode the base64 content
	decoded, err := base64.StdEncoding.DecodeString(string(chartBuf))
	if err != nil {
		t.Fatalf("failed to decode base64: %v", err)
	}

	// Test extracting a file from the chart
	content, err := GetFileFromChart(decoded, "extension.yaml")
	if err != nil {
		t.Fatalf("failed to get file from chart: %v", err)
	}

	// Verify the content
	if len(content) == 0 {
		t.Error("expected non-empty file content")
	}
}
