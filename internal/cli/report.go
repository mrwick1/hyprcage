package cli

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/hexadecimil/offstage/internal/config"
	"github.com/hexadecimil/offstage/internal/report"
)

const reportUsage = `usage: offstage report login            read your beta key from stdin and store it
       offstage report --title T [--expected E] [--actual A] [--screen S] [--area A] [--type T] [--dry-run]
       offstage report flush            send the reports that could not be sent before
       offstage report status OFS-N     the state of a report you filed`

func runReport(e *Env) int {
	fs := e.flags("report")
	title := fs.String("title", "", "one line that names the problem")
	expected := fs.String("expected", "", "what should have happened")
	actual := fs.String("actual", "", "what happened instead")
	screenName := fs.String("screen", "", "the agent screen the problem happened on")
	area := fs.String("area", "", "crash, isolation-leak, install, desktop or bare (a problem with the bare browser server, bare-mcp)")
	typ := fs.String("type", "", "bug (default), doubt, suggestion or feedback")
	dry := fs.Bool("dry-run", false, "build the bundle and print where it is, without sending it")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	url := config.Fallback().Report.URL
	switch fs.Arg(0) {
	case "login":
		fmt.Fprintln(e.Stderr, "paste your beta key and press Enter:")
		line, _ := bufio.NewReader(e.Stdin).ReadString('\n')
		if err := report.Login(line); err != nil {
			return e.errorf("%v", err)
		}
		fmt.Fprintf(e.Stdout, "beta key stored in %s\n", report.KeyPath())
		return ExitOK
	case "flush":
		sent, err := report.Flush(url, report.Key())
		for _, r := range sent {
			fmt.Fprintf(e.Stdout, "sent %s\n", r.ID)
		}
		if err != nil {
			return e.errorf("%v (the rest stays in %s)", err, report.QueueDir())
		}
		return ExitOK
	case "status":
		if fs.NArg() != 2 {
			return e.errorf("%s", reportUsage)
		}
		state, err := report.Status(url, report.Key(), fs.Arg(1))
		if err != nil {
			return e.errorf("%v", err)
		}
		fmt.Fprintf(e.Stdout, "%s: %s\n", fs.Arg(1), state)
		return ExitOK
	case "":
	default:
		return e.errorf("%s", reportUsage)
	}
	if strings.TrimSpace(*title) == "" {
		return e.errorf("%s", reportUsage)
	}
	b, err := report.Build(report.Request{Title: *title, Expected: *expected, Actual: *actual, Screen: *screenName, Area: *area, Type: *typ, Manual: true})
	if err != nil {
		return e.errorf("%v", err)
	}
	if *dry {
		dir, err := report.SaveTo(report.PreviewDir(), b) // not queued: a dry run is never sent
		if err != nil {
			return e.errorf("%v", err)
		}
		fmt.Fprintf(e.Stdout, "%s\n\nfiles: %s\nbundle: %s\n", b.Meta.Body, strings.Join(b.Files, ", "), dir)
		return ExitOK
	}
	res, err := report.Deliver(url, b)
	if err != nil {
		return e.errorf("%v", err)
	}
	report.ClearCrashes(b.Crashes)
	fmt.Fprintln(e.Stdout, res)
	return ExitOK
}
