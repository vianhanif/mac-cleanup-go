# mac-cleanup-go: App Uninstaller — Accuracy & Reliability Improvements

## 1. Goal

Close the gaps between the `mac-cleanup apps` implementation and its documented
behaviour so that the listing, process termination, and summary all work as
shown in the README.

---

## 2. Improvements

### 2.1 Sort apps alphabetically in the listing

**Problem:** `os.ReadDir` returns entries in filesystem order (essentially
arbitrary). The listing table can come out in a random order on every run,
making it hard to scan visually.

**Fix:** After compiling the `[]AppInfo` slice, sort it by `Name` (case-insensitive)
before printing. Add an optional `--sort-size` flag that sorts by `TotalBytes`
descending instead — useful when the goal is to reclaim the most space first.

```
# default — alphabetical
mac-cleanup apps

# largest first
mac-cleanup apps --sort-size
```

**Files:** `apps.go` (sort after parallel sizing), `main.go` (add `--sort-size`
to `appsFlags`), `apps_prompt.go` (no change needed)

---

### 2.2 Use `CFBundleExecutable` for process termination

**Problem:** `pkill -x <Name>` uses the display name (e.g. `"Alfred 5"`), but
`pkill -x` matches the running process's **binary name**, which comes from the
`CFBundleExecutable` key in `Info.plist` (e.g. `"Alfred"`). If they differ,
the app is never terminated before its files are deleted — on macOS, open file
handles can prevent removal or leave orphaned sockets.

**Fix:** Read `CFBundleExecutable` from the plist in `readAppInfo` and store it
in `AppInfo`. Use it in `uninstallApp` for the `pkill` call. Fall back to `Name`
if the key is absent.

```go
// AppInfo — new field
type AppInfo struct {
    ...
    Executable string // CFBundleExecutable — binary name used by pkill
}

// uninstallApp — updated termination
binary := app.Executable
if binary == "" {
    binary = app.Name
}
exec.Command("pkill", "-x", binary).Run()
```

**Files:** `apps.go` (`AppInfo` struct, `readAppInfo`, `uninstallApp`)

---

### 2.3 Scan `/Applications/Utilities/`

**Problem:** The current scan only checks `/Applications` and `~/Applications`
at the top level. Third-party utilities commonly installed outside the App Store
(Proxyman, Charles Proxy, Command X, Hex Fiend, etc.) live in
`/Applications/Utilities/` and are silently missed.

**Fix:** Add `/Applications/Utilities/` to the list of search directories in
`discoverNonAppStoreApps`. The same App Store receipt check (`_MASReceipt/receipt`)
filters out any Apple-shipped utilities (e.g. Terminal, Activity Monitor) that
happen to share the folder.

```go
searchDirs := []string{
    "/Applications",
    "/Applications/Utilities",
    expandHome("~/Applications"),
}
```

**Files:** `apps.go` (`discoverNonAppStoreApps`)

---

### 2.4 Surface discovery errors instead of silently showing 0 apps

**Problem:** The error return from `discoverNonAppStoreApps` is discarded with
`_`. If `/Applications` is unreadable (rare but possible under Full Disk Access
restrictions), the user sees `✓ 0 non-App-Store app(s) found` with no
explanation.

**Fix:** Capture the error and print a warning if the primary scan directory
fails. Partial results (from directories that did succeed) are still shown.

```go
apps, err := discoverNonAppStoreApps()
if err != nil {
    fmt.Printf("  %s  scan warning: %s\n", text.FgYellow.Sprint("!"), err)
}
```

Since `discoverNonAppStoreApps` already `continue`s on per-directory errors,
promote per-directory errors to the returned error when _all_ directories fail
so the caller can distinguish "zero apps installed" from "couldn't read anything".

**Files:** `apps.go` (`discoverNonAppStoreApps` return value, `RunApps`)

---

## 3. Scope summary

| File | Change |
| --- | --- |
| `apps.go` | Sort slice post-sizing; add `Executable` field; read `CFBundleExecutable`; use it in `pkill`; add `/Applications/Utilities/`; surface discovery errors |
| `apps_prompt.go` | No change |
| `main.go` | Add `--sort-size` to `appsFlags`; reject it as unknown elsewhere |
| `README.md` | Add `--sort-size` flag to the `apps` flags table and example |

---

## 4. No new dependencies

All changes use the standard library and the existing `plutil` / `pkill` calls
already present. No additions to `go.mod`.
