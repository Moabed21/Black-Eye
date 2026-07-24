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
	hasNetAdmin bool
	hasPtrace   bool
)

// Init detects effective privileges. Call once at startup before rendering.
func Init() {
	once.Do(func() {
		isRoot = os.Geteuid() == 0
		hasKill = isRoot || checkCap(5)    // CAP_KILL
		hasNetAdmin = isRoot || checkCap(12) // CAP_NET_ADMIN
		hasPtrace = isRoot || checkCap(19)   // CAP_SYS_PTRACE
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

// CanFirewall reports whether the process can modify firewall rules.
// Requires CAP_NET_ADMIN or root.
func CanFirewall() bool {
	return hasNetAdmin
}

// CanNetConfig reports whether the process can modify network configuration.
// Requires CAP_NET_ADMIN or root.
func CanNetConfig() bool {
	return hasNetAdmin
}

// CanPackageManage reports whether the process can install/remove packages.
// Requires root — there's no fine-grained capability for package management.
func CanPackageManage() bool {
	return isRoot
}

// CanManageUsers reports whether the process can create/delete users.
// Requires root.
func CanManageUsers() bool {
	return isRoot
}

// CanReadShadow reports whether the process can read /etc/shadow.
// True if root, or if the process's effective group is 'shadow'.
func CanReadShadow() bool {
	if isRoot {
		return true
	}
	_, err := os.Open("/etc/shadow")
	return err == nil
}

// CanReadProcIO reports whether the process can read /proc/[pid]/io
// for other users' processes. Requires CAP_SYS_PTRACE or root.
func CanReadProcIO() bool {
	return hasPtrace
}

// checkCap checks if the current process has the specified Linux capability.
func checkCap(capNum uint) bool {
	hdr := unix.CapUserHeader{
		Version: unix.LINUX_CAPABILITY_VERSION_3,
		Pid:     0, // 0 = self
	}
	var data [2]unix.CapUserData
	if err := unix.Capget(&hdr, &data[0]); err != nil {
		return false
	}
	idx := capNum / 32
	bit := uint32(1 << (capNum % 32))
	if idx > 1 {
		return false
	}
	return data[idx].Effective&bit != 0
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

