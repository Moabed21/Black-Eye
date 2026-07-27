// Package ports parses /proc/net/tcp, tcp6, udp, udp6 for listeners and connections.
// It resolves inodes to PIDs by scanning /proc/<pid>/fd symlinks.
// No external tools (ss, netstat, lsof) are used.
package ports

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"blackeye/internal/config"
	"blackeye/internal/resolver"
	"blackeye/internal/services"
)

// ListenerSnapshot is one row in the Listeners table.
type ListenerSnapshot struct {
	DisplayService string // "ssh (Secure Shell)" or ":49152"
	RawPort        uint16
	Protocol       string // "TCP" / "UDP" / "TCP6" / "UDP6"
	DisplayScope   string // "lo (Loopback)" / "All Interfaces" / "Local Network"
	RawAddr        string // "0.0.0.0"
	DisplayProcess string // "sshd (SSH Daemon)"
	Owner          string // "root"
	PID            int
	Flagged        bool // true = non-root process on port < 1024
	IsUnencrypted  bool // true = HTTP 80, FTP 21, Telnet 23, POP3 110, IMAP 143, etc.
}

// ConnectionSnapshot is one row in the Active Connections table.
type ConnectionSnapshot struct {
	LocalDisplay   string // "localhost:ssh (22)"
	RemoteDisplay  string // "192.168.1.10:52341"
	RemoteIP       string // "192.168.1.10"
	DisplayState   string // "ESTABLISHED (Connected)"
	DisplayProcess string // "sshd (SSH Daemon)"
	Owner          string
	PID            int
	IsPublicWAN    bool // true if remote IP is a public WAN address
}

// PortsSnapshot is the published payload.
type PortsSnapshot struct {
	Listeners   []ListenerSnapshot
	Connections []ConnectionSnapshot
	Timestamp   time.Time
}

// Service collects port and connection data.
type Service struct {
	interval     time.Duration
	trustedPorts map[uint16]bool
	out          chan interface{}
	health       atomic.Value
	cancel       context.CancelFunc
}

func New(cfg config.Config) *Service {
	trusted := make(map[uint16]bool)
	for _, p := range cfg.Ports.TrustedPorts {
		trusted[p] = true
	}
	s := &Service{
		interval:     time.Duration(cfg.Refresh.PortsInterval) * time.Second,
		trustedPorts: trusted,
		out:          make(chan interface{}, 4),
	}
	s.health.Store(services.HealthStatus{State: services.HealthOK})
	return s
}

func (s *Service) Name()   string                { return "Port Collector" }
func (s *Service) Topic()  string                { return "ports" }
func (s *Service) Output() <-chan interface{}     { return s.out }
func (s *Service) Health() services.HealthStatus { return s.health.Load().(services.HealthStatus) }
func (s *Service) Stop()   { if s.cancel != nil { s.cancel() } }
func (s *Service) Reload(cfg config.Config) {
	s.interval = time.Duration(cfg.Refresh.PortsInterval) * time.Second
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

func (s *Service) collect() (PortsSnapshot, error) {
	// Build inode→(pid,name,uid) map by scanning /proc/<pid>/fd.
	inodeMap := buildInodeMap()

	var listeners []ListenerSnapshot
	var connections []ConnectionSnapshot

	files := []struct {
		path  string
		proto string
		v6    bool
	}{
		{"/proc/net/tcp", "TCP", false},
		{"/proc/net/tcp6", "TCP6", true},
		{"/proc/net/udp", "UDP", false},
		{"/proc/net/udp6", "UDP6", true},
	}

	for _, f := range files {
		entries, err := parseNetFile(f.path, f.v6)
		if err != nil {
			continue // file may not exist
		}
		for _, e := range entries {
			pid, pname, uid := 0, "", -1
			if info, ok := inodeMap[e.inode]; ok {
				pid, pname, uid = info.pid, info.name, info.uid
			}
			owner := resolver.ByUID(uid)
			displayProc := resolver.ProcName(pname, "")
			if pname == "" {
				displayProc = "?"
			}

			if e.state == "0A" { // LISTEN
				scope, scopeRaw := scopeLabel(e.localAddr)
				flagged := !s.trustedPorts[e.localPort] && e.localPort < 1024 && uid != 0 && uid != -1
				unenc := e.localPort == 21 || e.localPort == 23 || e.localPort == 80 || e.localPort == 110 || e.localPort == 143 || e.localPort == 389 || e.localPort == 25
				listeners = append(listeners, ListenerSnapshot{
					DisplayService: resolver.Port(e.localPort),
					RawPort:        e.localPort,
					Protocol:       f.proto,
					DisplayScope:   scope,
					RawAddr:        scopeRaw,
					DisplayProcess: displayProc,
					Owner:          owner,
					PID:            pid,
					Flagged:        flagged,
					IsUnencrypted:  unenc,
				})
			} else if e.state != "07" { // skip CLOSE
				localDisp := fmt.Sprintf("%s:%s", localAddrDisplay(e.localAddr), resolver.Port(e.localPort))
				remoteDisp := fmt.Sprintf("%s:%s", e.remoteAddr, resolver.Port(e.remotePort))
				isWAN := e.remoteAddr != "" && e.remoteAddr != "0.0.0.0" && e.remoteAddr != "127.0.0.1" && e.remoteAddr != "::" && e.remoteAddr != "::1" && !isRFC1918(e.remoteAddr)
				connections = append(connections, ConnectionSnapshot{
					LocalDisplay:   localDisp,
					RemoteDisplay:  remoteDisp,
					RemoteIP:       e.remoteAddr,
					DisplayState:   resolver.TCPState(e.state),
					DisplayProcess: displayProc,
					Owner:          owner,
					PID:            pid,
					IsPublicWAN:    isWAN,
				})
			}
		}
	}

	return PortsSnapshot{
		Listeners:   listeners,
		Connections: connections,
		Timestamp:   time.Now(),
	}, nil
}

// --- inode resolution ---

type inodeInfo struct {
	pid  int
	name string
	uid  int
}

func buildInodeMap() map[uint64]inodeInfo {
	m := make(map[uint64]inodeInfo)
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return m
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}

		// Read process name.
		statusBytes, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
		if err != nil {
			continue
		}
		name := ""
		uid := -1
		for _, line := range strings.Split(string(statusBytes), "\n") {
			if strings.HasPrefix(line, "Name:") {
				name = strings.TrimSpace(strings.TrimPrefix(line, "Name:"))
			}
			if strings.HasPrefix(line, "Uid:") {
				fields := strings.Fields(strings.TrimPrefix(line, "Uid:"))
				if len(fields) > 0 {
					uid, _ = strconv.Atoi(fields[0])
				}
			}
		}

		fdDir := fmt.Sprintf("/proc/%d/fd", pid)
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}
			// Socket links look like: socket:[123456]
			if strings.HasPrefix(link, "socket:[") {
				inodeStr := strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")
				inode, err := strconv.ParseUint(inodeStr, 10, 64)
				if err == nil {
					m[inode] = inodeInfo{pid: pid, name: name, uid: uid}
				}
			}
		}
	}
	return m
}

// --- /proc/net parser ---

type netEntry struct {
	localAddr  string
	localPort  uint16
	remoteAddr string
	remotePort uint16
	state      string
	inode      uint64
}

func parseNetFile(path string, v6 bool) ([]netEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []netEntry
	scanner := bufio.NewScanner(f)
	first := true
	for scanner.Scan() {
		if first {
			first = false
			continue // skip header
		}
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 {
			continue
		}
		localAddr, localPort := parseHexAddr(fields[1], v6)
		remoteAddr, remotePort := parseHexAddr(fields[2], v6)
		state := strings.ToUpper(fields[3])
		inode, _ := strconv.ParseUint(fields[9], 10, 64)
		entries = append(entries, netEntry{
			localAddr:  localAddr,
			localPort:  localPort,
			remoteAddr: remoteAddr,
			remotePort: remotePort,
			state:      state,
			inode:      inode,
		})
	}
	return entries, scanner.Err()
}

// parseHexAddr decodes a "AABBCCDD:PPPP" hex address:port from /proc/net/tcp.
// For IPv4: address is little-endian 32-bit hex.
// For IPv6: address is 4×32-bit little-endian hex groups.
func parseHexAddr(s string, v6 bool) (ip string, port uint16) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return "?", 0
	}
	portN, err := strconv.ParseUint(parts[1], 16, 16)
	if err == nil {
		port = uint16(portN)
	}

	addrHex := parts[0]
	if v6 {
		// 32 hex chars = 16 bytes = IPv6
		if len(addrHex) == 32 {
			b, err := hex.DecodeString(addrHex)
			if err == nil {
				// Each 4-byte group is little-endian; reverse each group.
				for i := 0; i < 16; i += 4 {
					b[i], b[i+3] = b[i+3], b[i]
					b[i+1], b[i+2] = b[i+2], b[i+1]
				}
				ip = net.IP(b).String()
				return
			}
		}
	} else {
		// 8 hex chars = 4 bytes little-endian IPv4
		if len(addrHex) == 8 {
			b, err := hex.DecodeString(addrHex)
			if err == nil {
				ip = net.IPv4(b[3], b[2], b[1], b[0]).String()
				return
			}
		}
	}
	return addrHex, port
}

func scopeLabel(addr string) (display, raw string) {
	switch addr {
	case "0.0.0.0", "::":
		return "All Interfaces", addr
	case "127.0.0.1", "::1":
		return "lo (Loopback)", addr
	}
	if isRFC1918(addr) {
		return "Local Network", addr
	}
	return addr, addr
}

var rfc1918Nets = func() []*net.IPNet {
	cidrs := []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}
	var nets []*net.IPNet
	for _, c := range cidrs {
		_, n, _ := net.ParseCIDR(c)
		nets = append(nets, n)
	}
	return nets
}()

func isRFC1918(addr string) bool {
	ip := net.ParseIP(addr)
	if ip == nil {
		return false
	}
	for _, n := range rfc1918Nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func localAddrDisplay(addr string) string {
	if addr == "127.0.0.1" || addr == "::1" {
		return "localhost"
	}
	return addr
}
