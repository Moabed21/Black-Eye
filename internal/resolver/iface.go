package resolver

import (
	"os"
	"strings"
)

// Iface resolves a Linux network interface name to "name (type)" format.
// Detection uses prefix matching and /sys/class/net/<iface>/type.
func Iface(name string) string {
	kind := ifaceKind(name)
	if kind == "" {
		return name // self-describing or unknown
	}
	return name + " (" + kind + ")"
}

// ifaceKind returns the human-readable interface type for name.
// Returns "" when the name is already descriptive enough.
func ifaceKind(name string) string {
	// Exact names.
	switch name {
	case "lo":
		return "Loopback"
	case "docker0":
		return "Docker Bridge"
	case "virbr0":
		return "Virtual Bridge"
	}

	// Prefix patterns (ordered longest-first to avoid prefix collisions).
	prefixes := []struct {
		prefix string
		kind   string
	}{
		{"eth", "Ethernet"},
		{"enp", "Ethernet"},
		{"ens", "Ethernet"},
		{"eno", "Ethernet"},
		{"em", "Ethernet"},
		{"wlan", "Wi-Fi"},
		{"wlp", "Wi-Fi"},
		{"wls", "Wi-Fi"},
		{"tun", "VPN Tunnel"},
		{"tap", "VPN Tunnel"},
		{"veth", "Container Link"},
		{"br-", "Docker Network"},
		{"virbr", "Virtual Bridge"},
		{"bond", "Bonded Interface"},
		{"team", "Teamed Interface"},
		{"dummy", "Dummy Interface"},
		{"ib", "InfiniBand"},
		{"sit", "IPv6-in-IPv4 Tunnel"},
	}

	for _, p := range prefixes {
		if strings.HasPrefix(name, p.prefix) {
			return p.kind
		}
	}

	// Consult /sys/class/net/<name>/type for unrecognized names.
	return sysNetType(name)
}

// sysNetType reads the ARPHRD type from /sys/class/net/<name>/type and maps
// it to a human-readable string. Returns "" on any error.
func sysNetType(name string) string {
	data, err := os.ReadFile("/sys/class/net/" + name + "/type")
	if err != nil {
		return ""
	}
	switch strings.TrimSpace(string(data)) {
	case "1":
		return "Ethernet"
	case "772":
		return "Loopback"
	case "512":
		return "PPP"
	case "768":
		return "IP Tunnel"
	case "776":
		return "IPv6 Tunnel"
	default:
		return ""
	}
}
