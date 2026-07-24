package sysdetect

import (
	"bufio"
	"os"
	"strings"
)

// detectDistro parses /etc/os-release to identify the distribution.
// Returns (id, family, version, prettyName).
func detectDistro() (string, string, string, string) {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		// Fallback: try /usr/lib/os-release (used by some minimal images).
		f, err = os.Open("/usr/lib/os-release")
		if err != nil {
			return "unknown", "unknown", "", "Unknown Linux"
		}
	}
	defer f.Close()

	var id, idLike, versionID, prettyName string

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		key, value := parseOSReleaseLine(line)
		switch key {
		case "ID":
			id = value
		case "ID_LIKE":
			idLike = value
		case "VERSION_ID":
			versionID = value
		case "PRETTY_NAME":
			prettyName = value
		}
	}

	if id == "" {
		id = "unknown"
	}
	if prettyName == "" {
		prettyName = id
	}

	family := resolveFamily(id, idLike)

	return id, family, versionID, prettyName
}

// parseOSReleaseLine parses a KEY=VALUE or KEY="VALUE" line.
func parseOSReleaseLine(line string) (string, string) {
	line = strings.TrimSpace(line)
	if line == "" || line[0] == '#' {
		return "", ""
	}
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return "", ""
	}
	key := parts[0]
	value := parts[1]
	// Strip surrounding quotes.
	value = strings.Trim(value, "\"'")
	return key, value
}

// resolveFamily maps a distro ID (and optional ID_LIKE) to a family.
// Family grouping determines which package manager and service commands to use.
func resolveFamily(id, idLike string) string {
	// Direct ID mapping (highest priority).
	switch id {
	case "debian", "ubuntu", "linuxmint", "pop", "elementary", "zorin",
		"kali", "parrot", "raspbian", "lmde", "mx", "antiX", "deepin",
		"bunsenlabs", "devuan", "pureos", "trisquel":
		return "debian"
	case "fedora", "rhel", "centos", "rocky", "almalinux", "ol",
		"amzn", "scientific", "clearos", "eurolinux", "navy":
		return "rhel"
	case "arch", "manjaro", "endeavouros", "artix", "garuda",
		"cachyos", "arcolinux", "blackarch":
		return "arch"
	case "alpine":
		return "alpine"
	case "opensuse-leap", "opensuse-tumbleweed", "sles", "opensuse":
		return "suse"
	case "gentoo", "funtoo", "calculate":
		return "gentoo"
	case "void":
		return "void"
	case "nixos":
		return "nix"
	case "solus":
		return "solus"
	case "clear-linux-os":
		return "clearlinux"
	}

	// Fallback: check ID_LIKE (space-separated list of parent distros).
	if idLike != "" {
		likes := strings.Fields(idLike)
		for _, like := range likes {
			switch like {
			case "debian", "ubuntu":
				return "debian"
			case "rhel", "fedora", "centos":
				return "rhel"
			case "arch":
				return "arch"
			case "suse", "opensuse":
				return "suse"
			case "gentoo":
				return "gentoo"
			}
		}
	}

	return "unknown"
}
