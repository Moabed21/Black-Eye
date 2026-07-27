package resolver

import (
	"path/filepath"
	"strings"
)

// mountLabels maps well-known mount paths to "path (description)".
var mountLabels = map[string]string{
	"/":            "/ (Root Filesystem)",
	"/home":        "/home (User Home)",
	"/boot":        "/boot (Boot Partition)",
	"/boot/efi":    "/boot/efi (EFI System Partition)",
	"/var":         "/var (Variable Data)",
	"/var/log":     "/var/log (System Logs)",
	"/var/lib":     "/var/lib (Application Data)",
	"/var/lib/docker": "/var/lib/docker (Docker Storage)",
	"/tmp":         "/tmp (Temporary Files)",
	"/opt":         "/opt (Optional Software)",
	"/srv":         "/srv (Service Data)",
	"/usr":         "/usr (User Programs)",
	"/usr/local":   "/usr/local (Local Software)",
	"/data":        "/data (Data Volume)",
	"/mnt":         "/mnt (Mount Point)",
	"/media":       "/media (Removable Media)",
	"/run":         "/run (Runtime Data)",
}

// Mount resolves a filesystem mount point to a human-readable label.
// If the exact path is not in the table, a partial-match heuristic is applied.
// Paths that are already descriptive (e.g. /mnt/backups) are returned as-is.
func Mount(path string) string {
	if label, ok := mountLabels[path]; ok {
		return label
	}

	// Partial match: /var/lib/docker/... → use the closest parent label (excluding root /).
	p := path
	for p != "/" && p != "." {
		p = filepath.Dir(p)
		if p == "/" {
			break
		}
		if label, ok := mountLabels[p]; ok {
			// Show the full path but with the parent annotation.
			base := filepath.Base(path)
			parentLabel := strings.SplitN(label, " (", 2)
			if len(parentLabel) == 2 {
				desc := strings.TrimSuffix(parentLabel[1], ")")
				return path + " (" + desc + " — " + base + ")"
			}
		}
	}

	// Unknown path — show as-is (it's usually descriptive enough).
	return path
}
