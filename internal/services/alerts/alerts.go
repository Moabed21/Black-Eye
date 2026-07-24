// Package alerts monitors system metrics and generates alerts when thresholds
// are breached. It subscribes to CPU, memory, disk, and thermal bus topics
// and publishes alert events.
package alerts

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"blackeye/internal/bus"
	"blackeye/internal/config"
	"blackeye/internal/services"
	"blackeye/internal/services/cpu"
	"blackeye/internal/services/disk"
	"blackeye/internal/services/memory"
	"blackeye/internal/services/thermal"
)

// AlertLevel indicates the severity of an alert.
type AlertLevel string

const (
	AlertWarning  AlertLevel = "warning"
	AlertCritical AlertLevel = "critical"
)

// Alert represents a single threshold breach notification.
type Alert struct {
	Level     AlertLevel
	Source    string    // e.g., "CPU", "Memory", "Disk /", "Temperature"
	Message   string    // human-readable message
	Value     float64   // the actual value that triggered
	Threshold float64   // the threshold that was breached
	Timestamp time.Time
}

// Snapshot is the published payload — a batch of currently active alerts.
type Snapshot struct {
	Active    []Alert
	Timestamp time.Time
}

// Service watches metrics and publishes alerts.
type Service struct {
	bus      *bus.Bus
	cfg      config.Config
	out      chan interface{}
	health   atomic.Value
	cancel   context.CancelFunc

	mu      sync.Mutex
	active  map[string]Alert // keyed by source
	subCPU  <-chan interface{}
	subMem  <-chan interface{}
	subDisk <-chan interface{}
	subTemp <-chan interface{}
}

// New creates a new alert monitoring service.
func New(b *bus.Bus, cfg config.Config) *Service {
	s := &Service{
		bus:    b,
		cfg:    cfg,
		out:    make(chan interface{}, 8),
		active: make(map[string]Alert),
	}
	s.health.Store(services.HealthStatus{State: services.HealthOK})
	s.subCPU = b.Subscribe("cpu")
	s.subMem = b.Subscribe("memory")
	s.subDisk = b.Subscribe("disk")
	s.subTemp = b.Subscribe("thermal")
	return s
}

func (s *Service) Name() string                   { return "Alert Monitor" }
func (s *Service) Topic() string                   { return "alerts" }
func (s *Service) Output() <-chan interface{}       { return s.out }
func (s *Service) Health() services.HealthStatus   { return s.health.Load().(services.HealthStatus) }
func (s *Service) Stop()                           { if s.cancel != nil { s.cancel() } }
func (s *Service) Reload(cfg config.Config)        { s.mu.Lock(); s.cfg = cfg; s.mu.Unlock() }

func (s *Service) Start(ctx context.Context) error {
	ctx, s.cancel = context.WithCancel(ctx)

	for {
		select {
		case <-ctx.Done():
			return nil

		case v := <-s.subCPU:
			if snap, ok := v.(cpu.Snapshot); ok {
				s.checkCPU(snap)
			}

		case v := <-s.subMem:
			if snap, ok := v.(memory.Snapshot); ok {
				s.checkMemory(snap)
			}

		case v := <-s.subDisk:
			if snap, ok := v.(disk.Snapshot); ok {
				s.checkDisk(snap)
			}

		case v := <-s.subTemp:
			if snap, ok := v.(thermal.Snapshot); ok {
				s.checkThermal(snap)
			}
		}
	}
}

func (s *Service) checkCPU(snap cpu.Snapshot) {
	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()

	key := "CPU"
	pct := snap.TotalPercent

	if pct >= cfg.Alerts.CPUCritical {
		s.setAlert(key, Alert{
			Level:     AlertCritical,
			Source:    key,
			Message:   fmt.Sprintf("CPU at %.1f%% (critical ≥ %.0f%%)", pct, cfg.Alerts.CPUCritical),
			Value:     pct,
			Threshold: cfg.Alerts.CPUCritical,
			Timestamp: time.Now(),
		})
	} else if pct >= cfg.Alerts.CPUWarning {
		s.setAlert(key, Alert{
			Level:     AlertWarning,
			Source:    key,
			Message:   fmt.Sprintf("CPU at %.1f%% (warning ≥ %.0f%%)", pct, cfg.Alerts.CPUWarning),
			Value:     pct,
			Threshold: cfg.Alerts.CPUWarning,
			Timestamp: time.Now(),
		})
	} else {
		s.clearAlert(key)
	}
}

func (s *Service) checkMemory(snap memory.Snapshot) {
	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()

	key := "Memory"
	pct := snap.UsedPercent

	if pct >= cfg.Alerts.MemoryCritical {
		s.setAlert(key, Alert{
			Level:     AlertCritical,
			Source:    key,
			Message:   fmt.Sprintf("Memory at %.1f%% (critical ≥ %.0f%%)", pct, cfg.Alerts.MemoryCritical),
			Value:     pct,
			Threshold: cfg.Alerts.MemoryCritical,
			Timestamp: time.Now(),
		})
	} else if pct >= cfg.Alerts.MemoryWarning {
		s.setAlert(key, Alert{
			Level:     AlertWarning,
			Source:    key,
			Message:   fmt.Sprintf("Memory at %.1f%% (warning ≥ %.0f%%)", pct, cfg.Alerts.MemoryWarning),
			Value:     pct,
			Threshold: cfg.Alerts.MemoryWarning,
			Timestamp: time.Now(),
		})
	} else {
		s.clearAlert(key)
	}
}

func (s *Service) checkDisk(snap disk.Snapshot) {
	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()

	for _, d := range snap.Disks {
		key := fmt.Sprintf("Disk %s", d.DisplayMount)
		pct := d.UsedPercent

		if pct >= cfg.Alerts.DiskCritical {
			s.setAlert(key, Alert{
				Level:     AlertCritical,
				Source:    key,
				Message:   fmt.Sprintf("%s at %.1f%% (critical ≥ %.0f%%)", d.DisplayMount, pct, cfg.Alerts.DiskCritical),
				Value:     pct,
				Threshold: cfg.Alerts.DiskCritical,
				Timestamp: time.Now(),
			})
		} else if pct >= cfg.Alerts.DiskWarning {
			s.setAlert(key, Alert{
				Level:     AlertWarning,
				Source:    key,
				Message:   fmt.Sprintf("%s at %.1f%% (warning ≥ %.0f%%)", d.DisplayMount, pct, cfg.Alerts.DiskWarning),
				Value:     pct,
				Threshold: cfg.Alerts.DiskWarning,
				Timestamp: time.Now(),
			})
		} else {
			s.clearAlert(key)
		}
	}
}

func (s *Service) checkThermal(snap thermal.Snapshot) {
	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()

	for _, z := range snap.Zones {
		key := fmt.Sprintf("Temp %s", z.Type)
		temp := z.TempCelsius

		if temp >= cfg.Alerts.TempCritical {
			s.setAlert(key, Alert{
				Level:     AlertCritical,
				Source:    key,
				Message:   fmt.Sprintf("%s at %.0f°C (critical ≥ %.0f°C)", z.Type, temp, cfg.Alerts.TempCritical),
				Value:     temp,
				Threshold: cfg.Alerts.TempCritical,
				Timestamp: time.Now(),
			})
		} else if temp >= cfg.Alerts.TempWarning {
			s.setAlert(key, Alert{
				Level:     AlertWarning,
				Source:    key,
				Message:   fmt.Sprintf("%s at %.0f°C (warning ≥ %.0f°C)", z.Type, temp, cfg.Alerts.TempWarning),
				Value:     temp,
				Threshold: cfg.Alerts.TempWarning,
				Timestamp: time.Now(),
			})
		} else {
			s.clearAlert(key)
		}
	}
}

func (s *Service) setAlert(key string, alert Alert) {
	s.mu.Lock()
	s.active[key] = alert
	s.publishLocked()
	s.mu.Unlock()
}

func (s *Service) clearAlert(key string) {
	s.mu.Lock()
	if _, exists := s.active[key]; exists {
		delete(s.active, key)
		s.publishLocked()
	}
	s.mu.Unlock()
}

func (s *Service) publishLocked() {
	var alerts []Alert
	for _, a := range s.active {
		alerts = append(alerts, a)
	}
	snap := Snapshot{
		Active:    alerts,
		Timestamp: time.Now(),
	}
	select {
	case s.out <- snap:
	default:
	}
}
