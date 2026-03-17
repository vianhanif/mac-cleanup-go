package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/jedib0t/go-pretty/v6/progress"
	"github.com/jedib0t/go-pretty/v6/text"
)

var braille = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// clearLine erases the current terminal line and resets cursor to column 0.
func clearLine() { fmt.Print("\r\033[K") }

// printSpinner overwrites the current line with a braille spinner (no newline).
func printSpinner(spin, msg string) {
	fmt.Printf("\r\033[K  %s  %s", text.FgYellow.Sprint(spin), msg)
}

// ─── phase item ──────────────────────────────────────────────────────────────

// phaseItem carries the result of one parallel task once it finishes.
type phaseItem struct {
	label  string
	result string
	ok     bool
}

// ─── progress writer factory ─────────────────────────────────────────────────

// newPW returns a configured progress.Writer — identical style to kubectl-multi-logs.
func newPW() progress.Writer {
	pw := progress.NewWriter()
	pw.SetAutoStop(false)
	pw.SetStyle(progress.StyleBlocks)
	pw.Style().Colors = progress.StyleColors{
		Message: text.Colors{},
		Error:   text.Colors{text.FgRed},
		Stats:   text.Colors{text.FgHiBlack},
		Time:    text.Colors{text.FgHiBlack},
		Tracker: text.Colors{text.FgGreen},
		Value:   text.Colors{text.FgHiBlack},
		Speed:   text.Colors{text.FgHiBlack},
	}
	pw.Style().Chars.Finished = "█"
	pw.Style().Options.DoneString = ""
	pw.Style().Options.ErrorString = ""
	pw.Style().Visibility = progress.StyleVisibility{
		Time:           true,
		Tracker:        true,
		Value:          false,
		Percentage:     false,
		ETA:            false,
		ETAOverall:     false,
		Speed:          false,
		SpeedOverall:   false,
		TrackerOverall: false,
	}
	pw.SetTrackerPosition(progress.PositionRight)
	pw.SetUpdateFrequency(progressUpdateFreq)
	return pw
}

// ─── active stop (for signal handler) ────────────────────────────────────────

var (
	activePWMu sync.Mutex
	activeStop func()
)

func setActiveStop(fn func()) {
	activePWMu.Lock()
	activeStop = fn
	activePWMu.Unlock()
}

func stopActive() {
	activePWMu.Lock()
	fn := activeStop
	activePWMu.Unlock()
	if fn != nil {
		fn()
	}
}

// ─── phase monitor ────────────────────────────────────────────────────────────

// phaseMonitor shows a live progress bar for a batch of parallel tasks.
// One overall tracker counts completions; individual item trackers are appended
// as each result arrives on doneCh.
func phaseMonitor(title string, total int, doneCh <-chan phaseItem) {
	pw := newPW()

	// Use total*10 ticks so the heartbeat can show fine-grained movement.
	overall := &progress.Tracker{
		Message: fmt.Sprintf("  %s  0 / %d", text.Bold.Sprint(title), total),
		Total:   int64(total * 10),
	}
	pw.AppendTracker(overall)
	go pw.Render()

	setActiveStop(func() { pw.Stop() })

	// Monotonic advance: only move the tracker value forward, never back.
	var tvMu sync.Mutex
	var tv int64
	advance := func(target int64) {
		tvMu.Lock()
		defer tvMu.Unlock()
		if target > tv {
			overall.Increment(target - tv)
			tv = target
		}
	}

	// Heartbeat: pulse the bar forward at ~80% max so it never looks frozen.
	heartbeatStop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(150 * time.Millisecond)
		defer ticker.Stop()
		cap := int64(total * 8)
		tick := int64(0)
		for {
			select {
			case <-ticker.C:
				tick++
				if tick <= cap {
					advance(tick)
				}
			case <-heartbeatStop:
				return
			}
		}
	}()

	for i := 1; i <= total; i++ {
		item := <-doneCh

		overall.Message = fmt.Sprintf("  %s  %d / %d", text.Bold.Sprint(title), i, total)
		// Snap to the exact completion step for this item.
		advance(int64(i * 10))

		resultStr := text.FgHiBlack.Sprint(item.result)
		if !item.ok {
			resultStr = text.FgRed.Sprint(item.result)
		}

		t := &progress.Tracker{
			Message: fmt.Sprintf("  %-*s  %s", progressLabelWidth, item.label, resultStr),
			Total:   0,
		}
		pw.AppendTracker(t)
		if item.ok {
			t.MarkAsDone()
		} else {
			t.MarkAsErrored()
		}
	}

	close(heartbeatStop)
	overall.MarkAsDone()
	pw.Stop()
	// Wait for the render goroutine to fully exit before returning,
	// so callers can safely write to stdout (e.g. prompts, path lists).
	for pw.IsRenderInProgress() {
		time.Sleep(10 * time.Millisecond)
	}
	setActiveStop(nil)
}
