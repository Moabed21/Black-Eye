// Package config loads and validates the BlackEye TOML configuration file.
// Defaults are applied when the file does not exist. A clear error is returned
// if the file exists but contains invalid values.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// DefaultPath is the canonical config file location.
const DefaultPath = "~/.config/blackeye/config.toml"

// Config holds all runtime configuration for BlackEye.
type Config struct {
	Refresh RefreshConfig `toml:"refresh"`
	Alerts  AlertsConfig  `toml:"alerts"`
	Ports   PortsConfig   `toml:"ports"`
	Docker  DockerConfig  `toml:"docker"`
	Audit   AuditConfig   `toml:"audit"`
}

// RefreshConfig controls per-service polling intervals (in seconds).
type RefreshConfig struct {
	DashboardInterval int  `toml:"dashboard_interval"`
	ProcessInterval   int  `toml:"process_interval"`
	PortsInterval     int  `toml:"ports_interval"`
	DockerInterval    int  `toml:"docker_interval"`
	SystemdInterval   int  `toml:"systemd_interval"`
	DmesgStreaming    bool `toml:"dmesg_streaming"`
}

// CustomRule defines user-configured threshold alerts.
type CustomRule struct {
	ID       string  `toml:"id"`
	Metric   string  `toml:"metric"`   // "cpu", "ram", "swap", "disk", "temp", "auth_failures"
	Operator string  `toml:"operator"` // ">", ">=", "<", "=="
	Value    float64 `toml:"value"`
	Severity string  `toml:"severity"` // "warning", "critical"
	Label    string  `toml:"label"`
}

func (r CustomRule) Validate() error {
	validMetrics := map[string]bool{"cpu": true, "ram": true, "swap": true, "disk": true, "temp": true, "auth_failures": true}
	if !validMetrics[r.Metric] {
		return fmt.Errorf("invalid metric: %s", r.Metric)
	}
	validOps := map[string]bool{">": true, ">=": true, "<": true, "==": true}
	if !validOps[r.Operator] {
		return fmt.Errorf("invalid operator: %s", r.Operator)
	}

	switch r.Metric {
	case "cpu", "ram", "swap", "disk":
		if r.Value < 1.0 || r.Value > 100.0 {
			return fmt.Errorf("%s percentage threshold must be between 1%% and 100%%", r.Metric)
		}
	case "temp":
		if r.Value < 20.0 || r.Value > 120.0 {
			return fmt.Errorf("temperature threshold must be between 20°C and 120°C")
		}
	case "auth_failures":
		if r.Value < 1.0 || r.Value > 1000.0 {
			return fmt.Errorf("auth failures count must be between 1 and 1000")
		}
	}
	return nil
}

// AlertsConfig defines percentage/degree thresholds for color coding.
type AlertsConfig struct {
	CPUWarning    float64      `toml:"cpu_warning"`
	CPUCritical   float64      `toml:"cpu_critical"`
	MemoryWarning  float64      `toml:"memory_warning"`
	MemoryCritical float64      `toml:"memory_critical"`
	DiskWarning    float64      `toml:"disk_warning"`
	DiskCritical   float64      `toml:"disk_critical"`
	TempWarning    float64      `toml:"temp_warning"`
	TempCritical   float64      `toml:"temp_critical"`
	CustomRules    []CustomRule `toml:"custom_rules"`
}

// PortsConfig controls port highlighting behavior.
type PortsConfig struct {
	TrustedPorts []uint16 `toml:"trusted_ports"`
}

// DockerConfig controls Docker SDK connection.
type DockerConfig struct {
	Socket string `toml:"socket"`
}

// AuditConfig controls the audit log location.
type AuditConfig struct {
	LogPath string `toml:"log_path"`
}

// ServiceConfig is a per-service view of Config, passed to Service.Reload().
type ServiceConfig struct {
	Interval int
	Alerts   AlertsConfig
	Ports    PortsConfig
	Docker   DockerConfig
	Audit    AuditConfig
}

// Defaults returns a Config with all hardcoded default values.
// The application starts normally with these if no config file is found.
func Defaults() Config {
	return Config{
		Refresh: RefreshConfig{
			DashboardInterval: 2,
			ProcessInterval:   3,
			PortsInterval:     5,
			DockerInterval:    3,
			SystemdInterval:   5,
			DmesgStreaming:    true,
		},
		Alerts: AlertsConfig{
			CPUWarning:    70.0,
			CPUCritical:   85.0,
			MemoryWarning:  70.0,
			MemoryCritical: 90.0,
			DiskWarning:    80.0,
			DiskCritical:   95.0,
			TempWarning:    70.0,
			TempCritical:   85.0,
		},
		Ports: PortsConfig{
			TrustedPorts: []uint16{22, 80, 443, 5432},
		},
		Docker: DockerConfig{
			Socket: "/var/run/docker.sock",
		},
		Audit: AuditConfig{
			LogPath: "~/.local/share/blackeye/audit.log",
		},
	}
}

// Load reads the config file at path, applying defaults for any missing fields.
// If the file does not exist, Defaults() is returned with no error.
// If the file exists but is malformed, a descriptive error is returned.
func Load(path string) (Config, error) {
	cfg := Defaults()

	expanded := expandHome(path)
	data, err := os.ReadFile(expanded)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// No config file — use defaults silently.
			return cfg, nil
		}
		return cfg, fmt.Errorf("config: cannot read %q: %w", expanded, err)
	}

	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("config: invalid TOML in %q: %w", expanded, err)
	}

	if err := validate(cfg); err != nil {
		return cfg, fmt.Errorf("config: validation failed: %w", err)
	}

	return cfg, nil
}

// validate checks that all config values are within acceptable ranges.
func validate(cfg Config) error {
	r := cfg.Refresh
	if r.DashboardInterval < 1 {
		return fmt.Errorf("refresh.dashboard_interval must be ≥ 1, got %d", r.DashboardInterval)
	}
	if r.ProcessInterval < 1 {
		return fmt.Errorf("refresh.process_interval must be ≥ 1, got %d", r.ProcessInterval)
	}
	if r.PortsInterval < 1 {
		return fmt.Errorf("refresh.ports_interval must be ≥ 1, got %d", r.PortsInterval)
	}
	if r.DockerInterval < 1 {
		return fmt.Errorf("refresh.docker_interval must be ≥ 1, got %d", r.DockerInterval)
	}

	a := cfg.Alerts
	if a.CPUWarning < 0 || a.CPUWarning > 100 {
		return fmt.Errorf("alerts.cpu_warning must be 0–100, got %.1f", a.CPUWarning)
	}
	if a.CPUCritical < 0 || a.CPUCritical > 100 {
		return fmt.Errorf("alerts.cpu_critical must be 0–100, got %.1f", a.CPUCritical)
	}
	if a.CPUWarning >= a.CPUCritical {
		return fmt.Errorf("alerts.cpu_warning (%.1f) must be < cpu_critical (%.1f)", a.CPUWarning, a.CPUCritical)
	}

	if a.MemoryWarning < 0 || a.MemoryWarning > 100 {
		return fmt.Errorf("alerts.memory_warning must be 0–100, got %.1f", a.MemoryWarning)
	}
	if a.MemoryCritical < 0 || a.MemoryCritical > 100 {
		return fmt.Errorf("alerts.memory_critical must be 0–100, got %.1f", a.MemoryCritical)
	}
	if a.MemoryWarning >= a.MemoryCritical {
		return fmt.Errorf("alerts.memory_warning (%.1f) must be < memory_critical (%.1f)", a.MemoryWarning, a.MemoryCritical)
	}

	if a.DiskWarning < 0 || a.DiskWarning > 100 {
		return fmt.Errorf("alerts.disk_warning must be 0–100, got %.1f", a.DiskWarning)
	}
	if a.DiskCritical < 0 || a.DiskCritical > 100 {
		return fmt.Errorf("alerts.disk_critical must be 0–100, got %.1f", a.DiskCritical)
	}
	if a.DiskWarning >= a.DiskCritical {
		return fmt.Errorf("alerts.disk_warning (%.1f) must be < disk_critical (%.1f)", a.DiskWarning, a.DiskCritical)
	}

	return nil
}

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(path string) string {
	if len(path) == 0 || path[0] != '~' {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}

// ExpandPath is the public version of expandHome for use by other packages.
func ExpandPath(path string) string {
	return expandHome(path)
}
