// Package netstats collects network error and drop statistics from /proc/net/snmp.
package netstats

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"blackeye/internal/config"
	"blackeye/internal/services"
)

// Snapshot holds network-level statistics.
type Snapshot struct {
	TCPRetransmits uint64
	TCPErrors      uint64
	UDPDropped     uint64
	ICMPErrors     uint64
	Timestamp      time.Time
}

// Service reads /proc/net/snmp.
type Service struct {
	interval time.Duration
	out      chan interface{}
	health   atomic.Value
	cancel   context.CancelFunc
}

func New(cfg config.Config) *Service {
	s := &Service{
		interval: 5 * time.Second,
		out:      make(chan interface{}, 4),
	}
	s.health.Store(services.HealthStatus{State: services.HealthOK})
	return s
}

func (s *Service) Name()   string                { return "Network Stats Collector" }
func (s *Service) Topic()  string                { return "netstats" }
func (s *Service) Output() <-chan interface{}     { return s.out }
func (s *Service) Health() services.HealthStatus { return s.health.Load().(services.HealthStatus) }
func (s *Service) Stop()   { if s.cancel != nil { s.cancel() } }
func (s *Service) Reload(_ config.Config)        {}

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
	m, err := parseSnmp("/proc/net/snmp")
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		TCPRetransmits: m["Tcp:RetransSegs"],
		TCPErrors:      m["Tcp:InErrs"],
		UDPDropped:     m["Udp:RcvbufErrors"] + m["Udp:SndbufErrors"],
		ICMPErrors:     m["Icmp:InErrors"],
		Timestamp:      time.Now(),
	}, nil
}

// parseSnmp reads a /proc/net/snmp-style file where header and value lines alternate.
func parseSnmp(path string) (map[string]uint64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("netstats: open %s: %w", path, err)
	}
	defer f.Close()

	m := make(map[string]uint64)
	scanner := bufio.NewScanner(f)
	var headerLine string
	for scanner.Scan() {
		line := scanner.Text()
		if headerLine == "" {
			headerLine = line
			continue
		}
		// Parse header and value lines in pairs.
		headers := strings.Fields(headerLine)
		values := strings.Fields(line)
		if len(headers) == 0 || len(values) == 0 || headers[0] != values[0] || len(headers) != len(values) {
			headerLine = line
			continue
		}
		protocol := strings.TrimSuffix(headers[0], ":")
		for i := 1; i < len(headers); i++ {
			key := protocol + ":" + headers[i]
			val, err := strconv.ParseUint(values[i], 10, 64)
			if err == nil {
				m[key] = val
			}
		}
		headerLine = ""
	}
	return m, scanner.Err()
}
