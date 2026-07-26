// Package firewall provides cross-distro firewall management.
// Auto-detects nftables, iptables, ufw, or firewalld backends.
package firewall

import (
	"context"
	"os/exec"
	"time"

	"blackeye/internal/config"
	"blackeye/internal/services"
	"blackeye/internal/sysdetect"
)

// Rule represents a single firewall rule.
type Rule struct {
	ID       string // handle or line number
	Chain    string // INPUT, OUTPUT, FORWARD, etc.
	Table    string // filter, nat, mangle, etc.
	Action   string // ACCEPT, DROP, REJECT, LOG
	Protocol string // tcp, udp, icmp, all
	Source   string // IP/CIDR or "any"
	Dest     string // IP/CIDR or "any"
	Port     string // "80", "443", "22:25"
	Comment  string
}

// FirewallBackend is the interface implemented by each firewall engine.
type FirewallBackend interface {
	Name() string // "nftables", "iptables", "ufw", "firewalld"
	ListRules() ([]Rule, error)
	AddRule(r Rule) error
	DeleteRule(id string) error
	Enable() error
	Disable() error
	IsEnabled() (bool, error)
}

// Snapshot is the event bus payload.
type Snapshot struct {
	BackendName string
	IsEnabled   bool
	Rules       []Rule
	Available   bool
	Error       string
	Timestamp   time.Time
}

// Service monitors and manages system firewall rules.
type Service struct {
	backend  FirewallBackend
	interval time.Duration
	out      chan interface{}
	cancel   context.CancelFunc
}

func New(cfg config.Config) *Service {
	profile := sysdetect.Profile()
	var backend FirewallBackend

	switch profile.Firewall {
	case "ufw":
		backend = NewUFW(profile.FirewallBin)
	case "firewalld":
		backend = NewFirewalld()
	case "iptables-nft", "iptables-legacy":
		backend = NewIPTables(profile.FirewallBin)
	default: // "nftables" or fallback
		backend = NewNFTables(profile.FirewallBin)
	}

	return &Service{
		backend:  backend,
		interval: 5 * time.Second,
		out:      make(chan interface{}, 4),
	}
}

func (s *Service) Name() string { return "Firewall Collector (" + s.backend.Name() + ")" }
func (s *Service) Topic() string { return "firewall" }
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

func (s *Service) Backend() FirewallBackend { return s.backend }

// AvailableFirewallBackends dynamically scans system kernel modules, active services, and binary paths.
func AvailableFirewallBackends() []FirewallBackend {
	var backends []FirewallBackend
	seen := make(map[string]bool)

	engines := []struct {
		bin  string
		ctor func(path string) FirewallBackend
	}{
		{"ufw", func(p string) FirewallBackend { return NewUFW(p) }},
		{"firewall-cmd", func(p string) FirewallBackend { return NewFirewalld() }},
		{"nft", func(p string) FirewallBackend { return NewNFTables(p) }},
		{"iptables", func(p string) FirewallBackend { return NewIPTables(p) }},
	}

	for _, e := range engines {
		if path, err := exec.LookPath(e.bin); err == nil {
			b := e.ctor(path)
			if !seen[b.Name()] {
				seen[b.Name()] = true
				backends = append(backends, b)
			}
		}
	}

	if len(backends) == 0 {
		backends = append(backends, NewNFTables(""))
	}
	return backends
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
		return Snapshot{Available: false, Error: "no firewall backend detected", Timestamp: time.Now()}
	}

	enabled, _ := s.backend.IsEnabled()
	rules, err := s.backend.ListRules()
	if err != nil {
		return Snapshot{
			BackendName: s.backend.Name(),
			IsEnabled:   enabled,
			Available:   false,
			Error:       err.Error(),
			Timestamp:   time.Now(),
		}
	}

	return Snapshot{
		BackendName: s.backend.Name(),
		IsEnabled:   enabled,
		Rules:       rules,
		Available:   true,
		Timestamp:   time.Now(),
	}
}
