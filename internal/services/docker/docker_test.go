package docker_test

import (
	"context"
	"testing"
	"time"

	"blackeye/internal/config"
	dockersvc "blackeye/internal/services/docker"
)

func TestDockerServiceStartStop(t *testing.T) {
	cfg := config.Defaults()
	cfg.Refresh.DockerInterval = 1

	svc := dockersvc.New(cfg, false)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() { _ = svc.Start(ctx) }()

	select {
	case raw := <-svc.Output():
		_, ok := raw.(dockersvc.Snapshot)
		if !ok {
			t.Fatal("expected dockersvc.Snapshot, got something else")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for docker snapshot")
	}
}
