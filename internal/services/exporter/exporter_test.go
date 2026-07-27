package exporter

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExportJSONSnapshot(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "blackeye_export_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	targetFile := filepath.Join(tmpDir, "test_snapshot.json")
	sampleData := map[string]string{
		"status": "ok",
		"app":    "blackeye",
	}

	exportedPath, err := ExportJSONSnapshot(sampleData, targetFile)
	if err != nil {
		t.Fatalf("expected clean export, got error: %v", err)
	}

	if exportedPath != targetFile {
		t.Fatalf("expected export path %s, got %s", targetFile, exportedPath)
	}

	info, err := os.Stat(exportedPath)
	if err != nil || info.Size() == 0 {
		t.Fatalf("expected non-empty snapshot file at %s", exportedPath)
	}
}
