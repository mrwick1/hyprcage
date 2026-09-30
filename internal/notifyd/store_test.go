package notifyd

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAppendMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "n.jsonl")
	if err := Append(path, Entry{ID: 1, Event: "notify"}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, want 0600", fi.Mode().Perm())
	}
}

func TestPrune(t *testing.T) {
	path := filepath.Join(t.TempDir(), "n.jsonl")
	now := time.Now()
	for _, e := range []Entry{
		{ID: 1, Event: "notify", Time: now.Add(-49 * time.Hour)},
		{ID: 2, Event: "notify", Time: now.Add(-47 * time.Hour)},
	} {
		if err := Append(path, e); err != nil {
			t.Fatal(err)
		}
	}
	if err := Prune(path, now); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != 2 {
		t.Fatalf("got %+v, want only ID 2", got)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v after prune, want 0600", fi.Mode().Perm())
	}
}

func TestPruneKeepsFileOnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "n.jsonl")
	if err := Append(path, Entry{ID: 1, Event: "notify", Time: time.Now().Add(-49 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700)
	if err := Prune(path, time.Now()); err == nil {
		t.Fatal("Prune succeeded in a read-only directory")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Fatalf("file changed:\n%s\nwant:\n%s", after, before)
	}
}

func TestReadTornLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "n.jsonl")
	if err := Append(path, Entry{ID: 1, Event: "notify"}); err != nil {
		t.Fatal(err)
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"id":2,"app":"fir`)
	f.Close()
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("got %+v, want only ID 1", got)
	}
}
