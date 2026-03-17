package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ─── Command runner ───────────────────────────────────────────────────────────

// cmdResult holds the output of a shell command.
type cmdResult struct {
	stdout   string
	stderr   string
	duration time.Duration
	err      error
}

// runCmd runs a command with a context (for timeout), captures stdout+stderr.
func runCmd(ctx context.Context, name string, args ...string) cmdResult {
	start := time.Now()
	var outBuf, errBuf bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return cmdResult{
		stdout:   strings.TrimSpace(outBuf.String()),
		stderr:   strings.TrimSpace(errBuf.String()),
		duration: time.Since(start),
		err:      err,
	}
}

// runCmdSimple runs a command with no timeout and returns trimmed stdout.
// Use for fast read-only queries (brew info, df, etc.).
func runCmdSimple(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	return strings.TrimSpace(string(out)), err
}

// commandExists returns true if the named binary is in PATH.
func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// ─── Disk measurement ─────────────────────────────────────────────────────────

// bytesAt returns the disk usage of path in bytes (via du -sk).
// Returns 0 on any error rather than propagating — used for before/after diffs.
func bytesAt(path string) int64 {
	out, err := exec.Command("du", "-sk", path).Output()
	if err != nil {
		return 0
	}
	var kb int64
	fmt.Sscanf(strings.Fields(string(out))[0], "%d", &kb)
	return kb * 1024
}

// fmtBytes formats a byte count as a human-readable string.
func fmtBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// ─── Safe delete ──────────────────────────────────────────────────────────────

// safeDelete removes path, enforcing both allowedPathSuffixes and neverTouchPaths.
// Returns bytes freed (before - after du) and any error.
// HOME-relative paths (starting with ~) are expanded before checking.
func safeDelete(path string) (bytesFreed int64, err error) {
	expanded := expandHome(path)

	// Hard block — check neverTouchPaths first.
	home := os.Getenv("HOME")
	for _, blocked := range neverTouchPaths {
		blockedAbs := expandHome("~/" + strings.TrimPrefix(blocked, "~/"))
		// For absolute system paths keep as-is.
		if !strings.HasPrefix(blocked, "~") && !strings.HasPrefix(blocked, "/Library") {
			blockedAbs = blocked
		} else if strings.HasPrefix(blocked, "/") {
			blockedAbs = blocked
		}
		if strings.HasPrefix(expanded, blockedAbs) || strings.HasPrefix(expanded, filepath.Join(home, strings.TrimPrefix(blocked, "~/"))) {
			return 0, fmt.Errorf("refused: %q is in a protected path (%s)", path, blocked)
		}
	}

	// Allowlist — must match at least one allowed suffix.
	allowed := false
	for _, suffix := range allowedPathSuffixes {
		allowedAbs := filepath.Join(home, "Library", strings.TrimPrefix(suffix, "Library/"))
		if strings.HasPrefix(expanded, allowedAbs) {
			allowed = true
			break
		}
	}
	if !allowed {
		return 0, fmt.Errorf("refused: %q is not in the allowed path list", path)
	}

	// Sanity: never delete HOME itself or root.
	if expanded == home || expanded == "/" || expanded == "" {
		return 0, fmt.Errorf("refused: dangerous target %q", path)
	}

	if _, statErr := os.Stat(expanded); os.IsNotExist(statErr) {
		return 0, nil // nothing to do
	}

	before := bytesAt(expanded)
	if removeErr := os.RemoveAll(expanded); removeErr != nil {
		return 0, removeErr
	}
	return before, nil
}

// safeDeleteChildren deletes the direct children of dir (not dir itself),
// skipping any entry whose base name appears in the exclusions list.
// Returns total bytes freed and the count of deleted entries.
func safeDeleteChildren(dir string, exclusions []string) (bytesFreed int64, deleted int, err error) {
	expanded := expandHome(dir)
	entries, readErr := os.ReadDir(expanded)
	if readErr != nil {
		return 0, 0, readErr
	}

	excSet := make(map[string]bool, len(exclusions))
	for _, e := range exclusions {
		excSet[e] = true
	}

	for _, entry := range entries {
		if excSet[entry.Name()] {
			continue
		}
		childPath := filepath.Join(expanded, entry.Name())
		freed, delErr := safeDelete(childPath)
		if delErr != nil {
			// Log and continue — one failed entry should not abort the rest.
			continue
		}
		bytesFreed += freed
		deleted++
	}
	return bytesFreed, deleted, nil
}

// expandHome replaces a leading ~ with the user's HOME directory.
func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home := os.Getenv("HOME")
		return filepath.Join(home, path[2:])
	}
	return path
}
