package diagnostics

import (
	"testing"

	"blackeye/internal/services/firewall"
	"blackeye/internal/services/security"
)

func TestRunDiagnostics(t *testing.T) {
	secSnap := &security.Snapshot{
		SSHConfig: security.SSHConfig{
			Configured:      true,
			PermitRootLogin: "no",
		},
	}
	fwSnap := &firewall.Snapshot{
		BackendName: "ufw",
		Available:   true,
		IsEnabled:   true,
		Rules:       []firewall.Rule{{ID: "1", Direction: "IN", Action: "ALLOW"}},
	}

	report := RunDiagnostics(secSnap, fwSnap)
	if report.PassCount == 0 {
		t.Fatalf("expected at least 1 passing diagnostic item, got 0")
	}

	if len(report.Items) == 0 {
		t.Fatalf("expected diagnostic items in report, got 0")
	}
}
