# mac-cleanup-go

A macOS maintenance tool written in Go with an animated terminal UI featuring live progress bars and rounded summary tables.

## Features

- **Live progress bars** — animated phase monitors with per-task trackers
- **Parallel execution** — independent tasks fan out as goroutines, results collected via channels
- **Rounded summary tables** — per-task status, bytes freed, and duration after every run
- **Safety-first** — three hard-coded constraints enforced at the `safeDelete()` level, not just in comments

## Commands

| Command | Purpose |
|---|---|
| `mac-cleanup overview` | System dashboard — CPU, Memory, Disk, top dirs, Brew, Docker |
| `mac-cleanup safe` | Safe cleanup — caches, logs, Xcode DerivedData, Time Machine thin |
| `mac-cleanup deep` | Deep cleanup — safe + Docker prune + node_modules size report |
| `mac-cleanup brew` | Smart Homebrew upgrade — risk-scored, dependency-aware |
| `mac-cleanup brew-analyze` | Homebrew package audit — duplicates, stale versions, EOL/clashes |
| `mac-cleanup brew-full` | `brew-analyze` then `brew` upgrade in one run |
| `mac-cleanup help` | Show detailed usage |

Running with no arguments shows the full help screen.

## Install

**Prerequisites:** Go 1.25+, macOS

```bash
git clone https://github.com/vianhanif/mac-cleanup-go
cd mac-cleanup-go
go build -o mac-cleanup .
```

Then add an alias to your shell config (`~/.zshrc`, `~/.bashrc`, etc.):

```bash
# mac-cleanup
alias mac-cleanup="/path/to/mac-cleanup-go/mac-cleanup"

# Optional short aliases per command
alias cleanup="mac-cleanup safe"
alias cleanup-deep="mac-cleanup deep"
alias cleanup-overview="mac-cleanup overview"
alias brew-upgrade="mac-cleanup brew"
alias brew-audit="mac-cleanup brew-analyze"
```

Reload your shell:

```bash
source ~/.zshrc
```

## Usage

```
mac-cleanup <command>
```

### `overview`

Collects system metrics in parallel and renders two tables — system stats and top home directories.

```
🖥  System Dashboard
────────────────────────────────────────────────────────

╭──────────────────┬──────────────────────────────────────╮
│ CPU              │ ██████░░░░  58%                       │
│ Memory           │ ████░░░░░░  40%                       │
│ Disk /           │ ███████░░░  72%  89 GB used of 500 GB │
│ Brew Outdated    │ 4 packages                            │
│ Docker           │ running · 2.1 GB reclaimable          │
├──────────────────┼──────────────────────────────────────┤
│ Hostname         │ alvian-mbp                            │
│ macOS            │ 15.3.2                                │
│ Uptime           │ 3 days                                │
╰──────────────────┴──────────────────────────────────────╯

── Top Home Directories

╭──────────────────┬──────────╮
│ Path             │ Size     │
├──────────────────┼──────────┤
│ ~/Library        │ 48 GB    │
│ ~/Documents      │ 12 GB    │
╰──────────────────┴──────────╯
```

### `safe`

Cleans in parallel, then shows a summary table.

```
🧹 macOS Maintenance  · safe
────────────────────────────────────────────────────────

  Cleaning  3 / 4  [████████░░]  2s
    User Caches              ✓  843 MB freed   0.4s
    User Logs                ✓   12 MB freed   0.1s
    Xcode DerivedData        ✓  2.1 GB freed   0.2s
    TM Snapshots             ✓  thinned        1.8s

── Summary
╭──────────────────────┬────────┬──────────────┬────────╮
│ Task                 │ Status │ Result       │ Time   │
├──────────────────────┼────────┼──────────────┼────────┤
│ User Caches          │   ✓    │ 843 MB freed │ 0.4s   │
│ User Logs            │   ✓    │  12 MB freed │ 0.1s   │
│ Xcode DerivedData    │   ✓    │ 2.1 GB freed │ 0.2s   │
│ TM Snapshots         │   ✓    │ thinned      │ 1.8s   │
├──────────────────────┼────────┼──────────────┼────────┤
│ Total                │  4/4   │ 2.96 GB freed│        │
╰──────────────────────┴────────┴──────────────┴────────╯
```

### `brew`

**Phase 1 — Analysis** (parallel): for each outdated formula, runs `brew info`, `brew deps`, `brew uses --installed`, and checks `brew list --pinned`.

**Risk scoring per package:**

| Signal | Effect |
|---|---|
| 0 dependents (leaf) | risk 0 → auto-upgrade |
| 1–3 dependents | risk 1 → auto-upgrade |
| 4+ dependents | risk 2 → prompt |
| Major version bump (`1.x → 2.x`) | +1 risk |
| `keg-only` formula | −1 risk |
| In critical list or pinned | hard skip |

**Phase 2 — Upgrade**: auto-risk packages run in parallel goroutines; prompted packages pause the progress bar and ask `[y/N]`.

```
🍺 Smart Brew Upgrade
────────────────────────────────────────────────────────

  4 outdated packages found

  Analysing packages  4 / 4  [██████████]  1s
    jq          1.7.0 -> 1.7.1    leaf -> auto-upgrade
    wget        1.24.5 -> 1.25.0  leaf -> auto-upgrade
    libpng      1.6.43 -> 1.6.44  4 dependents - will prompt
    node        20.0 -> 22.0      critical runtime - skipping

  Upgrading  2 / 2  [██████████]  3s
    jq          -> 1.7.1   (0.4s)
    wget        -> 1.25.0  (0.8s)

  ! libpng has 4 dependent(s):
    Dependents: cairo, ffmpeg, imagemagick, libvips
    Version:    1.6.43 -> 1.6.44
  Upgrade? [y/N]:
```

### `brew-analyze`

Runs three analysis passes in parallel:

1. **Duplicates** — same binary stem installed from multiple taps
2. **Multi-version** — stale `pkg@N` slots alongside a newer base formula
3. **Clashes / EOL** — packages matched against a built-in EOL/shadowing rules table

Results are categorised into **auto-removable**, **prompt**, and **informational**, then offered for action.

```
🔍 Brew Package Analysis
────────────────────────────────────────────────────────

  Analysing  3 / 3  [██████████]  4s
    Duplicates            0 issue(s) found
    Multi-version slots   2 issue(s) found
    Clashes / EOL         3 issue(s) found

── Analysis Results
╭─────────────────┬──────────────────┬───────────────────────────────────────────╮
│ Package         │ Issue            │ Detail                                    │
├─────────────────┼──────────────────┼───────────────────────────────────────────┤
│ MULTIPLE VERSIONS                                                               │
│ node@18         │ ✓ auto-remove    │ node 22 installed; node@18 is a stale slot│
│ llvm@16         │ ⚠ prompt         │ required by: spirv-llvm-translator         │
├─────────────────┼──────────────────┼───────────────────────────────────────────┤
│ CLASHES / EOL                                                                   │
│ openssl@1.1     │ ℹ inform         │ openssl@1.1 EOL Sep 2023 → openssl@3      │
│ node@20         │ ℹ inform         │ Node 20 LTS ends Apr 2026                 │
│ curl            │ ℹ inform         │ Homebrew curl shadows macOS curl in PATH  │
╰─────────────────┴──────────────────┴───────────────────────────────────────────╯

── Recommended Actions

  Auto-removable (no dependents):
    [ ] node@18      brew uninstall node@18

  Needs your decision:
    [ ] llvm@16      required by: spirv-llvm-translator

  Informational (no action taken):
    [i] openssl@1.1  openssl@1.1 EOL Sep 2023 → openssl@3
    [i] node@20      Node 20 LTS ends Apr 2026
    [i] curl         Homebrew curl shadows macOS curl in PATH

  Apply 1 auto-removable package(s)? [y/N]:
```

## Safety Guarantees

These are enforced in code at the `safeDelete()` level — not just documentation.

### Constraint 1 — No slow bootup
`LaunchAgents/`, `StartupItems/`, and `Fonts/` are in `neverTouchPaths` and can never be passed to `safeDelete()`.

### Constraint 2 — No system crashes
- Only paths in `allowedPathSuffixes` (all under `~/Library/`) can be deleted
- No `diskutil`, `fsck`, `kextcache`, or SIP-protected path access
- `tmutil thinlocalsnapshots` only — no snapshot deletion, no `purge`
- Docker pruned only when `docker info` confirms it is running
- Critical packages (`node`, `go`, `rust`, `openssl@3`, `gcc`, `zsh`…) are never auto-upgraded

### Constraint 3 — No browser login resets
- `~/Library/Application Support/` is in `neverTouchPaths` — never touched
- Cache subdirectories excluded from the caches sweep:

  ```
  com.google.Chrome              com.google.Chrome.canary
  org.mozilla.firefox            org.mozilla.nightly
  com.microsoft.edgemac          com.microsoft.edgemac.Beta
  com.brave.Browser              com.brave.Browser.beta
  com.operasoftware.Opera        com.vivaldi.Vivaldi
  com.apple.Safari               com.apple.SafariTechnologyPreview
  com.tinyspeck.slackmacgap
  com.apple.Spotlight            com.apple.metadata
  com.apple.bird                 com.apple.akd
  com.apple.GSSFramework         CloudKit
  com.apple.iCloudDrive
  ```

### What is never auto-deleted
| Item | Rule |
|---|---|
| `~/Library/Application Support/` | `neverTouchPaths` hard block |
| `~/Library/LaunchAgents/` | `neverTouchPaths` hard block |
| `~/Library/Fonts/` | `neverTouchPaths` hard block |
| `/System/`, `/usr/`, `/bin/`… | `neverTouchPaths` hard block |
| Browser cache dirs | `cacheExclusions` list |
| `node_modules/` directories | reported only in `deep` mode, never deleted |
| Pinned brew packages | `brew list --pinned` checked before any upgrade |

## Code Structure

```
mac-cleanup-go/
├── main.go          — CLI entry point, signal handler, mode dispatch
├── config.go        — all safety constants (allowlist, blocklist, exclusions, EOL rules)
├── runner.go        — safeDelete(), runCmd(), bytesAt(), fmtBytes()
├── progress.go      — newPW(), phaseMonitor() — animated progress bars
├── overview.go      — overview mode, parallel metric collectors
├── cleanup.go       — safe + deep modes, parallel task runner
├── brew.go          — smart upgrade: risk scoring, auto/prompt/skip
├── brew_analyze.go  — 3-pass package audit: duplicates, multi-version, clashes
├── summary.go       — rounded summary tables + help screen
├── go.mod
└── go.sum
```

**Only one dependency:** [`github.com/jedib0t/go-pretty/v6`](https://github.com/jedib0t/go-pretty)


