package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jedib0t/go-pretty/v6/text"
)

// runWithSpinner runs fn in a goroutine while animating a braille spinner on
// the current line. Returns when fn completes.
func runWithSpinner(msg string, fn func()) {
	done := make(chan struct{})
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		frame := 0
		for {
			select {
			case <-ticker.C:
				printSpinner(braille[frame%len(braille)], msg)
				frame++
			case <-done:
				clearLine()
				return
			}
		}
	}()
	fn()
	close(done)
	<-exited
}

type brewRisk int

const (
	riskSafe   brewRisk = 0
	riskLow    brewRisk = 1
	riskMedium brewRisk = 2
	riskSkip   brewRisk = 3
)

type brewDecision int

const (
	decisionAuto brewDecision = iota
	decisionPrompt
	decisionSkip
)

type brewPkg struct {
	name        string
	currentVer  string
	newVer      string
	dependents  []string
	kegOnly     bool
	pinned      bool
	risk        brewRisk
	decision    brewDecision
	decisionMsg string
}

func isMajorBump(from, to string) bool {
	majorOf := func(v string) int {
		v = strings.TrimPrefix(v, "v")
		parts := strings.SplitN(v, ".", 2)
		if len(parts) == 0 {
			return -1
		}
		n, err := strconv.Atoi(parts[0])
		if err != nil {
			return -1
		}
		return n
	}
	mFrom, mTo := majorOf(from), majorOf(to)
	return mFrom >= 0 && mTo >= 0 && mTo > mFrom
}

func analyseBrewPackage(name, currentVer, newVer string) brewPkg {
	pkg := brewPkg{name: name, currentVer: currentVer, newVer: newVer}

	pinnedOut, _ := runCmdSimple("brew", "list", "--pinned")
	for _, p := range strings.Split(pinnedOut, "\n") {
		if strings.TrimSpace(p) == name {
			pkg.pinned = true
			pkg.risk = riskSkip
			pkg.decision = decisionSkip
			pkg.decisionMsg = "pinned - skipping"
			return pkg
		}
	}

	for _, c := range criticalPackages {
		if c == name {
			pkg.risk = riskSkip
			pkg.decision = decisionSkip
			pkg.decisionMsg = "critical runtime - skipping"
			return pkg
		}
	}

	infoOut, _ := runCmdSimple("brew", "info", "--json=v2", name)
	if strings.Contains(infoOut, `"keg_only":true`) {
		pkg.kegOnly = true
	}

	usesOut, _ := runCmdSimple("brew", "uses", "--installed", name)
	for _, line := range strings.Split(usesOut, "\n") {
		if dep := strings.TrimSpace(line); dep != "" {
			pkg.dependents = append(pkg.dependents, dep)
		}
	}

	var score brewRisk
	switch {
	case len(pkg.dependents) >= 4:
		score = riskMedium
	case len(pkg.dependents) >= 1:
		score = riskLow
	default:
		score = riskSafe
	}
	if isMajorBump(currentVer, newVer) {
		score++
	}
	if pkg.kegOnly && score > riskSafe {
		score--
	}
	if score > riskMedium {
		score = riskMedium
	}
	pkg.risk = score

	switch {
	case score <= riskLow:
		pkg.decision = decisionAuto
		if len(pkg.dependents) == 0 {
			pkg.decisionMsg = "leaf -> auto-upgrade"
		} else {
			pkg.decisionMsg = fmt.Sprintf("%d dependent(s) -> auto-upgrade", len(pkg.dependents))
		}
	case score == riskMedium:
		pkg.decision = decisionPrompt
		pkg.decisionMsg = fmt.Sprintf("%d dependents - will prompt", len(pkg.dependents))
	default:
		pkg.decision = decisionSkip
		pkg.decisionMsg = "skipping"
	}
	return pkg
}

type outdatedEntry struct {
	name       string
	currentVer string
	newVer     string
}

func fetchOutdated() ([]outdatedEntry, error) {
	out, err := runCmdSimple("brew", "outdated", "--formula", "--verbose")
	if err != nil && out == "" {
		return nil, err
	}
	var pkgs []outdatedEntry
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		e := outdatedEntry{name: parts[0]}
		if len(parts) >= 4 {
			e.currentVer = strings.Trim(parts[1], "()")
			e.newVer = parts[len(parts)-1]
		}
		pkgs = append(pkgs, e)
	}
	return pkgs, nil
}

type upgradeResult struct {
	pkg      brewPkg
	newVer   string
	status   string
	detail   string
	duration time.Duration
}

// extractErrLine picks the first "Error:" line from brew's stderr output.
// Brew often prefixes errors with bottle-download progress lines; this skips
// them so only the actionable message ends up in the progress tracker.
func extractErrLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); strings.HasPrefix(line, "Error:") {
			return truncate(line, truncErrMsgLen)
		}
	}
	// fallback: first non-empty line
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return truncate(line, truncErrMsgLen)
		}
	}
	return truncate(strings.TrimSpace(s), truncErrMsgLen)
}

func upgradePackage(pkg brewPkg) upgradeResult {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	res := runCmd(ctx, "brew", "upgrade", pkg.name)
	if res.err != nil {
		errMsg := extractErrLine(res.stderr)
		// brew upgrade can succeed at install but fail at the link step when
		// conflicting symlinks already exist (e.g. from a previous partial
		// upgrade). Attempt "brew link --overwrite" as automatic recovery.
		if strings.Contains(res.stderr, "brew link") || strings.Contains(res.stderr, "link step") {
			linkCtx, linkCancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer linkCancel()
			linkRes := runCmd(linkCtx, "brew", "link", "--overwrite", pkg.name)
			if linkRes.err == nil {
				return upgradeResult{pkg, pkg.newVer, "ok", pkg.newVer, time.Since(start)}
			}
			errMsg = "link: " + extractErrLine(linkRes.stderr)
		}
		return upgradeResult{pkg, "", "error", errMsg, time.Since(start)}
	}
	newVer := pkg.newVer
	for _, line := range strings.Split(res.stdout, "\n") {
		if strings.Contains(line, pkg.name) && strings.Contains(line, "->") {
			parts := strings.Split(line, "->")
			if len(parts) == 2 {
				newVer = strings.TrimSpace(parts[1])
			}
			break
		}
	}
	return upgradeResult{pkg, newVer, "ok", newVer, time.Since(start)}
}

type BrewUpgradeResult struct {
	Name     string
	Status   string
	Detail   string
	Duration time.Duration
}

func RunBrewUpgrade() []BrewUpgradeResult {
	if !commandExists("brew") {
		fmt.Println(text.FgRed.Sprint("  Homebrew not installed."))
		return nil
	}

	fmt.Println()
	fmt.Println(text.Bold.Sprint("Brew Smart Upgrade"))
	fmt.Println(strings.Repeat("-", 56))
	fmt.Println()

	fmt.Print("  Fetching outdated packages...")
	outdated, err := fetchOutdated()
	clearLine()
	if err != nil {
		fmt.Println(text.FgRed.Sprint("  Failed to fetch outdated packages: " + err.Error()))
		return nil
	}
	if len(outdated) == 0 {
		fmt.Println(text.FgGreen.Sprint("  All packages are up to date."))
		return nil
	}
	fmt.Printf("  %s outdated packages found\n\n", text.Bold.Sprint(strconv.Itoa(len(outdated))))

	analysed := make([]brewPkg, len(outdated))
	{
		type indexed struct {
			i   int
			pkg brewPkg
		}
		anCh := make(chan indexed, len(outdated))
		doneCh := make(chan phaseItem, len(outdated))
		var wg sync.WaitGroup

		for i, op := range outdated {
			i, op := i, op
			wg.Add(1)
			go func() {
				defer wg.Done()
				pkg := analyseBrewPackage(op.name, op.currentVer, op.newVer)
				anCh <- indexed{i, pkg}
				label := fmt.Sprintf("%-20s  %s -> %s",
					truncate(op.name, 20), op.currentVer, op.newVer)
				doneCh <- phaseItem{label: label, result: pkg.decisionMsg,
					ok: pkg.decision != decisionSkip}
			}()
		}
		go func() { wg.Wait(); close(anCh) }()

		phaseMonitor("Analysing packages", len(outdated), doneCh)

		for item := range anCh {
			analysed[item.i] = item.pkg
		}
	}

	var allResults []BrewUpgradeResult
	for _, pkg := range analysed {
		if pkg.decision == decisionSkip {
			allResults = append(allResults, BrewUpgradeResult{
				Name: pkg.name, Status: "skipped", Detail: pkg.decisionMsg,
			})
		}
	}

	var autoList, promptList []brewPkg
	for _, pkg := range analysed {
		switch pkg.decision {
		case decisionAuto:
			autoList = append(autoList, pkg)
		case decisionPrompt:
			promptList = append(promptList, pkg)
		}
	}

	if len(autoList) > 0 {
		fmt.Println()
		upgDoneCh := make(chan phaseItem, len(autoList))

		// Brew holds a file lock for the duration of each upgrade, so upgrades
		// must run sequentially. We dispatch them in a single goroutine and post
		// each result to upgDoneCh as it finishes so phaseMonitor stays live.
		go func() {
			for _, pkg := range autoList {
				r := upgradePackage(pkg)
				allResults = append(allResults, BrewUpgradeResult{
					Name:     r.pkg.name,
					Status:   r.status,
					Detail:   r.detail,
					Duration: r.duration,
				})
				ok := r.status == "ok"
				detail := r.detail
				if ok {
					detail = fmt.Sprintf("-> %s  (%s)", r.newVer, r.duration.Round(time.Millisecond))
				}
				upgDoneCh <- phaseItem{
					label:  truncate(pkg.name, progressLabelWidth),
					result: detail,
					ok:     ok,
				}
			}
		}()

		phaseMonitor("Upgrading", len(autoList), upgDoneCh)
	}

	for _, pkg := range promptList {
		fmt.Println()
		fmt.Printf("  %s  %s has %s:\n",
			text.FgYellow.Sprint("!"),
			text.Bold.Sprint(pkg.name),
			text.Bold.Sprint(fmt.Sprintf("%d dependent(s)", len(pkg.dependents))))
		fmt.Printf("    Dependents: %s\n", strings.Join(pkg.dependents, ", "))
		fmt.Printf("    Version:    %s -> %s", pkg.currentVer, pkg.newVer)
		if isMajorBump(pkg.currentVer, pkg.newVer) {
			fmt.Printf("  %s", text.FgYellow.Sprint("(major bump)"))
		}
		fmt.Println()
		fmt.Print("  Upgrade? [y/N]: ")

		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		answer := strings.ToLower(strings.TrimSpace(line))

		if answer == "y" || answer == "yes" {
			var r upgradeResult
			runWithSpinner(fmt.Sprintf("Upgrading %s...", pkg.name), func() {
				r = upgradePackage(pkg)
			})
			if r.status == "ok" {
				fmt.Printf("  %s %s -> %s  (%s)\n\n",
					text.FgGreen.Sprint("✓"), pkg.name, r.newVer, r.duration.Round(time.Millisecond))
			} else {
				fmt.Printf("  %s %s: %s\n\n", text.FgRed.Sprint("✗"), pkg.name, r.detail)
			}
			allResults = append(allResults, BrewUpgradeResult{
				Name:     pkg.name,
				Status:   r.status,
				Detail:   r.detail,
				Duration: r.duration,
			})
		} else {
			fmt.Printf("  %s Skipped %s\n\n", text.FgHiBlack.Sprint("-"), pkg.name)
			allResults = append(allResults, BrewUpgradeResult{
				Name: pkg.name, Status: "skipped", Detail: "user declined",
			})
		}
	}

	// ── Retry failed packages (once) ──────────────────────────────────────
	var failedPkgs []brewPkg
	for _, r := range allResults {
		if r.Status == "error" {
			failedPkgs = append(failedPkgs, brewPkg{name: r.Name})
		}
	}
	if len(failedPkgs) > 0 {
		fmt.Println()
		fmt.Printf("  %s %d package(s) failed. Retry once? [y/N]: ",
			text.FgYellow.Sprint("!"), len(failedPkgs))
		retryReader := bufio.NewReader(os.Stdin)
		retryLine, _ := retryReader.ReadString('\n')
		if answer := strings.ToLower(strings.TrimSpace(retryLine)); answer == "y" || answer == "yes" {
			retryDoneCh := make(chan phaseItem, len(failedPkgs))

			// Sequential for the same reason as the first pass — Brew file lock.
			go func() {
				for _, pkg := range failedPkgs {
					r := upgradePackage(pkg)
					for i, existing := range allResults {
						if existing.Name == r.pkg.name {
							allResults[i] = BrewUpgradeResult{
								Name:     r.pkg.name,
								Status:   r.status,
								Detail:   r.detail,
								Duration: existing.Duration + r.duration,
							}
							break
						}
					}
					ok := r.status == "ok"
					detail := r.detail
					if ok {
						detail = fmt.Sprintf("-> %s  (%s)", r.newVer, r.duration.Round(time.Millisecond))
					}
					retryDoneCh <- phaseItem{
						label:  truncate(pkg.name, progressLabelWidth),
						result: detail,
						ok:     ok,
					}
				}
			}()

			phaseMonitor("Retrying failed", len(failedPkgs), retryDoneCh)
		}
	}

	runWithSpinner("Running brew cleanup...", func() {
		runCmdSimple("brew", "cleanup", "-s") //nolint:errcheck
	})
	fmt.Printf("  %s brew cleanup done\n", text.FgGreen.Sprint("✓"))

	return allResults
}
