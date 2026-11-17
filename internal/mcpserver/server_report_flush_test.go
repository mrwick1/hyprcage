package mcpserver

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hexadecimil/offstage/internal/report"
)

func TestFlushReportsRetriesUntilTheRelayTakesTheReport(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("OFFSTAGE_BETA_KEY", "osk_abcdefghijklmnopqrstuvwxyz")
	old := reportBackoff
	reportBackoff = []time.Duration{10 * time.Millisecond}
	defer func() { reportBackoff = old }()
	var calls atomic.Int32
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(502)
			_, _ = io.WriteString(w, `{"error":"down"}`)
			return
		}
		w.WriteHeader(201)
		_, _ = io.WriteString(w, `{"id":"OFS-3"}`)
	}))
	defer relay.Close()
	b, err := report.Build(report.Request{Title: "retry", Manual: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := report.Save(b); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	defer close(stop)
	go flushReports(relay.URL, stop)
	wakeReports()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		// A listing can miss a bundle that a flush renames at that moment,
		// so wait for the second relay call before reading the queue.
		left, _ := filepath.Glob(filepath.Join(report.QueueDir(), "*"))
		if calls.Load() >= 2 && len(left) == 0 {
			if n := calls.Load(); n != 2 {
				t.Errorf("relay calls %d, want 2", n)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("queue not sent; relay calls %d", calls.Load())
}
