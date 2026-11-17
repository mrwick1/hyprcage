package update

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v0.11.0-beta.9", "v0.11.0-beta.10", true},
		{"v0.11.0-beta.10", "v0.11.0-beta.9", false},
		{"v0.11.0-beta.12", "v0.11.0", true},
		{"v0.11.0", "v0.11.0-beta.12", false},
		{"v0.11.0", "v0.11.0", false},
		{"v0.9.9", "v0.10.0", true},
		{"v1.0.0-alpha", "v1.0.0-beta", true},
		{"v1.0.0-beta", "v1.0.0-beta.1", true},
		{"v1.0.0-beta.1", "v1.0.0-beta", false},
		{"dev", "v1.0.0", false},
		{"v0.11.0-5-g1a2b3c4", "v0.12.0", false},
		{"v0.11.0", "garbage", false},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestEligible(t *testing.T) {
	for v, want := range map[string]bool{"v0.11.0-beta.12": true, "v1.0.0": true, "dev": false, "": false, "v0.11.0-5-g1a2b3c4": false, "source": false} {
		if got := Eligible(v); got != want {
			t.Errorf("Eligible(%q) = %v, want %v", v, got, want)
		}
	}
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// fixture serves plugin.json, the offstage asset and the bare assets.
type fixture struct {
	srv      *httptest.Server
	exe      string
	hits     atomic.Int32 // downloads of the offstage asset
	badSum   bool
	newExe   []byte
	newBare  []byte
	oldBare  []byte
	platform string
}

func newFixture(t *testing.T, version string) *fixture {
	t.Helper()
	plat, err := platform()
	if err != nil {
		t.Skip(err)
	}
	f := &fixture{platform: plat, newExe: []byte("new offstage"), newBare: []byte("new bare"), oldBare: []byte("old bare")}
	asset := "offstage-" + plat + exeSuffix()
	mAsset, dAsset := "bare-mcp-"+plat+exeSuffix(), "bare-drive-"+plat+exeSuffix()
	mux := http.NewServeMux()
	mux.HandleFunc("/plugin.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"name":"offstage","version":%q}`, version)
	})
	mux.HandleFunc("/SHA256SUMS", func(w http.ResponseWriter, r *http.Request) {
		s := sum(f.newExe)
		if f.badSum {
			s = sum([]byte("something else"))
		}
		fmt.Fprintf(w, "%s  %s\n", s, asset)
	})
	mux.HandleFunc("/"+asset, func(w http.ResponseWriter, r *http.Request) { f.hits.Add(1); w.Write(f.newExe) })
	mux.HandleFunc("/bare/SHA256SUMS", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  %s\n%s *%s\n", sum(f.newBare), mAsset, sum(f.newBare), dAsset)
	})
	mux.HandleFunc("/bare/"+mAsset, func(w http.ResponseWriter, r *http.Request) { w.Write(f.newBare) })
	mux.HandleFunc("/bare/"+dAsset, func(w http.ResponseWriter, r *http.Request) { w.Write(f.newBare) })
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	dir := t.TempDir()
	f.exe = filepath.Join(dir, "offstage"+exeSuffix())
	os.WriteFile(f.exe, []byte("old offstage"), 0o755)
	return f
}

func (f *fixture) installBare(t *testing.T) {
	for _, n := range []string{"bare-mcp", "bare-drive"} {
		os.WriteFile(filepath.Join(filepath.Dir(f.exe), n+exeSuffix()), f.oldBare, 0o755)
	}
}

func (f *fixture) opts(current string) Options {
	return Options{Exe: f.exe, Current: current, Base: f.srv.URL}
}

func read(t *testing.T, p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCheckInstallsNewerAndBare(t *testing.T) {
	f := newFixture(t, "0.11.0-beta.13")
	f.installBare(t)
	res, err := Check(f.opts("v0.11.0-beta.12"))
	if err != nil || res.Version != "v0.11.0-beta.13" || !res.Bare {
		t.Fatalf("Check = %+v, %v", res, err)
	}
	if got := read(t, f.exe); got != "new offstage" {
		t.Fatalf("binary = %q", got)
	}
	if got := read(t, filepath.Join(filepath.Dir(f.exe), "bare-mcp"+exeSuffix())); got != "new bare" {
		t.Fatalf("bare-mcp = %q", got)
	}
	fi, _ := os.Stat(f.exe)
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o100 == 0 {
		t.Fatal("binary is not executable")
	}
	// A second check finds nothing to do.
	if res, err := Check(f.opts("v0.11.0-beta.13")); err != nil || res.Version != "" || res.Bare {
		t.Fatalf("second Check = %+v, %v", res, err)
	}
}

func TestCheckLeavesBinaryOnChecksumMismatch(t *testing.T) {
	f := newFixture(t, "0.11.0-beta.13")
	f.badSum = true
	if _, err := Check(f.opts("v0.11.0-beta.12")); err == nil {
		t.Fatal("want a checksum error")
	}
	if got := read(t, f.exe); got != "old offstage" {
		t.Fatalf("binary = %q, want the old one", got)
	}
	left, _ := filepath.Glob(filepath.Join(filepath.Dir(f.exe), ".offstage-update-*"))
	if len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
}

func TestCheckNeverDowngrades(t *testing.T) {
	f := newFixture(t, "0.11.0-beta.12")
	res, err := Check(f.opts("v0.11.0-beta.13"))
	if err != nil || res.Version != "" {
		t.Fatalf("Check = %+v, %v", res, err)
	}
	if f.hits.Load() != 0 || read(t, f.exe) != "old offstage" {
		t.Fatal("downloaded or replaced on a newer build")
	}
}

func TestCheckSkipsDownloadWhenDiskIsAlreadyNewer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script")
	}
	f := newFixture(t, "0.11.0-beta.13")
	os.WriteFile(f.exe, []byte("#!/bin/sh\necho v0.11.0-beta.13\n"), 0o755)
	res, err := Check(f.opts("v0.11.0-beta.12"))
	if err != nil || res.Version != "" || f.hits.Load() != 0 {
		t.Fatalf("Check = %+v, %v, hits %d", res, err, f.hits.Load())
	}
}

func TestCheckKeepsOwnBare(t *testing.T) {
	f := newFixture(t, "0.11.0-beta.12")
	res, err := Check(f.opts("v0.11.0-beta.12"))
	if err != nil || res.Bare {
		t.Fatalf("Check = %+v, %v", res, err)
	}
}

func TestWatchSeesAStagedUpdate(t *testing.T) {
	f := newFixture(t, "0.11.0-beta.13")
	other := NewWatch(f.exe) // a second server, started before the update
	if _, err := Check(f.opts("v0.11.0-beta.12")); err != nil {
		t.Fatal(err)
	}
	if !other.Changed() {
		t.Fatal("the second server does not see the new binary")
	}
	if NewWatch(f.exe).Changed() {
		t.Fatal("a server started after the update sees a change")
	}
}

func TestCleanupRemovesOldFiles(t *testing.T) {
	f := newFixture(t, "0.11.0-beta.13")
	old := f.exe + ".old-123"
	os.WriteFile(old, nil, 0o644)
	Cleanup(f.exe)
	if _, err := os.Stat(old); err == nil {
		t.Fatal("old file kept")
	}
}
