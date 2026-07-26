// Package packages provides cross-distro package management.
// Auto-detects apt, dnf, yum, pacman, apk, zypper, or xbps backends.
package packages

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"blackeye/internal/config"
	"blackeye/internal/services"
	"blackeye/internal/sysdetect"
)

type Category string

const (
	CategorySystemCore    Category = "System Core"
	CategoryUserInstalled Category = "User App"
	CategoryDependency    Category = "Library"
	CategoryDevelopment   Category = "Development"
)

// Package represents a single software package.
type Package struct {
	Name        string
	Version     string
	Arch        string
	Size        string
	Description string
	Status      string   // "installed", "available", "upgradable"
	Category    Category // "System Core", "User App", "Library", "Development"
}

func (p Package) GetCategory() Category {
	if p.Category != "" {
		return p.Category
	}
	return ClassifyCategory(p.Name)
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

// AvailableBackends discovers all package managers and helpers installed on the host.
func AvailableBackends() []PkgBackend {
	var backends []PkgBackend
	seen := make(map[string]bool)

	// List of known package manager and helper executables to check dynamically on PATH
	helpers := []struct {
		name string
		ctor func(path string) PkgBackend
	}{
		{"pacman", func(p string) PkgBackend { return NewPacman(p) }},
		{"yay", func(p string) PkgBackend { return NewYay(p) }},
		{"paru", func(p string) PkgBackend { return NewYay(p) }},
		{"apt", func(p string) PkgBackend { return NewAPT(p) }},
		{"apt-get", func(p string) PkgBackend { return NewAPT(p) }},
		{"dnf", func(p string) PkgBackend { return NewDNF(p) }},
		{"yum", func(p string) PkgBackend { return NewDNF(p) }},
		{"apk", func(p string) PkgBackend { return NewAPK(p) }},
		{"zypper", func(p string) PkgBackend { return NewZypper(p) }},
	}

	for _, h := range helpers {
		if path, err := exec.LookPath(h.name); err == nil {
			b := h.ctor(path)
			if !seen[b.Name()] {
				seen[b.Name()] = true
				backends = append(backends, b)
			}
		}
	}

	if len(backends) == 0 {
		profile := sysdetect.Profile()
		backends = append(backends, NewAPT(profile.PkgBinary))
	}
	return backends
}

// FuzzySuggest returns candidates similar to query using substring and prefix matching.
func FuzzySuggest(query string, candidates []Package) []Package {
	if strings.TrimSpace(query) == "" {
		return nil
	}
	lower := strings.ToLower(query)
	var matches []Package

	for _, pkg := range candidates {
		pkgLower := strings.ToLower(pkg.Name)
		if strings.Contains(pkgLower, lower) || (len(lower) > 0 && strings.HasPrefix(pkgLower, string(lower[0]))) {
			matches = append(matches, pkg)
		}
		if len(matches) >= 10 {
			break
		}
	}
	return matches
}

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

// ClassifyCategory assigns a category to a package name based on system essential heuristics.
func ClassifyCategory(name string) Category {
	lower := strings.ToLower(name)

	coreTerms := []string{
		"linux", "kernel", "glibc", "libc6", "libc-", "libc", "systemd",
		"coreutils", "bash", "sh", "init", "sysv", "openrc", "busybox",
		"pacman", "apt", "dpkg", "dnf", "yum", "rpm", "apk", "zypper",
		"sudo", "shadow", "pam", "filesystem", "util-linux", "iproute", "dbus",
		"grub", "efibootmgr", "kmod", "udev", "polkit", "openssl", "ca-certificates",
		"binutils", "findutils", "diffutils", "gzip", "tar", "sed", "gawk", "grep",
	}

	for _, term := range coreTerms {
		if lower == term || strings.Contains(lower, term) {
			return CategorySystemCore
		}
	}

	if strings.HasPrefix(lower, "lib") || strings.Contains(lower, "libs") {
		return CategoryDependency
	}

	devTerms := []string{"gcc", "g++", "clang", "make", "cmake", "golang", "go", "rust", "python", "perl", "ruby", "openjdk", "sdk", "headers", "devel"}
	for _, term := range devTerms {
		if strings.Contains(lower, term) {
			return CategoryDevelopment
		}
	}

	return CategoryUserInstalled
}
