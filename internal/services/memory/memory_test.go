package memory_test

import (
	"context"
	"testing"
	"time"

	"blackeye/internal/config"
	"blackeye/internal/services/memory"
)

func TestMemoryServiceStartStop(t *testing.T) {
	cfg := config.Defaults()
	cfg.Refresh.DashboardInterval = 1

	svc := memory.New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() { _ = svc.Start(ctx) }()

	select {
	case raw := <-svc.Output():
		snap, ok := raw.(memory.Snapshot)
		if !ok {
			t.Fatal("expected memory.Snapshot, got something else")
		}
		if snap.TotalGiB <= 0 {
			t.Errorf("TotalGiB should be > 0, got %.2f", snap.TotalGiB)
		}
		if snap.UsedPercent < 0 || snap.UsedPercent > 100 {
			t.Errorf("UsedPercent out of range: %.2f", snap.UsedPercent)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for memory snapshot")
	}
}
