package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

// ─── Metric types ─────────────────────────────────────────────────────────────

type metricResult struct {
	label string
	value string
	bar   string // pre-rendered progress bar, empty if not applicable
	ok    bool
}

// renderBar renders a fixed-width ASCII progress bar (10 segments).
func renderBar(percent int) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	filled := percent / 10
	empty := 10 - filled
	return strings.Repeat("█", filled) + strings.Repeat("░", empty)
}

// ─── Metric collectors ────────────────────────────────────────────────────────

func collectCPU() metricResult {
	out, err := runCmdSimple("ps", "-A", "-o", "%cpu")
	if err != nil {
		return metricResult{"CPU", "unavailable", "", false}
	}
	var total float64
	for _, line := range strings.Split(out, "\n")[1:] { // skip header
		line = strings.TrimSpace(line)
		if v, e := strconv.ParseFloat(line, 64); e == nil {
			total += v
		}
	}
	// Cap at 100 for display (can exceed with multiple cores).
	pct := int(total)
	if pct > 100 {
		pct = 100
	}
	return metricResult{"CPU", fmt.Sprintf("%d%%", pct), renderBar(pct), true}
}

func collectMemory() metricResult {
	out, err := runCmdSimple("memory_pressure")
	if err != nil {
		return metricResult{"Memory", "unavailable", "", false}
	}
	var freePct int
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "System-wide memory free percentage") {
			parts := strings.Fields(line)
			for _, p := range parts {
				p = strings.TrimSuffix(p, "%")
				if v, e := strconv.Atoi(p); e == nil {
					freePct = v
					break
				}
			}
		}
	}
	usedPct := 100 - freePct
	return metricResult{"Memory", fmt.Sprintf("%d%%", usedPct), renderBar(usedPct), true}
}

func collectDisk() metricResult {
	out, err := runCmdSimple("df", "-H", "/")
	if err != nil {
		return metricResult{"Disk /", "unavailable", "", false}
	}
	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		return metricResult{"Disk /", "unavailable", "", false}
	}
	fields := strings.Fields(lines[1])
	if len(fields) < 5 {
		return metricResult{"Disk /", "unavailable", "", false}
	}
	size := fields[1]
	used := fields[2]
	pctStr := strings.TrimSuffix(fields[4], "%")
	pct, _ := strconv.Atoi(pctStr)
	value := fmt.Sprintf("%s used of %s", used, size)
	return metricResult{"Disk /", value, renderBar(pct), true}
}

func collectHostInfo() (hostname, macos, uptime string) {
	hostname, _ = os.Hostname()

	macos, _ = runCmdSimple("sw_vers", "-productVersion")

	uptimeRaw, _ := runCmdSimple("uptime")
	// Extract the "up X days/hours/min" portion — simplified parse.
	if idx := strings.Index(uptimeRaw, " up "); idx >= 0 {
		rest := uptimeRaw[idx+4:]
		if comma := strings.Index(rest, ","); comma >= 0 {
			uptime = strings.TrimSpace(rest[:comma])
		} else {
			uptime = strings.TrimSpace(rest)
		}
	}
	return
}

func collectBrew() metricResult {
	if !commandExists("brew") {
		return metricResult{"Brew Outdated", "not installed", "", false}
	}
	out, err := runCmdSimple("brew", "outdated", "--formula")
	if err != nil && out == "" {
		return metricResult{"Brew Outdated", "error checking", "", false}
	}
	count := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	if count == 0 {
		return metricResult{"Brew Outdated", "all up to date ✓", "", true}
	}
	return metricResult{"Brew Outdated", fmt.Sprintf("%d packages", count), "", true}
}

func collectDocker() metricResult {
	if !commandExists("docker") {
		return metricResult{"Docker", "not installed", "", false}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5e9) // 5s
	defer cancel()
	res := runCmd(ctx, "docker", "info")
	if res.err != nil {
		return metricResult{"Docker", "not running", "", false}
	}
	dfOut, dfErr := runCmdSimple("docker", "system", "df", "--format",
		"{{.Type}}: {{.Size}} ({{.Reclaimable}} reclaimable)")
	if dfErr != nil {
		return metricResult{"Docker", "running", "", true}
	}
	// Show first reclaimable total.
	lines := strings.Split(dfOut, "\n")
	summary := fmt.Sprintf("running · %d item(s)", len(lines))
	for _, l := range lines {
		if strings.Contains(l, "reclaimable") {
			summary = strings.TrimSpace(l)
			break
		}
	}
	return metricResult{"Docker", summary, "", true}
}

// ─── Top home directories ────────────────────────────────────────────────────

type dirEntry struct {
	path string
	size string
}

func collectTopDirs(n int) []dirEntry {
	home := os.Getenv("HOME")
	entries, err := os.ReadDir(home)
	if err != nil {
		return nil
	}
	type sized struct {
		path string
		kb   int64
	}
	var items []sized
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue // skip hidden dirs like .config, .ssh, etc.
		}
		fullPath := filepath.Join(home, e.Name())
		// du -sk returns size in 512-byte blocks on macOS when -k, so kb.
		out, err := exec.Command("du", "-sk", fullPath).Output()
		if err != nil {
			continue
		}
		fields := strings.Fields(string(out))
		if len(fields) < 1 {
			continue
		}
		kb, _ := strconv.ParseInt(fields[0], 10, 64)
		items = append(items, sized{path: "~/" + e.Name(), kb: kb})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].kb > items[j].kb })
	if len(items) > n {
		items = items[:n]
	}
	var result []dirEntry
	for _, item := range items {
		result = append(result, dirEntry{
			path: item.path,
			size: fmtBytes(item.kb * 1024),
		})
	}
	return result
}

// ─── Overview mode ────────────────────────────────────────────────────────────

// RunOverview collects and displays the system dashboard.
func RunOverview() {
	fmt.Println()
	fmt.Println(text.Bold.Sprint("🖥  System Dashboard"))
	fmt.Println(strings.Repeat("─", 56))

	// Fan out all metric collectors in parallel.
	type namedMetric struct {
		order int
		m     metricResult
	}
	ch := make(chan namedMetric, 6)
	var wg sync.WaitGroup

	collect := func(order int, fn func() metricResult) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch <- namedMetric{order, fn()}
		}()
	}

	collect(0, collectCPU)
	collect(1, collectMemory)
	collect(2, collectDisk)
	collect(3, collectBrew)
	collect(4, collectDocker)

	// Spinner while the parallel collectors run — avoids appearing frozen.
	spinDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		frame := 0
		for {
			select {
			case <-ticker.C:
				printSpinner(braille[frame%len(braille)], "Collecting system info...")
				frame++
			case <-spinDone:
				clearLine()
				return
			}
		}
	}()

	wg.Wait()
	close(spinDone)
	close(ch)

	metrics := make([]metricResult, 5)
	for nm := range ch {
		metrics[nm.order] = nm.m
	}

	hostname, macos, uptime := collectHostInfo()

	// ── System table ──
	fmt.Println()
	tw := table.NewWriter()
	tw.SetOutputMirror(os.Stdout)
	tw.SetStyle(table.StyleRounded)
	tw.Style().Options.SeparateRows = false

	for _, m := range metrics {
		if m.bar != "" {
			valueCol := fmt.Sprintf("%s  %s", m.bar, m.value)
			tw.AppendRow(table.Row{m.label, valueCol})
		} else {
			statusColor := text.FgGreen
			if !m.ok {
				statusColor = text.FgRed
			}
			tw.AppendRow(table.Row{m.label, statusColor.Sprint(m.value)})
		}
	}

	tw.AppendSeparator()
	tw.AppendRow(table.Row{"Hostname", hostname})
	tw.AppendRow(table.Row{"macOS Version", macos})
	tw.AppendRow(table.Row{"System Uptime", uptime})
	tw.Render()

	// ── Top directories table ──
	fmt.Println()
	fmt.Println(text.Bold.Sprint("── Top Home Directories"))
	fmt.Println()

	dirsCh := make(chan []dirEntry, 1)
	go func() { dirsCh <- collectTopDirs(8) }()
	spinDone2 := make(chan struct{})
	spinExited2 := make(chan struct{})
	go func() {
		defer close(spinExited2)
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		frame := 0
		for {
			select {
			case <-ticker.C:
				printSpinner(braille[frame%len(braille)], "Computing directory sizes...")
				frame++
			case <-spinDone2:
				clearLine()
				return
			}
		}
	}()
	dirs := <-dirsCh
	close(spinDone2)
	<-spinExited2 // wait for clearLine() to run before printing

	if len(dirs) > 0 {
		td := table.NewWriter()
		td.SetOutputMirror(os.Stdout)
		td.SetStyle(table.StyleRounded)
		td.AppendHeader(table.Row{"Path", "Size"})
		for _, d := range dirs {
			td.AppendRow(table.Row{d.path, d.size})
		}
		td.Render()
	} else {
		fmt.Println("  (could not read home directory sizes)")
	}
	fmt.Println()
}
