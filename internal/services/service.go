// Package services defines the Service interface that every BlackEye
// data-collection microservice must implement.
package services

import (
	"context"

	"blackeye/internal/config"
)

// HealthState represents a service's operational state.
type HealthState string

const (
	HealthOK       HealthState = "OK"
	HealthDegraded HealthState = "Degraded"
	HealthDown     HealthState = "Down"
)

// HealthStatus is the current health of a service.
type HealthStatus struct {
	State  HealthState
	Reason string // human-readable explanation when not OK
}

// Service is the interface every BlackEye microservice must implement.
// Each service runs in its own goroutine, publishes typed snapshots to
// the event bus, and can be started, stopped, and reloaded independently.
type Service interface {
	// Name returns the human-readable service name, e.g. "CPU Collector".
	Name() string

	// Topic returns the event bus topic this service publishes to.
	Topic() string

	// Start begins the collection loop. It blocks until ctx is cancelled.
	// The service must stop all goroutines before Start returns.
	Start(ctx context.Context) error

	// Stop signals the service to shut down gracefully.
	// It must be safe to call Stop multiple times.
	Stop()

	// Reload applies a new configuration without restarting the service.
	Reload(cfg config.Config)

	// Health returns the current operational state of the service.
	Health() HealthStatus

	// Output returns the read-only channel on which the service publishes
	// typed snapshots. Callers should not close this channel.
	Output() <-chan interface{}
}
