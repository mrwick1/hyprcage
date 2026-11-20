package report

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hexadecimil/offstage/internal/audit"
)

func buildFiles(t *testing.T) map[string]string {
	t.Helper()
	b, err := Build(Request{Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(b.Zip), int64(len(b.Zip)))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		data, _ := io.ReadAll(rc)
		rc.Close()
		got[f.Name] = string(data)
	}
	return got
}

func writeSessionLogs(t *testing.T, n int) {
	t.Helper()
	var a, bl strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&a, `{"time":"2026-01-01T00:00:00Z","tool":"click","session":"s1","args":{"i":%d}}`+"\n", i)
	}
	a.WriteString(`{"time":"2026-01-01T00:00:00Z","tool":"other","session":"s2"}` + "\n")
	a.WriteString(`{"time":"2026-01-01T00:00:00Z","tool":"path","session":"s1","error":"x","args":{"p":"/home/joel/x"}}` + "\n")
	bl.WriteString(`{"time":"t","session":"s1","tool":"click","code":"c","message":"bad /home/joel/x"}` + "\n")
	bl.WriteString(`{"time":"t","session":"s2","tool":"key","code":"c"}` + "\n")
	if err := os.WriteFile(audit.Path(), []byte(a.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(BlocksPath(), []byte(bl.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestBuildAddsSessionTraceAndBlocks(t *testing.T) {
	home(t)
	if err := os.MkdirAll(filepath.Dir(audit.Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	writeSessionLogs(t, 3)
	audit.SetSession("s1", "claude-code")
	t.Cleanup(func() { audit.SetSession("", "") })
	got := buildFiles(t)
	st, bl := got["session-trace.jsonl"], got["blocks.jsonl"]
	if st == "" || bl == "" {
		t.Fatalf("missing files: %v", got)
	}
	if strings.Contains(got["trace.jsonl"], "s2") {
		t.Errorf("a report with no screen holds another session:\n%s", got["trace.jsonl"])
	}
	if strings.Contains(st, "s2") || strings.Contains(bl, "s2") {
		t.Errorf("another session leaked:\n%s\n%s", st, bl)
	}
	if strings.Count(st, "\n") != 4 || strings.Count(bl, "\n") != 1 {
		t.Errorf("wrong line counts:\n%s\n%s", st, bl)
	}
	if strings.Contains(st, "/home/joel") || !strings.Contains(st, "~/x") {
		t.Errorf("session-trace not redacted:\n%s", st)
	}
	if strings.Contains(bl, "/home/joel") || !strings.Contains(bl, "~/x") {
		t.Errorf("blocks not redacted:\n%s", bl)
	}
}

func TestBuildSessionFilesAreCapped(t *testing.T) {
	home(t)
	if err := os.MkdirAll(filepath.Dir(audit.Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	writeSessionLogs(t, 600)
	audit.SetSession("s1", "")
	t.Cleanup(func() { audit.SetSession("", "") })
	st := buildFiles(t)["session-trace.jsonl"]
	if n := strings.Count(st, "\n"); n != 500 {
		t.Errorf("%d lines, want 500", n)
	}
	if !strings.Contains(st, `"x"`) {
		t.Error("newest call missing")
	}
}

func TestBuildOmitsSessionFilesWithoutSession(t *testing.T) {
	home(t)
	if err := os.MkdirAll(filepath.Dir(audit.Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	writeSessionLogs(t, 3)
	audit.SetSession("", "")
	got := buildFiles(t)
	if _, ok := got["session-trace.jsonl"]; ok {
		t.Error("session-trace.jsonl present")
	}
	if _, ok := got["blocks.jsonl"]; ok {
		t.Error("blocks.jsonl present")
	}
}
