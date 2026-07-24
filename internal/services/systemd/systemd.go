// Package systemd queries systemd unit states via the D-Bus unix socket.
// No 'systemctl' command is executed.
package systemd

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"blackeye/internal/config"
	"blackeye/internal/services"
)

// UnitSnapshot holds display-ready data for one systemd unit.
type UnitSnapshot struct {
	Name         string // "nginx.service"
	Description  string // "A high performance web server"
	DisplayState string // "active (Running)" / "failed (Error)"
	SubState     string // "running" / "dead" / "exited"
	Since        string // "3 days ago" / "Today 09:12"
	Flagged      bool   // true if state == "failed"
}

// Snapshot is the published payload.
type Snapshot struct {
	Units     []UnitSnapshot
	Available bool   // false if systemd D-Bus is unreachable
	Error     string
	Timestamp time.Time
}

// Service queries systemd unit states.
type Service struct {
	interval time.Duration
	out      chan interface{}
	health   atomic.Value
	cancel   context.CancelFunc
}

func New(cfg config.Config) *Service {
	s := &Service{
		interval: time.Duration(cfg.Refresh.SystemdInterval) * time.Second,
		out:      make(chan interface{}, 4),
	}
	s.health.Store(services.HealthStatus{State: services.HealthOK})
	return s
}

func (s *Service) Name()   string                { return "Systemd Monitor" }
func (s *Service) Topic()  string                { return "systemd" }
func (s *Service) Output() <-chan interface{}     { return s.out }
func (s *Service) Health() services.HealthStatus { return s.health.Load().(services.HealthStatus) }
func (s *Service) Stop()   { if s.cancel != nil { s.cancel() } }
func (s *Service) Reload(cfg config.Config) {
	s.interval = time.Duration(cfg.Refresh.SystemdInterval) * time.Second
}

// dbusSocket is the default D-Bus system socket.
const dbusSocket = "/run/dbus/system_bus_socket"

func (s *Service) Start(ctx context.Context) error {
	ctx, s.cancel = context.WithCancel(ctx)

	// Check if D-Bus socket is accessible before starting the loop.
	if _, err := os.Stat(dbusSocket); err != nil {
		snap := Snapshot{
			Available: false,
			Error:     fmt.Sprintf("systemd D-Bus socket not found at %s.\nThis tab requires systemd and D-Bus to be running.", dbusSocket),
			Timestamp: time.Now(),
		}
		s.health.Store(services.HealthStatus{State: services.HealthDown, Reason: "D-Bus unavailable"})
		select { case s.out <- snap: default: }
		<-ctx.Done()
		return nil
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
				select {
				case s.out <- Snapshot{Available: false, Error: err.Error(), Timestamp: time.Now()}:
				default:
				}
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

// CollectSnapshot queries systemd via D-Bus for all service units.
func CollectSnapshot() (Snapshot, error) {
	units, err := listUnitsViaDbus()
	if err != nil {
		return Snapshot{}, fmt.Errorf("systemd: D-Bus query failed: %w", err)
	}

	var snapUnits []UnitSnapshot
	for _, u := range units {
		displayState := u.activeState
		if u.subState != "" {
			displayState = fmt.Sprintf("%s (%s)", u.activeState, stateDescription(u.activeState, u.subState))
		}
		snapUnits = append(snapUnits, UnitSnapshot{
			Name:         u.name,
			Description:  u.description,
			DisplayState: displayState,
			SubState:     u.subState,
			Since:        "active",
			Flagged:      u.activeState == "failed",
		})
	}
	return Snapshot{Units: snapUnits, Available: true, Timestamp: time.Now()}, nil
}

func (s *Service) collect() (Snapshot, error) {
	return CollectSnapshot()
}

func stateDescription(active, sub string) string {
	switch active {
	case "active":
		switch sub {
		case "running":
			return "Running"
		case "exited":
			return "Exited (OK)"
		case "listening":
			return "Listening"
		}
	case "failed":
		return "Error"
	case "inactive":
		return "Stopped"
	case "activating":
		return "Starting…"
	case "deactivating":
		return "Stopping…"
	}
	return sub
}

// StartUnit sends a StartUnit D-Bus call for the given unit name.
func (s *Service) StartUnit(unitName string) error {
	return unitActionViaDbus("StartUnit", unitName)
}

// StopUnit sends a StopUnit D-Bus call for the given unit name.
func (s *Service) StopUnit(unitName string) error {
	return unitActionViaDbus("StopUnit", unitName)
}

// RestartUnit sends a RestartUnit D-Bus call for the given unit name.
func (s *Service) RestartUnit(unitName string) error {
	return unitActionViaDbus("RestartUnit", unitName)
}

