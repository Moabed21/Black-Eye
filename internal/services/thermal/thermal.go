// Package thermal reads CPU temperatures from /sys/class/thermal/thermal_zone*/temp.
package thermal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"blackeye/internal/config"
	"blackeye/internal/services"
)

// Snapshot holds one thermal zone reading.
type ZoneSnapshot struct {
	Zone        string  // e.g. "thermal_zone0"
	Type        string  // e.g. "x86_pkg_temp"
	TempCelsius float64
	Status      string  // "OK" | "Hot" | "Critical"
}

// Snapshot is the published payload containing all thermal zones.
type Snapshot struct {
	Zones     []ZoneSnapshot
	Timestamp time.Time
}

// Service reads thermal zones.
type Service struct {
	interval    time.Duration
	warnCelsius float64
	critCelsius float64
	out         chan interface{}
	health      atomic.Value
	cancel      context.CancelFunc
}

func New(cfg config.Config) *Service {
	s := &Service{
		interval:    time.Duration(cfg.Refresh.DashboardInterval) * time.Second,
		warnCelsius: cfg.Alerts.TempWarning,
		critCelsius: cfg.Alerts.TempCritical,
		out:         make(chan interface{}, 4),
	}
	s.health.Store(services.HealthStatus{State: services.HealthOK})
	return s
}

func (s *Service) Name()   string                { return "Thermal Collector" }
func (s *Service) Topic()  string                { return "thermal" }
func (s *Service) Output() <-chan interface{}     { return s.out }
func (s *Service) Health() services.HealthStatus { return s.health.Load().(services.HealthStatus) }
func (s *Service) Stop()   { if s.cancel != nil { s.cancel() } }
func (s *Service) Reload(cfg config.Config) {
	s.interval = time.Duration(cfg.Refresh.DashboardInterval) * time.Second
	s.warnCelsius = cfg.Alerts.TempWarning
	s.critCelsius = cfg.Alerts.TempCritical
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
	pattern := "/sys/class/thermal/thermal_zone*/temp"
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return Snapshot{}, fmt.Errorf("thermal: glob: %w", err)
	}
	if len(matches) == 0 {
		// No thermal zones — return empty snapshot (not an error on VMs).
		return Snapshot{Timestamp: time.Now()}, nil
	}

	var zones []ZoneSnapshot
	for _, tempPath := range matches {
		zoneDir := filepath.Dir(tempPath)
		zoneName := filepath.Base(zoneDir)

		zoneType := readSingleLine(filepath.Join(zoneDir, "type"))
		if zoneType == "" {
			zoneType = zoneName
		}

		data, err := os.ReadFile(tempPath)
		if err != nil {
			continue
		}
		milliC, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
		if err != nil {
			continue
		}
		celsius := milliC / 1000.0

		status := "OK"
		if celsius >= s.critCelsius {
			status = "Critical"
		} else if celsius >= s.warnCelsius {
			status = "Hot"
		}

		zones = append(zones, ZoneSnapshot{
			Zone:        zoneName,
			Type:        zoneType,
			TempCelsius: celsius,
			Status:      status,
		})
	}
	return Snapshot{Zones: zones, Timestamp: time.Now()}, nil
}

func readSingleLine(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
