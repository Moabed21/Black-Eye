package resolver

import "strings"

// dockerStatuses maps Docker container status strings to "status (description)".
var dockerStatuses = map[string]string{
	"running":    "running (Active)",
	"paused":     "paused (Frozen)",
	"exited":     "exited (Stopped)",
	"restarting": "restarting (Recovering…)",
	"dead":       "dead (Unrecoverable)",
	"created":    "created (Not Started)",
	"removing":   "removing (Being Deleted)",
}

// DockerStatus resolves a Docker container status to a human-readable label.
// Returns the raw status if unrecognised.
func DockerStatus(status string) string {
	lower := strings.ToLower(strings.TrimSpace(status))
	if label, ok := dockerStatuses[lower]; ok {
		return label
	}
	return status
}

// DockerStatusIcon returns a leading icon character for the status.
// Useful for compact table display alongside DockerStatus.
func DockerStatusIcon(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "running":
		return "●"
	case "paused":
		return "⏸"
	case "exited":
		return "✕"
	case "restarting":
		return "↻"
	case "dead":
		return "☠"
	case "created":
		return "○"
	default:
		return "?"
	}
}
