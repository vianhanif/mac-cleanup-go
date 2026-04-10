package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jedib0t/go-pretty/v6/text"
)

// ─── Task result ──────────────────────────────────────────────────────────────

// TaskResult captures the outcome of one cleanup task.
type TaskResult struct {
	Name       string
	Status     string   // "ok" | "skipped" | "error"
	Detail     string   // e.g. "843 MB freed", "not running", error message
	BytesFreed int64
	Duration   time.Duration
	Paths      []string // optional list of relevant paths (e.g. node_modules found)
}

// ─── Individual cleanup tasks ─────────────────────────────────────────────────

func cleanUserCaches() TaskResult {
	start := time.Now()
	cacheDir := expandHome("~/Library/Caches/")
	freed, _, err := safeDeleteChildren(cacheDir, cacheExclusions)
	if err != nil {
		return TaskResult{Name: "User Caches", Status: "error", Detail: err.Error(), Duration: time.Since(start)}
	}
	detail := fmtBytes(freed) + " freed"
	if freed == 0 {
		detail = "already clean"
	}
	return TaskResult{Name: "User Caches", Status: "ok", Detail: detail, BytesFreed: freed, Duration: time.Since(start)}
}

func cleanUserLogs() TaskResult {
	start := time.Now()
	logDir := expandHome("~/Library/Logs/")

	entries, err := os.ReadDir(logDir)
	if err != nil {
		return TaskResult{Name: "User Logs", Status: "error", Detail: err.Error(), Duration: time.Since(start)}
	}

	var freed int64
	for _, entry := range entries {
		childPath := filepath.Join(logDir, entry.Name())
		before := bytesAt(childPath)
		if entry.IsDir() {
			// Only delete files inside log subdirs, not the dir itself.
			innerEntries, _ := os.ReadDir(childPath)
			for _, inner := range innerEntries {
				innerPath := filepath.Join(childPath, inner.Name())
				if !inner.IsDir() {
					os.Remove(innerPath) //nolint:errcheck
				}
			}
		} else {
			os.Remove(childPath) //nolint:errcheck
		}
		freed += before
	}

	detail := fmtBytes(freed) + " freed"
	if freed == 0 {
		detail = "already clean"
	}
	return TaskResult{Name: "User Logs", Status: "ok", Detail: detail, BytesFreed: freed, Duration: time.Since(start)}
}

func cleanXcodeDerivedData() TaskResult {
	start := time.Now()
	path := expandHome("~/Library/Developer/Xcode/DerivedData/")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return TaskResult{Name: "Xcode DerivedData", Status: "skipped", Detail: "not found", Duration: time.Since(start)}
	}
	freed, err := safeDelete(path)
	if err != nil {
		return TaskResult{Name: "Xcode DerivedData", Status: "error", Detail: err.Error(), Duration: time.Since(start)}
	}
	detail := fmtBytes(freed) + " freed"
	if freed == 0 {
		detail = "already clean"
	}
	return TaskResult{Name: "Xcode DerivedData", Status: "ok", Detail: detail, BytesFreed: freed, Duration: time.Since(start)}
}

func cleanXcodeArchives() TaskResult {
	start := time.Now()
	path := expandHome("~/Library/Developer/Xcode/Archives/")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return TaskResult{Name: "Xcode Archives", Status: "skipped", Detail: "not found", Duration: time.Since(start)}
	}
	freed, err := safeDelete(path)
	if err != nil {
		return TaskResult{Name: "Xcode Archives", Status: "error", Detail: err.Error(), Duration: time.Since(start)}
	}
	detail := fmtBytes(freed) + " freed"
	if freed == 0 {
		detail = "already clean"
	}
	return TaskResult{Name: "Xcode Archives", Status: "ok", Detail: detail, BytesFreed: freed, Duration: time.Since(start)}
}

func thinTimeMachineSnapshots() TaskResult {
	start := time.Now()
	// First check if any snapshots exist — no sudo required.
	listOut, _ := runCmdSimple("tmutil", "listlocalsnapshots", "/")
	var snapshots []string
	for _, l := range strings.Split(listOut, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			snapshots = append(snapshots, l)
		}
	}
	if len(snapshots) == 0 {
		return TaskResult{Name: "TM Snapshots", Status: "skipped", Detail: "no local snapshots", Duration: time.Since(start)}
	}
	// Attempt thinning without sudo (works on macOS 12.3+).
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	res := runCmd(ctx, "tmutil", "thinlocalsnapshots", "/",
		tmutilFreeTarget, tmutilPriority)
	if res.err != nil {
		if strings.Contains(res.stderr, "permission") ||
			strings.Contains(res.stderr, "root") ||
			strings.Contains(res.stderr, "sudo") ||
			strings.Contains(res.stderr, "Not permitted") {
			hint := fmt.Sprintf("requires root — run: sudo tmutil thinlocalsnapshots / %s %s",
				tmutilFreeTarget, tmutilPriority)
			return TaskResult{Name: "TM Snapshots", Status: "skipped", Detail: hint, Duration: time.Since(start)}
		}
		return TaskResult{Name: "TM Snapshots", Status: "error", Detail: truncate(res.stderr, truncErrMsgLen), Duration: time.Since(start)}
	}
	detail := fmt.Sprintf("thinned (%d snapshot(s))", len(snapshots))
	return TaskResult{Name: "TM Snapshots", Status: "ok", Detail: detail, Duration: time.Since(start)}
}

func pruneDocker() TaskResult {
	start := time.Now()
	if !commandExists("docker") {
		return TaskResult{Name: "Docker Prune", Status: "skipped", Detail: "not installed", Duration: time.Since(start)}
	}
	// Check docker is running.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	info := runCmd(ctx, "docker", "info")
	if info.err != nil {
		return TaskResult{Name: "Docker Prune", Status: "skipped", Detail: "not running", Duration: time.Since(start)}
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), dockerPruneTimeout)
	defer cancel2()
	res := runCmd(ctx2, "docker", "system", "prune", "-af", "--volumes")
	if res.err != nil {
		return TaskResult{Name: "Docker Prune", Status: "error", Detail: truncate(res.stderr, truncErrMsgLen), Duration: time.Since(start)}
	}
	// Parse "Total reclaimed space: X" from output.
	detail := "pruned"
	for _, line := range strings.Split(res.stdout, "\n") {
		if strings.Contains(line, "Total reclaimed space") {
			fields := strings.Fields(line)
			if len(fields) >= 4 {
				detail = "freed " + fields[len(fields)-1]
			}
			break
		}
	}
	return TaskResult{Name: "Docker Prune", Status: "ok", Detail: detail, Duration: time.Since(start)}
}

func reportNodeModules() TaskResult {
	start := time.Now()
	home := os.Getenv("HOME")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res := runCmd(ctx, "find", home, "-type", "d", "-name", "node_modules",
		"-not", "-path", "*/\\.*", "-prune")
	if res.err != nil && res.stdout == "" {
		return TaskResult{Name: "node_modules", Status: "skipped", Detail: "find error", Duration: time.Since(start)}
	}
	var paths []string
	for _, line := range strings.Split(res.stdout, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			paths = append(paths, strings.Replace(line, home, "~", 1))
		}
	}
	detail := fmt.Sprintf("%d dirs found (not deleted — manual action required)", len(paths))
	if len(paths) == 0 {
		detail = "none found"
	}
	return TaskResult{Name: "node_modules", Status: "ok", Detail: detail, Paths: paths, Duration: time.Since(start)}
}

// truncate shortens s to at most n bytes, appending "…" if cut.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ─── Safe mode ────────────────────────────────────────────────────────────────

// cleanDiagnosticReports removes crash/hang report files older than 30 days
// from ~/Library/Logs/DiagnosticReports/ — recent files are left intact.
func cleanDiagnosticReports() TaskResult {
	start := time.Now()
	dir := expandHome("~/Library/Logs/DiagnosticReports/")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return TaskResult{Name: "Diagnostic Reports", Status: "skipped", Detail: "directory not found", Duration: time.Since(start)}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return TaskResult{Name: "Diagnostic Reports", Status: "error", Detail: err.Error(), Duration: time.Since(start)}
	}
	cutoff := time.Now().AddDate(0, 0, -30)
	var freed int64
	var removed int
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			p := filepath.Join(dir, e.Name())
			freed += bytesAt(p)
			os.Remove(p) //nolint:errcheck
			removed++
		}
	}
	if removed == 0 {
		return TaskResult{Name: "Diagnostic Reports", Status: "ok", Detail: "nothing older than 30 days", Duration: time.Since(start)}
	}
	detail := fmt.Sprintf("%d file(s) removed, %s freed", removed, fmtBytes(freed))
	return TaskResult{Name: "Diagnostic Reports", Status: "ok", Detail: detail, BytesFreed: freed, Duration: time.Since(start)}
}

// reportDeviceBackups reports the size and count of iOS/device backups stored in
// ~/Library/Application Support/MobileSync/Backup/ without deleting anything.
func reportDeviceBackups() TaskResult {
	start := time.Now()
	dir := expandHome("~/Library/Application Support/MobileSync/Backup/")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return TaskResult{Name: "Device Backups", Status: "skipped", Detail: "no backups found", Duration: time.Since(start)}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return TaskResult{Name: "Device Backups", Status: "error", Detail: err.Error(), Duration: time.Since(start)}
	}
	var backups []string
	for _, e := range entries {
		if e.IsDir() {
			info, _ := e.Info()
			modTime := "unknown"
			if info != nil {
				modTime = info.ModTime().Format("2006-01-02")
			}
			backups = append(backups, fmt.Sprintf("%s  (last modified %s)", e.Name(), modTime))
		}
	}
	total := bytesAt(dir)
	detail := fmt.Sprintf("%s total, %d backup(s) — manage in Finder", fmtBytes(total), len(backups))
	return TaskResult{Name: "Device Backups", Status: "ok", Detail: detail, Paths: backups, Duration: time.Since(start)}
}

// RunSafe runs the safe cleanup mode.
func RunSafe() []TaskResult {
	tasks := []struct {
		label string
		fn    func() TaskResult
	}{
		{"User Caches", cleanUserCaches},
		{"User Logs", cleanUserLogs},
		{"Xcode DerivedData", cleanXcodeDerivedData},
		{"TM Snapshots", thinTimeMachineSnapshots},
		{"Diagnostic Reports", cleanDiagnosticReports},
	}

	fmt.Println()
	fmt.Printf("%s  %s\n", text.Bold.Sprint("🧹 macOS Maintenance"), text.FgHiBlack.Sprint("· safe"))
	fmt.Println(strings.Repeat("─", 56))

	return runCleanTasks(tasks)
}

// ─── Deep mode ────────────────────────────────────────────────────────────────

// RunDeep runs the deep cleanup mode (safe + Docker + node_modules report).
func RunDeep() []TaskResult {
	tasks := []struct {
		label string
		fn    func() TaskResult
	}{
		{"User Caches", cleanUserCaches},
		{"User Logs", cleanUserLogs},
		{"Xcode DerivedData", cleanXcodeDerivedData},
		{"Xcode Archives", cleanXcodeArchives},
		{"TM Snapshots", thinTimeMachineSnapshots},
		{"Diagnostic Reports", cleanDiagnosticReports},
		{"Docker Prune", pruneDocker},
		{"node_modules", reportNodeModules},
		{"Device Backups", reportDeviceBackups},
	}

	fmt.Println()
	fmt.Printf("%s  %s\n", text.Bold.Sprint("🧹 macOS Maintenance"), text.FgHiBlack.Sprint("· deep"))
	fmt.Println(strings.Repeat("─", 56))

	results := runCleanTasks(tasks)

	// Print node_modules and device backup paths if any were found.
	for _, r := range results {
		if r.Name == "node_modules" && len(r.Paths) > 0 {
			fmt.Println()
			fmt.Println(text.Bold.Sprint("── node_modules found"))
			fmt.Println()
			for _, p := range r.Paths {
				fmt.Printf("  %s %s\n", text.FgHiBlack.Sprint("·"), p)
			}
		}
		if r.Name == "Device Backups" && len(r.Paths) > 0 {
			fmt.Println()
			fmt.Println(text.Bold.Sprint("── Device Backups"))
			fmt.Println()
			for _, p := range r.Paths {
				fmt.Printf("  %s %s\n", text.FgHiBlack.Sprint("·"), p)
			}
			fmt.Println(text.FgHiBlack.Sprint("  Manage in Finder → Go → Library → MobileSync → Backup"))
		}
	}

	return results
}

// ─── Task runner ──────────────────────────────────────────────────────────────

func runCleanTasks(tasks []struct {
	label string
	fn    func() TaskResult
}) []TaskResult {
	total := len(tasks)
	doneCh := make(chan phaseItem, total)
	resultCh := make(chan TaskResult, total)

	var wg sync.WaitGroup
	for _, t := range tasks {
		t := t
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := t.fn()
			resultCh <- r

			ok := r.Status == "ok"
			detail := r.Detail
			if r.Status == "skipped" {
				detail = text.FgHiBlack.Sprint("skipped — " + r.Detail)
			} else if !ok {
				detail = truncate(r.Detail, truncErrMsgLen)
			}
			doneCh <- phaseItem{
				label:  t.label,
				result: detail,
				ok:     ok || r.Status == "skipped",
			}
		}()
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	phaseMonitor("Cleaning", total, doneCh)

	var results []TaskResult
	for r := range resultCh {
		results = append(results, r)
	}
	return results
}
