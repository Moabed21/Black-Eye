// Package swap collects swap usage from /proc/meminfo.
package swap

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

// Snapshot holds a single swap usage reading.
type Snapshot struct {
	TotalGiB    float64
	UsedGiB     float64
	FreeGiB     float64
	UsedPercent float64
	Timestamp   time.Time
}

// Service collects swap stats from /proc/meminfo.
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

func (s *Service) Name()   string                { return "Swap Collector" }
func (s *Service) Topic()  string                { return "swap" }
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
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return Snapshot{}, fmt.Errorf("swap: open /proc/meminfo: %w", err)
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
		if !strings.HasPrefix(key, "Swap") {
			continue
		}
		valStr := strings.Fields(strings.TrimSpace(parts[1]))
		if len(valStr) == 0 {
			continue
		}
		val, err := strconv.ParseUint(valStr[0], 10, 64)
		if err == nil {
			m[key] = val
		}
	}
	if err := scanner.Err(); err != nil {
		return Snapshot{}, err
	}

	const gib = 1024.0 * 1024.0 * 1024.0
	const kib = 1024.0
	total := float64(m["SwapTotal"]) * kib
	free := float64(m["SwapFree"]) * kib
	used := total - free
	var pct float64
	if total > 0 {
		pct = used / total * 100
	}
	return Snapshot{
		TotalGiB:    total / gib,
		UsedGiB:     used / gib,
		FreeGiB:     free / gib,
		UsedPercent: pct,
		Timestamp:   time.Now(),
	}, nil
}
