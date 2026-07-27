// Package exporter handles JSON snapshot dumps of system telemetry and security state.
package exporter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func ExportJSONSnapshot(data interface{}, customPath string) (string, error) {
	targetPath := customPath
	if targetPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "/tmp"
		}
		dir := filepath.Join(home, ".local", "share", "blackeye")
		_ = os.MkdirAll(dir, 0755)
		targetPath = filepath.Join(dir, fmt.Sprintf("snapshot_%d.json", time.Now().Unix()))
	}

	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", err
	}

	err = os.WriteFile(targetPath, bytes, 0644)
	if err != nil {
		return "", err
	}

	return targetPath, nil
}
