package mcpserver

import (
	"time"

	"github.com/hexadecimil/offstage/internal/report"
)

// reportWake asks the flusher to send the report queue now.
var reportWake = make(chan struct{}, 1)

func wakeReports() {
	select {
	case reportWake <- struct{}{}:
	default:
	}
}

// reportBackoff is the wait before each retry after a failed flush.
var reportBackoff = []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute}

// flushReports sends the report queue in the background, so report_issue
// never waits for the relay. It runs until stop closes. The queue stays on
// disk, and the next server sends what this one could not.
func flushReports(url string, stop <-chan struct{}) {
	failures := 0
	var retry <-chan time.Time
	for {
		select {
		case <-stop:
			return
		case <-reportWake:
		case <-retry:
		}
		retry = nil
		key := report.Key()
		if key == "" {
			continue
		}
		if _, err := report.Flush(url, key); err != nil {
			retry = time.After(reportBackoff[min(failures, len(reportBackoff)-1)])
			failures++
			continue
		}
		failures = 0
	}
}
