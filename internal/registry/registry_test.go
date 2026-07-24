package registry

import (
	"context"
	"testing"
	"time"

	"blackeye/internal/bus"
	"blackeye/internal/config"
	"blackeye/internal/services"
)

type mockService struct {
	name   string
	topic  string
	out    chan interface{}
	stopCh chan struct{}
}

func newMockService(name, topic string) *mockService {
	return &mockService{
		name:   name,
		topic:  topic,
		out:    make(chan interface{}, 4),
		stopCh: make(chan struct{}),
	}
}

func (m *mockService) Name() string            { return m.name }
func (m *mockService) Topic() string           { return m.topic }
func (m *mockService) Output() <-chan interface{} { return m.out }
func (m *mockService) Health() services.HealthStatus {
	return services.HealthStatus{State: services.HealthOK}
}
func (m *mockService) Reload(cfg config.Config) {}

func (m *mockService) Start(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return nil
	case <-m.stopCh:
		return nil
	}
}

func (m *mockService) Stop() {
	select {
	case <-m.stopCh:
	default:
		close(m.stopCh)
	}
}

func TestRegistry_Lifecycle(t *testing.T) {
	b := bus.New()
	defer b.Close()

	reg := New(b)
	m1 := newMockService("Mock1", "mock1")
	m2 := newMockService("Mock2", "mock2")

	reg.Register(m1)
	reg.Register(m2)

	healths := reg.HealthAll()
	if len(healths) != 2 {
		t.Fatalf("expected 2 health statuses, got %d", len(healths))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reg.StartAll(ctx)

	sub := b.Subscribe("mock1")
	m1.out <- "snapshot1"

	select {
	case msg := <-sub:
		if msg != "snapshot1" {
			t.Errorf("expected snapshot1, got %v", msg)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timed out waiting for bridge snapshot")
	}

	reg.ReloadAll(config.Defaults())
	reg.StopAll()
}
