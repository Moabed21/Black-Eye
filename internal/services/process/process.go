// Package process collects running process info from /proc/<pid>/.
// It handles TOCTOU protection, kill events, and per-process detail data.
package process

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"blackeye/internal/config"
	"blackeye/internal/resolver"
	"blackeye/internal/services"
)

// ProcessSnapshot holds one process entry for the table.
type ProcessSnapshot struct {
	PID           int
	PPID          int
	DisplayName   string  // "nginx (Web Server)"
	RawName       string  // "nginx"  — used for TOCTOU re-check
	Owner         string  // "www-data"
	CPUPercent    float64
	MemoryMiB     float64
	ReadBps       float64 // I/O read bytes/sec
	WriteBps      float64 // I/O write bytes/sec
	DisplayStatus string  // "S (Sleeping)"
	StartedAt     string  // "Today 14:32" or "Jun 28 09:11"
	// Detail panel fields (populated lazily on demand).
	FullCmdLine string
	OpenFDs     int
	CgroupPath  string
}

// Snapshot is the published payload.
type Snapshot struct {
	Processes []ProcessSnapshot
	Timestamp time.Time
}

type cpuAccum struct {
	total uint64
	at    time.Time
}

type ioAccum struct {
	readBytes  uint64
	writeBytes uint64
	at         time.Time
}

// Service collects process list from /proc.
type Service struct {
	interval  time.Duration
	out       chan interface{}
	health    atomic.Value
	mu        sync.Mutex
	prevCPU   map[int]cpuAccum
	prevIO    map[int]ioAccum
	bootTime  uint64
	clkTck    float64
	cancel    context.CancelFunc
}

func New(cfg config.Config) *Service {
	s := &Service{
		interval: time.Duration(cfg.Refresh.ProcessInterval) * time.Second,
		out:      make(chan interface{}, 4),
		prevCPU:  make(map[int]cpuAccum),
		prevIO:   make(map[int]ioAccum),
		clkTck:   100.0, // SC_CLK_TCK default — 100Hz on most Linux systems
	}
	s.health.Store(services.HealthStatus{State: services.HealthOK})
	s.bootTime = readBootTime()
	return s
}

func (s *Service) Name()   string                { return "Process Collector" }
func (s *Service) Topic()  string                { return "process" }
func (s *Service) Output() <-chan interface{}     { return s.out }
func (s *Service) Health() services.HealthStatus { return s.health.Load().(services.HealthStatus) }
func (s *Service) Stop()   { if s.cancel != nil { s.cancel() } }
func (s *Service) Reload(cfg config.Config) {
	s.interval = time.Duration(cfg.Refresh.ProcessInterval) * time.Second
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

func (s *Service) collect() (Snapshot, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return Snapshot{}, fmt.Errorf("process: readdir /proc: %w", err)
	}

	now := time.Now()
	var procs []ProcessSnapshot

	s.mu.Lock()
	prevCPU := s.prevCPU
	prevIO := s.prevIO
	newCPU := make(map[int]cpuAccum)
	newIO := make(map[int]ioAccum)
	s.mu.Unlock()

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}

		p, err := s.readProcess(pid, now, prevCPU, newCPU, prevIO, newIO)
		if err != nil {
			continue // process may have exited
		}
		procs = append(procs, p)
	}

	s.mu.Lock()
	s.prevCPU = newCPU
	s.prevIO = newIO
	s.mu.Unlock()

	return Snapshot{Processes: procs, Timestamp: now}, nil
}

// FetchDetails reads the OpenFDs and Cgroup path for a specific process.
// It is intended to be called lazily for single processes.
func FetchDetails(p *ProcessSnapshot) {
	base := fmt.Sprintf("/proc/%d", p.PID)
	
	// Count Open FDs
	if fds, err := os.ReadDir(filepath.Join(base, "fd")); err == nil {
		p.OpenFDs = len(fds)
	}

	// Read Cgroup
	if cgData, err := os.ReadFile(filepath.Join(base, "cgroup")); err == nil {
		lines := strings.Split(string(cgData), "\n")
		if len(lines) > 0 {
			parts := strings.SplitN(lines[0], ":", 3)
			if len(parts) == 3 {
				p.CgroupPath = parts[2]
			} else {
				p.CgroupPath = lines[0]
			}
		}
	}
}

func (s *Service) readProcess(pid int, now time.Time, prevCPU, newCPU map[int]cpuAccum, prevIO, newIO map[int]ioAccum) (ProcessSnapshot, error) {
	base := fmt.Sprintf("/proc/%d", pid)

	// Parse /proc/<pid>/status for name, uid, state, vmrss.
	statusPath := filepath.Join(base, "status")
	status, err := parseStatus(statusPath)
	if err != nil {
		return ProcessSnapshot{}, err
	}

	// Parse /proc/<pid>/stat for cpu times and start time.
	statPath := filepath.Join(base, "stat")
	stat, err := parseStat(statPath)
	if err != nil {
		return ProcessSnapshot{}, err
	}

	// CPU percent.
	totalJiffies := stat.utime + stat.stime
	newCPU[pid] = cpuAccum{total: totalJiffies, at: now}
	var cpuPct float64
	if p, ok := prevCPU[pid]; ok {
		elapsed := now.Sub(p.at).Seconds()
		if elapsed > 0 {
			cpuPct = float64(totalJiffies-p.total) / s.clkTck / elapsed * 100
		}
	}

	// Read I/O stats from /proc/<pid>/io (if accessible)
	var readBps, writeBps float64
	if ioData, err := parseIO(filepath.Join(base, "io")); err == nil {
		newIO[pid] = ioAccum{readBytes: ioData.readBytes, writeBytes: ioData.writeBytes, at: now}
		if p, ok := prevIO[pid]; ok {
			elapsed := now.Sub(p.at).Seconds()
			if elapsed > 0 && ioData.readBytes >= p.readBytes && ioData.writeBytes >= p.writeBytes {
				readBps = float64(ioData.readBytes-p.readBytes) / elapsed
				writeBps = float64(ioData.writeBytes-p.writeBytes) / elapsed
			}
		}
	}

	// Read cmdline for name enrichment.
	cmdline := readCmdline(filepath.Join(base, "cmdline"))

	return ProcessSnapshot{
		PID:           pid,
		PPID:          stat.ppid,
		DisplayName:   resolver.ProcName(status.name, cmdline),
		RawName:       status.name,
		Owner:         resolver.ByUID(status.uid),
		CPUPercent:    cpuPct,
		MemoryMiB:     float64(status.vmRSSKiB) / 1024.0,
		ReadBps:       readBps,
		WriteBps:      writeBps,
		DisplayStatus: resolver.ProcStateStr(status.state),
		StartedAt:     formatStartTime(stat.starttime, s.bootTime, s.clkTck),
		FullCmdLine:   strings.ReplaceAll(cmdline, "\x00", " "),
	}, nil
}

// VerifyProcessName re-reads /proc/<pid>/status immediately before sending a
// signal. Returns an error if the name has changed since last snapshot.
// This is the TOCTOU protection required by the subject.
func VerifyProcessName(pid int, expectedName string) error {
	statusPath := fmt.Sprintf("/proc/%d/status", pid)
	s, err := parseStatus(statusPath)
	if err != nil {
		return fmt.Errorf("process %d no longer exists", pid)
	}
	if s.name != expectedName {
		return fmt.Errorf("process %d name changed: was %q, now %q — signal aborted", pid, expectedName, s.name)
	}
	return nil
}

// --- internal parsers ---

type statusFields struct {
	name     string
	uid      int
	state    string
	vmRSSKiB uint64
}

func parseStatus(path string) (statusFields, error) {
	f, err := os.Open(path)
	if err != nil {
		return statusFields{}, err
	}
	defer f.Close()

	var sf statusFields
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		val := strings.TrimSpace(parts[1])
		switch key {
		case "Name":
			sf.name = val
		case "State":
			if len(val) > 0 {
				sf.state = string(val[0])
			}
		case "Uid":
			fields := strings.Fields(val)
			if len(fields) > 0 {
				sf.uid, _ = strconv.Atoi(fields[0])
			}
		case "VmRSS":
			fields := strings.Fields(val)
			if len(fields) > 0 {
				sf.vmRSSKiB, _ = strconv.ParseUint(fields[0], 10, 64)
			}
		}
	}
	if sf.name == "" {
		return statusFields{}, fmt.Errorf("empty status")
	}
	return sf, scanner.Err()
}

type statFields struct {
	ppid         int
	utime, stime uint64
	starttime    uint64
}

func parseStat(path string) (statFields, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return statFields{}, err
	}
	// The process name is in parentheses and may contain spaces.
	// Skip past the closing ')' to get to the numeric fields.
	s := string(data)
	rparen := strings.LastIndex(s, ")")
	if rparen < 0 {
		return statFields{}, fmt.Errorf("invalid stat format")
	}
	fields := strings.Fields(s[rparen+1:])
	// After ')': state(0) ppid(1) ... utime(11) stime(12) ... starttime(19)
	if len(fields) < 20 {
		return statFields{}, fmt.Errorf("stat too short")
	}
	ppid, _ := strconv.Atoi(fields[1])
	utime, _ := strconv.ParseUint(fields[11], 10, 64)
	stime, _ := strconv.ParseUint(fields[12], 10, 64)
	start, _ := strconv.ParseUint(fields[19], 10, 64)
	return statFields{ppid: ppid, utime: utime, stime: stime, starttime: start}, nil
}

type ioFields struct {
	readBytes  uint64
	writeBytes uint64
}

func parseIO(path string) (ioFields, error) {
	f, err := os.Open(path)
	if err != nil {
		return ioFields{}, err
	}
	defer f.Close()

	var iof ioFields
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch key {
		case "read_bytes":
			iof.readBytes, _ = strconv.ParseUint(val, 10, 64)
		case "write_bytes":
			iof.writeBytes, _ = strconv.ParseUint(val, 10, 64)
		}
	}
	return iof, scanner.Err()
}

func readCmdline(path string) string {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return ""
	}
	return string(data)
}

func readBootTime() uint64 {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "btime") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				t, _ := strconv.ParseUint(fields[1], 10, 64)
				return t
			}
		}
	}
	return 0
}

func formatStartTime(startJiffies, bootTime uint64, clkTck float64) string {
	if bootTime == 0 || clkTck == 0 {
		return "?"
	}
	startSecs := bootTime + uint64(float64(startJiffies)/clkTck)
	t := time.Unix(int64(startSecs), 0)
	now := time.Now()
	if now.Day() == t.Day() && now.Month() == t.Month() && now.Year() == t.Year() {
		return "Today " + t.Format("15:04")
	}
	return t.Format("Jan 02 15:04")
}
