package firewall_test

import (
	"context"
	"testing"
	"time"

	"blackeye/internal/config"
	firewallsvc "blackeye/internal/services/firewall"
)

func TestFirewallServiceStartStop(t *testing.T) {
	cfg := config.Defaults()

	svc := firewallsvc.New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() { _ = svc.Start(ctx) }()

	select {
	case raw := <-svc.Output():
		_, ok := raw.(firewallsvc.Snapshot)
		if !ok {
			t.Fatal("expected firewallsvc.Snapshot, got something else")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for firewall snapshot")
	}
}
