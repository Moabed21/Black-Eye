// Package tabs implements the five TUI tabs for BlackEye.
// Each tab is a bubbletea Model that subscribes to bus topics and renders snapshots.
// Tabs contain zero business logic — they only render pre-resolved data.
package tabs

import tea "github.com/charmbracelet/bubbletea"

// busMsg wraps a value received from the event bus for delivery as a bubbletea Msg.
type busMsg struct {
	ch    <-chan interface{}
	topic string
	data  interface{}
}

// listenChan returns a tea.Cmd that listens to a specific channel and tags it.
func listenChan(ch <-chan interface{}, topic string) tea.Cmd {
	return func() tea.Msg {
		v := <-ch
		return busMsg{ch: ch, topic: topic, data: v}
	}
}
