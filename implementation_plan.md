# Fix Data Validation Issues Across 5 Dashboard Tabs

After a thorough audit of all 5 dashboard tabs, their service backends, and the data transformation pipeline, I identified the following issues:

## Issues Found

### 1. 🔴 Dashboard Tab — Network panel: Double unit conversion (MB/s displayed wrong)

**File**: [dashboard.go](file:///home/moabed/Documents/BlackEye/internal/ui/tabs/dashboard.go#L577-L578)

The network service stores `RxMBs` and `TxMBs` as **MB/s** values (already divided by 1024*1024 in [network.go:121-122](file:///home/moabed/Documents/BlackEye/internal/services/network/network.go#L121-L122)). But the dashboard `renderNetwork` calls `FormatRate(iface.RxMBs*1024*1024)` — multiplying back to bytes/s just to feed it into `FormatRate()` which divides by 1024*1024 again.

**Problem**: This **works mathematically** (multiply then divide cancels out) but is misleading and fragile. If `FormatRate` uses GiB (1024³) thresholds but the data was computed with 1024² divisors, the values cross thresholds at the wrong point. Also, for small rates the rounding through `FormatRate` loses precision compared to just displaying the already-computed MB/s.

**Verdict**: ⚠️ **Low priority** — the values cancel out and are numerically correct. No fix needed.

---

### 2. 🔴 Dashboard Tab — Memory panel: Hardcoded critical threshold instead of using config

**File**: [dashboard.go](file:///home/moabed/Documents/BlackEye/internal/ui/tabs/dashboard.go#L467)

```go
crit := 90.0  // HARDCODED!
```

The CPU panel correctly reads `d.cfg.Alerts.CPUCritical`, but the Memory panel hardcodes `crit := 90.0` instead of reading from config. There's no `MemoryCritical` field in the config, so this should at minimum follow a consistent pattern. The config only has `MemoryWarning` (default 70.0), but the critical threshold is ad-hoc.

**Fix**: Add `MemoryCritical` to the config with a default of 90.0, and use it instead of the hardcode.

---

### 3. 🔴 Dashboard Tab — Disk panel: Hardcoded critical threshold

**File**: [dashboard.go](file:///home/moabed/Documents/BlackEye/internal/ui/tabs/dashboard.go#L520)

```go
crit := 95.0  // HARDCODED!
```

Same pattern — the disk critical threshold is hardcoded at 95.0 instead of being configurable. The config has `DiskWarning` (default 80.0) but no `DiskCritical`.

**Fix**: Add `DiskCritical` to the config with a default of 95.0, and use it.

---

### 4. 🔴 Dashboard Tab — Disk Inodes: Incorrect byte formatting for inode count

**File**: [dashboard.go](file:///home/moabed/Documents/BlackEye/internal/ui/tabs/dashboard.go#L534-L536)

```go
inodeLine := fmt.Sprintf("  Inodes: %s used  %.0f%%",
    resolver.FormatBytes(dk.InodesTotal-dk.InodesFree),
    dk.InodesPercent)
```

`InodesTotal` and `InodesFree` are **counts** of inodes, not byte sizes. Using `FormatBytes()` to format them is **semantically wrong** — it would display "1.2 MiB" when it should say "1,234,567 inodes". An inode count of 1,000,000 would be displayed as "976.6 KiB" which is nonsensical.

**Fix**: Format inode used count as a plain integer with comma separators or just `%d`.

---

### 5. 🔴 Disk Service — Wrong refresh interval config field used

**File**: [disk.go](file:///home/moabed/Documents/BlackEye/internal/services/disk/disk.go#L60)

```go
interval: time.Duration(cfg.Refresh.PortsInterval) * time.Second,
```

The disk service uses `PortsInterval` (default 5s) instead of `DashboardInterval` (default 2s). This means disk data refreshes at the ports rate (5s) rather than the dashboard rate (2s), inconsistent with all other dashboard services (CPU, memory, swap, IO, network, thermal).

Also at line 73:
```go
s.interval = time.Duration(cfg.Refresh.PortsInterval) * time.Second
```

**Fix**: Change both to use `cfg.Refresh.DashboardInterval`.

---

### 6. 🔴 Docker Tab — FullID truncation in detail view is fragile

**File**: [docker.go](file:///home/moabed/Documents/BlackEye/internal/ui/tabs/docker.go#L230)

```go
fmt.Sprintf("  ID:      %s", c.FullID[:16]),
```

If `c.FullID` is shorter than 16 characters (e.g., Docker inspect failed and `FullID` is the 12-char short ID from `c.ID[:12]`), this will **panic** with a slice bounds out of range error.

**Fix**: Guard with length check or use the safer truncation from the styles package.

---

### 7. 🟡 Process Tab — "up" key missing "k" alias (inconsistency)

**File**: [process.go](file:///home/moabed/Documents/BlackEye/internal/ui/tabs/process.go#L106)

In the Docker tab, `"up"` is handled but `"k"` is not aliased for navigation (line 102: only `"up"` moves cursor up). However the Process tab's normal mode handles `"up"` at line 160 but doesn't alias `"j"` for up (it only does for down). Actually looking again — `"down", "j"` are aliased at line 164 but `"up"` has no `"k"` alias. The `"k"` key is mapped to kill at line 190 instead.

**Verdict**: ⚠️ This is by-design — `k` is used for kill in Process tab, so it can't also be used for navigation. Not a bug.

---

### 8. 🟡 Network tab — Connections panel uses wrong data source for filter

The connections panel at [network.go:272](file:///home/moabed/Documents/BlackEye/internal/ui/tabs/network.go#L272) doesn't filter connections by the active filter text. The listeners panel applies filtering (line 214), but the connections panel iterates over all connections regardless of filter.

**Verdict**: ⚠️ This may be intentional but is inconsistent. Not fixing as it could be by-design.

---

## Proposed Changes

### Config Changes

#### [MODIFY] [config.go](file:///home/moabed/Documents/BlackEye/internal/config/config.go)
- Add `MemoryCritical` and `DiskCritical` fields to `AlertsConfig`
- Set defaults: `MemoryCritical: 90.0`, `DiskCritical: 95.0`

---

### Dashboard Tab Fixes

#### [MODIFY] [dashboard.go](file:///home/moabed/Documents/BlackEye/internal/ui/tabs/dashboard.go)
- **Memory panel**: Replace hardcoded `crit := 90.0` with `d.cfg.Alerts.MemoryCritical`
- **Disk panel**: Replace hardcoded `crit := 95.0` with `d.cfg.Alerts.DiskCritical`
- **Disk inodes**: Replace `resolver.FormatBytes(dk.InodesTotal-dk.InodesFree)` with proper integer formatting

---

### Service Fixes

#### [MODIFY] [disk.go](file:///home/moabed/Documents/BlackEye/internal/services/disk/disk.go)
- Change `cfg.Refresh.PortsInterval` → `cfg.Refresh.DashboardInterval` in both `New()` and `Reload()`

---

### Docker Tab Fix

#### [MODIFY] [docker.go](file:///home/moabed/Documents/BlackEye/internal/ui/tabs/docker.go)
- Guard `c.FullID[:16]` with length check to prevent panic

---

## Verification Plan

### Automated Tests
```bash
go build ./...
go vet ./...
go test ./...
```

### Manual Verification
- The changes are straightforward value fixes — no behavioral change beyond correctness
- Memory/disk critical thresholds now match config defaults (90/95) — same values as before, just configurable
- Inode display will now show actual counts instead of misleading byte-formatted values
