package network_test

import (
	"context"
	"testing"
	"time"

	"blackeye/internal/config"
	netsvc "blackeye/internal/services/network"
)

func TestNetworkServiceStartStop(t *testing.T) {
	cfg := config.Defaults()
	cfg.Refresh.DashboardInterval = 1

	svc := netsvc.New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() { _ = svc.Start(ctx) }()

	select {
	case raw := <-svc.Output():
		snap, ok := raw.(netsvc.Snapshot)
		if !ok {
			t.Fatal("expected netsvc.Snapshot, got something else")
		}
		if len(snap.Ifaces) == 0 {
			t.Error("expected at least 1 network interface")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for network snapshot")
	}
}
