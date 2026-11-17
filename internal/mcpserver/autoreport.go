package mcpserver

import (
	"fmt"
	"strings"
	"sync"

	"github.com/hexadecimil/offstage/internal/audit"
	"github.com/hexadecimil/offstage/internal/desktop"
	"github.com/hexadecimil/offstage/internal/perceive"
	"github.com/hexadecimil/offstage/internal/report"
)

// offstage files a beta report by itself when a tool hits a block. The
// agent does nothing. The title prefix marks these reports for review.
const (
	autoTitlePrefix = "[auto] "
	autoMax         = 20 // automatic reports per server process
	autoStreak      = 3  // failures of one tool on one screen in a row
)

// autoBugCodes point at offstage itself, never at the caller.
var autoBugCodes = map[string]bool{
	"internal": true, "capture_failed": true, "cage_failed": true,
	"cdp_unreachable": true, "hyprland_unreachable": true, "notifyd_down": true,
}

// autoIgnored are the caller's mistakes and guard refusals. They stay in
// the block log only.
var autoIgnored = map[string]bool{
	"invalid_name": true, "not_owner": true, "ack_required": true, "system_prompt": true,
}

type autoState struct {
	mu     sync.Mutex
	filed  map[string]bool // fingerprints filed by this process
	count  int
	streak map[string]int // screen and tool -> failures in a row
	failed string         // the agent screen of the last failure, until a desktop report
}

func newAutoState() *autoState {
	return &autoState{filed: map[string]bool{}, streak: map[string]int{}}
}

var autoReports = newAutoState()

// autoEnabled turns automatic reports off for tests that do not test them.
var autoEnabled = true

// decide returns the report to file after call e, or nil. msg is the
// error text of a failed call.
func (a *autoState) decide(e audit.Entry, msg string) *reportIn {
	if e.Tool == "report_issue" || autoIgnored[e.Error] {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	noop := false
	if v, ok := e.Verdict.(*perceive.Verification); ok && v.Verdict == perceive.NoOp {
		noop = true
	}
	onDesktop := desktop.IsDesktop(e.Screen) || strings.HasPrefix(e.Tool, "desktop_")
	key := e.Screen + "\x00" + e.Tool
	var in *reportIn
	switch {
	case autoBugCodes[e.Error]:
		in = &reportIn{Type: "bug", Title: fmt.Sprintf("%s failed with %s", e.Tool, e.Error),
			Expected: "the tool works", Actual: msg}
		if e.Error == "internal" {
			in.Area = "crash"
		}
	case e.Error != "" || noop:
		a.streak[key]++
		if !onDesktop {
			a.failed = e.Screen
		}
		if a.streak[key] == autoStreak {
			what := e.Error
			if what == "" {
				what = "no visible effect"
			}
			in = &reportIn{Type: "doubt", Title: fmt.Sprintf("%s failed %d times in a row (%s)", e.Tool, autoStreak, what), Actual: msg}
		}
	default:
		delete(a.streak, key)
	}
	if in == nil && onDesktop && a.failed != "" {
		in = &reportIn{Type: "suggestion", Screen: a.failed,
			Title:  fmt.Sprintf("agent fell back to the human's desktop (%s) after a failure on its screen", e.Tool),
			Actual: "the agent called " + e.Tool + " after a failed call on " + a.failed}
		a.failed = ""
	}
	if in == nil {
		return nil
	}
	fp := in.Type + "|" + e.Tool + "|" + e.Error
	if a.filed[fp] || a.count >= autoMax {
		return nil
	}
	a.filed[fp] = true
	a.count++
	if in.Screen == "" && !onDesktop {
		in.Screen = e.Screen
	}
	in.Title = autoTitlePrefix + in.Title
	in.Actual = "offstage filed this report by itself after this call.\n" + in.Actual
	return in
}

// autoReport files the report that call e asks for, in the background.
// With a server, it takes the server lock, so the report carries a
// screenshot and the element tree of the screen. A human who asked to
// confirm each report gets no automatic report.
func autoReport(e audit.Entry, msg string, servers []*Server) {
	if !autoEnabled || len(servers) > 0 && servers[0].cfg.Report.Confirm {
		return
	}
	in := autoReports.decide(e, msg)
	if in == nil {
		return
	}
	queue := report.QueueDir() // now: the goroutine can outlive the caller's environment
	if len(servers) == 0 {
		go func() {
			b, err := report.Build(report.Request{Title: in.Title, Expected: in.Expected, Actual: in.Actual, Area: in.Area, Type: in.Type, Screen: in.Screen})
			if err != nil {
				return
			}
			if _, err := report.SaveTo(queue, b); err == nil {
				report.ClearCrashes(b.Crashes)
				wakeReports()
			}
		}()
		return
	}
	s := servers[0]
	go func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		_, _ = s.fileReport(*in, queue)
	}()
}
