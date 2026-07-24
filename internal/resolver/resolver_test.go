package resolver_test

import (
	"testing"

	"blackeye/internal/resolver"
)

func TestPort_WellKnown(t *testing.T) {
	resolver.InitPorts()
	tests := []struct {
		port uint16
		want string
	}{
		{22, "ssh (Secure Shell)"},
		{80, "http (Web — unencrypted)"},
		{443, "https (Secure Web)"},
		{5432, "postgresql (PostgreSQL Database)"},
		{3306, "mysql (MySQL Database)"},
		{6379, "redis (Redis Cache)"},
		{27017, "mongodb (MongoDB Database)"},
	}
	for _, tt := range tests {
		got := resolver.Port(tt.port)
		if got != tt.want {
			t.Errorf("Port(%d) = %q, want %q", tt.port, got, tt.want)
		}
	}
}

func TestPort_Unknown(t *testing.T) {
	resolver.InitPorts()
	got := resolver.Port(49999)
	if got != ":49999" {
		t.Errorf("Port(49999) = %q, want \":49999\"", got)
	}
}

func TestTCPState(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"01", "ESTABLISHED (Connected)"},
		{"0A", "LISTEN (Waiting for connections)"},
		{"06", "TIME_WAIT (Cooling Down)"},
		{"0a", "LISTEN (Waiting for connections)"},   // lowercase
		{"FF", "FF"},                                  // unknown — raw passthrough
	}
	for _, tt := range tests {
		got := resolver.TCPState(tt.input)
		if got != tt.want {
			t.Errorf("TCPState(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestProcState(t *testing.T) {
	tests := []struct {
		input byte
		want  string
	}{
		{'R', "R (Running)"},
		{'S', "S (Sleeping)"},
		{'D', "D (Waiting for I/O)"},
		{'Z', "Z (Zombie — orphaned)"},
		{'T', "T (Stopped)"},
	}
	for _, tt := range tests {
		got := resolver.ProcState(tt.input)
		if got != tt.want {
			t.Errorf("ProcState(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestProcName_Daemon(t *testing.T) {
	tests := []struct {
		kernel  string
		cmdline string
		want    string
	}{
		{"nginx", "", "nginx (Web Server)"},
		{"sshd", "", "sshd (SSH Daemon)"},
		{"postgres", "", "postgres (PostgreSQL Database)"},
		{"dockerd", "", "dockerd (Docker Engine)"},
		{"vim", "", "vim"},                           // self-describing
		{"bash", "", "bash"},                         // self-describing
		{"python3", "/usr/bin/python3\x00script.py", "python3 (Python 3 Runtime)"},
	}
	for _, tt := range tests {
		got := resolver.ProcName(tt.kernel, tt.cmdline)
		if got != tt.want {
			t.Errorf("ProcName(%q, %q) = %q, want %q", tt.kernel, tt.cmdline, got, tt.want)
		}
	}
}

func TestDockerStatus(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"running", "running (Active)"},
		{"exited", "exited (Stopped)"},
		{"paused", "paused (Frozen)"},
		{"restarting", "restarting (Recovering…)"},
		{"unknown_state", "unknown_state"},
	}
	for _, tt := range tests {
		got := resolver.DockerStatus(tt.input)
		if got != tt.want {
			t.Errorf("DockerStatus(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestMount(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/", "/ (Root Filesystem)"},
		{"/home", "/home (User Home)"},
		{"/boot", "/boot (Boot Partition)"},
		{"/mnt/backups", "/mnt/backups (Mount Point — backups)"},
		{"/custom/backups", "/custom/backups"},
	}
	for _, tt := range tests {
		got := resolver.Mount(tt.input)
		if got != tt.want {
			t.Errorf("Mount(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input uint64
		want  string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1.0 KiB"},
		{1024 * 1024, "1.0 MiB"},
		{1024 * 1024 * 1024, "1.0 GiB"},
		{1536 * 1024 * 1024, "1.5 GiB"},
	}
	for _, tt := range tests {
		got := resolver.FormatBytes(tt.input)
		if got != tt.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatHelpers(t *testing.T) {
	if got := resolver.FormatRate(2048); got != "2.0 KB/s" {
		t.Errorf("FormatRate unexpected: %s", got)
	}
	if got := resolver.FormatPercent(85.4); got != "85.4%" {
		t.Errorf("FormatPercent unexpected: %s", got)
	}
	if got := resolver.FormatTemp(65.0); got != "65°C" {
		t.Errorf("FormatTemp unexpected: %s", got)
	}
}

func TestDockerStatusIcon(t *testing.T) {
	if icon := resolver.DockerStatusIcon("running"); icon != "●" {
		t.Errorf("expected ● for running, got %s", icon)
	}
	if icon := resolver.DockerStatusIcon("unknown"); icon != "?" {
		t.Errorf("expected ? for unknown, got %s", icon)
	}
}

func TestPortWithNumber(t *testing.T) {
	resolver.InitPorts()
	res := resolver.PortWithNumber(22)
	if res == "" {
		t.Error("expected non-empty PortWithNumber")
	}
}

func TestUserResolver(t *testing.T) {
	_ = resolver.InitUsers()
	rootName := resolver.ByUID(0)
	if rootName != "root" {
		t.Logf("ByUID(0) = %s (expected root if /etc/passwd has 0)", rootName)
	}

	strRes := resolver.ByUIDStr("0")
	if strRes != rootName {
		t.Errorf("ByUIDStr(\"0\") expected %s, got %s", rootName, strRes)
	}
}

func TestIface(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"lo", "lo (Loopback)"},
		{"eth0", "eth0 (Ethernet)"},
		{"wlan0", "wlan0 (Wi-Fi)"},
		{"docker0", "docker0 (Docker Bridge)"},
		{"tun0", "tun0 (VPN Tunnel)"},
		{"veth3a1b", "veth3a1b (Container Link)"},
	}
	for _, tt := range tests {
		got := resolver.Iface(tt.input)
		if got != tt.want {
			t.Errorf("Iface(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
