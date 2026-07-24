// Package cpu collects CPU usage statistics from /proc/stat.
// It computes per-core and total usage as a percentage delta between ticks.
package cpu

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"blackeye/internal/config"
	"blackeye/internal/services"
)

// Snapshot holds a single CPU usage reading.
type Snapshot struct {
	TotalPercent float64
	CorePercent  []float64
	CoreFreqs    []CoreFreq
	Timestamp    time.Time
}

// cpuTimes holds raw jiffie counts for one CPU line from /proc/stat.
type cpuTimes struct {
	user, nice, system, idle, iowait, irq, softirq, steal uint64
}

func (t cpuTimes) total() uint64 {
	return t.user + t.nice + t.system + t.idle + t.iowait + t.irq + t.softirq + t.steal
}
func (t cpuTimes) active() uint64 { return t.total() - t.idle - t.iowait }

// Service collects CPU usage from /proc/stat.
type Service struct {
	interval time.Duration
	out      chan interface{}
	health   atomic.Value
	mu       sync.Mutex
	prev     []cpuTimes // previous tick: [0] = total, [1..n] = per core
	cancel   context.CancelFunc
}

// New creates a CPU collector with the given config.
func New(cfg config.Config) *Service {
	s := &Service{
		interval: time.Duration(cfg.Refresh.DashboardInterval) * time.Second,
		out:      make(chan interface{}, 4),
	}
	s.health.Store(services.HealthStatus{State: services.HealthOK})
	return s
}

func (s *Service) Name()               string                   { return "CPU Collector" }
func (s *Service) Topic()              string                   { return "cpu" }
func (s *Service) Output()             <-chan interface{}        { return s.out }
func (s *Service) Health()             services.HealthStatus    { return s.health.Load().(services.HealthStatus) }

func (s *Service) Start(ctx context.Context) error {
	ctx, s.cancel = context.WithCancel(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// Seed the previous reading.
	times, err := readStat()
	if err != nil {
		s.setHealth(services.HealthDown, err.Error())
		return err
	}
	s.mu.Lock()
	s.prev = times
	s.mu.Unlock()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			snap, err := s.collect()
			if err != nil {
				s.setHealth(services.HealthDegraded, err.Error())
				continue
			}
			s.setHealth(services.HealthOK, "")
			select {
			case s.out <- snap:
			default:
			}
		}
	}
}

func (s *Service) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *Service) Reload(cfg config.Config) {
	s.mu.Lock()
	s.interval = time.Duration(cfg.Refresh.DashboardInterval) * time.Second
	s.mu.Unlock()
}

func (s *Service) collect() (Snapshot, error) {
	curr, err := readStat()
	if err != nil {
		return Snapshot{}, err
	}

	s.mu.Lock()
	prev := s.prev
	s.prev = curr
	s.mu.Unlock()

	if len(curr) == 0 || len(prev) == 0 {
		return Snapshot{}, fmt.Errorf("cpu: empty /proc/stat")
	}

	snap := Snapshot{
		TotalPercent: pct(prev[0], curr[0]),
		Timestamp:    time.Now(),
	}
	// Per-core (skip index 0 which is the aggregate "cpu" line).
	coreCount := len(curr) - 1
	if coreCount > 0 {
		for i := 1; i < len(curr) && i < len(prev); i++ {
			snap.CorePercent = append(snap.CorePercent, pct(prev[i], curr[i]))
		}
		snap.CoreFreqs = readCoreFreqs(coreCount)
	}
	return snap, nil
}

// pct computes the usage percentage between two cpuTimes readings.
func pct(prev, curr cpuTimes) float64 {
	deltaTot := float64(curr.total() - prev.total())
	deltaAct := float64(curr.active() - prev.active())
	if deltaTot <= 0 {
		return 0
	}
	p := deltaAct / deltaTot * 100
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	return p
}

// readStat parses /proc/stat and returns one cpuTimes per CPU line.
// Index 0 is the aggregate; index 1..n are per-core.
func readStat() ([]cpuTimes, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return nil, fmt.Errorf("cpu: open /proc/stat: %w", err)
	}
	defer f.Close()

	var times []cpuTimes
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}
		u0, err0 := strconv.ParseUint(fields[1], 10, 64)
		u1, err1 := strconv.ParseUint(fields[2], 10, 64)
		u2, err2 := strconv.ParseUint(fields[3], 10, 64)
		u3, err3 := strconv.ParseUint(fields[4], 10, 64)
		u4, err4 := strconv.ParseUint(fields[5], 10, 64)
		u5, err5 := strconv.ParseUint(fields[6], 10, 64)
		u6, err6 := strconv.ParseUint(fields[7], 10, 64)
		var u7 uint64
		if len(fields) > 8 {
			u7, _ = strconv.ParseUint(fields[8], 10, 64)
		}
		if err0 == nil && err1 == nil && err2 == nil && err3 == nil && err4 == nil && err5 == nil && err6 == nil {
			times = append(times, cpuTimes{
				user: u0, nice: u1, system: u2, idle: u3,
				iowait: u4, irq: u5, softirq: u6, steal: u7,
			})
		}
	}
	return times, scanner.Err()
}

func (s *Service) setHealth(state services.HealthState, reason string) {
	s.health.Store(services.HealthStatus{State: state, Reason: reason})
}
