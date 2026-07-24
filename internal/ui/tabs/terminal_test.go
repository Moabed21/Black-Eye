package tabs_test

import (
	"testing"

	"blackeye/internal/ui/tabs"
)

func TestTerminalModel(t *testing.T) {
	term := tabs.NewTerminal()

	if term.IsFocused() {
		t.Error("expected terminal to start unfocused (navigation mode)")
	}

	if term.IsStarted() {
		t.Error("expected terminal shell to start unstarted before size message")
	}
}
