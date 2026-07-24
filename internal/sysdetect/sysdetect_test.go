package sysdetect

import (
	"testing"
)

func TestDetect(t *testing.T) {
	Detect()
	prof := Profile()

	if prof.Distro == "" {
		t.Error("expected non-empty Distro")
	}
	if prof.DistroFamily == "" {
		t.Error("expected non-empty DistroFamily")
	}
	if prof.InitSystem == "" {
		t.Error("expected non-empty InitSystem")
	}
	if prof.DefaultShell == "" {
		t.Error("expected non-empty DefaultShell")
	}

	t.Logf("Detected Profile: %s", prof.String())
	for _, line := range prof.LogLines() {
		t.Log(line)
	}
}

func TestResolveFamily(t *testing.T) {
	tests := []struct {
		id     string
		idLike string
		want   string
	}{
		{"ubuntu", "debian", "debian"},
		{"debian", "", "debian"},
		{"fedora", "rhel", "rhel"},
		{"centos", "rhel fedora", "rhel"},
		{"manjaro", "arch", "arch"},
		{"arch", "", "arch"},
		{"alpine", "", "alpine"},
		{"opensuse-tumbleweed", "opensuse suse", "suse"},
		{"void", "", "void"},
		{"nixos", "", "nix"},
		{"unknown-distro", "debian", "debian"},
		{"custom-linux", "", "unknown"},
	}

	for _, tt := range tests {
		got := resolveFamily(tt.id, tt.idLike)
		if got != tt.want {
			t.Errorf("resolveFamily(%q, %q) = %q; want %q", tt.id, tt.idLike, got, tt.want)
		}
	}
}
