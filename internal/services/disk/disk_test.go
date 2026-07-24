package disk_test

import (
	"context"
	"testing"
	"time"

	"blackeye/internal/config"
	"blackeye/internal/services/disk"
)

func TestDiskServiceStartStop(t *testing.T) {
	cfg := config.Defaults()
	cfg.Refresh.DashboardInterval = 1

	svc := disk.New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() { _ = svc.Start(ctx) }()

	select {
	case raw := <-svc.Output():
		snap, ok := raw.(disk.Snapshot)
		if !ok {
			t.Fatal("expected disk.Snapshot, got something else")
		}
		if len(snap.Disks) == 0 {
			t.Error("expected at least one disk entry")
		}
		for _, d := range snap.Disks {
			if d.RawMount == "" {
				t.Error("RawMount should not be empty")
			}
			if d.UsedPercent < 0 || d.UsedPercent > 100 {
				t.Errorf("UsedPercent out of bounds for %s: %.2f", d.RawMount, d.UsedPercent)
			}
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for disk snapshot")
	}
}
