package sysinfo_test

import (
	"context"
	"testing"
	"time"

	"blackeye/internal/config"
	"blackeye/internal/services/sysinfo"
)

func TestSysInfoServiceStartStop(t *testing.T) {
	cfg := config.Defaults()
	cfg.Refresh.SystemdInterval = 1

	svc := sysinfo.New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() { _ = svc.Start(ctx) }()

	select {
	case raw := <-svc.Output():
		snap, ok := raw.(sysinfo.Snapshot)
		if !ok {
			t.Fatal("expected sysinfo.Snapshot, got something else")
		}
		if snap.KernelVersion == "" {
			t.Error("KernelVersion should not be empty")
		}
		if snap.Uptime == "" {
			t.Error("Uptime should not be empty")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for sysinfo snapshot")
	}
}
