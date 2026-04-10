package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

// printAppList renders a numbered table of all discovered apps.
func printAppList(apps []AppInfo) {
	fmt.Println(text.Bold.Sprint("── Non-App-Store Applications"))
	fmt.Println()

	tw := table.NewWriter()
	tw.SetOutputMirror(os.Stdout)
	tw.SetStyle(table.StyleRounded)
	tw.AppendHeader(table.Row{"#", "Name", "Version", "Total Size", "Path"})
	tw.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1, WidthMax: 4},
		{Number: 2, WidthMax: 28},
		{Number: 3, WidthMax: 12},
		{Number: 4, WidthMax: 12, Align: text.AlignRight},
		{Number: 5, WidthMax: 50},
	})

	for i, app := range apps {
		tw.AppendRow(table.Row{
			text.FgHiBlack.Sprint(fmt.Sprintf("%d", i+1)),
			app.Name,
			app.Version,
			fmtBytes(app.TotalBytes),
			text.FgHiBlack.Sprint(app.Path),
		})
	}
	tw.Render()
	fmt.Println()
}

// selectApps shows the app list and prompts the user to choose which ones to
// uninstall. Accepts comma/space-separated numbers (e.g. "1,3,5" or "all").
// Returns the chosen subset; empty slice means none selected.
func selectApps(apps []AppInfo) []AppInfo {
	printAppList(apps)

	fmt.Printf("  Enter app number(s) to uninstall %s\n",
		text.FgHiBlack.Sprint("(e.g. 1,3,5  or  all  or  q to quit)"),
	)
	fmt.Print("  > ")

	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	input := strings.TrimSpace(line)

	if input == "" || input == "q" || input == "quit" {
		return nil
	}

	if strings.ToLower(input) == "all" {
		return apps
	}

	// Parse comma- or space-separated numbers.
	raw := strings.FieldsFunc(input, func(r rune) bool {
		return r == ',' || r == ' '
	})

	seen := make(map[int]bool)
	var selected []AppInfo
	for _, token := range raw {
		n, err := strconv.Atoi(strings.TrimSpace(token))
		if err != nil || n < 1 || n > len(apps) {
			fmt.Printf("  %s  skipping invalid input %q\n",
				text.FgYellow.Sprint("!"), token)
			continue
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		selected = append(selected, apps[n-1])
	}
	return selected
}

// confirmUninstall prints the list of apps to be removed and asks for a y/N
// confirmation. Returns true only on explicit "y" or "yes".
func confirmUninstall(apps []AppInfo) bool {
	fmt.Println()
	fmt.Printf("  %s  The following %s app(s) will be %s:\n\n",
		text.FgYellow.Sprint("!"),
		text.Bold.Sprint(fmt.Sprintf("%d", len(apps))),
		text.FgRed.Sprint("permanently removed"),
	)
	for _, app := range apps {
		fmt.Printf("    %s  %s  %s\n",
			text.FgRed.Sprint("✗"),
			text.Bold.Sprint(app.Name),
			text.FgHiBlack.Sprint("(~"+fmtBytes(app.TotalBytes)+")"),
		)
		fmt.Printf("       %s\n", text.FgHiBlack.Sprint(app.Path))
	}
	fmt.Println()
	fmt.Print("  Confirm uninstall? [y/N]: ")

	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}
