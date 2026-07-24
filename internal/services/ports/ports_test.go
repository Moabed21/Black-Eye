package ports_test

import (
	"context"
	"testing"
	"time"

	"blackeye/internal/config"
	portssvc "blackeye/internal/services/ports"
)

func TestPortsServiceStartStop(t *testing.T) {
	cfg := config.Defaults()
	cfg.Refresh.PortsInterval = 1

	svc := portssvc.New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() { _ = svc.Start(ctx) }()

	select {
	case raw := <-svc.Output():
		_, ok := raw.(portssvc.PortsSnapshot)
		if !ok {
			t.Fatal("expected portssvc.PortsSnapshot, got something else")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for ports snapshot")
	}
}
