// Package initsys provides an init-system-agnostic service management layer.
// Supports systemd (D-Bus), OpenRC (rc-service/rc-update), and SysVinit.
package initsys

import (
	"context"
	"time"

	"blackeye/internal/config"
	"blackeye/internal/services"
	"blackeye/internal/sysdetect"
)

// UnitState represents the normalized operational state of a service/unit.
type UnitState string

const (
	StateActive   UnitState = "active"
	StateInactive UnitState = "inactive"
	StateFailed   UnitState = "failed"
	StateUnknown  UnitState = "unknown"
)

// Unit holds display-ready status data for a single service/unit.
type Unit struct {
	Name         string    // e.g. "nginx.service" or "nginx"
	Description  string    // "High performance web server"
	ActiveState  UnitState // "active", "inactive", "failed"
	SubState     string    // "running", "dead", "exited", "stopped"
	EnabledState string    // "enabled", "disabled", "masked", "unknown"
	DisplayState string    // "active (running)"
	Flagged      bool      // true if state == "failed"
}

// LogEntry represents a single log line associated with a service.
type LogEntry struct {
	Timestamp time.Time
	Message   string
	Priority  string // "info", "warn", "err", "emerg"
}

// InitBackend defines the interface implemented by each init system backend.
type InitBackend interface {
	Name() string // "systemd", "openrc", "sysvinit"
	ListUnits() ([]Unit, error)
	Start(name string) error
	Stop(name string) error
	Restart(name string) error
	Enable(name string) error
	Disable(name string) error
	Mask(name string) error
	Unmask(name string) error
	UnitLogs(name string, lines int) ([]LogEntry, error)
}

// Snapshot is the published event payload.
type Snapshot struct {
	InitName  string // "systemd" | "openrc" | "sysvinit"
	Units     []Unit
	Available bool
	Error     string
	Timestamp time.Time
}

// Service is the init-system monitoring microservice.
type Service struct {
	backend  InitBackend
	interval time.Duration
	out      chan interface{}
	cancel   context.CancelFunc
}

// New creates an init service using the backend matching the detected init system.
func New(cfg config.Config) *Service {
	profile := sysdetect.Profile()
	var backend InitBackend

	switch profile.InitSystem {
	case "openrc":
		backend = NewOpenRC()
	case "sysvinit":
		backend = NewSysVinit()
	default: // "systemd" or fallback
		backend = NewSystemd()
	}

	return &Service{
		backend:  backend,
		interval: time.Duration(cfg.Refresh.SystemdInterval) * time.Second,
		out:      make(chan interface{}, 4),
	}
}

func (s *Service) Name() string { return "Init System Collector (" + s.backend.Name() + ")" }
func (s *Service) Topic() string { return "systemd" } // maintains bus topic compatibility
func (s *Service) Output() <-chan interface{} { return s.out }
func (s *Service) Health() services.HealthStatus {
	return services.HealthStatus{State: services.HealthOK}
}
func (s *Service) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
}
func (s *Service) Reload(cfg config.Config) {
	s.interval = time.Duration(cfg.Refresh.SystemdInterval) * time.Second
}

func (s *Service) Backend() InitBackend {
	return s.backend
}

func (s *Service) Start(ctx context.Context) error {
	ctx, s.cancel = context.WithCancel(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// Initial collect
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
	units, err := s.backend.ListUnits()
	if err != nil {
		return Snapshot{
			InitName:  s.backend.Name(),
			Available: false,
			Error:     err.Error(),
			Timestamp: time.Now(),
		}
	}
	return Snapshot{
		InitName:  s.backend.Name(),
		Units:     units,
		Available: true,
		Timestamp: time.Now(),
	}
}
