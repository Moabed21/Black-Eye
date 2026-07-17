// Package disk collects mounted filesystem usage via /proc/mounts + statfs.
// Only real filesystems are included; virtual ones (tmpfs, devtmpfs, etc.) are excluded.
package disk

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"blackeye/internal/resolver"
	"blackeye/internal/config"
	"blackeye/internal/services"
	"golang.org/x/sys/unix"
)

// DiskSnapshot holds usage info for one mounted filesystem.
type DiskSnapshot struct {
	DisplayMount  string  // "/ (Root Filesystem)"
	RawMount      string  // "/"
	Device        string  // "/dev/sda1"
	FSType        string  // "ext4"
	TotalGiB      float64
	UsedGiB       float64
	FreeGiB       float64
	UsedPercent   float64
	InodesTotal   uint64
	InodesFree    uint64
	InodesPercent float64
}

// Snapshot is the published payload.
type Snapshot struct {
	Disks     []DiskSnapshot
	Timestamp time.Time
}

// excludedFS are virtual filesystem types to ignore.
var excludedFS = map[string]bool{
	"tmpfs": true, "devtmpfs": true, "proc": true, "sysfs": true,
	"cgroup": true, "cgroup2": true, "pstore": true, "efivarfs": true,
	"bpf": true, "tracefs": true, "securityfs": true, "debugfs": true,
	"hugetlbfs": true, "mqueue": true, "fusectl": true, "overlay": true,
	"squashfs": true, "iso9660": true,
}

// Service collects disk usage.
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

func (s *Service) Name()   string                { return "Disk Collector" }
func (s *Service) Topic()  string                { return "disk" }
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
	mounts, err := parseMounts()
	if err != nil {
		return Snapshot{}, err
	}

	const gib = 1024.0 * 1024.0 * 1024.0
	var disks []DiskSnapshot
	seen := make(map[string]bool)

	for _, m := range mounts {
		if excludedFS[m.fsType] {
			continue
		}
		if seen[m.device] {
			continue // deduplicate bind mounts of same device
		}
		seen[m.device] = true

		var stat unix.Statfs_t
		if err := unix.Statfs(m.mountPoint, &stat); err != nil {
			continue // inaccessible — skip
		}

		blockSize := uint64(stat.Bsize)
		total := stat.Blocks * blockSize
		free := stat.Bfree * blockSize
		used := total - free

		var pct float64
		if total > 0 {
			pct = float64(used) / float64(total) * 100
		}

		var inodePct float64
		if stat.Files > 0 {
			inodePct = float64(stat.Files-stat.Ffree) / float64(stat.Files) * 100
		}

		disks = append(disks, DiskSnapshot{
			DisplayMount:  resolver.Mount(m.mountPoint),
			RawMount:      m.mountPoint,
			Device:        m.device,
			FSType:        m.fsType,
			TotalGiB:      float64(total) / gib,
			UsedGiB:       float64(used) / gib,
			FreeGiB:       float64(free) / gib,
			UsedPercent:   pct,
			InodesTotal:   stat.Files,
			InodesFree:    stat.Ffree,
			InodesPercent: inodePct,
		})
	}
	return Snapshot{Disks: disks, Timestamp: time.Now()}, nil
}

type mountEntry struct {
	device     string
	mountPoint string
	fsType     string
}

func parseMounts() ([]mountEntry, error) {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return nil, fmt.Errorf("disk: open /proc/mounts: %w", err)
	}
	defer f.Close()

	var entries []mountEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		entries = append(entries, mountEntry{
			device:     fields[0],
			mountPoint: fields[1],
			fsType:     fields[2],
		})
	}
	return entries, scanner.Err()
}
