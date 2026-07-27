// Package security performs system security audits including SUID/SGID binary scans,
// sshd_config policy parsing, auth log brute-force inspection, and world-writable audits.
package security

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"blackeye/internal/bus"
	"blackeye/internal/config"
	"blackeye/internal/services"
)

type SUIDBinary struct {
	Path        string
	Owner       string
	Permissions string
	IsRisk      bool
}

type SSHConfig struct {
	Configured             bool
	PermitRootLogin        string
	PasswordAuthentication string
	PubkeyAuthentication   string
	X11Forwarding          string
	MaxAuthTries           int
}

type AuthFailure struct {
	IP        string
	Count     int
	LastSeen  time.Time
	TargetUser string
}

type Snapshot struct {
	SUIDBinaries    []SUIDBinary
	SSHConfig       SSHConfig
	AuthFailures    []AuthFailure
	WorldWritables  []string
	SecurityRiskScore int // 0 to 100%
	Timestamp       time.Time
}

type Service struct {
	bus    *bus.Bus
	cfg    config.Config
	out    chan interface{}
	health atomic.Value
	cancel context.CancelFunc
}

func New(b *bus.Bus, cfg config.Config) *Service {
	s := &Service{
		bus: b,
		cfg: cfg,
		out: make(chan interface{}, 8),
	}
	s.health.Store(services.HealthStatus{State: services.HealthOK})
	return s
}

func (s *Service) Name() string                 { return "Security Auditor" }
func (s *Service) Topic() string                { return "security" }
func (s *Service) Output() <-chan interface{}    { return s.out }
func (s *Service) Health() services.HealthStatus{ return s.health.Load().(services.HealthStatus) }
func (s *Service) Stop()                        { if s.cancel != nil { s.cancel() } }
func (s *Service) Reload(cfg config.Config)     { s.cfg = cfg }

func (s *Service) Start(ctx context.Context) error {
	ctx, s.cancel = context.WithCancel(ctx)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	s.collectAndPublish()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s.collectAndPublish()
		}
	}
}

func (s *Service) collectAndPublish() {
	suid := ScanSUIDBinaries()
	ssh := ParseSSHConfig()
	failures := ScanAuthLogs()
	writables := ScanWorldWritables()

	riskScore := CalculateRiskScore(suid, ssh, failures)

	snap := Snapshot{
		SUIDBinaries:      suid,
		SSHConfig:         ssh,
		AuthFailures:      failures,
		WorldWritables:    writables,
		SecurityRiskScore: riskScore,
		Timestamp:         time.Now(),
	}

	select {
	case s.out <- snap:
	default:
	}
}

func ScanSUIDBinaries() []SUIDBinary {
	var results []SUIDBinary
	knownSafe := map[string]bool{
		"/usr/bin/sudo": true, "/usr/bin/passwd": true, "/usr/bin/su": true,
		"/usr/bin/mount": true, "/usr/bin/umount": true, "/usr/bin/newgrp": true,
		"/usr/bin/chfn": true, "/usr/bin/chsh": true, "/usr/bin/gpasswd": true,
		"/usr/bin/pkexec": true, "/usr/lib/polkit-1/polkit-agent-helper-1": true,
		"/usr/lib/dbus-1.0/dbus-daemon-launch-helper": true,
	}

	searchDirs := []string{"/usr/bin", "/usr/sbin", "/bin", "/sbin"}
	for _, dir := range searchDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			info, err := entry.Info()
			if err != nil {
				continue
			}
			mode := info.Mode()
			if mode&os.ModeSetuid != 0 || mode&os.ModeSetgid != 0 {
				isRisk := !knownSafe[path]
				results = append(results, SUIDBinary{
					Path:        path,
					Owner:       "root",
					Permissions: mode.String(),
					IsRisk:      isRisk,
				})
			}
		}
	}
	return results
}

func ParseSSHConfig() SSHConfig {
	cfg := SSHConfig{
		Configured:             false,
		PermitRootLogin:        "prohibit-password",
		PasswordAuthentication: "yes",
		PubkeyAuthentication:   "yes",
		X11Forwarding:          "no",
		MaxAuthTries:           6,
	}

	file, err := os.Open("/etc/ssh/sshd_config")
	if err != nil {
		return cfg
	}
	defer file.Close()

	cfg.Configured = true
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		key := strings.ToLower(parts[0])
		val := parts[1]

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
	return cfg
}

func ScanAuthLogs() []AuthFailure {
	var failures []AuthFailure
	logPaths := []string{"/var/log/auth.log", "/var/log/secure"}
	for _, path := range logPaths {
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		defer file.Close()

		ipMap := make(map[string]int)
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "Failed password") {
				fields := strings.Fields(line)
				for i, f := range fields {
					if f == "from" && i+1 < len(fields) {
						ip := fields[i+1]
						ipMap[ip]++
					}
				}
			}
		}
		for ip, count := range ipMap {
			failures = append(failures, AuthFailure{
				IP:        ip,
				Count:     count,
				LastSeen:  time.Now(),
				TargetUser: "root",
			})
		}
		break
	}
	return failures
}

func ScanWorldWritables() []string {
	var writables []string
	_ = filepath.Walk("/etc", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if info.Mode().Perm()&0002 != 0 {
			writables = append(writables, path)
		}
		return nil
	})
	return writables
}

func CalculateRiskScore(suid []SUIDBinary, ssh SSHConfig, failures []AuthFailure) int {
	score := 0
	for _, b := range suid {
		if b.IsRisk {
			score += 15
		}
	}
	if ssh.PermitRootLogin == "yes" {
		score += 30
	}
	if len(failures) > 0 {
		score += len(failures) * 10
	}
	if score > 100 {
		score = 100
	}
	return score
}
