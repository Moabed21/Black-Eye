package security

import (
	"testing"
)

func TestScanSUIDBinaries(t *testing.T) {
	suids := ScanSUIDBinaries()
	if suids == nil {
		t.Fatalf("expected non-nil suid list, got nil")
	}
}

func TestParseSSHConfig(t *testing.T) {
	cfg := ParseSSHConfig()
	if cfg.MaxAuthTries <= 0 {
		t.Fatalf("expected valid MaxAuthTries, got %d", cfg.MaxAuthTries)
	}
}

func TestCalculateRiskScore(t *testing.T) {
	suid := []SUIDBinary{{Path: "/tmp/fake", IsRisk: true}}
	ssh := SSHConfig{PermitRootLogin: "yes"}
	failures := []AuthFailure{{IP: "1.2.3.4", Count: 5}}

	score := CalculateRiskScore(suid, ssh, failures)
	if score <= 0 {
		t.Fatalf("expected positive risk score for vulnerable config, got %d", score)
	}
}
