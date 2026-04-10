package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

// ─── PrintCleanupSummary ──────────────────────────────────────────────────────

// PrintCleanupSummary renders a rounded table for safe/deep cleanup results.
func PrintCleanupSummary(results []TaskResult, out io.Writer) {
	if len(results) == 0 {
		return
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, text.Bold.Sprint("── Summary"))
	fmt.Fprintln(out)

	tw := table.NewWriter()
	tw.SetOutputMirror(out)
	tw.SetStyle(table.StyleRounded)
	tw.AppendHeader(table.Row{"Task", "Status", "Result", "Time"})

	var totalFreed int64
	ok, skipped, errored := 0, 0, 0

	for _, r := range results {
		statusIcon := ""
		switch r.Status {
		case "ok":
			statusIcon = text.FgGreen.Sprint("✓")
			ok++
		case "skipped":
			statusIcon = text.FgHiBlack.Sprint("–")
			skipped++
		default:
			statusIcon = text.FgRed.Sprint("✗")
			errored++
		}
		totalFreed += r.BytesFreed

		durationStr := ""
		if r.Duration > 0 {
			durationStr = r.Duration.Round(time.Millisecond).String()
		}

		tw.AppendRow(table.Row{r.Name, statusIcon, r.Detail, durationStr})
	}

	tw.AppendSeparator()
	statusSummary := fmt.Sprintf("%d/%d ok", ok, len(results))
	if errored > 0 {
		statusSummary += fmt.Sprintf("  %d error(s)", errored)
	}
	tw.AppendRow(table.Row{
		text.Bold.Sprint("Total"),
		statusSummary,
		fmtBytes(totalFreed) + " freed",
		"",
	})
	tw.Render()
	fmt.Fprintln(out)
}

// ─── PrintBrewSummary ─────────────────────────────────────────────────────────

// PrintBrewSummary renders a rounded table for brew upgrade results.
func PrintBrewSummary(results []BrewUpgradeResult, out io.Writer) {
	if len(results) == 0 {
		return
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, text.Bold.Sprint("── Summary"))
	fmt.Fprintln(out)

	tw := table.NewWriter()
	tw.SetOutputMirror(out)
	tw.SetStyle(table.StyleRounded)
	tw.AppendHeader(table.Row{"Package", "Status", "Detail", "Time"})

	ok, skipped, errored := 0, 0, 0
	var totalDuration time.Duration

	for _, r := range results {
		statusIcon := ""
		switch r.Status {
		case "ok":
			statusIcon = text.FgGreen.Sprint("✓")
			ok++
		case "skipped":
			statusIcon = text.FgHiBlack.Sprint("–")
			skipped++
		default:
			statusIcon = text.FgRed.Sprint("✗")
			errored++
		}
		totalDuration += r.Duration

		durationStr := ""
		if r.Duration > 0 {
			durationStr = r.Duration.Round(time.Millisecond).String()
		}

		tw.AppendRow(table.Row{r.Name, statusIcon, truncate(r.Detail, 40), durationStr})
	}

	tw.AppendSeparator()
	statusSummary := fmt.Sprintf("%d upgraded", ok)
	if skipped > 0 {
		statusSummary += fmt.Sprintf("  %d skipped", skipped)
	}
	if errored > 0 {
		statusSummary += fmt.Sprintf("  %d error(s)", errored)
	}
	durationStr := ""
	if totalDuration > 0 {
		durationStr = totalDuration.Round(time.Second).String()
	}
	tw.AppendRow(table.Row{
		text.Bold.Sprint("Total"),
		statusSummary,
		"",
		durationStr,
	})
	tw.Render()
	fmt.Fprintln(out)
}

// ─── printDone ────────────────────────────────────────────────────────────────

func printDone() {
	fmt.Println(text.FgGreen.Sprint("✓ Done."))
}

// ─── printUsage ───────────────────────────────────────────────────────────────

// printUsage prints a detailed, formatted help screen when no argument is given.
func printUsage() {
	fmt.Println()
	fmt.Println(text.Bold.Sprint("mac-cleanup — macOS maintenance tool"))
	fmt.Println()

	type cmd struct {
		name    string
		purpose string
		detail  string
	}

	commands := []cmd{
		{
			"overview",
			"System dashboard",
			"Shows CPU, memory, disk usage, top home directories, Homebrew\n" +
				"                     outdated count, and Docker status — all collected in\n" +
				"                     parallel and rendered as formatted tables.",
		},
		{
			"safe",
			"Safe cleanup",
			"Removes user caches (browsers excluded), user logs, Xcode\n" +
				"                     DerivedData, and thins local Time Machine snapshots.\n" +
				"                     Nothing outside ~/Library is touched.",
		},
		{
			"deep",
			"Deep cleanup",
			"Everything in safe mode, plus: Xcode Archives, Docker system\n" +
				"                     prune (if Docker is running), and a node_modules size\n" +
				"                     report (reported only — never auto-deleted).",
		},
		{
			"brew",
			"Smart Homebrew upgrade",
			"Analyses each outdated formula for risk (dependents, major\n" +
				"                     version bumps, keg-only). Low-risk packages upgrade\n" +
				"                     automatically; high-risk ones pause for your approval.\n" +
				"                     Critical runtimes (node, go, rust, openssl…) are\n" +
				"                     always skipped.",
		},
		{
			"brew-analyze",
			"Homebrew package audit",
			"Runs three parallel analysis passes:\n" +
				"                       1. Duplicates — same binary from multiple taps\n" +
				"                       2. Multi-version — stale pkg@N slots next to newer\n" +
				"                       3. Clashes/EOL — known bad/outdated packages\n" +
				"                     Offers to auto-remove safe candidates; prompts for\n" +
				"                     blocked ones; shows clashes as info only.",
		},
		{
			"brew-full",
			"Full Homebrew maintenance",
			"Runs brew-analyze first, then brew smart upgrade in one pass.",
		},
		{
			"apps",
			"App uninstaller",
			"Lists all non-App-Store applications (/Applications,\n" +
				"                     /Applications/Utilities, ~/Applications),\n" +
				"                     lets you select one or more to remove, then\n" +
				"                     cleanly uninstalls each one — deleting the\n" +
				"                     .app bundle plus all associated caches,\n" +
				"                     preferences, containers, and support files.\n" +
				"                     Flags: --list (list only),\n" +
				"                     --dry-run (preview without deleting),\n" +
				"                     --sort-size (largest apps first).",
		},
		{
			"help",
			"Show this help",
			"",
		},
	}

	tw := table.NewWriter()
	tw.SetOutputMirror(os.Stdout)
	tw.SetStyle(table.StyleRounded)
	tw.AppendHeader(table.Row{"Command", "Purpose", "What it does"})
	tw.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1, WidthMax: 14},
		{Number: 2, WidthMax: 24},
		{Number: 3, WidthMax: 55},
	})

	for _, c := range commands {
		tw.AppendRow(table.Row{
			text.FgCyan.Sprint(c.name),
			text.Bold.Sprint(c.purpose),
			c.detail,
		})
		tw.AppendSeparator()
	}
	tw.Render()

	fmt.Println()
	fmt.Println(text.FgHiBlack.Sprint("  Safety guarantees (always enforced):"))
	fmt.Println(text.FgHiBlack.Sprint("  · browser login sessions are never touched (~/Library/Application Support/)"))
	fmt.Println(text.FgHiBlack.Sprint("  · LaunchAgents, Fonts, StartupItems are never touched (protects boot speed)"))
	fmt.Println(text.FgHiBlack.Sprint("  · system paths (/System, /usr, /bin…) are hard-blocked"))
	fmt.Println(text.FgHiBlack.Sprint("  · node_modules are reported but never auto-deleted"))
	fmt.Println(text.FgHiBlack.Sprint("  · critical brew packages (runtimes, crypto) are never auto-upgraded"))
	fmt.Println()
}
