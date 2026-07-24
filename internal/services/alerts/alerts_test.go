package alerts_test

import (
	"context"
	"testing"
	"time"

	"blackeye/internal/bus"
	"blackeye/internal/config"
	alertssvc "blackeye/internal/services/alerts"
	cpusvc "blackeye/internal/services/cpu"
)

func TestAlertsService(t *testing.T) {
	b := bus.New()
	defer b.Close()

	cfg := config.Defaults()
	cfg.Alerts.CPUWarning = 50.0
	cfg.Alerts.CPUCritical = 80.0

	svc := alertssvc.New(b, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() { _ = svc.Start(ctx) }()

	// Trigger CPU alert
	b.Publish("cpu", cpusvc.Snapshot{
		TotalPercent: 90.0,
		Timestamp:    time.Now(),
	})

	select {
	case raw := <-svc.Output():
		snap, ok := raw.(alertssvc.Snapshot)
		if !ok {
			t.Fatal("expected alertssvc.Snapshot, got something else")
		}
		if len(snap.Active) == 0 {
			t.Error("expected at least one active alert for 90% CPU")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for alert snapshot")
	}
}
