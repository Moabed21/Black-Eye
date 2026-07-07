// Package registry manages the lifecycle of all BlackEye services.
// It starts, stops, and reloads services in dependency order, and
// bridges each service's output channel to the shared event bus.
package registry

import (
	"context"
	"sync"

	"blackeye/internal/bus"
	"blackeye/internal/config"
	"blackeye/internal/services"
)

// Registry holds all registered services and orchestrates their lifecycles.
type Registry struct {
	mu       sync.RWMutex
	svcs     []services.Service
	bus      *bus.Bus
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// New creates a Registry that will publish service output to b.
func New(b *bus.Bus) *Registry {
	return &Registry{bus: b}
}

// Register adds a service. Must be called before StartAll.
func (r *Registry) Register(s services.Service) {
	r.mu.Lock()
	r.svcs = append(r.svcs, s)
	r.mu.Unlock()
}

// StartAll starts every registered service in its own goroutine and
// bridges its output channel to the event bus.
func (r *Registry) StartAll(ctx context.Context) {
	ctx, r.cancel = context.WithCancel(ctx)

	r.mu.RLock()
	svcs := r.svcs
	r.mu.RUnlock()

	for _, svc := range svcs {
		svc := svc // capture for goroutine

		// Bridge: forward service output to the event bus.
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			out := svc.Output()
			for {
				select {
				case <-ctx.Done():
					return
				case snapshot, ok := <-out:
					if !ok {
						return
					}
					r.bus.Publish(svc.Topic(), snapshot)
				}
			}
		}()

		// Run the service's collection loop.
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			_ = svc.Start(ctx) // errors are reflected in Health()
		}()
	}
}

// StopAll cancels the context and waits for all services to exit.
func (r *Registry) StopAll() {
	if r.cancel != nil {
		r.cancel()
	}
	r.wg.Wait()
}

// ReloadAll propagates a new configuration to every service.
func (r *Registry) ReloadAll(cfg config.Config) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, svc := range r.svcs {
		svc.Reload(cfg)
	}
}

// HealthAll returns the current health status of every service.
func (r *Registry) HealthAll() []services.HealthStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]services.HealthStatus, len(r.svcs))
	for i, svc := range r.svcs {
		out[i] = svc.Health()
	}
	return out
}
