# mac-cleanup-go Changelog

## Process Overview — `overview --verbose` (2026-04-10)
**Files:** `overview.go`, `main.go`, `README.md`

Extended `mac-cleanup overview` with a `--verbose` flag for richer diagnostic snapshot of running processes.

**New Flag:**
```bash
mac-cleanup overview --verbose
```

**Verbose Panels:**
1. **Top 10 by CPU** - Extended process list, sorted by CPU descending
2. **Top 10 by Memory** - Separate sort by RSS
3. **Zombie / stuck processes** - Processes in state `Z` or 0% CPU with RSS > 500 MB

**Richer Columns:**
- **PID** - For cross-referencing in Activity Monitor
- **Running time** - "Started" (Today HH:MM or Mon D, HH:MM) and "Running For" (1d 23h, 4h 35m, etc.)

**Contextual Hints:**
| Pattern | Hint |
|---------|------|
| `Xcode`, `xcodebuild` | "Xcode build in progress — normal during compilation" |
| `Google Chrome Helper`, `Safari Web Content` | "Browser renderer — consider closing idle tabs" |
| `mds`, `mdworker` | "Spotlight indexing — usually settles within minutes" |
| `com.apple.cloudd`, `bird` | "iCloud sync — normal after large file changes" |
| `kernel_task` | "Thermal throttling — high ambient temp or blocked vents" |

**Safety:** Strictly read-only. No `kill`, `pkill`, `killall`, or `launchctl` calls in verbose path.

---

## Diagnostic Improvements (2026-04-10)
**Files:** `overview.go`, `cleanup.go`, `phases.go`, `README.md`

### 2.1 Top CPU & Memory Processes in `overview`
Added `collectTopProcesses(n int)` collector running `ps aux` sorted by CPU and RSS. Displays top 5 by CPU and top 5 by memory, skipping kernel threads.

### 2.2 Swap Usage in `overview`
Parse `sysctl vm.swapusage` to extract swap used/total. Highlights in yellow when > 1 GB, red when > 3 GB.

### 2.3 LaunchAgents Count in `overview`
Counts `.plist` files in `~/Library/LaunchAgents/` and `/Library/LaunchAgents/`. Shows note suggesting review via System Settings → General → Login Items when count > 10.

### 2.4 iOS/Device Backup Size in `overview` and `deep`
- **overview:** `collectMobileSyncSize()` adds "Device Backups" row (runs `du -sh` on `~/Library/Application Support/MobileSync/Backup/`)
- **deep:** `reportDeviceBackups()` lists individual backup UUIDs with last-modified date (report-only, no deletion)

### 2.5 Diagnostic/Crash Reports in `safe` Cleanup
Added `cleanDiagnosticReports()` task to `RunSafe` that deletes files older than 30 days from `~/Library/Logs/DiagnosticReports/`. Recent issues remain diagnosable.

### 2.6 Fix CPU % Always Showing 100%
Divided process sum by `sysctl -n hw.logicalcpu` to normalize to true system-wide percentage (0–100). When result exceeds 70%, appends name of highest-CPU process inline.

---

## App Uninstaller Feature (2026-04-10)
**Files:** `apps.go` (new), `apps_prompt.go` (new), `main.go`, `README.md`

New interactive uninstallation support for non-App-Store macOS applications:

**New Command:**
```bash
mac-cleanup apps              # Interactive selection + uninstall
mac-cleanup apps --list       # List only, no prompt
mac-cleanup apps --dry-run    # Show what would be deleted
```

**App Discovery:**
1. Walks `/Applications` and `~/Applications` for `.app` bundles
2. Reads `Contents/Info.plist` for `provisioningProfile` (App Store marker)
3. Checks `Contents/_MASReceipt/receipt` (canonical App Store marker)
4. Classifies apps lacking both markers as non-App-Store

**AppInfo Struct:**
```go
type AppInfo struct {
    Name         string   // CFBundleDisplayName / CFBundleName
    Path         string   // absolute path
    BundleID     string   // CFBundleIdentifier
    Version      string   // CFBundleShortVersionString
    TotalBytes   int64    // .app bundle + support files
    SupportPaths []string // paths to be deleted
}
```

**Clean Uninstall Logic:**
1. Terminate process if running (`pkill -x <AppName>`)
2. Delete `.app` bundle (`os.RemoveAll`)
3. Remove support files by BundleID:
   - `~/Library/Application Support/`
   - `~/Library/Caches/`
   - `~/Library/Preferences/`
   - `~/Library/Logs/`
   - `~/Library/Containers/`
   - `~/Library/Group Containers/`
   - `~/Library/Saved Application State/`
   - `~/Library/WebKit/`

**Safety Constraints:**
- Never delete `/` or paths with < 3 components
- Explicit confirmation for paths outside `~/Library` and `/Applications`
- Dry-run mode available

**Summary Output:**
Shows expected vs actual bytes freed for each app, with timing information.

**New Dependencies:**
- `howett.net/plist` - Parse `Info.plist`
- `github.com/charmbracelet/bubbletea` - Interactive TUI prompt (evaluate; fallback to `bufio`)
- `github.com/jedib0t/go-pretty/v6` - Summary table (already present)