# mac-cleanup-go: Process Overview — `overview --verbose`

## 1. Goal

Extend `mac-cleanup overview` with a `--verbose` flag that surfaces a richer,
read-only snapshot of running processes — enough to diagnose what is causing
CPU or memory pressure without taking any automated action.

No processes are killed or modified at any point. This feature is strictly
diagnostic and complies with all existing Safety Guarantees.

---

## 2. What changes

### 2.1 New flag: `overview --verbose`

```
mac-cleanup overview --verbose
```

In normal mode, `overview` already shows **Top 5 Processes by CPU** in a
compact table. `--verbose` expands this into three additional panels:

| Panel | Description |
| --- | --- |
| **Top 10 by CPU** | Extended process list, sorted by CPU descending |
| **Top 10 by Memory** | Separate sort pass by RSS — different culprits often appear here |
| **Zombie / stuck processes** | Processes in state `Z` (zombie) or with 0% CPU but RSS > 500 MB — quietly consuming memory without doing work |

---

### 2.2 Richer per-process detail

Normal mode shows: `Process · CPU% · Memory`

`--verbose` adds two more columns:

| Column | Source | Why useful |
| --- | --- | --- |
| **PID** | `ps -axo pid` | Lets the user cross-reference in Activity Monitor or run `kill` manually if they choose to |
| **Running time** | `ps -axo etime` | A process that has been at 40% CPU for 3 days is very different from one that spiked 10 seconds ago |

```
── Top Processes (by CPU) — verbose

╭───────┬──────────────────────────────┬──────────┬───────────┬────────────╮
│ PID   │ Process                      │   CPU %  │    Memory │    Runtime │
├───────┼──────────────────────────────┼──────────┼───────────┼────────────┤
│ 1847  │ Xcode                        │   42.1%  │    1.8 GB │    2:14:03 │
│ 3201  │ Google Chrome Helper         │   18.3%  │   512 MB  │    0:08:11 │
│ 892   │ coreaudiod                   │    9.2%  │    48 MB  │  3d 04:12  │
│ 7741  │ com.apple.WebKit.Networking  │    6.1%  │   230 MB  │    1:02:44 │
╰───────┴──────────────────────────────┴──────────┴───────────┴────────────╯

── Top Processes (by Memory) — verbose

╭───────┬──────────────────────────────┬──────────┬───────────┬────────────╮
│ PID   │ Process                      │   CPU %  │    Memory │    Runtime │
├───────┼──────────────────────────────┼──────────┼───────────┼────────────┤
│ 1847  │ Xcode                        │   42.1%  │    1.8 GB │    2:14:03 │
│ 5512  │ Simulator                    │    0.2%  │    1.1 GB │    0:44:18 │
│ 3201  │ Google Chrome Helper         │   18.3%  │   512 MB  │    0:08:11 │
╰───────┴──────────────────────────────┴──────────┴───────────┴────────────╯

── Zombie / Stuck Processes

╭───────┬──────────────────────────────┬────────┬───────────╮
│ PID   │ Process                      │ State  │    Memory │
├───────┼──────────────────────────────┼────────┼───────────┤
│ 9103  │ com.apple.mdworker_shared    │ zombie │     0 MB  │
│ 4481  │ CoreBrightness               │ stuck  │   612 MB  │
╰───────┴──────────────────────────────┴────────┴───────────╯
  ℹ  Zombie/stuck processes consume resources but are not killed automatically.
  ℹ  Use Activity Monitor to investigate and force-quit if needed.
```

---

### 2.3 Contextual hints per process

For the top offender in each panel, append a one-line context hint when the
process name matches a known pattern:

| Pattern | Hint |
| --- | --- |
| `Xcode`, `xcodebuild` | "Xcode build in progress — normal during compilation" |
| `Google Chrome Helper`, `Safari Web Content` | "Browser renderer — consider closing idle tabs" |
| `mds`, `mdworker` | "Spotlight indexing — usually settles within minutes" |
| `com.apple.cloudd`, `bird` | "iCloud sync — normal after large file changes" |
| `kernel_task` | "Thermal throttling — high ambient temp or blocked vents" |
| _(unknown)_ | No hint shown |

This gives actionable context without any automated intervention.

---

### 2.4 Safety: read-only guarantee

`--verbose` is strictly read-only. The following constraints are hard-coded:

- No `kill`, `pkill`, `killall`, or signal-sending calls anywhere in the
  verbose path
- No `launchctl` calls
- PID column is informational only — the tool makes no use of it internally

---

## 3. Scope summary

| File | Change |
| --- | --- |
| `overview.go` | Add `--verbose` flag parsing; extend `collectTopProcesses` to return PID + etime; add `collectZombieProcesses`; add `collectMemoryProcesses`; render three verbose panels with hint lookup |
| `main.go` | Pass `verbose bool` from `os.Args` to `RunOverview` |
| `summary.go` | No change |
| `README.md` | Document `--verbose` flag under the `overview` section |

---

## 4. No new dependencies

All data comes from `ps -axo pid,%cpu,rss,state,etime,comm`. No additions to
`go.mod`.
