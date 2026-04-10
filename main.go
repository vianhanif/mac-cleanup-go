package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/jedib0t/go-pretty/v6/text"
)

// overviewFlags parses --verbose from os.Args[2:] for the overview command.
func overviewFlags() (verbose bool) {
	for _, arg := range os.Args[2:] {
		switch arg {
		case "--verbose":
			verbose = true
		default:
			fmt.Printf("\n  %s unknown flag %q for 'overview'\n", text.FgRed.Sprint("\u2717"), arg)
			fmt.Printf("  Valid flags: --verbose\n\n")
			os.Exit(1)
		}
	}
	return
}

// appsFlags parses --list, --dry-run, and --sort-size from os.Args[2:] for the
// apps command. Exits with an error message for any unrecognised flag so typos
// never silently skip dry-run mode and trigger a real uninstall.
func appsFlags() (listOnly, dryRun, sortBySize bool) {
	for _, arg := range os.Args[2:] {
		switch arg {
		case "--list":
			listOnly = true
		case "--dry-run":
			dryRun = true
		case "--sort-size":
			sortBySize = true
		default:
			fmt.Printf("\n  %s unknown flag %q for 'apps'\n", text.FgRed.Sprint("✗"), arg)
			fmt.Printf("  Valid flags: --list, --dry-run, --sort-size\n\n")
			os.Exit(1)
		}
	}
	return
}

func main() {
	// Install signal handler so Ctrl+C stops any active progress renderer cleanly.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		stopActive()
		fmt.Println()
		fmt.Println(text.FgYellow.Sprint("  Interrupted."))
		os.Exit(1)
	}()

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(0)
	}

	mode := os.Args[1]

	switch mode {
	case "overview":
		RunOverview(overviewFlags())

	case "safe":
		results := RunSafe()
		PrintCleanupSummary(results, os.Stdout)
		printDone()

	case "deep":
		results := RunDeep()
		PrintCleanupSummary(results, os.Stdout)
		printDone()

	case "brew":
		results := RunBrewUpgrade()
		PrintBrewSummary(results, os.Stdout)
		printDone()

	case "brew-analyze":
		RunBrewAnalyze()
		printDone()

	case "brew-full":
		RunBrewFull()
		printDone()

	case "apps":
		listOnly, dryRun, sortBySize := appsFlags()
		RunApps(listOnly, dryRun, sortBySize)
		if !dryRun {
			printDone()
		}

	case "help", "-h", "--help":
		printUsage()

	default:
		fmt.Printf("  %s unknown command %q\n\n", text.FgRed.Sprint("✗"), mode)
		printUsage()
		os.Exit(1)
	}
}
