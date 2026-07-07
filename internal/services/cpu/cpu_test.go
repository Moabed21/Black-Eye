package cpu_test

import (
	"context"
	"testing"
	"time"

	"blackeye/internal/config"
	"blackeye/internal/services/cpu"
)

func TestCPUServiceStartStop(t *testing.T) {
	cfg := config.Defaults()
	cfg.Refresh.DashboardInterval = 1

	svc := cpu.New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() { _ = svc.Start(ctx) }()

	// Wait for at least one snapshot.
	select {
	case raw := <-svc.Output():
		snap, ok := raw.(cpu.Snapshot)
		if !ok {
			t.Fatal("expected cpu.Snapshot, got something else")
		}
		if snap.TotalPercent < 0 || snap.TotalPercent > 100 {
			t.Errorf("TotalPercent out of range: %.2f", snap.TotalPercent)
		}
		if snap.Timestamp.IsZero() {
			t.Error("Timestamp is zero")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for CPU snapshot")
	}
}

func TestCPUHealth(t *testing.T) {
	cfg := config.Defaults()
	svc := cpu.New(cfg)
	h := svc.Health()
	// Before Start, service is healthy (not yet started is fine).
	if h.State == "" {
		t.Error("Health state should not be empty")
	}
}
