package initsys

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"blackeye/internal/services/systemd"
)

// SystemdBackend implements InitBackend for systemd systems.
type SystemdBackend struct{}

func NewSystemd() *SystemdBackend {
	return &SystemdBackend{}
}

func (b *SystemdBackend) Name() string { return "systemd" }

func (b *SystemdBackend) ListUnits() ([]Unit, error) {
	// Query systemd D-Bus socket directly (raw D-Bus call)
	snap, err := systemd.CollectSnapshot()
	if err != nil {
		return nil, err
	}

	var units []Unit
	for _, u := range snap.Units {
		st := StateInactive
		switch u.SubState {
		case "running", "listening", "active":
			st = StateActive
		case "failed", "error":
			st = StateFailed
		}
		if u.Flagged {
			st = StateFailed
		}

		units = append(units, Unit{
			Name:         u.Name,
			Description:  u.Description,
			ActiveState:  st,
			SubState:     u.SubState,
			EnabledState: "enabled",
			DisplayState: u.DisplayState,
			Flagged:      u.Flagged,
		})
	}
	return units, nil
}

func (b *SystemdBackend) Start(name string) error {
	return execSystemctl("start", name)
}

func (b *SystemdBackend) Stop(name string) error {
	return execSystemctl("stop", name)
}

func (b *SystemdBackend) Restart(name string) error {
	return execSystemctl("restart", name)
}

func (b *SystemdBackend) Enable(name string) error {
	return execSystemctl("enable", name)
}

func (b *SystemdBackend) Disable(name string) error {
	return execSystemctl("disable", name)
}

func (b *SystemdBackend) Mask(name string) error {
	return execSystemctl("mask", name)
}

func (b *SystemdBackend) Unmask(name string) error {
	return execSystemctl("unmask", name)
}

func (b *SystemdBackend) UnitLogs(name string, lines int) ([]LogEntry, error) {
	if lines <= 0 {
		lines = 50
	}
	cmd := exec.Command("journalctl", "-u", name, "-n", strconv.Itoa(lines), "--output=json", "--no-pager")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("journalctl: %w", err)
	}

	var logEntries []LogEntry
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry struct {
			Message          string `json:"MESSAGE"`
			RealtimeTimestamp string `json:"__REALTIME_TIMESTAMP"`
			Priority         string `json:"PRIORITY"`
		}
		if err := json.Unmarshal(line, &entry); err == nil {
			ts := time.Now()
			if micro, err := strconv.ParseInt(entry.RealtimeTimestamp, 10, 64); err == nil {
				ts = time.Unix(0, micro*1000)
			}
			prio := "info"
			switch entry.Priority {
			case "0", "1", "2", "3":
				prio = "err"
			case "4":
				prio = "warn"
			}
			logEntries = append(logEntries, LogEntry{
				Timestamp: ts,
				Message:   entry.Message,
				Priority:  prio,
			})
		}
	}
	return logEntries, nil
}

func execSystemctl(action, unit string) error {
	cmd := exec.Command("systemctl", action, unit)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl %s %s: %s (%w)", action, unit, strings.TrimSpace(string(out)), err)
	}
	return nil
}
