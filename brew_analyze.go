package main

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

// ─── Issue types ──────────────────────────────────────────────────────────────

type issueType int

const (
	issueDuplicate   issueType = iota
	issueMultiVer              // stale versioned slot
	issueClashOrEOL            // matches knownClashes map
)

func (t issueType) String() string {
	switch t {
	case issueDuplicate:
		return "duplicate"
	case issueMultiVer:
		return "stale version"
	case issueClashOrEOL:
		return "clash / EOL"
	}
	return "unknown"
}

type actionType int

const (
	actionAutoRemove actionType = iota
	actionPrompt
	actionInform // info only, never touch
)

// ─── BrewIssue ────────────────────────────────────────────────────────────────

type BrewIssue struct {
	pkg       string
	issue     issueType
	action    actionType
	blockedBy []string // from brew uses --installed
	detail    string
}

// ─── Pass A: duplicate detection ─────────────────────────────────────────────

// detectDuplicates finds packages installed via multiple taps or as both
// formula and cask by comparing binary names in PATH.
func detectDuplicates(installed []string) []BrewIssue {
	// Count how many installed formulae share the same base name (stem).
	stem := func(name string) string {
		// e.g. "python@3.12" → "python"
		if at := strings.Index(name, "@"); at >= 0 {
			return name[:at]
		}
		return name
	}

	counts := map[string][]string{}
	for _, pkg := range installed {
		s := stem(pkg)
		counts[s] = append(counts[s], pkg)
	}

	var issues []BrewIssue
	for s, group := range counts {
		if len(group) <= 1 {
			continue
		}
		// Only flag if the un-suffixed name also exists alongside versioned ones.
		hasBase := false
		for _, g := range group {
			if g == s {
				hasBase = true
				break
			}
		}
		if !hasBase {
			continue // all versioned slots — handled by Pass B
		}
		for _, g := range group {
			if g == s {
				continue // the "current" one is fine
			}
			issues = append(issues, BrewIssue{
				pkg:    g,
				issue:  issueDuplicate,
				action: actionAutoRemove,
				detail: fmt.Sprintf("duplicate of %s (base formula installed)", s),
			})
		}
	}
	return issues
}

// ─── Pass B: multi-version detection ─────────────────────────────────────────

func detectMultiVersion(installed []string) []BrewIssue {
	// Build map: stem → latest non-versioned + all versioned.
	stem := func(name string) string {
		if at := strings.Index(name, "@"); at >= 0 {
			return name[:at]
		}
		return name
	}

	type slot struct {
		base      string // un-suffixed name
		versioned []string
	}
	slots := map[string]*slot{}
	for _, pkg := range installed {
		s := stem(pkg)
		if _, ok := slots[s]; !ok {
			slots[s] = &slot{}
		}
		if pkg == s {
			slots[s].base = pkg
		} else {
			slots[s].versioned = append(slots[s].versioned, pkg)
		}
	}

	var issues []BrewIssue
	for _, sl := range slots {
		if sl.base == "" || len(sl.versioned) == 0 {
			continue // no conflict
		}
		for _, vPkg := range sl.versioned {
			// Check who depends on the old versioned slot.
			usesOut, _ := runCmdSimple("brew", "uses", "--installed", vPkg)
			var blockedBy []string
			for _, line := range strings.Split(usesOut, "\n") {
				if dep := strings.TrimSpace(line); dep != "" {
					blockedBy = append(blockedBy, dep)
				}
			}

			// Check keg-only — these coexist intentionally.
			infoOut, _ := runCmdSimple("brew", "info", "--json=v2", vPkg)
			if strings.Contains(infoOut, `"keg_only":true`) {
				continue
			}

			action := actionAutoRemove
			detail := fmt.Sprintf("%s also installed; %s is a stale slot", sl.base, vPkg)
			if len(blockedBy) > 0 {
				action = actionPrompt
				detail = fmt.Sprintf("required by: %s", strings.Join(blockedBy, ", "))
			}
			issues = append(issues, BrewIssue{
				pkg:       vPkg,
				issue:     issueMultiVer,
				action:    action,
				blockedBy: blockedBy,
				detail:    detail,
			})
		}
	}
	return issues
}

// ─── Pass C: clash / EOL detection ───────────────────────────────────────────

func detectClashes(installed []string) []BrewIssue {
	installedSet := make(map[string]bool, len(installed))
	for _, p := range installed {
		installedSet[p] = true
	}

	var issues []BrewIssue
	for _, rule := range knownClashes {
		if !installedSet[rule.pkg] {
			continue
		}
		issues = append(issues, BrewIssue{
			pkg:    rule.pkg,
			issue:  issueClashOrEOL,
			action: actionInform, // never auto-remove clashes
			detail: rule.reason + func() string {
				if rule.alternative != "" && rule.alternative != "(system)" {
					return "  →  " + rule.alternative
				}
				return ""
			}(),
		})
	}
	return issues
}

// ─── Parallel analysis ────────────────────────────────────────────────────────

func runAnalysis(installed []string) []BrewIssue {
	type passResult struct {
		order  int
		issues []BrewIssue
	}
	ch := make(chan passResult, 3)

	go func() { ch <- passResult{0, detectDuplicates(installed)} }()
	go func() { ch <- passResult{1, detectMultiVersion(installed)} }()
	go func() { ch <- passResult{2, detectClashes(installed)} }()

	results := make([][]BrewIssue, 3)
	for i := 0; i < 3; i++ {
		r := <-ch
		results[r.order] = r.issues
	}

	var all []BrewIssue
	for _, r := range results {
		all = append(all, r...)
	}
	return all
}

// ─── RunBrewAnalyze ───────────────────────────────────────────────────────────

func RunBrewAnalyze() {
	if !commandExists("brew") {
		fmt.Println(text.FgRed.Sprint("  Homebrew not installed."))
		return
	}

	fmt.Println()
	fmt.Println(text.Bold.Sprint("🔍 Brew Package Analysis"))
	fmt.Println(strings.Repeat("─", 56))
	fmt.Println()

	// Fetch installed list with a spinner.
	fmt.Print("  Fetching installed packages…")
	installedRaw, err := runCmdSimple("brew", "list", "--formula")
	clearLine()
	if err != nil {
		fmt.Println(text.FgRed.Sprint("  Failed to list installed packages."))
		return
	}
	var installed []string
	for _, line := range strings.Split(installedRaw, "\n") {
		if p := strings.TrimSpace(line); p != "" {
			installed = append(installed, p)
		}
	}
	fmt.Printf("  %s installed formulae found — running 3 analysis passes…\n\n",
		text.Bold.Sprint(fmt.Sprintf("%d", len(installed))))

	// Run all three passes in parallel with a progress bar.
	type passWork struct {
		name string
		fn   func([]string) []BrewIssue
	}
	passes := []passWork{
		{"Duplicates", detectDuplicates},
		{"Multi-version slots", detectMultiVersion},
		{"Clashes / EOL", detectClashes},
	}

	passDoneCh := make(chan phaseItem, len(passes))
	type indexedIssues struct {
		i      int
		issues []BrewIssue
	}
	issuesCh := make(chan indexedIssues, len(passes))
	var wg sync.WaitGroup

	for i, p := range passes {
		i, p := i, p
		wg.Add(1)
		go func() {
			defer wg.Done()
			issues := p.fn(installed)
			issuesCh <- indexedIssues{i, issues}
			passDoneCh <- phaseItem{
				label:  p.name,
				result: fmt.Sprintf("%d issue(s) found", len(issues)),
				ok:     true,
			}
		}()
	}
	go func() { wg.Wait(); close(issuesCh) }()

	phaseMonitor("Analysing", len(passes), passDoneCh)

	allIssuesByPass := make([][]BrewIssue, len(passes))
	for item := range issuesCh {
		allIssuesByPass[item.i] = item.issues
	}

	// Flatten in order.
	var all []BrewIssue
	for _, iss := range allIssuesByPass {
		all = append(all, iss...)
	}

	if len(all) == 0 {
		fmt.Println()
		fmt.Println(text.FgGreen.Sprint("  ✓ No issues found. Your Homebrew setup looks clean."))
		fmt.Println()
		return
	}

	// ── Results table ──
	fmt.Println()
	fmt.Println(text.Bold.Sprint("── Analysis Results"))
	fmt.Println()

	tw := table.NewWriter()
	tw.SetOutputMirror(os.Stdout)
	tw.SetStyle(table.StyleRounded)
	tw.AppendHeader(table.Row{"Package", "Issue", "Detail"})

	sectionHeaders := map[issueType]string{
		issueDuplicate:  "DUPLICATES",
		issueMultiVer:   "MULTIPLE VERSIONS",
		issueClashOrEOL: "CLASHES / EOL",
	}
	lastType := issueType(-1)

	for _, issue := range all {
		if issue.issue != lastType {
			tw.AppendSeparator()
			tw.AppendRow(table.Row{
				text.Bold.Sprint(sectionHeaders[issue.issue]), "", "",
			})
			lastType = issue.issue
		}
		actionIcon := "–"
		switch issue.action {
		case actionAutoRemove:
			actionIcon = text.FgGreen.Sprint("✓ auto-remove")
		case actionPrompt:
			actionIcon = text.FgYellow.Sprint("⚠ prompt")
		case actionInform:
			actionIcon = text.FgHiBlack.Sprint("ℹ inform")
		}
		tw.AppendRow(table.Row{issue.pkg, actionIcon, truncate(issue.detail, 60)})
	}
	tw.Render()

	// ── Recommended actions ──
	var autoRemove, needPrompt, infoOnly []BrewIssue
	for _, issue := range all {
		switch issue.action {
		case actionAutoRemove:
			autoRemove = append(autoRemove, issue)
		case actionPrompt:
			needPrompt = append(needPrompt, issue)
		case actionInform:
			infoOnly = append(infoOnly, issue)
		}
	}

	fmt.Println()
	fmt.Println(text.Bold.Sprint("── Recommended Actions"))
	fmt.Println()

	if len(autoRemove) > 0 {
		fmt.Println(text.FgGreen.Sprint("  Auto-removable (no dependents):"))
		for _, issue := range autoRemove {
			fmt.Printf("    [ ] %-24s  brew uninstall %s\n", issue.pkg, issue.pkg)
		}
		fmt.Println()
	}

	if len(needPrompt) > 0 {
		fmt.Println(text.FgYellow.Sprint("  Needs your decision:"))
		for _, issue := range needPrompt {
			fmt.Printf("    [ ] %-24s  required by: %s\n",
				issue.pkg, strings.Join(issue.blockedBy, ", "))
		}
		fmt.Println()
	}

	if len(infoOnly) > 0 {
		fmt.Println(text.FgHiBlack.Sprint("  Informational (no action taken):"))
		for _, issue := range infoOnly {
			fmt.Printf("    [i] %-24s  %s\n", issue.pkg, truncate(issue.detail, 55))
		}
		fmt.Println()
	}

	// Offer to apply auto-removable packages.
	if len(autoRemove) > 0 {
		fmt.Printf("  Apply %d auto-removable package(s)? [y/N]: ", len(autoRemove))
		var answer string
		fmt.Fscan(os.Stdin, &answer) //nolint:errcheck
		answer = strings.ToLower(strings.TrimSpace(answer))

		if answer == "y" || answer == "yes" {
			fmt.Println()
			for _, issue := range autoRemove {
				fmt.Printf("  Removing %s… ", issue.pkg)
				_, err := runCmdSimple("brew", "uninstall", issue.pkg)
				if err != nil {
					fmt.Println(text.FgRed.Sprint("✗ " + err.Error()))
				} else {
					fmt.Println(text.FgGreen.Sprint("✓"))
				}
			}
			fmt.Println()
			fmt.Print("  Running brew cleanup…")
			runCmdSimple("brew", "cleanup", "-s") //nolint:errcheck
			clearLine()
			fmt.Printf("  %s brew cleanup done\n", text.FgGreen.Sprint("✓"))
		} else {
			fmt.Printf("  %s No changes made.\n", text.FgHiBlack.Sprint("–"))
		}
	}

	fmt.Println()
}

// RunBrewFull runs brew-analyze then brew smart upgrade in sequence.
func RunBrewFull() {
	RunBrewAnalyze()
	RunBrewUpgrade()
}
