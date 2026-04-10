package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

// ─── Data types ───────────────────────────────────────────────────────────────

// AppInfo holds metadata for a discovered non-App-Store application.
type AppInfo struct {
	Name         string
	Path         string
	BundleID     string
	Version      string
	TotalBytes   int64    // pre-computed: .app bundle + all support files
	SupportPaths []string // resolved ~/Library paths deleted on uninstall
}

// AppUninstallResult captures the outcome of uninstalling one app.
type AppUninstallResult struct {
	Name          string
	Status        string // "ok" | "error" | "dry-run"
	ExpectedBytes int64  // TotalBytes captured before deletion
	BytesFreed    int64
	Duration      time.Duration
	RemovedPaths  []string
	ErrMsg        string
}

// ─── Discovery ────────────────────────────────────────────────────────────────

// discoverNonAppStoreApps scans /Applications and ~/Applications and returns
// apps that were not installed via the Apple App Store.
func discoverNonAppStoreApps() ([]AppInfo, error) {
	searchDirs := []string{
		"/Applications",
		expandHome("~/Applications"),
	}

	var apps []AppInfo
	seen := make(map[string]bool)

	for _, dir := range searchDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // dir may not exist
		}
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".app") {
				continue
			}
			appPath := filepath.Join(dir, entry.Name())
			if seen[appPath] {
				continue
			}
			seen[appPath] = true

			if isAppStoreApp(appPath) {
				continue
			}

			apps = append(apps, readAppInfo(appPath))
		}
	}

	// Pre-compute size for each app in parallel.
	var wg sync.WaitGroup
	for i := range apps {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			a := &apps[idx]
			a.SupportPaths = supportPaths(*a)
			a.TotalBytes = bytesAt(a.Path)
			for _, p := range a.SupportPaths {
				a.TotalBytes += bytesAt(p)
			}
		}(i)
	}
	wg.Wait()

	return apps, nil
}

// isAppStoreApp returns true when the .app bundle contains the canonical
// App Store receipt at Contents/_MASReceipt/receipt.
func isAppStoreApp(appPath string) bool {
	receipt := filepath.Join(appPath, "Contents", "_MASReceipt", "receipt")
	_, err := os.Stat(receipt)
	return err == nil
}

// readAppInfo extracts display name, bundle ID, and version from Info.plist
// using the macOS-native plutil tool (no extra dependencies required).
func readAppInfo(appPath string) AppInfo {
	name := strings.TrimSuffix(filepath.Base(appPath), ".app")
	info := AppInfo{Name: name, Path: appPath}

	plistPath := filepath.Join(appPath, "Contents", "Info.plist")
	out, err := exec.Command("plutil", "-convert", "json", "-o", "-", plistPath).Output()
	if err != nil {
		return info
	}

	var plist map[string]any
	if json.Unmarshal(out, &plist) != nil {
		return info
	}

	if v, ok := plist["CFBundleIdentifier"].(string); ok {
		info.BundleID = v
	}
	if v, ok := plist["CFBundleShortVersionString"].(string); ok {
		info.Version = v
	}
	// Prefer display name, fall back to bundle name, then the filename-derived name.
	if v, ok := plist["CFBundleDisplayName"].(string); ok && v != "" {
		info.Name = v
	} else if v, ok := plist["CFBundleName"].(string); ok && v != "" {
		info.Name = v
	}

	return info
}

// ─── Support-path resolution ──────────────────────────────────────────────────

// supportPaths returns all ~/Library support paths associated with an app,
// filtered to only those that actually exist on disk.
func supportPaths(app AppInfo) []string {
	home := os.Getenv("HOME")
	lib := filepath.Join(home, "Library")
	bid := app.BundleID
	name := app.Name

	type locationDef struct {
		base  string
		names []string
	}

	locs := []locationDef{
		{filepath.Join(lib, "Application Support"), []string{bid, name}},
		{filepath.Join(lib, "Caches"), []string{bid, name}},
		{filepath.Join(lib, "Logs"), []string{bid, name}},
		{filepath.Join(lib, "Containers"), []string{bid}},
		{filepath.Join(lib, "Saved Application State"), []string{bid + ".savedState"}},
		{filepath.Join(lib, "WebKit"), []string{bid}},
	}

	// Preferences: match <BundleID>.plist and <BundleID>.<variant>.plist
	prefsDir := filepath.Join(lib, "Preferences")
	if bid != "" {
		entries, _ := os.ReadDir(prefsDir)
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), bid) && strings.HasSuffix(e.Name(), ".plist") {
				locs = append(locs, locationDef{prefsDir, []string{e.Name()}})
			}
		}
	}

	// Group Containers: dirs whose name contains the BundleID
	if bid != "" {
		gcDir := filepath.Join(lib, "Group Containers")
		entries, _ := os.ReadDir(gcDir)
		for _, e := range entries {
			if strings.Contains(e.Name(), bid) {
				locs = append(locs, locationDef{gcDir, []string{e.Name()}})
			}
		}
	}

	var paths []string
	seen := make(map[string]bool)
	for _, loc := range locs {
		for _, n := range loc.names {
			if n == "" {
				continue
			}
			p := filepath.Join(loc.base, n)
			if seen[p] {
				continue
			}
			seen[p] = true
			if _, err := os.Stat(p); err == nil {
				paths = append(paths, p)
			}
		}
	}
	return paths
}

// ─── Uninstall ────────────────────────────────────────────────────────────────

// uninstallApp terminates the app if running, removes the .app bundle, and
// deletes all associated support files. When dryRun is true nothing is deleted.
func uninstallApp(app AppInfo, dryRun bool) AppUninstallResult {
	start := time.Now()
	result := AppUninstallResult{Name: app.Name, ExpectedBytes: app.TotalBytes}

	// Use pre-computed support paths if available, else resolve fresh.
	spaths := app.SupportPaths
	if spaths == nil {
		spaths = supportPaths(app)
	}
	toRemove := append([]string{app.Path}, spaths...)

	if dryRun {
		result.Status = "dry-run"
		result.RemovedPaths = toRemove
		result.Duration = time.Since(start)
		return result
	}

	// Best-effort: terminate the process before removing files.
	exec.Command("pkill", "-x", app.Name).Run() //nolint:errcheck

	var totalFreed int64
	var removed []string
	var lastErr error

	for _, p := range toRemove {
		if _, err := os.Stat(p); os.IsNotExist(err) {
			continue
		}
		freed := bytesAt(p)
		if err := os.RemoveAll(p); err != nil {
			lastErr = err
			continue
		}
		totalFreed += freed
		removed = append(removed, p)
	}

	result.BytesFreed = totalFreed
	result.RemovedPaths = removed
	result.Duration = time.Since(start)
	if lastErr != nil {
		result.Status = "error"
		result.ErrMsg = lastErr.Error()
	} else {
		result.Status = "ok"
	}
	return result
}

// ─── Summary ──────────────────────────────────────────────────────────────────

// PrintAppUninstallSummary renders a rounded summary table for app uninstall results.
func PrintAppUninstallSummary(results []AppUninstallResult, dryRun bool, out io.Writer) {
	if len(results) == 0 {
		return
	}

	fmt.Fprintln(out)
	title := "── App Uninstall Summary"
	if dryRun {
		title = "── App Uninstall Dry-run  (nothing deleted)"
	}
	fmt.Fprintln(out, text.Bold.Sprint(title))
	fmt.Fprintln(out)

	tw := table.NewWriter()
	tw.SetOutputMirror(out)
	tw.SetStyle(table.StyleRounded)
	tw.AppendHeader(table.Row{"App", "Status", "Expected", "Freed", "Time"})

	var totalExpected, totalFreed int64
	for _, r := range results {
		var statusIcon string
		switch r.Status {
		case "ok":
			statusIcon = text.FgGreen.Sprint("✓ removed")
		case "dry-run":
			statusIcon = text.FgCyan.Sprint("~ dry-run")
		default:
			statusIcon = text.FgRed.Sprint("✗ error")
		}

		expected := fmtBytes(r.ExpectedBytes)
		freed := fmtBytes(r.BytesFreed)
		if r.Status == "dry-run" {
			freed = "—"
		}
		totalExpected += r.ExpectedBytes
		totalFreed += r.BytesFreed

		tw.AppendRow(table.Row{
			r.Name,
			statusIcon,
			expected,
			freed,
			r.Duration.Round(time.Millisecond).String(),
		})
	}

	tw.AppendSeparator()
	tw.AppendRow(table.Row{
		text.Bold.Sprint("Total"),
		fmt.Sprintf("%d app(s)", len(results)),
		fmtBytes(totalExpected),
		fmtBytes(totalFreed) + " freed",
		"",
	})
	tw.Render()
	fmt.Fprintln(out)
}

// ─── Entry point ──────────────────────────────────────────────────────────────

// RunApps is the entry point for the `mac-cleanup apps` command.
func RunApps(listOnly, dryRun bool) {
	fmt.Println()

	var apps []AppInfo
	runWithSpinner("Scanning applications and computing sizes…", func() {
		apps, _ = discoverNonAppStoreApps()
	})

	if len(apps) == 0 {
		fmt.Println(text.FgGreen.Sprint("  ✓ No non-App-Store applications found."))
		fmt.Println()
		return
	}

	fmt.Printf("  %s  %s non-App-Store app(s) found\n\n",
		text.FgGreen.Sprint("✓"),
		text.Bold.Sprint(fmt.Sprintf("%d", len(apps))),
	)

	if listOnly {
		printAppList(apps)
		return
	}

	selected := selectApps(apps)
	if len(selected) == 0 {
		fmt.Println(text.FgYellow.Sprint("  No apps selected. Nothing to do."))
		fmt.Println()
		return
	}

	if !dryRun && !confirmUninstall(selected) {
		fmt.Println(text.FgYellow.Sprint("  Cancelled."))
		fmt.Println()
		return
	}

	var results []AppUninstallResult
	for _, app := range selected {
		results = append(results, uninstallApp(app, dryRun))
	}
	PrintAppUninstallSummary(results, dryRun, os.Stdout)
}
