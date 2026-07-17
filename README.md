# BlackEye — Linux System Administration Dashboard

A real-time TUI (Terminal User Interface) system monitor built in Go. BlackEye reads directly from `/proc`, `/sys`, and system sockets — no `os/exec` shelling, no external commands.

```
┌─[1] Dashboard─[2] Processes─[3] Network─[4] Docker─[5] Services──────────┐
│  System Info                                                               │
│  Hostname: myserver  │  Uptime: 14 days  │  Kernel: 6.12.5-amd64          │
│                                                                            │
│  CPU                                                                       │
│  Total:  23.4%  ███████░░░░░░░░░░░░░░░░░░░░░░                             │
│  Core0:  31.2%  ██████████░░░░░░░░░░                                       │
│  Core1:  15.6%  ████░░░░░░░░░░░░░░░░                                       │
│                                                                            │
│  Memory                                                                    │
│  RAM:  5.2 GiB / 15.6 GiB  ████████████░░░░░░░░░░░░  33%                 │
│  Swap: 0.1 GiB /  4.0 GiB  █░░░░░░░░░░░░░░░░░░░░░░░   3%                │
└────────────────────────────────────────────────────────────────────────────┘
  BlackEye  │  ● normal                           q quit  │  ? help  │  1–5 tabs
```

**Theme:** Features a sleek **Navy Blue & Gold** premium color palette, with dynamic highlighting and flagged row warnings (⚠).

## Quick Start

```bash
# 1. Install dependencies and build
make

# 2. Run (normal user — read-only mode)
./blackeye

# 3. Run with elevated privileges (enables process kill / container stop)
sudo ./blackeye
```

## Requirements

| Requirement      | Version     | Notes                            |
|-----------------|-------------|----------------------------------|
| **Go**          | ≥ 1.21      | Build only                       |
| **Linux**       | ≥ 4.x       | Uses `/proc`, `/sys`             |
| **Terminal**    | ≥ 80×24     | 256-color support recommended    |
| **Docker** (optional) | Any   | For Tab 4                        |
| **systemd** (optional) | Any  | For Tab 5                        |

## Architecture

BlackEye follows a **microservices-inspired architecture** where each data collector is an independent service with its own goroutine, lifecycle, and health status:

```
main.go
  ├── config/       → TOML configuration with defaults
  ├── privilege/    → Detects root/CAP_KILL at startup
  ├── resolver/     → Human-friendly name mapping
  ├── bus/          → Fan-out event bus (thread-safe)
  ├── registry/     → Service lifecycle manager
  ├── services/     → 16 independent data collectors
  │   ├── audit/        Append-only audit logger
  │   ├── cpu/          Per-core CPU % from /proc/stat
  │   ├── memory/       RAM usage from /proc/meminfo
  │   ├── swap/         Swap usage from /proc/meminfo
  │   ├── thermal/      Temperatures from /sys/class/thermal
  │   ├── sysinfo/      Hostname, kernel, uptime, load avg
  │   ├── disk/         Mountpoints + inode usage
  │   ├── io/           Disk I/O rates from /proc/diskstats
  │   ├── network/      Interface traffic from /proc/net/dev
  │   ├── netstats/     TCP/UDP/ICMP errors from /proc/net/snmp
  │   ├── routing/      Routes + ARP from /proc/net/route
  │   ├── process/      Process list from /proc/<pid>/
  │   ├── ports/        Listeners + connections from /proc/net/tcp
  │   ├── docker/       Container stats via Docker SDK
  │   ├── systemd/      Unit states via D-Bus socket
  │   └── dmesg/        Kernel log from /dev/kmsg
  └── ui/           → Bubbletea TUI
      ├── styles/       Shared color palette + lipgloss styles
      ├── app.go        Root model, tab routing, status bar
      └── tabs/         5 tab models (dashboard, process, network, docker, services)
```

## 5 Tabs

### Tab 1 — Dashboard
System overview: hostname, uptime, CPU bars with per-core %, memory/swap usage, disk + I/O, network interfaces with traffic rates, temperature readings.

**Drag-and-Drop Flexible Layout**: The Dashboard supports a fully dynamic layout manager! If your terminal supports mouse events, you can **click and drag** any panel to rearrange the grid.
- Drop on the **left/right** of a panel to place it **beside** it (horizontally).
- Drop on the **top/bottom** of a panel to place it **above/below** it.
- Panels automatically and flexibly align their width depending on how many components share a row!

### Tab 2 — Processes
Interactive process table with **sort** (PID/CPU/Memory/Name), **filter** by name/user, **detail panel** (full command, FDs, cgroup), and **kill** support (SIGTERM with confirm, SIGKILL with PID re-type). Features a **smart scrolling viewport** that tracks your cursor seamlessly. Kill is only available when running as root or with `CAP_KILL`.

### Tab 3 — Network
Five sub-panels: **Interfaces** (traffic rates), **Listeners** (open ports with service names like "22/tcp → ssh (Secure Shell)"), **Connections** (active TCP with state), **Routing** (route table + ARP cache), **Statistics** (retransmits, errors, drops). Listeners and Connections lists feature smooth **viewport scrolling**.

### Tab 4 — Docker
Container table with CPU%, memory, status. **Detail panel** shows environment variables (sensitive values like `PASSWORD` are `[REDACTED]`), volume mounts, network info, port mappings. **Stop/Restart** with confirmation dialog. The detail panel includes **manual scrolling** for containers with extensive environments. Gracefully handles Docker being unavailable using the v28+ SDK.

### Tab 5 — Services
**Systemd units** with filter (all/failed/running) and a **smart scrolling viewport**, **Kernel log** streaming with color-coded severity levels (emerg=red, warn=yellow, info=green) that automatically windows the latest logs to your terminal size.

## Keyboard Shortcuts

| Key         | Context     | Action                        |
|-------------|-------------|-------------------------------|
| `Mouse Drag`| Dashboard   | Rearrange panels dynamically  |
| `q`/`Ctrl+C` | Global    | Quit                          |
| `1`–`5`    | Global      | Switch tab                    |
| `?`        | Global      | Toggle help overlay           |
| `↑`/`↓`   | Tables      | Navigate rows / scroll        |
| `PgUp`/`PgDn`| Scrollable| Fast scroll up/down           |
| `Home`     | Scrollable  | Reset scroll to top           |
| `j`        | Tables      | Navigate down                 |
| `/`        | Tables      | Start filter                  |
| `ESC`      | Any         | Cancel filter / close detail  |
| `s`        | Process     | Cycle sort column             |
| `r`        | Process     | Reverse sort order            |
| `Enter`    | Process     | Toggle detail panel           |
| `k`        | Process†    | Send SIGTERM (with confirm)   |
| `K`        | Process†    | Send SIGKILL (type PID)       |
| `Tab`      | Network     | Cycle sub-panels              |
| `Enter`    | Docker      | Container detail              |
| `s`        | Docker†     | Stop container (confirm)      |
| `r`        | Docker†     | Restart container (confirm)   |
| `Tab`      | Services    | Switch services/dmesg         |
| `a`/`f`/`r` | Services  | Show all/failed/running       |

†Requires root or `CAP_KILL`

## Configuration

BlackEye reads from `~/.config/blackeye/config.toml`. If the file doesn't exist, sensible defaults are used.

```toml
[refresh]
dashboard_interval = 2   # seconds
process_interval   = 3
ports_interval     = 5
docker_interval    = 3
systemd_interval   = 5
dmesg_streaming    = true

[alerts]
cpu_warning    = 70.0   # % → yellow
cpu_critical   = 85.0   # % → red
memory_warning = 70.0
disk_warning   = 80.0
temp_warning   = 70.0   # °C
temp_critical  = 85.0

[ports]
trusted_ports = [22, 80, 443, 5432]  # ports NOT flagged as suspicious

[docker]
socket = "/var/run/docker.sock"

[audit]
log_path = "~/.local/share/blackeye/audit.log"
```

Override the config path:
```bash
BLACKEYE_CONFIG=/etc/blackeye.toml ./blackeye
```

## Human-Friendly Naming

BlackEye resolves raw system identifiers to readable labels:

| Raw Identifier | Displayed As                    |
|---------------|---------------------------------|
| Port 22       | `ssh (Secure Shell)`            |
| Port 3306     | `mysql (MySQL Database)`        |
| `eth0`        | `eth0 (Ethernet)`               |
| `wlan0`       | `wlan0 (WiFi)`                  |
| Process `S`   | `S (Sleeping)`                  |
| Process `Z`   | `Z (Zombie — orphaned)`         |
| TCP `01`      | `ESTABLISHED (Connected)`       |
| Container     | `running (Active)` with 🟢 icon |
| Mount `/`     | `/ (Root Filesystem)`           |

## Security

- **No `os/exec`** — all data comes from `/proc`, `/sys`, and sockets
- **Input validation** — filter inputs restricted to `[a-zA-Z0-9._-]`
- **TOCTOU protection** — process name is re-verified before sending signals
- **Privilege awareness** — kill/stop buttons hidden without `CAP_KILL`
- **Audit logging** — all destructive actions logged to append-only file
- **Docker secrets** — env vars containing `PASSWORD`, `TOKEN`, etc. are `[REDACTED]`

## Audit Log

All destructive actions (kill process, stop/restart container) are logged:
```
[2026-07-08T14:32:01+03:00] uid=0 user=root action=kill_process target=nginx pid=1234 result=success
[2026-07-08T14:35:12+03:00] uid=0 user=root action=stop_container target=redis id=abc123def456 result=requested
```

The log is **append-only** and located at `~/.local/share/blackeye/audit.log`.

## Building from Source

```bash
# Clone
git clone <repo-url> && cd BlackEye

# Install dependencies
go mod tidy
go mod vendor

# Build
go build -trimpath -o blackeye .

# Run tests
go test ./internal/... -coverprofile=coverage.out -covermode=atomic

# Or use the Makefile
make        # build
make run    # build + run
make test   # run tests with coverage
make clean  # remove artifacts
```

## License

This project is licensed under the **MIT License**. See the `LICENSE` file for details.
