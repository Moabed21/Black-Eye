// Package security provides SUID/SGID audit, SSH configuration inspection,
// authentication failure tracking (brute-force detection), and file security checks.
package security

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"blackeye/internal/config"
	"blackeye/internal/services"
)

// SUIDBinary holds information about a SUID/SGID executable.
type SUIDBinary struct {
	Path        string
	Owner       string
	Group       string
	Permissions string
	IsRisk      bool // true if not in standard whitelist
}

// SSHConfigHolds key sshd configuration options.
type SSHConfig struct {
	PermitRootLogin        string // "yes", "no", "prohibit-password"
	PasswordAuthentication string // "yes", "no"
	PubkeyAuthentication   string // "yes", "no"
	X11Forwarding          string // "yes", "no"
	MaxAuthTries           int
	Configured             bool
}

// BruteForceIP represents a remote IP attempting brute-force authentication.
type BruteForceIP struct {	IP          string
	FailCount   int
	LastAttempt string
}

// Snapshot is the event bus payload for security telemetry.
type Snapshot struct {
	SUIDBinaries   []SUIDBinary
	SSHConfig      SSHConfig
	BruteForceIPs  []BruteForceIP
	WorldWritables []string // Paths of world-writable files in /etc
	Timestamp      time.Time
}

// Service collects security and access control metrics.
type Service struct {
	interval time.Duration
	out      chan interface{}
	health   atomic.Value
	cancel   context.CancelFunc
}

func New(cfg config.Config) *Service {
	s := &Service{
		interval: 15 * time.Second,
		out:      make(chan interface{}, 4),
	}
	s.health.Store(services.HealthStatus{State: services.HealthOK})
	return s
}

func (s *Service) Name() string                { return "Security & Access Control Collector" }
func (s *Service) Topic() string               { return "security" }
func (s *Service) Output() <-chan interface{}  { return s.out }
func (s *Service) Health() services.HealthStatus { return s.health.Load().(services.HealthStatus) }
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

	// Initial collection
	snap := s.collect()
	select {
	case s.out <- snap:
	default:
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			snap := s.collect()
			select {
			case s.out <- snap:
			default:
			}
		}
	}
}

func (s *Service) collect() Snapshot {
	return Snapshot{
		SUIDBinaries:   scanSUIDBinaries(),
		SSHConfig:      parseSSHConfig(),
		BruteForceIPs:  scanAuthLogs(),
		WorldWritables: scanWorldWritables(),
		Timestamp:      time.Now(),
	}
}

// Standard expected Linux SUID binaries
var standardSUIDWhitelist = map[string]bool{
	"su":            true,
	"sudo":          true,
	"passwd":        true,
	"ping":          true,
	"mount":         true,
	"umount":        true,
	"pkexec":        true,
	"chsh":          true,
	"chfn":          true,
	"gpasswd":       true,
	"newgrp":        true,
	"fusermount":    true,
	"fusermount3":   true,
	"dbus-daemon-launch-helper": true,
}

func scanSUIDBinaries() []SUIDBinary {
	var list []SUIDBinary
	dirs := []string{"/usr/bin", "/usr/sbin", "/bin", "/sbin"}

	for _, d := range dirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			mode := info.Mode()
			if mode&os.ModeSetuid != 0 || mode&os.ModeSetgid != 0 {
				fullPath := filepath.Join(d, entry.Name())
				baseName := entry.Name()
				isRisk := !standardSUIDWhitelist[baseName]

				list = append(list, SUIDBinary{
					Path:        fullPath,
					Owner:       "root",
					Group:       "root",
					Permissions: mode.String(),
					IsRisk:      isRisk,
				})
			}
		}
	}
	return list
}

func parseSSHConfig() SSHConfig {
	cfg := SSHConfig{
		PermitRootLogin:        "yes", // default sshd behavior if unconfigured
		PasswordAuthentication: "yes",
		PubkeyAuthentication:   "yes",
		X11Forwarding:          "no",
		MaxAuthTries:           6,
		Configured:             false,
	}

	files := []string{"/etc/ssh/sshd_config"}
	if matches, err := filepath.Glob("/etc/ssh/sshd_config.d/*.conf"); err == nil {
		files = append(files, matches...)
	}

	for _, file := range files {
		f, err := os.Open(file)
		if err != nil {
			continue
		}
		cfg.Configured = true
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				key := strings.ToLower(fields[0])
				val := strings.ToLower(fields[1])

				switch key {
				case "permitrootlogin":
					cfg.PermitRootLogin = val
				case "passwordauthentication":
					cfg.PasswordAuthentication = val
				case "pubkeyauthentication":
					cfg.PubkeyAuthentication = val
				case "x11forwarding":
					cfg.X11Forwarding = val
				case "maxauthtries":
					if n, err := strconv.Atoi(val); err == nil {
						cfg.MaxAuthTries = n
					}
				}
			}
		}
		f.Close()
	}

	return cfg
}

var ipRegex = regexp.MustCompile(`(?:Failed|Invalid)\s+.*?\s+from\s+([0-9]+\.[0-9]+\.[0-9]+\.[0-9]+)`)

func scanAuthLogs() []BruteForceIP {
	counts := make(map[string]int)

	// Check auth.log or secure
	logFiles := []string{"/var/log/auth.log", "/var/log/secure"}
	for _, lf := range logFiles {
		f, err := os.Open(lf)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "Failed password") || strings.Contains(line, "Invalid user") {
				matches := ipRegex.FindStringSubmatch(line)
				if len(matches) > 1 {
					counts[matches[1]]++
				}
			}
		}
		f.Close()
	}

	// Fallback to journalctl if log files are empty or unreadable
	if len(counts) == 0 {
		if path, err := exec.LookPath("journalctl"); err == nil {
			cmd := exec.Command(path, "-u", "sshd", "--since", "1 hour ago", "--no-pager")
			if out, err := cmd.Output(); err == nil {
				scanner := bufio.NewScanner(strings.NewReader(string(out)))
				for scanner.Scan() {
					line := scanner.Text()
					if strings.Contains(line, "Failed password") || strings.Contains(line, "Invalid user") {
						matches := ipRegex.FindStringSubmatch(line)
						if len(matches) > 1 {
							counts[matches[1]]++
						}
					}
				}
			}
		}
	}

	var results []BruteForceIP
	for ip, count := range counts {
		if count >= 3 {
			results = append(results, BruteForceIP{
				IP:          ip,
				FailCount:   count,
				LastAttempt: "recent",
			})
		}
	}
	return results
}

func scanWorldWritables() []string {
	var list []string
	_ = filepath.Walk("/etc", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if info.Mode().Perm()&0002 != 0 {
			list = append(list, path)
		}
		return nil
	})
	if len(list) > 10 {
		list = list[:10] // limit to max 10
	}
	return list
}
