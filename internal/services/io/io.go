// Package io collects disk I/O rates (read/write MB/s) from /proc/diskstats.
package io

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

// IOSnapshot holds I/O rate info for one block device.
type IOSnapshot struct {
	Device   string  // "sda"
	ReadMBs  float64 // MB/s
	WriteMBs float64 // MB/s
}

// Snapshot is the published payload.
type Snapshot struct {
	Devices   []IOSnapshot
	Timestamp time.Time
}

type ioCounters struct {
	sectorsRead, sectorsWritten uint64
	at                          time.Time
}

// Service collects disk I/O rates.
type Service struct {
	interval time.Duration
	out      chan interface{}
	health   atomic.Value
	mu       sync.Mutex
	prev     map[string]ioCounters
	cancel   context.CancelFunc
}

func New(cfg config.Config) *Service {
	s := &Service{
		interval: time.Duration(cfg.Refresh.DashboardInterval) * time.Second,
		out:      make(chan interface{}, 4),
		prev:     make(map[string]ioCounters),
	}
	s.health.Store(services.HealthStatus{State: services.HealthOK})
	return s
}

func (s *Service) Name()   string                { return "Disk I/O Collector" }
func (s *Service) Topic()  string                { return "io" }
func (s *Service) Output() <-chan interface{}     { return s.out }
func (s *Service) Health() services.HealthStatus { return s.health.Load().(services.HealthStatus) }
func (s *Service) Stop()   { if s.cancel != nil { s.cancel() } }
func (s *Service) Reload(cfg config.Config) {
	s.interval = time.Duration(cfg.Refresh.DashboardInterval) * time.Second
}

func (s *Service) Start(ctx context.Context) error {
	ctx, s.cancel = context.WithCancel(ctx)
	curr, err := readDiskstats()
	if err == nil {
		s.mu.Lock()
		s.prev = curr
		s.mu.Unlock()
	}

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
	curr, err := readDiskstats()
	if err != nil {
		return Snapshot{}, err
	}
	now := time.Now()

	s.mu.Lock()
	prev := s.prev
	s.prev = curr
	s.mu.Unlock()

	const sectorSize = 512.0
	const mb = 1024.0 * 1024.0

	var devices []IOSnapshot
	for name, c := range curr {
		p, ok := prev[name]
		if !ok {
			continue
		}
		elapsed := now.Sub(p.at).Seconds()
		if elapsed <= 0 {
			continue
		}
		readMBs := float64(c.sectorsRead-p.sectorsRead) * sectorSize / elapsed / mb
		writeMBs := float64(c.sectorsWritten-p.sectorsWritten) * sectorSize / elapsed / mb
		devices = append(devices, IOSnapshot{
			Device:   name,
			ReadMBs:  readMBs,
			WriteMBs: writeMBs,
		})
	}
	return Snapshot{Devices: devices, Timestamp: now}, nil
}

// readDiskstats parses /proc/diskstats.
// Kernel format: major minor name ... sectors_read ... sectors_written ...
// We only track physical disks (not partitions or virtual devices).
func readDiskstats() (map[string]ioCounters, error) {
	f, err := os.Open("/proc/diskstats")
	if err != nil {
		return nil, fmt.Errorf("io: open /proc/diskstats: %w", err)
	}
	defer f.Close()

	m := make(map[string]ioCounters)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 14 {
			continue
		}
		name := fields[2]
		// Skip partitions (sdaN, nvme0n1pN) and virtual devices (loop, ram, dm-).
		if isPartitionOrVirtual(name) {
			continue
		}
		sectorsRead, _ := strconv.ParseUint(fields[5], 10, 64)
		sectorsWritten, _ := strconv.ParseUint(fields[9], 10, 64)
		m[name] = ioCounters{
			sectorsRead:    sectorsRead,
			sectorsWritten: sectorsWritten,
			at:             time.Now(),
		}
	}
	return m, scanner.Err()
}

func isPartitionOrVirtual(name string) bool {
	prefixesToSkip := []string{"loop", "ram", "dm-", "sr", "fd"}
	for _, p := range prefixesToSkip {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	// Skip sdaN, nvme0n1pN (partitions end in a digit after a letter).
	if len(name) >= 2 {
		last := name[len(name)-1]
		if last >= '0' && last <= '9' {
			prev := name[len(name)-2]
			if prev >= 'a' && prev <= 'z' {
				return true
			}
		}
	}
	return false
}
