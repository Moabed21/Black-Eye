package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfig_Defaults(t *testing.T) {
	cfg := Defaults()

	if cfg.Refresh.DashboardInterval != 2 {
		t.Errorf("expected DashboardInterval=2, got %d", cfg.Refresh.DashboardInterval)
	}
	if cfg.Alerts.CPUWarning != 70.0 {
		t.Errorf("expected CPUWarning=70.0, got %f", cfg.Alerts.CPUWarning)
	}
	if len(cfg.Ports.TrustedPorts) == 0 {
		t.Error("expected non-empty trusted ports default")
	}
}

func TestConfig_Load_NonExistentFile(t *testing.T) {
	cfg, err := Load("/path/does/not/exist/config.toml")
	if err != nil {
		t.Fatalf("expected no error loading non-existent file, got %v", err)
	}
	if cfg.Refresh.DashboardInterval != 2 {
		t.Errorf("expected default DashboardInterval=2, got %d", cfg.Refresh.DashboardInterval)
	}
}

func TestConfig_Load_ValidTOML(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.toml")

	tomlContent := `
[refresh]
dashboard_interval = 5
process_interval = 4

[alerts]
cpu_warning = 60.0
cpu_critical = 90.0
memory_warning = 75.0
memory_critical = 95.0
disk_warning = 85.0
disk_critical = 98.0

[ports]
trusted_ports = [8080, 9090]
`
	if err := os.WriteFile(configFile, []byte(tomlContent), 0644); err != nil {
		t.Fatalf("failed to write temp config file: %v", err)
	}

	cfg, err := Load(configFile)
	if err != nil {
		t.Fatalf("failed to load valid config: %v", err)
	}

	if cfg.Refresh.DashboardInterval != 5 {
		t.Errorf("expected DashboardInterval=5, got %d", cfg.Refresh.DashboardInterval)
	}
	if cfg.Alerts.CPUWarning != 60.0 {
		t.Errorf("expected CPUWarning=60.0, got %f", cfg.Alerts.CPUWarning)
	}
	if len(cfg.Ports.TrustedPorts) != 2 || cfg.Ports.TrustedPorts[0] != 8080 {
		t.Errorf("unexpected trusted ports: %v", cfg.Ports.TrustedPorts)
	}
}

func TestConfig_Load_InvalidValidation(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.toml")

	// Invalid: cpu_warning >= cpu_critical
	tomlContent := `
[alerts]
cpu_warning = 90.0
cpu_critical = 80.0
`
	if err := os.WriteFile(configFile, []byte(tomlContent), 0644); err != nil {
		t.Fatalf("failed to write temp config file: %v", err)
	}

	_, err := Load(configFile)
	if err == nil {
		t.Error("expected validation error for cpu_warning >= cpu_critical, got nil")
	}
}

func TestConfig_ExpandPath(t *testing.T) {
	path := ExpandPath("/absolute/path")
	if path != "/absolute/path" {
		t.Errorf("expected /absolute/path unchanged, got %s", path)
	}

	expanded := ExpandPath("~/testfile")
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, "testfile")
	if expanded != expected {
		t.Errorf("expected %s, got %s", expected, expanded)
	}
}
