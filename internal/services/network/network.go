// Package network collects per-interface rx/tx byte rates from /proc/net/dev.
package network

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
	"blackeye/internal/resolver"
	"blackeye/internal/services"
)

// IfaceSnapshot holds one network interface reading.
type IfaceSnapshot struct {
	DisplayName  string  // "eth0 (Ethernet)"
	RawName      string  // "eth0"
	RxBps        float64 // Bytes/sec received
	TxBps        float64 // Bytes/sec transmitted
	RxBytesTotal uint64  // Total lifetime bytes received
	TxBytesTotal uint64  // Total lifetime bytes transmitted
	RxErrors     uint64
	TxErrors     uint64
	RxMBs        float64 // MB/s received (legacy compatibility)
	TxMBs        float64 // MB/s transmitted (legacy compatibility)
}

// Snapshot is the published payload.
type Snapshot struct {
	Ifaces    []IfaceSnapshot
	Timestamp time.Time
}

type ifaceCounters struct {
	rxBytes, txBytes   uint64
	rxErrors, txErrors uint64
	at                 time.Time
}

// Service collects network stats.
type Service struct {
	interval time.Duration
	out      chan interface{}
	health   atomic.Value
	mu       sync.Mutex
	prev     map[string]ifaceCounters
	cancel   context.CancelFunc
}

func New(cfg config.Config) *Service {
	s := &Service{
		interval: time.Duration(cfg.Refresh.DashboardInterval) * time.Second,
		out:      make(chan interface{}, 4),
		prev:     make(map[string]ifaceCounters),
	}
	s.health.Store(services.HealthStatus{State: services.HealthOK})
	return s
}

func (s *Service) Name()   string                { return "Network Collector" }
func (s *Service) Topic()  string                { return "network" }
func (s *Service) Output() <-chan interface{}     { return s.out }
func (s *Service) Health() services.HealthStatus { return s.health.Load().(services.HealthStatus) }
func (s *Service) Stop()   { if s.cancel != nil { s.cancel() } }
func (s *Service) Reload(cfg config.Config) {
	s.interval = time.Duration(cfg.Refresh.DashboardInterval) * time.Second
}

func (s *Service) Start(ctx context.Context) error {
	ctx, s.cancel = context.WithCancel(ctx)
	// Seed with initial reading.
	curr, err := readNetDev()
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
	curr, err := readNetDev()
	if err != nil {
		return Snapshot{}, err
	}
	now := time.Now()

	s.mu.Lock()
	prev := s.prev
	s.prev = curr
	s.mu.Unlock()

	var ifaces []IfaceSnapshot
	for name, c := range curr {
		p, hasPrev := prev[name]
		var rxBps, txBps float64
		if hasPrev {
			elapsed := now.Sub(p.at).Seconds()
			if elapsed > 0 {
				rxBps = float64(c.rxBytes-p.rxBytes) / elapsed
				txBps = float64(c.txBytes-p.txBytes) / elapsed
			}
		}
		rxMBs := rxBps / (1024 * 1024)
		txMBs := txBps / (1024 * 1024)

		ifaces = append(ifaces, IfaceSnapshot{
			DisplayName:  resolver.Iface(name),
			RawName:      name,
			RxBps:        rxBps,
			TxBps:        txBps,
			RxBytesTotal: c.rxBytes,
			TxBytesTotal: c.txBytes,
			RxMBs:        rxMBs,
			TxMBs:        txMBs,
			RxErrors:     c.rxErrors,
			TxErrors:     c.txErrors,
		})
	}
	return Snapshot{Ifaces: ifaces, Timestamp: now}, nil
}

// readNetDev parses /proc/net/dev into a name→counters map.
func readNetDev() (map[string]ifaceCounters, error) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return nil, fmt.Errorf("network: open /proc/net/dev: %w", err)
	}
	defer f.Close()

	m := make(map[string]ifaceCounters)
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if lineNum <= 2 { // skip header lines
			continue
		}
		line := strings.TrimSpace(scanner.Text())
		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}
		name := strings.TrimSpace(line[:colonIdx])
		fields := strings.Fields(line[colonIdx+1:])
		// /proc/net/dev columns: rx_bytes rx_pkts rx_errs rx_drop ... tx_bytes tx_pkts tx_errs ...
		if len(fields) < 16 {
			continue
		}
		rxBytes, _ := strconv.ParseUint(fields[0], 10, 64)
		rxErrors, _ := strconv.ParseUint(fields[2], 10, 64)
		txBytes, _ := strconv.ParseUint(fields[8], 10, 64)
		txErrors, _ := strconv.ParseUint(fields[10], 10, 64)
		m[name] = ifaceCounters{
			rxBytes:  rxBytes,
			txBytes:  txBytes,
			rxErrors: rxErrors,
			txErrors: txErrors,
			at:       time.Now(),
		}
	}
	return m, scanner.Err()
}
