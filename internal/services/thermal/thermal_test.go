package thermal_test

import (
	"context"
	"testing"
	"time"

	"blackeye/internal/config"
	"blackeye/internal/services/thermal"
)

func TestThermalServiceStartStop(t *testing.T) {
	cfg := config.Defaults()
	cfg.Refresh.DashboardInterval = 1

	svc := thermal.New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() { _ = svc.Start(ctx) }()

	select {
	case raw := <-svc.Output():
		_, ok := raw.(thermal.Snapshot)
		if !ok {
			t.Fatal("expected thermal.Snapshot, got something else")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for thermal snapshot")
	}
}
