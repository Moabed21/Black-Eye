// Package tabs implements the five TUI tabs for BlackEye.
// Each tab is a bubbletea Model that subscribes to bus topics and renders snapshots.
// Tabs contain zero business logic — they only render pre-resolved data.
package tabs

// busMsg wraps a value received from the event bus for delivery as a bubbletea Msg.
type busMsg struct {
	topic string
	data  interface{}
}
