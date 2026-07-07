// Package dmesg streams kernel ring buffer entries from /dev/kmsg.
// It decodes the structured format and classifies entries by log level.
package dmesg

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

// Entry is one kernel ring buffer message.
type Entry struct {
	Timestamp time.Time
	Level     string // "emerg" | "alert" | "crit" | "err" | "warn" | "notice" | "info" | "debug"
	Facility  string // "kern" | "user" | "daemon" | ...
	Message   string
}

// Snapshot contains the latest batch of dmesg entries.
type Snapshot struct {
	Entries   []Entry
	Timestamp time.Time
}

var levelNames = []string{
	"emerg", "alert", "crit", "err", "warn", "notice", "info", "debug",
}

var facilityNames = []string{
	"kern", "user", "mail", "daemon", "auth", "syslog",
	"lpr", "news", "uucp", "cron", "authpriv", "ftp",
}

// Service streams /dev/kmsg.
type Service struct {
	out    chan interface{}
	health atomic.Value
	cancel context.CancelFunc
}

func New(_ config.Config) *Service {
	s := &Service{out: make(chan interface{}, 16)}
	s.health.Store(services.HealthStatus{State: services.HealthOK})
	return s
}

func (s *Service) Name()   string                { return "Kernel Log Collector" }
func (s *Service) Topic()  string                { return "dmesg" }
func (s *Service) Output() <-chan interface{}     { return s.out }
func (s *Service) Health() services.HealthStatus { return s.health.Load().(services.HealthStatus) }
func (s *Service) Stop()   { if s.cancel != nil { s.cancel() } }
func (s *Service) Reload(_ config.Config)        {}

func (s *Service) Start(ctx context.Context) error {
	ctx, s.cancel = context.WithCancel(ctx)

	f, err := os.Open("/dev/kmsg")
	if err != nil {
		s.health.Store(services.HealthStatus{
			State:  services.HealthDown,
			Reason: fmt.Sprintf("Cannot open /dev/kmsg: %v (try running with more privileges)", err),
		})
		<-ctx.Done()
		return nil
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var batch []Entry
	flushTicker := time.NewTicker(500 * time.Millisecond)
	defer flushTicker.Stop()

	readCh := make(chan string, 64)
	go func() {
		for scanner.Scan() {
			select {
			case readCh <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
		close(readCh)
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case line, ok := <-readCh:
			if !ok {
				return nil
			}
			if e, err := parseLine(line); err == nil {
				batch = append(batch, e)
			}
		case <-flushTicker.C:
			if len(batch) > 0 {
				snap := Snapshot{Entries: batch, Timestamp: time.Now()}
				batch = nil
				select {
				case s.out <- snap:
				default:
				}
			}
		}
	}
}

// parseLine decodes one /dev/kmsg line.
// Format: "priority,sequence,timestamp_us,-;message"
// priority = facility*8 + level
func parseLine(line string) (Entry, error) {
	// Split at the first ';' to separate metadata from message.
	semiIdx := strings.Index(line, ";")
	if semiIdx < 0 {
		return Entry{}, fmt.Errorf("no semicolon")
	}
	meta := line[:semiIdx]
	msg := line[semiIdx+1:]

	parts := strings.SplitN(meta, ",", 4)
	if len(parts) < 3 {
		return Entry{}, fmt.Errorf("short meta")
	}

	priority, err := strconv.Atoi(parts[0])
	if err != nil {
		return Entry{}, err
	}
	level := priority & 0x07
	facility := (priority >> 3) & 0x0F

	levelName := "info"
	if level < len(levelNames) {
		levelName = levelNames[level]
	}
	facilityName := "kern"
	if facility < len(facilityNames) {
		facilityName = facilityNames[facility]
	}

	// Timestamp is in microseconds since boot.
	tsMicros, _ := strconv.ParseInt(parts[2], 10, 64)
	ts := time.Now().Add(-time.Since(time.Now()) + time.Duration(tsMicros)*time.Microsecond)

	// Clean up multi-line continuations.
	msg = strings.ReplaceAll(msg, "\n ", " ")

	return Entry{
		Timestamp: ts,
		Level:     levelName,
		Facility:  facilityName,
		Message:   strings.TrimSpace(msg),
	}, nil
}
