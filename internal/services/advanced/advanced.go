// Package advanced collects SSH sessions, Cron/Timer entries, and storage topology.
package advanced

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"blackeye/internal/config"
	"blackeye/internal/services"
)

// SSHSession represents an active SSH login session.
type SSHSession struct {
	User      string
	TTY       string
	FromIP    string
	LoginTime string
	PID       int
}

// CronEntry represents a scheduled cron job or systemd timer.
type CronEntry struct {
	User     string
	Schedule string // e.g. "0 * * * *" or "daily"
	Command  string
	Source   string // "crontab", "cron.d", "systemd-timer"
}

// StorageVolume represents a physical or logical volume.
type StorageVolume struct {
	Name       string // e.g. "vg0-root" or "sda1"
	Type       string // "LVM LV", "Partition", "RAID"
	Size       string
	MountPoint string
}

// Snapshot is the event bus payload.
type Snapshot struct {
	SSHSessions []SSHSession
	CronEntries []CronEntry
	Volumes     []StorageVolume
	Available   bool
	Error       string
	Timestamp   time.Time
}

// Service collects advanced system platform metrics.
type Service struct {
	interval time.Duration
	out      chan interface{}
	cancel   context.CancelFunc
}

func New(cfg config.Config) *Service {
	return &Service{
		interval: 15 * time.Second,
		out:      make(chan interface{}, 4),
	}
}

func (s *Service) Name() string { return "Advanced Platform Collector" }
func (s *Service) Topic() string { return "advanced" }
func (s *Service) Output() <-chan interface{} { return s.out }
func (s *Service) Health() services.HealthStatus {
	return services.HealthStatus{State: services.HealthOK}
}
func (s *Service) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
}
func (s *Service) Reload(cfg config.Config) {}

func (s *Service) Start(ctx context.Context) error {
	ctx, s.cancel = context.WithCancel(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// Initial snapshot
	select {
	case s.out <- s.collect():
	default:
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			select {
			case s.out <- s.collect():
			default:
			}
		}
	}
}

func (s *Service) collect() Snapshot {
	sshList, _ := parseSSHSessions()
	cronList, _ := parseCronEntries()
	vols, _ := parseVolumes()

	return Snapshot{
		SSHSessions: sshList,
		CronEntries: cronList,
		Volumes:     vols,
		Available:   true,
		Timestamp:   time.Now(),
	}
}

func parseSSHSessions() ([]SSHSession, error) {
	cmd := exec.Command("who", "-u")
	out, err := cmd.Output()
	if err != nil {
		return nil, nil
	}

	var sessions []SSHSession
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 5 {
			user := fields[0]
			tty := fields[1]
			login := fields[2] + " " + fields[3]
			pid := 0
			fromIP := "local"

			for _, f := range fields[4:] {
				if strings.HasPrefix(f, "(") && strings.HasSuffix(f, ")") {
					fromIP = strings.Trim(f, "()")
				} else if p, err := strconv.Atoi(f); err == nil && pid == 0 {
					pid = p
				}
			}

			sessions = append(sessions, SSHSession{
				User:      user,
				TTY:       tty,
				FromIP:    fromIP,
				LoginTime: login,
				PID:       pid,
			})
		}
	}
	return sessions, nil
}

func parseCronEntries() ([]CronEntry, error) {
	var entries []CronEntry

	// Parse /etc/cron.d/ files
	if matches, err := filepath.Glob("/etc/cron.d/*"); err == nil {
		for _, file := range matches {
			if f, err := os.Open(file); err == nil {
				scanner := bufio.NewScanner(f)
				for scanner.Scan() {
					line := strings.TrimSpace(scanner.Text())
					if line == "" || strings.HasPrefix(line, "#") || strings.Contains(line, "=") {
						continue
					}
					fields := strings.Fields(line)
					if len(fields) >= 6 {
						entries = append(entries, CronEntry{
							Schedule: strings.Join(fields[:5], " "),
							User:     fields[5],
							Command:  strings.Join(fields[6:], " "),
							Source:   filepath.Base(file),
						})
					}
				}
				f.Close()
			}
		}
	}

	return entries, nil
}

func parseVolumes() ([]StorageVolume, error) {
	var vols []StorageVolume

	// Check for LVM logical volumes via lvs if present
	if _, err := exec.LookPath("lvs"); err == nil {
		cmd := exec.Command("lvs", "--noheadings", "-o", "lv_name,vg_name,lv_size")
		if out, err := cmd.Output(); err == nil {
			scanner := bufio.NewScanner(strings.NewReader(string(out)))
			for scanner.Scan() {
				fields := strings.Fields(scanner.Text())
				if len(fields) >= 3 {
					vols = append(vols, StorageVolume{
						Name:       fields[1] + "/" + fields[0],
						Type:       "LVM Logical Volume",
						Size:       fields[2],
						MountPoint: "/dev/" + fields[1] + "/" + fields[0],
					})
				}
			}
		}
	}

	return vols, nil
}
