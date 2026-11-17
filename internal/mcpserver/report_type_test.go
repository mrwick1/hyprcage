package mcpserver

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hexadecimil/offstage/internal/config"
	"github.com/hexadecimil/offstage/internal/report"
)

func TestReportIssueType(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		t.Skip("reports are for linux and windows")
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	s := &Server{}
	s.cfg = config.Fallback()
	s.cfg.Report.Confirm = true
	preview := func(in reportIn) string {
		res, err := s.reportIssue(in)
		if err != nil {
			t.Fatal(err)
		}
		return resultText(res)
	}
	if got := preview(reportIn{Title: "which tool?", Type: "doubt"}); !strings.Contains(got, "Type: doubt") {
		t.Errorf("doubt preview: %s", got)
	}
	if got := preview(reportIn{Title: "broken", Expected: "a", Actual: "b"}); strings.Contains(got, "Type:") || !strings.Contains(got, "Expected:") {
		t.Errorf("default is not a bug: %s", got)
	}
}

func TestReportIssueQueuesWithoutWaitingForTheRelay(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		t.Skip("reports are for linux and windows")
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("OFFSTAGE_BETA_KEY", "osk_abcdefghijklmnopqrstuvwxyz")
	release := make(chan struct{})
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	defer relay.Close()
	defer close(release)
	s := &Server{}
	s.cfg = config.Fallback()
	s.cfg.Report.URL = relay.URL
	start := time.Now()
	res, err := s.reportIssue(reportIn{Title: "slow relay", Type: "doubt"})
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("report_issue waited %v for the relay", d)
	}
	if got := resultText(res); !strings.Contains(got, "queued") {
		t.Errorf("result: %s", got)
	}
	if left, _ := filepath.Glob(filepath.Join(report.QueueDir(), "*")); len(left) != 1 {
		t.Errorf("queue: %v", left)
	}
}
