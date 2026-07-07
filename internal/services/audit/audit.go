// Package audit implements the BlackEye audit service.
// It listens on the "audit_event" bus topic and writes entries to
// ~/.local/share/blackeye/audit.log in append-only mode.
// The file is NEVER truncated or overwritten.
package audit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"blackeye/internal/config"
	"blackeye/internal/services"
)

// Event represents a single auditable action.
type Event struct {
	UID    int
	User   string
	Action string // "kill_process" | "stop_container" | "restart_container" | "show_secrets"
	Target string // process name or container name
	PID    int    // for process actions (0 otherwise)
	ID     string // for container actions (empty otherwise)
	Result string // "success" | "denied" | "error: <msg>"
}

// Service is the audit log writer. It subscribes to the "audit_event" topic.
type Service struct {
	logPath string
	mu      sync.Mutex
	file    *os.File
	health  atomic.Value // stores services.HealthStatus
	stopCh  chan struct{}
}

// New creates an audit Service writing to the path from cfg.
func New(cfg config.Config) *Service {
	s := &Service{
		logPath: config.ExpandPath(cfg.Audit.LogPath),
		stopCh:  make(chan struct{}),
	}
	s.health.Store(services.HealthStatus{State: services.HealthOK})
	return s
}

func (s *Service) Name()  string { return "Audit Logger" }
func (s *Service) Topic() string { return "" } // audit is a subscriber, not a publisher

// Output returns a nil channel — audit service does not publish snapshots.
func (s *Service) Output() <-chan interface{} { return nil }

// Start opens the audit log file and waits for context cancellation.
// The caller must publish Event values to the event bus topic "audit_event".
// Use WriteEvent directly to write from other services.
func (s *Service) Start(ctx context.Context) error {
	if err := s.openLog(); err != nil {
		s.health.Store(services.HealthStatus{
			State:  services.HealthDown,
			Reason: err.Error(),
		})
		return err
	}
	<-ctx.Done()
	s.mu.Lock()
	if s.file != nil {
		s.file.Close()
		s.file = nil
	}
	s.mu.Unlock()
	return nil
}

func (s *Service) Stop() {
	select {
	case s.stopCh <- struct{}{}:
	default:
	}
}

func (s *Service) Reload(cfg config.Config) {
	newPath := config.ExpandPath(cfg.Audit.LogPath)
	s.mu.Lock()
	defer s.mu.Unlock()
	if newPath != s.logPath {
		s.logPath = newPath
		if s.file != nil {
			s.file.Close()
			s.file = nil
		}
		_ = s.openLogLocked()
	}
}

func (s *Service) Health() services.HealthStatus {
	return s.health.Load().(services.HealthStatus)
}

// WriteEvent formats and appends a single audit entry to the log.
// It is goroutine-safe and can be called from any service.
func (s *Service) WriteEvent(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.file == nil {
		if err := s.openLogLocked(); err != nil {
			return
		}
	}

	ts := time.Now().Format(time.RFC3339)
	var line string

	switch e.Action {
	case "kill_process", "stop_process":
		line = fmt.Sprintf("[%s] uid=%d user=%s action=%s target=%s pid=%d result=%s\n",
			ts, e.UID, e.User, e.Action, e.Target, e.PID, e.Result)
	case "stop_container", "restart_container":
		line = fmt.Sprintf("[%s] uid=%d user=%s action=%s target=%s id=%s result=%s\n",
			ts, e.UID, e.User, e.Action, e.Target, e.ID, e.Result)
	default:
		line = fmt.Sprintf("[%s] uid=%d user=%s action=%s target=%s result=%s\n",
			ts, e.UID, e.User, e.Action, e.Target, e.Result)
	}

	_, _ = fmt.Fprint(s.file, line)
	_ = s.file.Sync()
}

// openLog ensures the audit log directory exists and opens the file append-only.
func (s *Service) openLog() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.openLogLocked()
}

// openLogLocked is the internal opener — caller must hold s.mu.
func (s *Service) openLogLocked() error {
	dir := filepath.Dir(s.logPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("audit: cannot create log directory %q: %w", dir, err)
	}
	f, err := os.OpenFile(s.logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("audit: cannot open log %q: %w", s.logPath, err)
	}
	s.file = f
	s.health.Store(services.HealthStatus{State: services.HealthOK})
	return nil
}
