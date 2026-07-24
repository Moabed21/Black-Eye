package advanced_test

import (
	"context"
	"testing"
	"time"

	"blackeye/internal/config"
	advancedsvc "blackeye/internal/services/advanced"
)

func TestAdvancedServiceStartStop(t *testing.T) {
	cfg := config.Defaults()

	svc := advancedsvc.New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() { _ = svc.Start(ctx) }()

	select {
	case raw := <-svc.Output():
		_, ok := raw.(advancedsvc.Snapshot)
		if !ok {
			t.Fatal("expected advancedsvc.Snapshot, got something else")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for advanced snapshot")
	}
}
