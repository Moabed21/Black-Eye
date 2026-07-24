package dmesg_test

import (
	"context"
	"testing"
	"time"

	"blackeye/internal/config"
	"blackeye/internal/services/dmesg"
)

func TestDmesgServiceStartStop(t *testing.T) {
	cfg := config.Defaults()

	svc := dmesg.New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	go func() { _ = svc.Start(ctx) }()

	time.Sleep(100 * time.Millisecond)
	h := svc.Health()
	if h.State == "" {
		t.Error("expected non-empty health state")
	}
}
