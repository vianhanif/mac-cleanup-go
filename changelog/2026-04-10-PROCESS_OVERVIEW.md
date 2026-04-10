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
| **Running time** | `ps -axo etime` | A process that has been at 40% CPU for 3 days is very different from one that spiked 10 seconds ago. Rendered as two columns: **Started** ("Today HH:MM" or "Mon D, HH:MM") and **Running For** (e.g. `1d 23h`, `4h 35m`, `32m 27s`) |

```
── Top Processes (by CPU) — verbose

╭───────┬─────────────────────────────────┬───────┬────────┬──────────────┬─────────────╮
│ PID   │ PROCESS                         │ CPU % │ MEMORY │ STARTED      │ RUNNING FOR │
├───────┼─────────────────────────────────┼───────┼────────┼──────────────┼─────────────┤
│ 280   │ com.manageengine.appctrl.driver │ 89.3% │  11 MB │ Apr 8, 13:50 │      1d 23h │
│ 67261 │ WindowServer                    │ 16.8% │  46 MB │ Apr 9, 16:16 │     20h 46m │
│ 162   │ trustd                          │ 10.1% │   7 MB │ Apr 8, 13:50 │      1d 23h │
│ 68174 │ stable                          │  4.5% │  98 MB │  Today 08:26 │      4h 35m │
│ 77424 │ Lark Helper (Renderer)          │  1.7% │ 228 MB │  Today 11:16 │      1h 46m │
╰───────┴─────────────────────────────────┴───────┴────────┴──────────────┴─────────────╯

── Top Processes (by Memory) — verbose

╭───────┬─────────────────────────────────┬───────┬────────┬──────────────┬─────────────╮
│ PID   │ PROCESS                         │ CPU % │ MEMORY │ STARTED      │ RUNNING FOR │
├───────┼─────────────────────────────────┼───────┼────────┼──────────────┼─────────────┤
│ 86502 │ Code Helper (Plugin)            │  0.1% │ 544 MB │  Today 12:30 │     32m 27s │
│ 86312 │ Code Helper (Renderer)          │  0.0% │ 426 MB │  Today 12:29 │      33m 7s │
│ 285   │ mds_stores                      │  0.0% │ 322 MB │ Apr 8, 13:50 │      1d 23h │
│ 77424 │ Lark Helper (Renderer)          │  1.7% │ 228 MB │  Today 11:16 │      1h 46m │
│ 68173 │ Google Chrome                   │  1.0% │ 169 MB │  Today 08:26 │      4h 35m │
╰───────┴─────────────────────────────────┴───────┴────────┴──────────────┴─────────────╯

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
