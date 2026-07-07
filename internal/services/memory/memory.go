// Package memory collects RAM usage from /proc/meminfo.
package memory

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"blackeye/internal/config"
	"blackeye/internal/services"
)

// Snapshot holds a single memory reading.
type Snapshot struct {
	TotalGiB    float64
	UsedGiB     float64
	FreeGiB     float64
	BuffersGiB  float64
	CachedGiB   float64
	AvailableGiB float64
	UsedPercent float64
	Timestamp   time.Time
}

// Service collects memory stats from /proc/meminfo.
type Service struct {
	interval time.Duration
	out      chan interface{}
	health   atomic.Value
	cancel   context.CancelFunc
}

func New(cfg config.Config) *Service {
	s := &Service{
		interval: time.Duration(cfg.Refresh.DashboardInterval) * time.Second,
		out:      make(chan interface{}, 4),
	}
	s.health.Store(services.HealthStatus{State: services.HealthOK})
	return s
}

func (s *Service) Name()   string                { return "Memory Collector" }
func (s *Service) Topic()  string                { return "memory" }
func (s *Service) Output() <-chan interface{}     { return s.out }
func (s *Service) Health() services.HealthStatus { return s.health.Load().(services.HealthStatus) }
func (s *Service) Stop()   { if s.cancel != nil { s.cancel() } }
func (s *Service) Reload(cfg config.Config) {
	s.interval = time.Duration(cfg.Refresh.DashboardInterval) * time.Second
}

func (s *Service) Start(ctx context.Context) error {
	ctx, s.cancel = context.WithCancel(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			snap, err := collect()
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

func collect() (Snapshot, error) {
	fields, err := parseMeminfo()
	if err != nil {
		return Snapshot{}, err
	}
	const kib = 1024.0
	const gib = kib * kib * kib
	total := float64(fields["MemTotal"]) * kib
	free := float64(fields["MemFree"]) * kib
	buffers := float64(fields["Buffers"]) * kib
	cached := float64(fields["Cached"]) * kib
	available := float64(fields["MemAvailable"]) * kib
	used := total - free - buffers - cached
	if used < 0 { used = 0 }

	var pct float64
	if total > 0 {
		pct = used / total * 100
	}
	return Snapshot{
		TotalGiB:    total / gib,
		UsedGiB:     used / gib,
		FreeGiB:     free / gib,
		BuffersGiB:  buffers / gib,
		CachedGiB:   cached / gib,
		AvailableGiB: available / gib,
		UsedPercent: pct,
		Timestamp:   time.Now(),
	}, nil
}

// parseMeminfo returns key→kB value map from /proc/meminfo.
func parseMeminfo() (map[string]uint64, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return nil, fmt.Errorf("memory: open /proc/meminfo: %w", err)
	}
	defer f.Close()

	m := make(map[string]uint64)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		valStr := strings.Fields(strings.TrimSpace(parts[1]))
		if len(valStr) == 0 {
			continue
		}
		val, err := strconv.ParseUint(valStr[0], 10, 64)
		if err == nil {
			m[key] = val
		}
	}
	return m, scanner.Err()
}
