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

// SysVinitBackend implements InitBackend for SysVinit systems (Devuan, legacy Debian/RHEL).
type SysVinitBackend struct{}

func NewSysVinit() *SysVinitBackend {
	return &SysVinitBackend{}
}

func (b *SysVinitBackend) Name() string { return "sysvinit" }

func (b *SysVinitBackend) ListUnits() ([]Unit, error) {
	entries, err := os.ReadDir("/etc/init.d")
	if err != nil {
		return nil, fmt.Errorf("sysvinit: readdir /etc/init.d: %w", err)
	}

	var units []Unit
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") || name == "README" || name == "skeleton" {
			continue
		}

		// Check status using service command or init script
		subState := "unknown"
		st := StateInactive

		cmd := exec.Command("service", name, "status")
		out, err := cmd.CombinedOutput()
		outStr := strings.ToLower(string(out))

		if err == nil && (strings.Contains(outStr, "running") || strings.Contains(outStr, "is active")) {
			st = StateActive
			subState = "running"
		} else if strings.Contains(outStr, "stopped") || strings.Contains(outStr, "is not running") {
			st = StateInactive
			subState = "stopped"
		} else if strings.Contains(outStr, "failed") {
			st = StateFailed
			subState = "failed"
		}

		units = append(units, Unit{
			Name:         name,
			Description:  "SysVinit init script",
			ActiveState:  st,
			SubState:     subState,
			EnabledState: "enabled",
			DisplayState: fmt.Sprintf("%s (%s)", st, subState),
			Flagged:      st == StateFailed,
		})
	}
	return units, nil
}

func (b *SysVinitBackend) Start(name string) error {
	return execSysVService(name, "start")
}

func (b *SysVinitBackend) Stop(name string) error {
	return execSysVService(name, "stop")
}

func (b *SysVinitBackend) Restart(name string) error {
	return execSysVService(name, "restart")
}

func (b *SysVinitBackend) Enable(name string) error {
	if _, err := exec.LookPath("update-rc.d"); err == nil {
		cmd := exec.Command("update-rc.d", name, "enable")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("update-rc.d %s enable: %s (%w)", name, strings.TrimSpace(string(out)), err)
		}
		return nil
	} else if _, err := exec.LookPath("chkconfig"); err == nil {
		cmd := exec.Command("chkconfig", name, "on")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("chkconfig %s on: %s (%w)", name, strings.TrimSpace(string(out)), err)
		}
		return nil
	}
	return fmt.Errorf("no enable tool (update-rc.d/chkconfig) found")
}

func (b *SysVinitBackend) Disable(name string) error {
	if _, err := exec.LookPath("update-rc.d"); err == nil {
		cmd := exec.Command("update-rc.d", name, "disable")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("update-rc.d %s disable: %s (%w)", name, strings.TrimSpace(string(out)), err)
		}
		return nil
	} else if _, err := exec.LookPath("chkconfig"); err == nil {
		cmd := exec.Command("chkconfig", name, "off")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("chkconfig %s off: %s (%w)", name, strings.TrimSpace(string(out)), err)
		}
		return nil
	}
	return fmt.Errorf("no disable tool (update-rc.d/chkconfig) found")
}

func (b *SysVinitBackend) Mask(name string) error {
	return fmt.Errorf("masking is not supported on SysVinit")
}

func (b *SysVinitBackend) Unmask(name string) error {
	return fmt.Errorf("unmasking is not supported on SysVinit")
}

func (b *SysVinitBackend) UnitLogs(name string, lines int) ([]LogEntry, error) {
	if lines <= 0 {
		lines = 50
	}
	logPath := "/var/log/syslog"
	if _, err := os.Stat(logPath); err != nil {
		logPath = "/var/log/messages"
	}

	f, err := os.Open(logPath)
	if err != nil {
		return nil, fmt.Errorf("sysvinit log: %w", err)
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

func execSysVService(name, action string) error {
	var cmd *exec.Cmd
	if _, err := exec.LookPath("service"); err == nil {
		cmd = exec.Command("service", name, action)
	} else {
		cmd = exec.Command(filepath.Join("/etc/init.d", name), action)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("service %s %s: %s (%w)", name, action, strings.TrimSpace(string(out)), err)
	}
	return nil
}
