package netstats_test

import (
	"context"
	"testing"
	"time"

	"blackeye/internal/config"
	"blackeye/internal/services/netstats"
)

func TestNetStatsServiceStartStop(t *testing.T) {
	cfg := config.Defaults()

	svc := netstats.New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() { _ = svc.Start(ctx) }()

	time.Sleep(100 * time.Millisecond)
	h := svc.Health()
	if h.State == "" {
		t.Error("expected non-empty health state")
	}
}
