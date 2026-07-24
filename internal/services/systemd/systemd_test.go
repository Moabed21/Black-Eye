package systemd_test

import (
	"context"
	"testing"
	"time"

	"blackeye/internal/config"
	systemdsvc "blackeye/internal/services/systemd"
)

func TestSystemdServiceStartStop(t *testing.T) {
	cfg := config.Defaults()
	cfg.Refresh.SystemdInterval = 1

	svc := systemdsvc.New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() { _ = svc.Start(ctx) }()

	select {
	case raw := <-svc.Output():
		_, ok := raw.(systemdsvc.Snapshot)
		if !ok {
			t.Fatal("expected systemdsvc.Snapshot, got something else")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for systemd snapshot")
	}
}
