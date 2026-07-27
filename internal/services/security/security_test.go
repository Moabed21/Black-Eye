package security

import (
	"testing"
)

func TestParseSSHConfig(t *testing.T) {
	cfg := parseSSHConfig()
	if !cfg.Configured {
		t.Log("sshd_config not present or unreadable, defaults used")
	} else {
		t.Logf("sshd_config parsed: PermitRootLogin=%s, PasswordAuth=%s", cfg.PermitRootLogin, cfg.PasswordAuthentication)
	}
}

func TestScanSUIDBinaries(t *testing.T) {
	suidList := scanSUIDBinaries()
	t.Logf("Found %d SUID/SGID binaries on host", len(suidList))
	for _, b := range suidList {
		if b.IsRisk {
			t.Logf("Flagged potential custom/risk SUID binary: %s", b.Path)
		}
	}
}
