// Package diagnostics provides health checks and system risk auditing.
package diagnostics

import (
	"context"
	"fmt"
	"os/exec"
	"syscall"

	"blackeye/internal/services/firewall"
	"blackeye/internal/services/security"
)

type ItemStatus string

const (
	StatusPass ItemStatus = "PASS"
	StatusWarn ItemStatus = "WARN"
	StatusFail ItemStatus = "FAIL"
)

type DiagnosticItem struct {
	Category string
	Name     string
	Status   ItemStatus
	Detail   string
	Tip      string
}

type DiagnosticReport struct {
	OverallStatus ItemStatus
	PassCount     int
	WarnCount     int
	FailCount     int
	Items         []DiagnosticItem
}

func RunDiagnostics(secSnap *security.Snapshot, fwSnap *firewall.Snapshot) DiagnosticReport {
	var report DiagnosticReport

	// 1. Root Disk Free Space
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err == nil {
		freeBytes := stat.Bavail * uint64(stat.Bsize)
		totalBytes := stat.Blocks * uint64(stat.Bsize)
		usedPercent := 100.0 - (float64(freeBytes) / float64(totalBytes) * 100.0)

		item := DiagnosticItem{
			Category: "Storage",
			Name:     "Root (/) Filesystem Space",
			Detail:   fmt.Sprintf("%.1f%% used (%.1f GB free)", usedPercent, float64(freeBytes)/1073741824.0),
		}
		if usedPercent > 90.0 {
			item.Status = StatusFail
			item.Tip = "Clean up package cache (pacman -Sc / apt clean) or old log files"
		} else if usedPercent > 80.0 {
			item.Status = StatusWarn
			item.Tip = "Monitor storage usage"
		} else {
			item.Status = StatusPass
			item.Tip = "Healthy disk space"
		}
		report.Items = append(report.Items, item)
	}

	// 2. Failed Systemd Units
	cmd := exec.CommandContext(context.Background(), "systemctl", "list-units", "--state=failed", "--no-legend")
	output, err := cmd.Output()
	failedCount := 0
	if err == nil && len(output) > 0 {
		lines := 0
		for _, b := range output {
			if b == '\n' {
				lines++
			}
		}
		failedCount = lines
	}

	failedItem := DiagnosticItem{
		Category: "Services",
		Name:     "Failed Systemd Service Units",
		Detail:   fmt.Sprintf("%d failed units detected", failedCount),
	}
	if failedCount > 0 {
		failedItem.Status = StatusWarn
		failedItem.Tip = "Inspect failed services via Tab 5 (Services) or 'systemctl --failed'"
	} else {
		failedItem.Status = StatusPass
		failedItem.Tip = "All systemd service units running normally"
	}
	report.Items = append(report.Items, failedItem)

	// 3. Firewall Engine Security
	fwItem := DiagnosticItem{
		Category: "Network Security",
		Name:     "Active Host Firewall Protection",
	}
	if fwSnap != nil && fwSnap.Available && fwSnap.IsEnabled {
		fwItem.Status = StatusPass
		fwItem.Detail = fmt.Sprintf("Backend '%s' ACTIVE with %d active rules", fwSnap.BackendName, len(fwSnap.Rules))
		fwItem.Tip = "Firewall actively enforcing traffic policies"
	} else {
		fwItem.Status = StatusFail
		fwItem.Detail = "Firewall INACTIVE or no active backend"
		fwItem.Tip = "Enable firewall via Tab 7 (Firewall) or 'sudo ufw enable'"
	}
	report.Items = append(report.Items, fwItem)

	// 4. SSH Daemon Root Login Policy
	sshItem := DiagnosticItem{
		Category: "Access Control",
		Name:     "SSH Daemon Root Authentication",
	}
	if secSnap != nil && secSnap.SSHConfig.Configured {
		perm := secSnap.SSHConfig.PermitRootLogin
		if perm == "yes" {
			sshItem.Status = StatusFail
			sshItem.Detail = "PermitRootLogin is enabled ('yes')"
			sshItem.Tip = "Set 'PermitRootLogin no' in /etc/ssh/sshd_config"
		} else if perm == "prohibit-password" || perm == "without-password" {
			sshItem.Status = StatusWarn
			sshItem.Detail = "Root login allowed with pubkey only ('prohibit-password')"
			sshItem.Tip = "Ensure key authentication is enforced"
		} else {
			sshItem.Status = StatusPass
			sshItem.Detail = fmt.Sprintf("PermitRootLogin is secure ('%s')", perm)
			sshItem.Tip = "Root SSH login disabled"
		}
	} else {
		sshItem.Status = StatusPass
		sshItem.Detail = "Standard SSH security policy active"
		sshItem.Tip = "Default configuration"
	}
	report.Items = append(report.Items, sshItem)

	// 5. SUID Privilege Escalation Risk Audit
	suidItem := DiagnosticItem{
		Category: "Binary Audit",
		Name:     "SUID / SGID Executable Risk Scan",
	}
	riskCount := 0
	if secSnap != nil {
		for _, b := range secSnap.SUIDBinaries {
			if b.IsRisk {
				riskCount++
			}
		}
	}
	if riskCount > 0 {
		suidItem.Status = StatusWarn
		suidItem.Detail = fmt.Sprintf("%d non-standard SUID binaries identified", riskCount)
		suidItem.Tip = "Review SUID audit sub-panel in Tab 9 (Users & Security)"
	} else {
		suidItem.Status = StatusPass
		suidItem.Detail = "No unknown/risky SUID binaries detected"
		suidItem.Tip = "SUID binary integrity clean"
	}
	report.Items = append(report.Items, suidItem)

	// Summarize counts
	report.OverallStatus = StatusPass
	for _, item := range report.Items {
		switch item.Status {
		case StatusPass:
			report.PassCount++
		case StatusWarn:
			report.WarnCount++
			if report.OverallStatus == StatusPass {
				report.OverallStatus = StatusWarn
			}
		case StatusFail:
			report.FailCount++
			report.OverallStatus = StatusFail
		}
	}

	return report
}
