package audit_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"blackeye/internal/config"
	auditsvc "blackeye/internal/services/audit"
)

func TestAuditService(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test_audit.log")

	cfg := config.Defaults()
	cfg.Audit.LogPath = logFile

	svc := auditsvc.New(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = svc.Start(ctx) }()

	time.Sleep(50 * time.Millisecond)

	svc.WriteEvent(auditsvc.Event{
		UID:    1000,
		User:   "testuser",
		Action: "kill_process",
		Target: "nginx",
		PID:    1234,
		Result: "success",
	})

	time.Sleep(50 * time.Millisecond)
	cancel()

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed to read audit log file: %v", err)
	}

	if len(data) == 0 {
		t.Error("audit log file should not be empty after WriteEvent")
	}
}
