// Package privilege detects the effective privileges of the running process.
// BlackEye uses this to show or hide destructive keybindings (kill, stop).
// Destructive actions are hidden entirely — not just disabled — when running
// without sufficient privilege.
package privilege

import (
	"os"
	"sync"

	"golang.org/x/sys/unix"
)

var (
	once       sync.Once
	hasKill    bool
	isRoot     bool
	hasDocker  bool
)

// Init detects effective privileges. Call once at startup before rendering.
func Init() {
	once.Do(func() {
		isRoot = os.Geteuid() == 0
		hasKill = isRoot || checkCapKill()
		hasDocker = checkDockerSocket()
	})
}

// CanKill reports whether the process can send signals to other processes.
// True when running as root or when CAP_KILL is set.
func CanKill() bool {
	return hasKill
}

// IsRoot reports whether the process is running as root (euid == 0).
func IsRoot() bool {
	return isRoot
}

// HasDockerAccess reports whether /var/run/docker.sock is accessible.
func HasDockerAccess() bool {
	return hasDocker
}

// checkCapKill uses the Linux capabilities API to test for CAP_KILL (5).
func checkCapKill() bool {
	// Use the prctl/capget syscall to check the effective capability set.
	// CAP_KILL is capability number 5.
	hdr := unix.CapUserHeader{
		Version: unix.LINUX_CAPABILITY_VERSION_3,
		Pid:     0, // 0 = self
	}
	var data [2]unix.CapUserData
	if err := unix.Capget(&hdr, &data[0]); err != nil {
		return false
	}
	const capKill = uint32(1 << 5)
	return data[0].Effective&capKill != 0
}

// checkDockerSocket tests whether the Docker socket is accessible.
func checkDockerSocket() bool {
	_, err := os.Stat("/var/run/docker.sock")
	if err != nil {
		return false
	}
	f, err := os.Open("/var/run/docker.sock")
	if err != nil {
		return false
	}
	f.Close()
	return true
}
