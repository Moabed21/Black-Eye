# BlackEye v1.2.1 — Linux System Administration Dashboard

![Version](https://img.shields.io/badge/version-1.2.1-gold.svg)
![Build Status](https://img.shields.io/badge/build-passing-brightgreen.svg)
![License](https://img.shields.io/badge/license-MIT-blue.svg)
![Go Version](https://img.shields.io/badge/go-%E2%89%A51.21-00ADD8.svg)

A real-time, high-performance TUI (Terminal User Interface) system monitor and administration dashboard built in Go. BlackEye reads directly from Linux kernel virtual filesystems (`/proc`, `/sys`), D-Bus, and system sockets with zero external binary dependencies.

```
┌─[1] Dashboard─[2] Processes─[3] Network─[4] Docker─[5] Services─[6] Terminal─[7] Firewall─[8] Packages─[9] Users─[0] Advanced─┐
│  System Info                                                                                                                   │
│  Hostname: myserver  │  Uptime: 14 days  │  Kernel: 6.12.5-amd64  │  OS: Linux x86_64                                          │
│                                                                                                                                │
│  CPU                                                                                                                           │
│  Total:  23.4%  ███████░░░░░░░░░░░░░░░░░░░░░░                                                                                  │
│  Core0:  31.2%  ██████████░░░░░░░░░░                                                                                           │
│  Core1:  15.6%  ████░░░░░░░░░░░░░░░░                                                                                           │
│                                                                                                                                │
│  Memory                                                                                                                        │
│  RAM:  5.2 GiB / 15.6 GiB  ████████████░░░░░░░░░░░░  33%                                                                     │
│  Swap: 0.1 GiB /  4.0 GiB  █░░░░░░░░░░░░░░░░░░░░░░░   3%                                                                    │
└────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┘
  BlackEye v1.2.1  │  ● normal                                                                      q quit  │  ? help  │  1–9,0 tabs
```

**Theme:** Premium **Navy Blue & Gold** color palette with dynamic highlighting, alert toasts (⚠/🔴), and drag-and-drop panel header positioning.

---

## What's New in v1.2.1

- **Embedded PTY Terminal Engine (Tab 6)**: Built-in pseudoterminal shell launcher with a column-based cursor engine supporting Starship, Zsh, Bash, Htop, Vim, ANSI 256/TrueColor, and OSC title sequence filtering.
- **Expanded 10-Tab Dashboard Suite**: Added dedicated tabs for **Firewall** management, **Package Manager** operations, **User Security & Sudoers Audit**, and **Advanced Storage Topology & SSH Sessions**.
- **22 Independent Microservices**: Expanded backend collectors with zero-allocation line parsers for low heap overhead and low CPU consumption.
- **Scrollable Help Overlay (`?`)**: Full viewport scrolling (`↑`/`↓`/`PgUp`/`PgDn`) for the interactive shortcuts guide.
- **Comprehensive Unit Test Suite**: 26 unit test packages covering lifecycles, health statuses, and edge cases.

---

## Quick Start

```bash
# 1. Build the binary
make

# 2. Run as normal user (read-only monitoring mode)
./blackeye

# 3. Run with root privileges (enables process signals, firewall wizard, service controls)
sudo ./blackeye

# 4. Run full test suite with coverage report
make test
```

---

## Requirements

| Requirement | Version | Notes |
| :--- | :--- | :--- |
| **Go** | ≥ 1.21 | Build & compile |
| **Linux** | ≥ 4.x | Kernel `/proc` & `/sys` interfaces |
| **Terminal** | ≥ 80×24 | 256-color or TrueColor recommended |
| **Docker** *(optional)* | Any | For Container Tab 4 |
| **Systemd** *(optional)* | Any | For Services Tab 5 (auto-detects SysVinit/OpenRC) |

---

## Architecture

BlackEye implements a decoupled, fan-out event bus architecture bridging 22 background microservices to 10 TUI tab views:

```
main.go
  ├── config/       → TOML configuration loader with fallback defaults
  ├── privilege/    → Startup POSIX capability & root EUID detector
  ├── sysdetect/    → Hardware, VM, Cloud, and Init-system detector
  ├── resolver/     → Human-friendly name mapping (ports, PIDs, UIDs, mounts)
  ├── bus/          → Fan-out event bus with non-blocking pub/sub
  ├── registry/     → Service lifecycle orchestrator & goroutine bridge
  ├── services/     → 22 independent data collectors
  │   ├── advanced/     Active SSH logins, cron/timers, storage topology
  │   ├── alerts/       Rule-based threshold alert engine
  │   ├── audit/        Append-only security action logger
  │   ├── cpu/          Per-core CPU % from /proc/stat
  │   ├── disk/         Mountpoints & inode usage
  │   ├── dmesg/        Kernel log streaming from /dev/kmsg
  │   ├── docker/       Container stats & env inspect via Docker SDK
  │   ├── firewall/     Active iptables/nftables rule parser
  │   ├── initsys/      Init system autodetector (Systemd/SysV/OpenRC/Docker)
  │   ├── io/           Disk I/O rates from /proc/diskstats
  │   ├── memory/       RAM breakdown from /proc/meminfo
  │   ├── netstats/     TCP/UDP/ICMP error rates from /proc/net/snmp
  │   ├── network/      Network interface traffic from /proc/net/dev
  │   ├── packages/     Package manager stats (pacman/apt/dnf/apk/snap/flatpak)
  │   ├── ports/        Socket listeners & connections from /proc/net/tcp
  │   ├── process/      Process tree & detailed stats from /proc/<pid>/
  │   ├── routing/      Routes & ARP table from /proc/net/route
  │   ├── swap/         Swap memory stats from /proc/meminfo
  │   ├── sysinfo/      Kernel, uptime, OS release, load averages
  │   ├── systemd/      Unit states & unit logs via D-Bus
  │   ├── thermal/      Hardware thermal sensors from /sys/class/thermal
  │   └── users/        Local user accounts, system groups, sudoers rules
  └── ui/           → Bubbletea TUI
      ├── styles/       Design system tokens, lipgloss styles & sparklines
      ├── app.go        Root model, tab routing, status bar & scrollable help modal
      └── tabs/         10 TUI tab models
```

---

## 10 Dashboard Tabs

### [1] Dashboard
Overview panel displaying hostname, uptime, kernel release, per-core CPU bars, memory/swap gauges, disk I/O rates, network traffic, and thermal sensors.
- **Drag-and-Drop Layout**: Click and drag any panel header to rearrange panels dynamically.

### [2] Processes
Process manager supporting **Tree View** (`t`), **Sort** (`s`/`F6`) by CPU/Memory/IO/PID, **Reverse Sort** (`r`), **Search Filter** (`/`), and **Process Signal Menu** (`k`/`F9`, `K`).

### [3] Network
Sub-panels for **Interfaces** (bandwidth rates), **Listeners** (open ports mapped to service names like `22/tcp → ssh`), **Active Connections**, **Routing Table & ARP Cache**, and **Network Errors** (`/proc/net/snmp`).

### [4] Docker
Container monitoring showing CPU%, memory usage, status icons, volume mounts, network info, redacted env vars (`PASSWORD` $\rightarrow$ `[REDACTED]`), container logs (`l`), **Stop** (`s`), and **Restart** (`r`).

### [5] Services
**Systemd Units** (filter all/failed/running, view unit logs `l`, start/stop/restart/enable/disable/mask) and streaming **Kernel dmesg logs** color-coded by severity.

### [6] Terminal
Embedded pseudoterminal shell. Press **`i`** or **`Enter`** to focus input mode and interact with `bash`, `zsh`, `starship`, `htop`, `vim`, or `tmux`. Press **`Esc Esc`** to return to navigation mode and scroll through past output.

### [7] Firewall
Active **iptables** / **nftables** rule viewer, rule deletion (`d`), toggle enable/disable (`e`), and interactive **Add Rule Wizard** (`a`) for quick port/protocol blocking.

### [8] Packages
Installed packages viewer across `pacman`, `apt`/`dpkg`, `dnf`/`rpm`, `apk`, `snap`, and `flatpak`. Supports repository package search (`/`), installation (`Enter`), removal (`r`), and system upgrade wizard (`u`).

### [9] Users
User security auditing: local user accounts, system groups, sudoers rules, system user toggle (`h`), user creation wizard (`a`), and account deletion (`d`).

### [0] Advanced
Active **SSH login sessions** (with session termination `k`), systemd timers & cron jobs, and **Storage Topology** (NVMe/SATA/ZFS/LVM).

---

## Keyboard Shortcuts

| Key | Context | Action |
| :--- | :--- | :--- |
| **`q`** / **`Ctrl+C`** | Global | Quit application |
| **`1`** – **`9`**, **`0`** | Global | Switch between 10 tabs |
| **`?`** | Global | Open scrollable keyboard shortcuts guide |
| **`↑`** / **`↓`** | Tables / Help | Navigate rows / scroll view |
| **`PgUp`** / **`PgDn`** | Scrollable | Fast scroll up/down |
| **`Home`** / **`End`** | Scrollable | Jump to top / bottom |
| **`Tab`** | Sub-panels | Cycle sub-panel views |
| **`/`** | Tables | Filter items by text query |
| **`ESC`** | Any | Clear filter / close modal |
| **`i`** / **`Enter`** | Terminal | Focus shell input mode |
| **`Esc Esc`** | Terminal | Exit focus mode to navigation mode |
| **`t`** | Process | Toggle Tree view vs Flat list |
| **`s`** | Process | Cycle sort column |
| **`r`** | Process | Reverse sort order |
| **`k`** | Process / SSH† | Terminate process or active SSH session |
| **`l`** | Docker / Services | Tail container or unit logs |
| **`a`** | Firewall / Users | Launch Add Rule or Add User wizard |

†*Requires root or `CAP_KILL` capability*

---

## Configuration

BlackEye reads from `~/.config/blackeye/config.toml` (or custom path via `BLACKEYE_CONFIG` environment variable):

```toml
[refresh]
dashboard_interval = 2   # seconds
process_interval   = 3
ports_interval     = 5
docker_interval    = 3
systemd_interval   = 5
dmesg_streaming    = true

[alerts]
cpu_warning    = 70.0   # % → yellow warning
cpu_critical   = 85.0   # % → red alert
memory_warning = 70.0
disk_warning   = 80.0
temp_warning   = 70.0   # °C
temp_critical  = 85.0

[ports]
trusted_ports = [22, 80, 443, 5432, 8080]

[docker]
socket = "/var/run/docker.sock"

[audit]
log_path = "~/.local/share/blackeye/audit.log"
```

---

## Security & Audit Logging

- **No Unsafe Execution**: Data is parsed directly from `/proc`, `/sys`, D-Bus, and Unix sockets without executing arbitrary shell scripts.
- **Input Validation**: Filter inputs restricted to safe characters (`[a-zA-Z0-9._-]`).
- **TOCTOU Protection**: Target process names are re-verified prior to issuing signals.
- **Privilege Checking**: Destructive actions are hidden or locked when running unprivileged.
- **Append-Only Audit Log**: All administrative actions (killing processes, stopping containers, firewall modifications) are logged to `~/.local/share/blackeye/audit.log`:

```text
[2026-07-24T14:32:01+03:00] uid=0 user=root action=kill_process target=nginx pid=1234 result=success
[2026-07-24T14:35:12+03:00] uid=0 user=root action=stop_container target=redis id=abc123def456 result=requested
```

---

## Building & Testing

```bash
# Clone repository
git clone <repo-url> && cd BlackEye

# Build binary
make

# Run tests across all 26 packages
make test

# Rebuild from scratch
make re

# Clean build artifacts
make clean
```

---

## License

This project is licensed under the **MIT License**. See the [LICENSE](LICENSE) file for details.
