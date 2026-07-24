package sysdetect

import "fmt"

// String returns a one-line summary suitable for logging.
func (p SystemProfile) String() string {
	return fmt.Sprintf("%s (%s) | init:%s | pkg:%s | fw:%s | net:%s | ctr:%s",
		p.DistroName, p.DistroFamily,
		p.InitSystem,
		p.PkgManager,
		p.Firewall,
		p.NetManager,
		p.Container,
	)
}

// LogLines returns multi-line detailed output for debug logging.
func (p SystemProfile) LogLines() []string {
	lines := []string{
		fmt.Sprintf("  Distro:    %s (%s family, version %s)", p.Distro, p.DistroFamily, p.DistroVersion),
		fmt.Sprintf("  Init:      %s", p.InitSystem),
		fmt.Sprintf("  Packages:  %s (%s)", p.PkgManager, p.PkgBinary),
		fmt.Sprintf("  Firewall:  %s (%s)", p.Firewall, p.FirewallBin),
		fmt.Sprintf("  NetMgr:    %s", p.NetManager),
		fmt.Sprintf("  Container: %s (%s)", p.Container, p.ContainerSock),
		fmt.Sprintf("  Security:  SELinux=%v AppArmor=%v", p.HasSELinux, p.HasAppArmor),
		fmt.Sprintf("  Storage:   LVM=%v", p.HasLVM),
		fmt.Sprintf("  Shell:     %s", p.DefaultShell),
	}
	return lines
}
