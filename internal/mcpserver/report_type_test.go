package mcpserver

import (
	"runtime"
	"strings"
	"testing"

	"github.com/hexadecimil/offstage/internal/config"
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
