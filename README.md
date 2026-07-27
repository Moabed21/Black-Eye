# BlackEye v1.3.0 — Enterprise Security Analyst & Access Control Dashboard

![Version](https://img.shields.io/badge/version-1.3.0-gold.svg)
![Build Status](https://img.shields.io/badge/build-passing-brightgreen.svg)
![License](https://img.shields.io/badge/license-MIT-blue.svg)
![Go Version](https://img.shields.io/badge/go-%E2%89%A51.21-00ADD8.svg)

A real-time, high-performance TUI (Terminal User Interface) system monitor, security analyst suite, and administration dashboard built in Go. BlackEye reads directly from Linux kernel virtual filesystems (`/proc`, `/sys`), D-Bus, system sockets, and native system APIs with zero external binary dependencies.

```
┌─[1] Dashboard─[2] Processes─[3] Network─[4] Docker─[5] Services─[6] Terminal─[7] Firewall─[8] Packages─[9] Users─[0] Advanced─┐
│  System Info                                                                                                                   │
│  Hostname: myserver  │  Uptime: 14 days  │  Kernel: 6.12.5-amd64  │  OS: Linux x86_64                                          │
│  Security Risk Score: 15/100 (LOW RISK)                                                                                        │
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
  BlackEye v1.3.0  │  ● normal                                                                      q quit  │  ? help  │  1–9,0 tabs
```

**Theme:** Premium **Navy Blue & Gold** color palette with dynamic highlighting, alert toasts (⚠/🔴), and drag-and-drop panel header positioning.

---

## What's New in v1.3.0

- **Enterprise Security Analyst & Access Control Suite (Tab 9)**:
  - **Sudoers NOPASSWD Audit**: Audits `/etc/sudoers` and `/etc/sudoers.d/*` for `NOPASSWD` rules and wildcard privilege escalation risks.
  - **SUID/SGID Privilege Escalation (`PrivEsc`) Scanner**: Walks `/usr/bin`, `/usr/sbin`, `/bin`, `/sbin` checking file mode bits for root SUID/SGID executables and flagging non-standard binaries (`⚠️ UNKNOWN / PRIVESC RISK`).
  - **SSH Daemon Security Policy Auditor**: Parses `/etc/ssh/sshd_config` for `PermitRootLogin`, `PasswordAuthentication`, `PubkeyAuthentication`, and `X11Forwarding` settings.
- **Combined Cross-Tab Incident Response Workflows**:
  - **Network $\rightarrow$ Firewall Rule Wizard**: Press **`f`** on any listener socket in Tab 3 to jump directly to Tab 7 (Firewall) with the Add Rule Wizard pre-filled for that port and protocol.
  - **SSH Session $\rightarrow$ Firewall IP Ban**: Press **`b`** on any active SSH login in Tab 0 (Advanced) to create an instant Firewall DROP rule for that remote IP address.
  - **Socket PID Termination**: Press **`k`** on any active connection in Tab 3 to send `SIGTERM` to the process owning the socket.
- **Unencrypted Listener Badges & Public WAN Scope**:
  - **`⚠️ UNENCRYPTED`** badges on insecure listener sockets (HTTP `80`, FTP `21`, Telnet `23`, POP3 `110`, IMAP `143`, LDAP `389`).
  - **`🌐 WAN`** vs **`🔒 LAN`** scope badges on active network socket connections.
- **System Security Risk Score Matrix**: Real-time Security Risk Score (0–100%) calculated dynamically on Tab 1 (Dashboard) combining firewall state, root SSH logins, unencrypted sockets, and authentication failures.
- **CLI Sub-Commands & Headless JSON Health Export**:
  - Launch sub-commands: `blackeye net`, `blackeye sec`, `blackeye pkg`, `blackeye proc`, `blackeye term`, `blackeye fw`, or `--tab <name>` to launch directly into specialized focus views.
  - Headless `--export json` / `--health` flag to print JSON health snapshots to stdout for CI/CD pipelines.

---

## Quick Start

```bash
# 1. Build the binary
make

# 2. Run as normal user (read-only monitoring mode)
./blackeye

# 3. Run with root privileges (enables process signals, firewall rules, package & container controls)
sudo ./blackeye

# 4. Launch specialized sub-commands directly
./blackeye net   # Direct launch into Network & Socket Security mode
./blackeye sec   # Direct launch into Security Analyst & Access Control mode
./blackeye --export json # Print JSON health payload to stdout

# 5. Run full test suite
make test
```

---

## Requirements

| Requirement | Version | Notes |
| :--- | :--- | :--- |
| **Go** | ≥ 1.21 | Build & compile |
| **Linux** | ≥ 4.x | Kernel `/proc` & `/sys` interfaces |
| **Terminal** | ≥ 80×24 | 256-color or TrueColor recommended |
| **Docker** *(optional)* | Any | For Container Tab 4 operations |
| **Systemd** *(optional)* | Any | For Services Tab 5 (auto-detects SysVinit/OpenRC) |

---

## Architecture

BlackEye implements a decoupled, fan-out event bus architecture bridging 23 background microservices to 10 TUI tab views:

```
main.go
  ├── config/       → TOML configuration loader with fallback defaults
  ├── privilege/    → Startup POSIX capability & root EUID detector
  ├── sysdetect/    → Hardware, VM, Cloud, and Init-system detector
  ├── resolver/     → Human-friendly name mapping (ports, PIDs, UIDs, mounts)
  ├── bus/          → Fan-out event bus with non-blocking pub/sub
  ├── registry/     → Service lifecycle orchestrator & goroutine bridge
  ├── services/     → 23 independent data collectors
  │   ├── advanced/     Active SSH logins, cron/timers, storage topology
  │   ├── alerts/       Rule-based threshold alert engine
  │   ├── audit/        Append-only security action logger
  │   ├── cpu/          Per-core CPU % from /proc/stat
  │   ├── disk/         Mountpoints & inode usage
  │   ├── dmesg/        Kernel log streaming from /dev/kmsg
  │   ├── docker/       Live Container SDK (start, stop, restart, pause, remove, logs)
  │   ├── firewall/     Smart auto-discovery engine (ufw, firewalld, iptables, nftables)
  │   ├── initsys/      Init system autodetector (Systemd/SysV/OpenRC/Docker)
  │   ├── io/           Disk I/O rates from /proc/diskstats
  │   ├── memory/       RAM breakdown from /proc/meminfo
  │   ├── netstats/     TCP/UDP/ICMP error rates from /proc/net/snmp
  │   ├── network/      Network interface traffic from /proc/net/dev (auto-scaled B/s rates)
  │   ├── packages/     Smart package manager discovery (pacman, yay, apt, dnf, apk, etc.)
  │   ├── ports/        Socket listeners & connections from /proc/net/tcp (WAN/LAN, Unencrypted)
  │   ├── process/      Process tree & detailed stats from /proc/<pid>/
  │   ├── routing/      Routes & ARP table from /proc/net/route
  │   ├── security/     SUID/SGID scanner, SSH policy auditor, auth log brute-force parser
  │   ├── swap/         Swap memory stats from /proc/meminfo
  │   ├── sysinfo/      Kernel, uptime, OS release, load averages
  │   ├── systemd/      Unit states & unit logs via D-Bus
  │   ├── thermal/      Hardware thermal sensors from /sys/class/thermal
  │   └── users/        Local user accounts, system groups, sudoers rules
  └── ui/           → Bubbletea TUI
      ├── styles/       Design system tokens, lipgloss styles & sparklines
      ├── app.go        Root model, viewport height clamp & scrollable help modal
      └── tabs/         10 TUI tab models
```

---

## 10 Dashboard Tabs

### [1] Dashboard
Overview panel displaying hostname, uptime, kernel release, per-core CPU bars, memory/swap gauges, disk I/O rates, network traffic, thermal sensors, and real-time **Security Analyst Risk Score (0–100%)**.

### [2] Processes
Process manager supporting **Tree View** (`t`), **Sort** (`s`/`F6`) by CPU/Memory/IO/PID, **Reverse Sort** (`r`), **Search Filter** (`/`), and **Process Signal Menu** (`k`/`F9`, `K`).

### [3] Network
Sub-panels for **Interfaces** (auto-scaled bandwidth rates & cumulative bytes), **Listeners** (`⚠️ UNENCRYPTED` badges & press **`f`** to prefill Firewall rule), **Active Connections** (`🌐 WAN` vs `🔒 LAN` scope badges & press **`k`** to terminate socket PID), **Routing Table & ARP Cache**, and **Network Errors**.

### [4] Docker
Container management showing CPU%, memory usage, status icons, volume mounts, redacted env vars, container logs (`l`), **Start** (`a`), **Stop** (`s`), **Restart** (`r`), **Pause/Unpause** (`p`), and **Remove** (`d`).

### [5] Services
**Systemd Units** (filter all/failed/running, view unit logs `l`, start/stop/restart/enable/disable/mask) and streaming **Kernel dmesg logs** color-coded by severity.

### [6] Terminal
Embedded pseudoterminal shell. Press **`i`** or **`Enter`** to focus input mode and interact with `bash`, `zsh`, `starship`, `htop`, `vim`, or `tmux`. Press **`Esc Esc`** to return to navigation mode.

### [7] Firewall
Active rule viewer for **ufw**, **firewalld**, **iptables**, and **nftables**. Supports live Engine Switcher (**`b`**), rule deletion (**`d`**), toggle enable/disable (**`e`**), and interactive **Add Rule Wizard** (**`a`**).

### [8] Packages
Cross-distro package management dynamically discovering `pacman`, `yay`, `paru`, `apt`, `dnf`, `apk`, `zypper`, `flatpak`, and `snap`. Supports Helper Switcher (**`b`**), Category Filtering (**`c`**), repo search (**`/`**), installation (**`Enter`**), removal (**`r`** with System Core safety warnings), and system upgrade wizard (**`u`**).

### [9] Users & Security
User security & access control auditing: local user accounts, system groups, sudoers rules with `NOPASSWD` warnings, **SUID/SGID PrivEsc Auditor**, **SSH Daemon Security Policy Auditor**, system user toggle (`h`), user creation wizard (`a`), and account deletion (`d`).

### [0] Advanced
Active **SSH login sessions** (with session termination **`k`** and instant Firewall IP Ban **`b`**), systemd timers & cron jobs, and **Storage Topology** (NVMe/SATA/ZFS/LVM).

---

## Keyboard Shortcuts

| Key | Context | Action |
| :--- | :--- | :--- |
| **`q`** / **`Ctrl+C`** | Global | Quit application |
| **`1`** – **`9`**, **`0`** | Global | Switch between 10 tabs |
| **`?`** | Global | Open scrollable keyboard shortcuts guide |
| **`Tab`** | Sub-panels | Cycle sub-panel views |
| **`f`** | Network (Listeners) | Jump to Firewall tab with rule wizard pre-filled for highlighted socket port |
| **`b`** | Advanced (SSH) / Firewall / Packages | Instant Firewall IP Ban, switch firewall engine, or switch package manager |
| **`c`** | Packages | Cycle category filter (System Core, User App, Library, Dev) |
| **`/`** | Tables | Filter items by text query |
| **`ESC`** | Any | Clear filter / close modal dialog |
| **`i`** / **`Enter`** | Terminal | Focus shell input mode |
| **`Esc Esc`** | Terminal | Exit focus mode to navigation mode |
| **`t`** | Process | Toggle Tree view vs Flat list |
| **`s`** | Process / Docker | Cycle sort column or Stop container |
| **`r`** | Process / Docker | Reverse sort order or Restart container |
| **`a`** | Docker / Firewall / Users | Start container, launch Add Rule wizard, or Add User |
| **`p`** | Docker | Pause / Unpause container |
| **`d`** | Docker / Firewall / Users | Remove container, delete rule, or delete user |
| **`k`** | Process / Sockets / SSH† | Terminate process, socket PID, or active SSH session |
| **`l`** | Docker / Services | Tail container or unit logs |

†*Requires root or `CAP_KILL` capability*

---

## Building & Testing

```bash
# Build binary
make

# Run tests across all packages
make test

# Rebuild from scratch
make re

# Clean build artifacts
make clean
```

---

## License

This project is licensed under the **MIT License**. See the [LICENSE](LICENSE) file for details.
