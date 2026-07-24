package swap_test

import (
	"context"
	"testing"
	"time"

	"blackeye/internal/config"
	"blackeye/internal/services/swap"
)

func TestSwapServiceStartStop(t *testing.T) {
	cfg := config.Defaults()
	cfg.Refresh.DashboardInterval = 1

	svc := swap.New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() { _ = svc.Start(ctx) }()

	select {
	case raw := <-svc.Output():
		snap, ok := raw.(swap.Snapshot)
		if !ok {
			t.Fatal("expected swap.Snapshot, got something else")
		}
		if snap.UsedPercent < 0 || snap.UsedPercent > 100 {
			t.Errorf("UsedPercent out of range: %.2f", snap.UsedPercent)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for swap snapshot")
	}
}
