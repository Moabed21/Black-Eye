package process_test

import (
	"context"
	"os"
	"testing"
	"time"

	"blackeye/internal/config"
	procsvc "blackeye/internal/services/process"
)

func TestProcessServiceStartStop(t *testing.T) {
	cfg := config.Defaults()
	cfg.Refresh.ProcessInterval = 1

	svc := procsvc.New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() { _ = svc.Start(ctx) }()

	select {
	case raw := <-svc.Output():
		snap, ok := raw.(procsvc.Snapshot)
		if !ok {
			t.Fatal("expected procsvc.Snapshot, got something else")
		}
		if len(snap.Processes) == 0 {
			t.Error("expected at least one running process")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for process snapshot")
	}
}

func TestVerifyProcessName(t *testing.T) {
	selfPID := os.Getpid()
	// Current process is running
	err := procsvc.VerifyProcessName(selfPID, "nonexistent_proc_name_12345")
	if err == nil {
		t.Error("expected TOCTOU verification error for mismatched process name")
	}

	// Non-existent PID
	errInvalid := procsvc.VerifyProcessName(9999999, "test")
	if errInvalid == nil {
		t.Error("expected error for non-existent PID")
	}
}
