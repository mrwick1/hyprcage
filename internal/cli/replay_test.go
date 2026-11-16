package cli

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestReadTraceTakesZipQueueDirAndFile(t *testing.T) {
	dir := t.TempDir()
	line := `{"time":"2026-11-16T10:00:00Z","tool":"screen_create"}` + "\n"
	plain := filepath.Join(dir, "trace.jsonl")
	os.WriteFile(plain, []byte(line), 0o600)
	queue := filepath.Join(dir, "queued")
	os.Mkdir(queue, 0o700)
	f, _ := os.Create(filepath.Join(queue, "bundle.zip"))
	zw := zip.NewWriter(f)
	w, _ := zw.Create("trace.jsonl")
	w.Write([]byte(line))
	zw.Close()
	f.Close()
	for _, p := range []string{plain, dir, queue, filepath.Join(queue, "bundle.zip")} {
		tr, err := readTrace(p)
		if err != nil || len(tr) != 1 || tr[0].Tool != "screen_create" {
			t.Errorf("%s: %v, %v", p, tr, err)
		}
	}
	if _, err := readTrace(filepath.Join(dir, "missing")); err == nil {
		t.Error("a missing path gave no error")
	}
}
