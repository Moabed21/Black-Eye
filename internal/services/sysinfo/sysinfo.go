// Package sysinfo collects static and semi-static system information:
// hostname, kernel version, uptime, load average, CPU model and core count.
package sysinfo

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"blackeye/internal/config"
	"blackeye/internal/services"
)

// Snapshot holds a system info reading.
type Snapshot struct {
	Hostname      string
	KernelVersion string
	Uptime        string
	LoadAvg1      float64
	LoadAvg5      float64
	LoadAvg15     float64
	CPUModel      string
	CPUCores      int
	CPUThreads    int
	Timestamp     time.Time
}

// Service collects system info.
type Service struct {
	interval      time.Duration
	out           chan interface{}
	health        atomic.Value
	cancel        context.CancelFunc
	// Cached fields that don't change at runtime.
	cpuModel   string
	cpuCores   int
	cpuThreads int
	kernel     string
	hostname   string
}

func New(cfg config.Config) *Service {
	s := &Service{
		interval: time.Duration(cfg.Refresh.SystemdInterval) * time.Second,
		out:      make(chan interface{}, 4),
	}
	s.health.Store(services.HealthStatus{State: services.HealthOK})
	return s
}

func (s *Service) Name()   string                { return "System Info Collector" }
func (s *Service) Topic()  string                { return "sysinfo" }
func (s *Service) Output() <-chan interface{}     { return s.out }
func (s *Service) Health() services.HealthStatus { return s.health.Load().(services.HealthStatus) }
func (s *Service) Stop()   { if s.cancel != nil { s.cancel() } }
func (s *Service) Reload(cfg config.Config) {
	s.interval = time.Duration(cfg.Refresh.SystemdInterval) * time.Second
}

func (s *Service) Start(ctx context.Context) error {
	ctx, s.cancel = context.WithCancel(ctx)

	// Cache static fields once.
	s.cpuModel, s.cpuCores, s.cpuThreads = parseCPUInfo()
	s.kernel = parseKernelVersion()
	s.hostname, _ = os.Hostname()

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			snap, err := s.collect()
			if err != nil {
				s.health.Store(services.HealthStatus{State: services.HealthDegraded, Reason: err.Error()})
				continue
			}
			s.health.Store(services.HealthStatus{State: services.HealthOK})
			select {
			case s.out <- snap:
			default:
			}
		}
	}
}

func (s *Service) collect() (Snapshot, error) {
	uptime, err := parseUptime()
	if err != nil {
		return Snapshot{}, err
	}
	l1, l5, l15, err := parseLoadAvg()
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		Hostname:      s.hostname,
		KernelVersion: s.kernel,
		Uptime:        formatUptime(uptime),
		LoadAvg1:      l1,
		LoadAvg5:      l5,
		LoadAvg15:     l15,
		CPUModel:      s.cpuModel,
		CPUCores:      s.cpuCores,
		CPUThreads:    s.cpuThreads,
		Timestamp:     time.Now(),
	}, nil
}

func parseUptime() (float64, error) {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, fmt.Errorf("sysinfo: read /proc/uptime: %w", err)
	}
	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return 0, fmt.Errorf("sysinfo: empty /proc/uptime")
	}
	return strconv.ParseFloat(fields[0], 64)
}

func parseLoadAvg() (l1, l5, l15 float64, err error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0, fmt.Errorf("sysinfo: read /proc/loadavg: %w", err)
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return 0, 0, 0, fmt.Errorf("sysinfo: invalid /proc/loadavg")
	}
	l1, _ = strconv.ParseFloat(fields[0], 64)
	l5, _ = strconv.ParseFloat(fields[1], 64)
	l15, _ = strconv.ParseFloat(fields[2], 64)
	return l1, l5, l15, nil
}

func parseKernelVersion() string {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return "unknown"
	}
	fields := strings.Fields(string(data))
	if len(fields) >= 3 {
		return fields[2] // e.g. "6.5.0-45-generic"
	}
	return strings.TrimSpace(string(data))
}

func parseCPUInfo() (model string, cores, threads int) {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return "unknown", 0, 0
	}
	defer f.Close()

	physMap := make(map[string]bool)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if model == "" && strings.HasPrefix(line, "model name") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				model = strings.TrimSpace(parts[1])
			}
		}
		if strings.HasPrefix(line, "processor") {
			threads++
		}
		if strings.HasPrefix(line, "core id") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				physMap[strings.TrimSpace(parts[1])] = true
			}
		}
	}
	cores = len(physMap)
	if cores == 0 {
		cores = threads // single-socket or no core id
	}
	if model == "" {
		model = "unknown"
	}
	return model, cores, threads
}

// formatUptime converts uptime seconds into a human-readable string.
func formatUptime(secs float64) string {
	s := int(math.Round(secs))
	days := s / 86400
	s %= 86400
	hours := s / 3600
	s %= 3600
	mins := s / 60

	switch {
	case days > 0:
		return fmt.Sprintf("%d day(s) %d hour(s) %d min", days, hours, mins)
	case hours > 0:
		return fmt.Sprintf("%d hour(s) %d min", hours, mins)
	default:
		return fmt.Sprintf("%d min", mins)
	}
}
