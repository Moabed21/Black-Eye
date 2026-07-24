// Package sysdetect probes the Linux environment at startup to build a
// SystemProfile. Every feature in BlackEye checks this profile to decide
// which backend to use and which tabs to show. No feature may assume a
// specific distro, init system, or package manager.
//
// All detection is done by reading /proc, /sys, /etc, and probing file
// paths — zero shelling out.
package sysdetect

import (
	"os"
	"sync"
)

// SystemProfile holds the detected characteristics of the running system.
// Populated once at startup via Detect() and immutable thereafter.
type SystemProfile struct {
	// Distro identity (from /etc/os-release).
	Distro        string // "ubuntu", "fedora", "arch", "alpine", "debian", etc.
	DistroFamily  string // "debian", "rhel", "arch", "alpine", "suse", "gentoo", "void", "nix", "unknown"
	DistroVersion string // "22.04", "39", "rolling"
	DistroName    string // Pretty name: "Ubuntu 22.04 LTS"

	// Init system.
	InitSystem string // "systemd", "openrc", "sysvinit", "runit", "s6", "unknown"
	HasSystemd bool
	HasOpenRC  bool

	// Package manager.
	PkgManager string // "apt", "dnf", "yum", "pacman", "zypper", "apk", "emerge", "xbps", "nix", "none"
	PkgBinary  string // full path: "/usr/bin/apt"

	// Firewall.
	Firewall    string // "nftables", "iptables-nft", "iptables-legacy", "ufw", "firewalld", "none"
	FirewallBin string // full path to the detected binary

	// Network manager.
	NetManager string // "NetworkManager", "systemd-networkd", "ifupdown", "netplan", "connman", "none"

	// Container runtime.
	Container     string // "docker", "podman", "containerd", "none"
	ContainerSock string // socket path

	// Security modules.
	HasSELinux  bool
	HasAppArmor bool

	// Storage.
	HasLVM bool

	// Shell.
	DefaultShell string // from $SHELL
}

var (
	once    sync.Once
	profile SystemProfile
)

// Detect probes the system and populates the global profile.
// Safe to call multiple times — only the first call does work.
func Detect() {
	once.Do(func() {
		profile = detect()
	})
}

// Profile returns the detected system profile. Must call Detect() first.
func Profile() SystemProfile {
	return profile
}

// detect runs all probes and assembles the profile.
func detect() SystemProfile {
	p := SystemProfile{}

	// 1. Distro identity.
	p.Distro, p.DistroFamily, p.DistroVersion, p.DistroName = detectDistro()

	// 2. Init system.
	p.InitSystem = detectInitSystem()
	p.HasSystemd = p.InitSystem == "systemd"
	p.HasOpenRC = p.InitSystem == "openrc"

	// 3. Package manager.
	p.PkgManager, p.PkgBinary = detectPackageManager()

	// 4. Firewall.
	p.Firewall, p.FirewallBin = detectFirewall()

	// 5. Network manager.
	p.NetManager = detectNetManager()

	// 6. Container runtime.
	p.Container, p.ContainerSock = detectContainerRuntime()

	// 7. Security modules.
	p.HasSELinux = probeFile("/sys/fs/selinux/enforce")
	p.HasAppArmor = probeDir("/sys/kernel/security/apparmor")

	// 8. Storage.
	p.HasLVM = probeAnyBinary("/sbin/lvm", "/usr/sbin/lvm", "/usr/bin/lvm")

	// 9. Shell.
	p.DefaultShell = os.Getenv("SHELL")
	if p.DefaultShell == "" {
		p.DefaultShell = "/bin/sh"
	}

	return p
}

// detectInitSystem determines the init system by inspecting PID 1.
func detectInitSystem() string {
	// Read the symlink of /proc/1/exe to identify PID 1.
	exe, err := os.Readlink("/proc/1/exe")
	if err != nil {
		// Fallback: check for known paths.
		if probeFile("/run/systemd/system") {
			return "systemd"
		}
		if probeFile("/run/openrc") {
			return "openrc"
		}
		return "unknown"
	}

	// Check the executable name.
	switch {
	case containsAny(exe, "systemd"):
		return "systemd"
	case containsAny(exe, "openrc", "openrc-init"):
		return "openrc"
	case containsAny(exe, "runit"):
		return "runit"
	case containsAny(exe, "s6-svscan", "s6"):
		return "s6"
	case containsAny(exe, "init"):
		// Could be sysvinit or busybox init.
		// Check for sysvinit-specific markers.
		if probeFile("/etc/inittab") {
			return "sysvinit"
		}
		// Busybox init on Alpine without OpenRC marker.
		if probeDir("/run/openrc") {
			return "openrc"
		}
		return "sysvinit"
	}

	// Additional fallback checks.
	if probeDir("/run/systemd/system") {
		return "systemd"
	}
	if probeDir("/run/openrc") {
		return "openrc"
	}

	return "unknown"
}

// detectPackageManager probes for known package manager binaries.
// Returns (name, binary_path). Order matters — first match wins.
func detectPackageManager() (string, string) {
	probes := []struct {
		name string
		paths []string
	}{
		{"apt", []string{"/usr/bin/apt", "/usr/bin/apt-get"}},
		{"dnf", []string{"/usr/bin/dnf"}},
		{"yum", []string{"/usr/bin/yum"}},
		{"pacman", []string{"/usr/bin/pacman"}},
		{"zypper", []string{"/usr/bin/zypper"}},
		{"apk", []string{"/sbin/apk", "/usr/sbin/apk"}},
		{"emerge", []string{"/usr/bin/emerge"}},
		{"xbps", []string{"/usr/bin/xbps-install"}},
		{"nix", []string{"/usr/bin/nix-env", "/nix/var/nix/profiles/default/bin/nix-env"}},
	}

	for _, p := range probes {
		for _, path := range p.paths {
			if probeFile(path) {
				return p.name, path
			}
		}
	}
	return "none", ""
}

// detectFirewall probes for firewall backends.
func detectFirewall() (string, string) {
	// Check ufw first (it's a frontend — if present, it's the primary interface).
	if path := findBinary("/usr/sbin/ufw", "/usr/bin/ufw"); path != "" {
		return "ufw", path
	}

	// Check firewalld via D-Bus marker file.
	if probeFile("/run/dbus/system_bus_socket") {
		// Check if firewalld is running by looking for its PID file or runtime dir.
		if probeFile("/run/firewalld/firewalld.pid") || probeDir("/run/firewalld") {
			return "firewalld", "/usr/bin/firewall-cmd"
		}
	}

	// Check nftables.
	if path := findBinary("/usr/sbin/nft", "/sbin/nft"); path != "" {
		return "nftables", path
	}

	// Check iptables.
	if path := findBinary("/usr/sbin/iptables", "/sbin/iptables"); path != "" {
		// Determine if this is iptables-nft or iptables-legacy.
		// Check symlink target.
		target, err := os.Readlink(path)
		if err == nil && containsAny(target, "nft") {
			return "iptables-nft", path
		}
		return "iptables-legacy", path
	}

	return "none", ""
}

// detectNetManager determines the active network manager.
func detectNetManager() string {
	// NetworkManager: check D-Bus or PID.
	if probeFile("/run/NetworkManager/NetworkManager.pid") ||
		probeDir("/run/NetworkManager") {
		return "NetworkManager"
	}

	// systemd-networkd: check its runtime directory.
	if probeDir("/run/systemd/netif") {
		return "systemd-networkd"
	}

	// netplan (Ubuntu): check config directory.
	if probeDir("/etc/netplan") {
		return "netplan"
	}

	// connman: check PID file.
	if probeFile("/run/connman/connmand.pid") || probeDir("/run/connman") {
		return "connman"
	}

	// ifupdown (Debian traditional).
	if probeFile("/etc/network/interfaces") {
		return "ifupdown"
	}

	return "none"
}

// detectContainerRuntime probes for container runtime sockets.
func detectContainerRuntime() (string, string) {
	sockets := []struct {
		name string
		paths []string
	}{
		{"docker", []string{"/var/run/docker.sock", "/run/docker.sock"}},
		{"podman", []string{"/var/run/podman/podman.sock", "/run/podman/podman.sock", "/run/user/1000/podman/podman.sock"}},
		{"containerd", []string{"/run/containerd/containerd.sock"}},
	}

	for _, s := range sockets {
		for _, path := range s.paths {
			if probeSocket(path) {
				return s.name, path
			}
		}
	}
	return "none", ""
}
