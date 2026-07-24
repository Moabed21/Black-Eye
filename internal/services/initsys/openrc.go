package initsys

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// OpenRCBackend implements InitBackend for OpenRC systems (Alpine, Gentoo, Void OpenRC).
type OpenRCBackend struct{}

func NewOpenRC() *OpenRCBackend {
	return &OpenRCBackend{}
}

func (b *OpenRCBackend) Name() string { return "openrc" }

func (b *OpenRCBackend) ListUnits() ([]Unit, error) {
	// Read init.d scripts
	entries, err := os.ReadDir("/etc/init.d")
	if err != nil {
		return nil, fmt.Errorf("openrc: readdir /etc/init.d: %w", err)
	}

	// Read runlevel status from rc-status if available
	activeMap := make(map[string]string)
	if out, err := exec.Command("rc-status", "-a").Output(); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(out)))
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) >= 2 {
				name := fields[0]
				status := strings.ToLower(fields[len(fields)-1])
				status = strings.Trim(status, "[]")
				activeMap[name] = status
			}
		}
	}

	var units []Unit
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || e.Name() == "functions.sh" {
			continue
		}
		name := e.Name()
		subState := "stopped"
		st := StateInactive

		if status, ok := activeMap[name]; ok {
			subState = status
			if strings.Contains(status, "started") || strings.Contains(status, "running") {
				st = StateActive
			} else if strings.Contains(status, "crashed") || strings.Contains(status, "failed") {
				st = StateFailed
			}
		} else {
			// Check runtime dir /run/openrc/started/
			if _, err := os.Stat(filepath.Join("/run/openrc/started", name)); err == nil {
				st = StateActive
				subState = "started"
			} else if _, err := os.Stat(filepath.Join("/run/openrc/crashed", name)); err == nil {
				st = StateFailed
				subState = "crashed"
			}
		}

		units = append(units, Unit{
			Name:         name,
			Description:  "OpenRC init script",
			ActiveState:  st,
			SubState:     subState,
			EnabledState: "enabled",
			DisplayState: fmt.Sprintf("%s (%s)", st, subState),
			Flagged:      st == StateFailed,
		})
	}
	return units, nil
}

func (b *OpenRCBackend) Start(name string) error {
	return execRCService(name, "start")
}

func (b *OpenRCBackend) Stop(name string) error {
	return execRCService(name, "stop")
}

func (b *OpenRCBackend) Restart(name string) error {
	return execRCService(name, "restart")
}

func (b *OpenRCBackend) Enable(name string) error {
	cmd := exec.Command("rc-update", "add", name, "default")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("rc-update add %s: %s (%w)", name, strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (b *OpenRCBackend) Disable(name string) error {
	cmd := exec.Command("rc-update", "del", name, "default")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("rc-update del %s: %s (%w)", name, strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (b *OpenRCBackend) Mask(name string) error {
	return fmt.Errorf("masking is not supported on OpenRC")
}

func (b *OpenRCBackend) Unmask(name string) error {
	return fmt.Errorf("unmasking is not supported on OpenRC")
}

func (b *OpenRCBackend) UnitLogs(name string, lines int) ([]LogEntry, error) {
	if lines <= 0 {
		lines = 50
	}
	// Tail /var/log/messages or /var/log/syslog for service entries
	logPath := "/var/log/messages"
	if _, err := os.Stat("/var/log/syslog"); err == nil {
		logPath = "/var/log/syslog"
	}

	f, err := os.Open(logPath)
	if err != nil {
		return nil, fmt.Errorf("openrc log: %w", err)
	}
	defer f.Close()

	var matched []LogEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, name) {
			prio := "info"
			if strings.Contains(strings.ToLower(line), "error") || strings.Contains(strings.ToLower(line), "fail") {
				prio = "err"
			} else if strings.Contains(strings.ToLower(line), "warn") {
				prio = "warn"
			}
			matched = append(matched, LogEntry{
				Timestamp: time.Now(),
				Message:   line,
				Priority:  prio,
			})
		}
	}
	if len(matched) > lines {
		matched = matched[len(matched)-lines:]
	}
	return matched, nil
}

func execRCService(name, action string) error {
	// Try rc-service first, fallback to /etc/init.d/<name>
	var cmd *exec.Cmd
	if _, err := exec.LookPath("rc-service"); err == nil {
		cmd = exec.Command("rc-service", name, action)
	} else {
		cmd = exec.Command(filepath.Join("/etc/init.d", name), action)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("service %s %s: %s (%w)", name, action, strings.TrimSpace(string(out)), err)
	}
	return nil
}
