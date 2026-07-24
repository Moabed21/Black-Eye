// Package routing parses /proc/net/route (routing table) and /proc/net/arp (ARP table).
package routing

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"blackeye/internal/config"
	"blackeye/internal/resolver"
	"blackeye/internal/services"
)

// RouteEntry is one row in the routing table.
type RouteEntry struct {
	Destination  string // "0.0.0.0/0 (Default Route)" or "192.168.1.0/24"
	Gateway      string // "192.168.1.1" or "—"
	DisplayIface string // "eth0 (Ethernet)"
	Flags        string
	Metric       int
}

// ARPEntry is one row in the ARP table.
type ARPEntry struct {
	IP           string
	MAC          string
	DisplayIface string // "eth0 (Ethernet)"
	State        string // "reachable" / "stale" / "incomplete"
}

// Snapshot is the published payload.
type Snapshot struct {
	Routes    []RouteEntry
	ARP       []ARPEntry
	Timestamp time.Time
}

// Service collects routing and ARP data.
type Service struct {
	interval time.Duration
	out      chan interface{}
	health   atomic.Value
	cancel   context.CancelFunc
}

func New(cfg config.Config) *Service {
	s := &Service{
		interval: 10 * time.Second,
		out:      make(chan interface{}, 4),
	}
	s.health.Store(services.HealthStatus{State: services.HealthOK})
	return s
}

func (s *Service) Name()   string                { return "Routing Collector" }
func (s *Service) Topic()  string                { return "routing" }
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
	routes, err := parseRoutes()
	if err != nil {
		return Snapshot{}, err
	}
	arps, _ := parseARP() // ARP errors are non-fatal
	return Snapshot{Routes: routes, ARP: arps, Timestamp: time.Now()}, nil
}

// parseRoutes reads /proc/net/route.
// Columns: Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT
func parseRoutes() ([]RouteEntry, error) {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return nil, fmt.Errorf("routing: open /proc/net/route: %w", err)
	}
	defer f.Close()

	var routes []RouteEntry
	scanner := bufio.NewScanner(f)
	first := true
	for scanner.Scan() {
		if first { first = false; continue }
		fields := strings.Fields(scanner.Text())
		if len(fields) < 8 {
			continue
		}
		iface := fields[0]
		dest := hexToIPv4(fields[1])
		gw := hexToIPv4(fields[2])
		flags, _ := strconv.ParseUint(fields[3], 16, 32)
		metric, _ := strconv.Atoi(fields[6])
		mask := hexToIPv4(fields[7])

		// Compute prefix length from mask.
		prefix := maskToPrefix(mask)
		var destination string
		if dest == "0.0.0.0" {
			destination = "0.0.0.0/0 (Default Route)"
		} else {
			destination = fmt.Sprintf("%s/%d", dest, prefix)
		}

		gwDisplay := gw
		if gw == "0.0.0.0" {
			gwDisplay = "—"
		}

		routes = append(routes, RouteEntry{
			Destination:  destination,
			Gateway:      gwDisplay,
			DisplayIface: resolver.Iface(iface),
			Flags:        routeFlags(uint32(flags)),
			Metric:       metric,
		})
	}
	return routes, scanner.Err()
}

// parseARP reads /proc/net/arp.
func parseARP() ([]ARPEntry, error) {
	f, err := os.Open("/proc/net/arp")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []ARPEntry
	scanner := bufio.NewScanner(f)
	first := true
	for scanner.Scan() {
		if first { first = false; continue }
		fields := strings.Fields(scanner.Text())
		if len(fields) < 6 {
			continue
		}
		ip := fields[0]
		mac := fields[3]
		iface := fields[5]
		flagsHex := fields[2]

		state := "reachable"
		flagVal, _ := strconv.ParseUint(flagsHex, 0, 32)
		if flagVal == 0 {
			state = "incomplete"
		} else if mac == "00:00:00:00:00:00" {
			state = "incomplete"
		}

		entries = append(entries, ARPEntry{
			IP:           ip,
			MAC:          mac,
			DisplayIface: resolver.Iface(iface),
			State:        state,
		})
	}
	return entries, scanner.Err()
}

// hexToIPv4 decodes a little-endian hex IPv4 address from /proc/net/route.
func hexToIPv4(h string) string {
	b, err := hex.DecodeString(h)
	if err != nil || len(b) != 4 {
		return h
	}
	return net.IPv4(b[3], b[2], b[1], b[0]).String()
}

func maskToPrefix(mask string) int {
	ip := net.ParseIP(mask)
	if ip == nil {
		return 0
	}
	ip = ip.To4()
	if ip == nil {
		return 0
	}
	m := net.IPMask(ip)
	ones, _ := m.Size()
	return ones
}

func routeFlags(f uint32) string {
	var parts []string
	if f&0x1 != 0 { parts = append(parts, "U") }
	if f&0x2 != 0 { parts = append(parts, "G") }
	if f&0x4 != 0 { parts = append(parts, "H") }
	return strings.Join(parts, "")
}
