# mac-cleanup-go: Diagnostic Improvements

## 1. Goal

Close the gap between the tool's stated purpose ("identify what's causing Mac
slowdown after long-term use") and what `mac-cleanup overview` actually surfaces.
Currently it shows utilization percentages with no culprits. These improvements
make the output diagnostic rather than just a status dashboard.

---

## 2. Improvements

### 2.1 Top CPU & memory processes in `overview`

**Problem:** `overview` reports aggregate CPU % and memory % but names no
processes. A user seeing "CPU 80%" has no idea which process is responsible —
the very thing they need to diagnose slowdown.

**Fix:** Add a `collectTopProcesses(n int)` collector that runs `ps aux` sorted
by CPU (and a second pass by RSS), takes the top N, and renders them in the
overview table. Display top 5 by CPU and top 5 by memory, skipping kernel
threads.

```
── Top Processes (CPU)
╭──────────────────────────┬──────────┬───────────╮
│ Process                  │   CPU %  │   Memory  │
├──────────────────────────┼──────────┼───────────┤
│ Xcode                    │   42.1%  │   1.8 GB  │
│ Google Chrome Helper     │   18.3%  │   512 MB  │
│ coreaudiod               │    9.2%  │    48 MB  │
╰──────────────────────────┴──────────┴───────────╯
```

**Files:** `overview.go` (new `collectTopProcesses`, wire into `RunOverview`)

---

### 2.2 Swap usage in `overview`

**Problem:** `memory_pressure` returns a free %, but heavy swap is the clearest
signal that a Mac is memory-constrained — especially on 8 GB M-series machines.
The current output gives no indication of whether the system is swapping.

**Fix:** Parse `sysctl vm.swapusage` to extract swap used/total and add a
`Swap` row to the system stats table. Highlight in yellow when swap used > 1 GB,
red when > 3 GB.

```go
func collectSwap() metricResult {
    out, _ := runCmdSimple("sysctl", "vm.swapusage")
    // parse "xsu_used = X.XXM" from output
    ...
}
```

```
│ Swap             │ 2.4 GB used of 8.0 GB              │   ← yellow
```

**Files:** `overview.go` (new `collectSwap`, add row to stats table)

---

### 2.3 LaunchAgents count in `overview`

**Problem:** `~/Library/LaunchAgents/` and `/Library/LaunchAgents/` accumulate
silently over long-term use. Many third-party apps register persistent background
agents here without the user's awareness. This is a primary cause of slow boot
and idle CPU consumption. The tool currently hard-blocks these paths from
deletion but never reports them.

**Fix:** Add a `collectLaunchAgents()` collector that counts `.plist` files in
both directories and shows the total. If count > 10, add a note suggesting
review via System Settings → General → Login Items.

```
│ Launch Agents    │ 14 items  ⚠ consider reviewing in Login Items  │
```

**Files:** `overview.go` (new `collectLaunchAgents`, add row to stats table)

---

### 2.4 iOS/device backup size in `overview` and `deep`

**Problem:** `~/Library/Application Support/MobileSync/Backup/` commonly grows
to 10–40 GB+ and is never inspected. It is one of the largest sources of
recoverable disk space on a developer's machine.

**Fix:**
- In `overview`: add a `collectMobileSyncSize()` collector that runs `du -sh`
  on the path (if it exists) and adds a `Device Backups` row to the top
  directories table.
- In `deep`: add a `reportDeviceBackups()` task that reports the size and
  lists individual backup UUIDs with their last-modified date, but does **not**
  delete — informs the user to manage via Finder or iTunes.

```
│ Device Backups   │ 28 GB  (3 backup(s) — manage in Finder)       │
```

**Files:** `overview.go` (new collector), `cleanup.go` (new report-only task in `RunDeep`)

---

### 2.5 Diagnostic/crash reports in `safe` cleanup

**Problem:** `~/Library/Logs/DiagnosticReports/` accumulates `.crash`,
`.hang`, and `.ips` files indefinitely. Large numbers indicate repeated
background failures. The `safe` pass currently cleans `~/Library/Logs/` but
explicitly skips subdirectories' contents inconsistently — DiagnosticReports
is not covered.

**Fix:** Add a `cleanDiagnosticReports()` task to `RunSafe` that deletes files
older than 30 days from `~/Library/Logs/DiagnosticReports/`. Files newer than
30 days are left intact so recent issues remain diagnosable.

```go
func cleanDiagnosticReports() TaskResult {
    path := expandHome("~/Library/Logs/DiagnosticReports/")
    // delete files where ModTime < now-30days
    ...
}
```

**Files:** `cleanup.go` (new task), `phases.go` (add to `RunSafe` task list)

### 2.6 Fix CPU % always showing 100%

**Problem:** `collectCPU` sums `%cpu` from `ps -A` across all processes without
dividing by the number of logical CPUs. On a 10-core Mac, even moderate load
results in a raw sum far above 100, which is then hard-capped — the bar is
permanently maxed out and gives no useful signal.

**Fix:** Divide the process sum by `sysctl -n hw.logicalcpu` to normalise to a
true system-wide percentage (0–100). Additionally, when the result exceeds 70%,
append the name of the single highest-CPU process inline so the user sees the
actual culprit without scrolling to the process table.

```
│ CPU  │ ███████░░░  74%  · highest: Xcode (42.1%)  │
```

**Files:** `overview.go` (`collectCPU`)

---

## 3. Scope summary

| File | Change |
| --- | --- |
| `overview.go` | Fix `collectCPU` normalization; add `collectTopProcesses`, `collectSwap`, `collectLaunchAgents`, `collectMobileSyncSize`; wire all into `RunOverview` |
| `cleanup.go` | Add `cleanDiagnosticReports`, `reportDeviceBackups` |
| `phases.go` | Add `cleanDiagnosticReports` to `RunSafe`; add `reportDeviceBackups` to `RunDeep` |
| `README.md` | Update `overview` and `deep` sections to document new rows/tasks |

---

## 4. No new dependencies

All changes use the standard library and existing macOS CLI tools (`ps`, `sysctl`,
`du`). No additions to `go.mod`.
