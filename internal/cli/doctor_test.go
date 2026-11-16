package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestBareCheck(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	if st, _ := bareCheck(dir); st != "fail" {
		t.Fatalf("empty dir: status %q, want fail", st)
	}
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	for _, n := range []string{"bare-mcp", "bare-drive"} {
		if err := os.WriteFile(filepath.Join(dir, n+ext), nil, 0o755); err != nil {
			t.Fatal(err)
		}
		if n == "bare-mcp" {
			if st, d := bareCheck(dir); st != "fail" || d == "" {
				t.Fatalf("one binary: %q %q, want fail", st, d)
			}
		}
	}
	if st, _ := bareCheck(dir); st != "ok" {
		t.Fatalf("both binaries: status %q, want ok", st)
	}
}
