package sysdetect

import (
	"os"
	"strings"
)

// probeFile returns true if the path exists and is accessible (file or dir).
func probeFile(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// probeDir returns true if the path exists and is a directory.
func probeDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// probeSocket returns true if the path is an accessible Unix socket.
func probeSocket(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	// Check if it's a socket by file mode.
	return info.Mode()&os.ModeSocket != 0
}

// probeAnyBinary returns true if any of the given paths exist.
func probeAnyBinary(paths ...string) bool {
	for _, p := range paths {
		if probeFile(p) {
			return true
		}
	}
	return false
}

// findBinary returns the first path that exists, or "" if none do.
func findBinary(paths ...string) string {
	for _, p := range paths {
		if probeFile(p) {
			return p
		}
	}
	return ""
}

// containsAny returns true if s contains any of the substrings.
func containsAny(s string, subs ...string) bool {
	lower := strings.ToLower(s)
	for _, sub := range subs {
		if strings.Contains(lower, strings.ToLower(sub)) {
			return true
		}
	}
	return false
}
