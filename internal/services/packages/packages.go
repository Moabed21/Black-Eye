// Package packages provides cross-distro package management.
// Auto-detects apt, dnf, yum, pacman, apk, zypper, or xbps backends.
package packages

import (
	"context"
	"time"

	"blackeye/internal/config"
	"blackeye/internal/services"
	"blackeye/internal/sysdetect"
)

// Package represents a single software package.
type Package struct {
	Name        string
	Version     string
	Arch        string
	Size        string
	Description string
	Status      string // "installed", "available", "upgradable"
}

// PkgBackend is the interface implemented by each distro package manager.
type PkgBackend interface {
	Name() string // "apt", "dnf", "pacman", "apk", "zypper", "xbps"
	ListInstalled() ([]Package, error)
	Search(query string) ([]Package, error)
	Install(names ...string) (string, error)
	Remove(names ...string) (string, error)
	ListUpgradable() ([]Package, error)
	UpgradeAll() (string, error)
}

// Snapshot is the event bus payload.
type Snapshot struct {
	BackendName    string
	InstalledCount int
	UpgradableCount int
	Installed      []Package
	Available      bool
	Error          string
	Timestamp      time.Time
}

// Service monitors and manages system packages.
type Service struct {
	backend  PkgBackend
	interval time.Duration
	out      chan interface{}
	cancel   context.CancelFunc
}

func New(cfg config.Config) *Service {
	profile := sysdetect.Profile()
	var backend PkgBackend

	switch profile.PkgManager {
	case "dnf", "yum":
		backend = NewDNF(profile.PkgBinary)
	case "pacman":
		backend = NewPacman(profile.PkgBinary)
	case "apk":
		backend = NewAPK(profile.PkgBinary)
	case "zypper":
		backend = NewZypper(profile.PkgBinary)
	default: // "apt" or fallback
		backend = NewAPT(profile.PkgBinary)
	}

	return &Service{
		backend:  backend,
		interval: 30 * time.Second, // packages change infrequently
		out:      make(chan interface{}, 4),
	}
}

func (s *Service) Name() string { return "Package Manager Collector (" + s.backend.Name() + ")" }
func (s *Service) Topic() string { return "packages" }
func (s *Service) Output() <-chan interface{} { return s.out }
func (s *Service) Health() services.HealthStatus {
	return services.HealthStatus{State: services.HealthOK}
}
func (s *Service) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
}
func (s *Service) Reload(cfg config.Config) {}

func (s *Service) Backend() PkgBackend { return s.backend }

func (s *Service) Start(ctx context.Context) error {
	ctx, s.cancel = context.WithCancel(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// Initial snapshot
	select {
	case s.out <- s.collect():
	default:
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			select {
			case s.out <- s.collect():
			default:
			}
		}
	}
}

func (s *Service) collect() Snapshot {
	if s.backend == nil {
		return Snapshot{Available: false, Error: "no package manager detected", Timestamp: time.Now()}
	}

	installed, err := s.backend.ListInstalled()
	if err != nil {
		return Snapshot{
			BackendName: s.backend.Name(),
			Available:   false,
			Error:       err.Error(),
			Timestamp:   time.Now(),
		}
	}

	upgradable, _ := s.backend.ListUpgradable()

	return Snapshot{
		BackendName:     s.backend.Name(),
		InstalledCount:  len(installed),
		UpgradableCount: len(upgradable),
		Installed:       installed,
		Available:       true,
		Timestamp:       time.Now(),
	}
}
