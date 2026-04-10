# mac-cleanup-go: App Uninstaller Feature

## 1. Goal

Add interactive uninstallation support for non-App-Store macOS applications:

- List all installed applications that were **not** installed via the Apple App Store.
- Present an interactive prompt allowing the user to select one or more apps to uninstall.
- Perform a **clean uninstall** — remove the `.app` bundle plus associated support files (preferences, caches, logs, containers).

---

## 2. New Command

```
mac-cleanup apps
```

Added alongside existing commands (`overview`, `safe`, `deep`, `brew`, etc.).

---

## 3. Scope

### Files to create

| File | Purpose |
|---|---|
| `apps.go` | Core logic: app discovery, App Store detection, clean uninstall |
| `apps_prompt.go` | Interactive multi-select prompt and confirmation flow |

### Files to modify

| File | Change |
|---|---|
| `main.go` | Add `"apps"` case to the `switch mode` block |
| `README.md` | Document new `mac-cleanup apps` command |

---

## 4. App Discovery

### Detection strategy

1. Walk `/Applications` and `~/Applications` for `.app` bundles (non-recursive at top level).
2. For each `.app`, read `Contents/Info.plist` and check for the key `provisioningProfile` — its presence indicates App Store distribution.
3. Additionally check `Contents/_MASReceipt/receipt` — existence of this receipt file is the canonical App Store marker.
4. Any app that **lacks** both markers is classified as a non-App-Store app.

### App metadata collected per entry

```go
type AppInfo struct {
    Name          string   // display name from CFBundleDisplayName / CFBundleName
    Path          string   // absolute path, e.g. /Applications/Foo.app
    BundleID      string   // CFBundleIdentifier
    Version       string   // CFBundleShortVersionString
    TotalBytes    int64    // pre-computed: .app bundle + all support files on disk
    SupportPaths  []string // resolved paths that will be deleted on uninstall
}
```

### Size pre-computation

After discovering each app's support paths, sum the disk usage of every path that exists:

```
TotalBytes = bytesAt(app.Path) + Σ bytesAt(p) for p in SupportPaths
```

`bytesAt` uses `du -sk` (already available in `runner.go`) and is called in a goroutine per app so the scan stays fast even with many apps.

The computed size is shown in the listing table as **"Total Size"** so the user can see upfront how much space will be reclaimed before selecting anything.

---

## 5. Interactive Prompt

### UI flow

The listing table now includes a **Total Size** column showing the combined disk usage of the `.app` bundle plus all associated support files already on disk:

```
mac-cleanup apps

  ✓  42 non-App-Store app(s) found

── Non-App-Store Applications

╭────┬──────────────────────┬─────────┬────────────┬──────────────────────────────────────╮
│  # │ Name                 │ Version │ Total Size │ Path                                 │
├────┼──────────────────────┼─────────┼────────────┼──────────────────────────────────────┤
│  1 │ Alfred 5             │ 5.5     │    252 MB  │ /Applications/Alfred 5.app           │
│  2 │ Bear                 │ 2.4.1   │     19 MB  │ /Applications/Bear.app               │
│  3 │ Docker               │ 4.40.0  │    4.1 GB  │ /Applications/Docker.app             │
│  4 │ iTerm                │ 3.5.10  │     85 MB  │ /Applications/iTerm.app              │
│  … │ …                    │ …       │ …          │ …                                    │
╰────┴──────────────────────┴─────────┴────────────┴──────────────────────────────────────╯

  Enter app number(s) to uninstall (e.g. 1,3,5  or  all  or  q to quit)
  >
```

- Multi-select via comma-separated numbers, `all`, or `q` to quit.
- The **Total Size** estimate reflects `.app` bundle + all support-file locations summed before any deletion.

---

## 6. Clean Uninstall Logic

For each selected app:

1. **Terminate** the process if running (`pkill -x <AppName>` via `runner.go`'s `RunCommand`).
2. **Delete** the `.app` bundle (`os.RemoveAll`).
3. **Remove support files** by scanning the following locations using the app's `BundleID`:

| Location | Pattern |
|---|---|
| `~/Library/Application Support/` | `<BundleID>/` or `<AppName>/` |
| `~/Library/Caches/` | `<BundleID>/` or `<AppName>/` |
| `~/Library/Preferences/` | `<BundleID>.plist` and variants |
| `~/Library/Logs/` | `<BundleID>/` or `<AppName>/` |
| `~/Library/Containers/` | `<BundleID>/` |
| `~/Library/Group Containers/` | dirs containing `<BundleID>` |
| `~/Library/Saved Application State/` | `<BundleID>.savedState/` |
| `~/Library/WebKit/` | `<BundleID>/` |

4. **Report** bytes freed and list of removed paths (reuse `PrintCleanupSummary` pattern).

### Safety constraints (inherited from `safeDelete()` policy)

- Never delete `/` or paths with fewer than 3 path components.
- Require explicit confirmation before deleting any path outside `~/Library` and `/Applications`.
- All deletions logged to a dry-run list first; actual removal only after user confirms.

---

## 7. Dry-run Mode

Add `--dry-run` flag:

```
mac-cleanup apps --dry-run
```

Prints what would be removed without deleting anything. Useful for inspection before committing.

---

## 8. Summary Output

After uninstall, print a rounded summary table that displays both the **expected** size (computed at listing time) and the **actual** bytes freed (measured after deletion), so the user can see the expectation vs. reality for each app:

```
── App Uninstall Summary

╭──────────┬───────────┬──────────┬──────────┬────────╮
│ App      │ Status    │ Expected │ Freed    │ Time   │
├──────────┼───────────┼──────────┼──────────┼────────┤
│ Alfred 5 │ ✓ removed │  252 MB  │  248 MB  │ 0.31s  │
│ Bear     │ ✓ removed │   19 MB  │   18 MB  │ 0.08s  │
├──────────┼───────────┼──────────┼──────────┼────────┤
│ Total    │ 2 app(s)  │  271 MB  │  266 MB  │        │
╰──────────┴───────────┴──────────┴──────────┴────────╯
```

- **Expected** — `TotalBytes` captured at scan time (before any deletion).
- **Freed** — actual bytes removed, measured with `du -sk` before/after.
- Small discrepancies are normal (OS metadata, in-use files released after process termination).

For `--dry-run`, the **Freed** column shows `—` and **Expected** shows the projected reclaim.

---

## 9. Dependencies

| Package | Use | Already in go.mod? |
|---|---|---|
| `howett.net/plist` | Parse `Info.plist` | No — add |
| `github.com/charmbracelet/bubbletea` | Interactive TUI prompt | No — evaluate; fallback to `bufio` |
| `github.com/jedib0t/go-pretty/v6` | Summary table | Yes |

---

## 10. Example CLI invocation

```bash
# Interactive selection + uninstall
mac-cleanup apps

# List only, no prompt
mac-cleanup apps --list

# Dry-run: show what would be deleted
mac-cleanup apps --dry-run
```
