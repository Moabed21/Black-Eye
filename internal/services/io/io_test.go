package io_test

import (
	"context"
	"testing"
	"time"

	"blackeye/internal/config"
	iosvc "blackeye/internal/services/io"
)

func TestIOServiceStartStop(t *testing.T) {
	cfg := config.Defaults()
	cfg.Refresh.DashboardInterval = 1

	svc := iosvc.New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() { _ = svc.Start(ctx) }()

	select {
	case raw := <-svc.Output():
		_, ok := raw.(iosvc.Snapshot)
		if !ok {
			t.Fatal("expected iosvc.Snapshot, got something else")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for io snapshot")
	}
}
