package report

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hexadecimil/offstage/internal/audit"
)

func home(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("OFFSTAGE_BETA_KEY", "")
	return root
}

const logText = `{"time":"2026-11-16T10:00:00Z","tool":"screen_list"}
{"time":"2026-11-16T10:00:01Z","tool":"screen_create","args":{"size":"1280x800"}}
{"time":"2026-11-16T10:00:02Z","screen":"hc-a","tool":"app_launch","args":{"command":["chrome"]}}
{"time":"2026-11-16T10:00:03Z","screen":"hc-b","tool":"click","args":{"x":1,"y":2}}
{"time":"2026-11-16T10:00:04Z","screen":"hc-a","tool":"click","args":{"x":5,"y":6},"error":"capture_failed"}
`

func TestTraceKeepsOneScreenFromItsCreate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "actions.jsonl")
	if err := os.WriteFile(p, []byte(logText), 0o600); err != nil {
		t.Fatal(err)
	}
	var tools []string
	for _, e := range Trace(p, "hc-a") {
		tools = append(tools, e.Tool)
	}
	if got := strings.Join(tools, ","); got != "screen_create,app_launch,click" {
		t.Errorf("trace %s", got)
	}
	if n := len(Trace(p, "")); n != 5 {
		t.Errorf("no screen: %d entries, want 5", n)
	}
	if Trace(p, "hc-none") != nil {
		t.Error("unknown screen has a trace")
	}
}

func TestFingerprintFollowsTheLastError(t *testing.T) {
	a := []audit.Entry{{Tool: "click", Error: "capture_failed"}}
	b := []audit.Entry{{Tool: "type"}, {Tool: "click", Error: "capture_failed"}}
	if Fingerprint(a) == "" || Fingerprint(a) != Fingerprint(b) {
		t.Errorf("same failure, fingerprints %q and %q", Fingerprint(a), Fingerprint(b))
	}
	if Fingerprint([]audit.Entry{{Tool: "click"}}) != "" {
		t.Error("a trace with no error has a fingerprint")
	}
}

func TestBuildRedactsUserHostAndKeepsSteps(t *testing.T) {
	root := home(t)
	if err := os.MkdirAll(filepath.Join(root, "state", "offstage", "log"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(audit.Path(), []byte(logText), 0o600); err != nil {
		t.Fatal(err)
	}
	u, _ := user.Current()
	host, _ := os.Hostname()
	logLine := "opened /home/" + u.Username + "/x on " + host + " with osk_ABCDEFGHIJKLMNOPQRST\n"
	if err := os.WriteFile(filepath.Join(root, "state", "offstage", "log", "hc-a-app-1-1.log"), []byte(logLine), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := Build(Request{Title: "click on " + host, Expected: "a click", Actual: "capture_failed", Screen: "hc-a",
		Extra: map[string][]byte{"snapshot.txt": []byte("user " + u.Username), "screenshot.png": []byte("PNG " + u.Username)}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.Meta.Body, "3. +2000ms click") || !strings.Contains(b.Meta.Body, "error capture_failed") {
		t.Errorf("steps missing:\n%s", b.Meta.Body)
	}
	if b.Meta.Fingerprint == "" || b.Meta.Kind != "agent" {
		t.Errorf("meta %+v", b.Meta)
	}
	zr, err := zip.NewReader(bytes.NewReader(b.Zip), int64(len(b.Zip)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		rc, _ := f.Open()
		data, _ := io.ReadAll(rc)
		rc.Close()
		text := string(data)
		if f.Name == "screenshot.png" {
			continue // binary files are not rewritten
		}
		for _, bad := range []string{u.Username, host, "ABCDEFGHIJKLMNOPQRST"} {
			if len(bad) > 2 && strings.Contains(text, bad) {
				t.Errorf("%s still has %q: %s", f.Name, bad, text)
			}
		}
	}
	if strings.Contains(b.Meta.Title, host) {
		t.Errorf("title has the host: %s", b.Meta.Title)
	}
}

func TestZipDropsScreenshotWhenTooBig(t *testing.T) {
	big := make([]byte, maxZip+1)
	_, _ = rand.Read(big) // random bytes do not compress
	z, names, err := zipFiles(map[string][]byte{"trace.jsonl": []byte("{}\n"), "screenshot.png": big})
	if err != nil {
		t.Fatal(err)
	}
	if len(z) > maxZip || strings.Contains(strings.Join(names, ","), "screenshot.png") || !strings.Contains(strings.Join(names, ","), "dropped.txt") {
		t.Errorf("size %d, names %v", len(z), names)
	}
}

func TestLoginStoresKeyForTheUserOnly(t *testing.T) {
	home(t)
	if err := Login("not-a-key"); err == nil {
		t.Error("a key without osk_ was accepted")
	}
	if err := Login("osk_abcdefghijklmnopqrstuvwxyz\n"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(KeyPath())
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("key file %v %v", fi, err)
	}
	if Key() != "osk_abcdefghijklmnopqrstuvwxyz" {
		t.Errorf("Key() = %q", Key())
	}
}

func TestDeliverQueuesWhenRelayIsDownThenFlushes(t *testing.T) {
	home(t)
	t.Setenv("OFFSTAGE_BETA_KEY", "osk_abcdefghijklmnopqrstuvwxyz")
	up := false
	var got []string
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !up {
			w.WriteHeader(502)
			_, _ = io.WriteString(w, `{"error":"down"}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer osk_abcdefghijklmnopqrstuvwxyz" {
			t.Errorf("auth %q", r.Header.Get("Authorization"))
		}
		_ = r.ParseMultipartForm(1 << 20)
		got = append(got, r.FormValue("meta"))
		w.WriteHeader(201)
		_, _ = io.WriteString(w, `{"id":"OFS-7","duplicate":false}`)
	}))
	defer relay.Close()
	b, err := Build(Request{Title: "first", Manual: true})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := Deliver(relay.URL, b)
	if err != nil || !strings.Contains(msg, "waits in") {
		t.Fatalf("down: %q %v", msg, err)
	}
	up = true
	b2, _ := Build(Request{Title: "second", Manual: true})
	msg, err = Deliver(relay.URL, b2)
	if err != nil || msg != "filed OFS-7" {
		t.Fatalf("up: %q %v", msg, err)
	}
	if len(got) != 2 || !strings.Contains(got[0], `"title":"first"`) || !strings.Contains(got[1], `"title":"second"`) {
		t.Errorf("relay got %v", got)
	}
	if left, _ := filepath.Glob(filepath.Join(QueueDir(), "*")); len(left) != 0 {
		t.Errorf("queue not empty: %v", left)
	}
}

func TestRejectedBundleIsNotRetried(t *testing.T) {
	home(t)
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = io.WriteString(w, `{"error":"bad"}`)
	}))
	defer relay.Close()
	b, _ := Build(Request{Title: "x", Manual: true})
	if _, err := Save(b); err != nil {
		t.Fatal(err)
	}
	if _, err := Flush(relay.URL, "osk_abcdefghijklmnopqrstuvwxyz"); err != nil {
		t.Fatal(err)
	}
	left, _ := filepath.Glob(filepath.Join(QueueDir(), "*.rejected"))
	if len(left) != 1 {
		t.Errorf("rejected bundles: %v", left)
	}
}

func TestFlushNeverSendsPreviews(t *testing.T) {
	home(t)
	sent := 0
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent++
		_, _ = io.WriteString(w, `{"id":"OFS-9"}`)
	}))
	defer relay.Close()
	b, _ := Build(Request{Title: "preview", Manual: true})
	if _, err := SaveTo(PreviewDir(), b); err != nil {
		t.Fatal(err)
	}
	if _, err := Flush(relay.URL, "osk_abcdefghijklmnopqrstuvwxyz"); err != nil {
		t.Fatal(err)
	}
	if sent != 0 {
		t.Errorf("flush sent %d previews", sent)
	}
}

func TestBuildType(t *testing.T) {
	root := home(t)
	if err := os.MkdirAll(filepath.Join(root, "state", "offstage"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(audit.Path(), []byte(logText), 0o600); err != nil {
		t.Fatal(err)
	}
	build := func(typ, expected string) *Bundle {
		b, err := Build(Request{Title: "same title", Expected: expected, Type: typ})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	bug, bugDefault, doubt := build("", "x"), build("bug", "x"), build("doubt", "")
	if bug.Meta.Type != "" || bugDefault.Meta.Type != "" || bug.Meta.Fingerprint != bugDefault.Meta.Fingerprint {
		t.Errorf("bug meta %+v, explicit bug %+v", bug.Meta, bugDefault.Meta)
	}
	if doubt.Meta.Type != "doubt" || doubt.Meta.Fingerprint == "" || doubt.Meta.Fingerprint == bug.Meta.Fingerprint {
		t.Errorf("doubt meta %+v, bug fingerprint %s", doubt.Meta, bug.Meta.Fingerprint)
	}
	if strings.Contains(doubt.Meta.Body, "Expected:") || !strings.Contains(doubt.Meta.Body, "Type: doubt") {
		t.Errorf("doubt body:\n%s", doubt.Meta.Body)
	}
	if _, err := Build(Request{Title: "t", Type: "rant"}); err == nil {
		t.Error("an unknown type was accepted")
	}
}

func TestEnvHasVersionsAndEffectiveConfig(t *testing.T) {
	home(t)
	env := Env()
	for _, key := range []string{"offstage ", "os: ", "go: go", "chrome: ", "effective config:", "DefaultWidth = 1280", "Guard.Enabled = ", "Report.URL = "} {
		if !strings.Contains(env, key) {
			t.Errorf("env.txt has no %q:\n%s", key, env)
		}
	}
}

func TestRecentLogIsCapped(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	var b strings.Builder
	b.WriteString(now.Add(-time.Hour).Format(time.RFC3339Nano) + " too old (restored)\n")
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&b, "%s line %d (restored)\n", now.Add(-time.Minute).Format(time.RFC3339Nano), i)
	}
	if err := os.WriteFile(filepath.Join(dir, "restore.log"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	var plain strings.Builder
	for i := 0; i < 500; i++ {
		fmt.Fprintf(&plain, "frame %d\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "record-hc-a.log"), []byte(plain.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	got := RecentLog(dir, "hc-a", now)
	if strings.Contains(got, "too old") || strings.Contains(got, "line 99 ") || !strings.Contains(got, "line 399 ") {
		t.Errorf("restore.log not cut to the recent 300 lines:\n%s", got)
	}
	if strings.Contains(got, "frame 199\n") || !strings.Contains(got, "frame 200\n") || !strings.Contains(got, "frame 499\n") {
		t.Errorf("record log is not its last 300 lines")
	}
	if n := strings.Count(got, "\n"); n != 300+300+2 {
		t.Errorf("%d lines", n)
	}
	huge := strings.Repeat("x", 1000) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "restore.log"), []byte(strings.Repeat(huge, 299)), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := RecentLog(dir, "", now); len(got) > maxLogTxt+8 {
		t.Errorf("log.txt is %d bytes", len(got))
	}
}

func TestTimelineHasOneLinePerCallAndIsCapped(t *testing.T) {
	var trace []audit.Entry
	for i := 0; i < 250; i++ {
		trace = append(trace, audit.Entry{Time: "2026-11-16T10:00:00Z", Tool: "click", Ms: 12})
	}
	trace[249].Error = "capture_failed"
	trace[249].Ms = 0
	tl := Timeline(trace)
	lines := strings.Split(strings.TrimRight(tl, "\n"), "\n")
	if len(lines) != timelineLines || lines[0] != "2026-11-16T10:00:00Z click 12ms ok" || lines[199] != "2026-11-16T10:00:00Z click - capture_failed" {
		t.Errorf("%d lines, first %q, last %q", len(lines), lines[0], lines[len(lines)-1])
	}
}

func TestBuildAddsLogAndTimelineRedacted(t *testing.T) {
	root := home(t)
	logDir := filepath.Join(root, "state", "offstage", "log")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(audit.Path(), []byte(logText), 0o600); err != nil {
		t.Fatal(err)
	}
	u, _ := user.Current()
	line := time.Now().UTC().Format(time.RFC3339Nano) + " moved /home/" + u.Username + "/win for jane.doe@example.com (restored)\n"
	if err := os.WriteFile(filepath.Join(logDir, "restore.log"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := Build(Request{Title: "t", Screen: "hc-a"})
	if err != nil {
		t.Fatal(err)
	}
	zr, _ := zip.NewReader(bytes.NewReader(b.Zip), int64(len(b.Zip)))
	got := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		data, _ := io.ReadAll(rc)
		rc.Close()
		got[f.Name] = string(data)
	}
	if !strings.Contains(got["timeline.txt"], "app_launch") || !strings.Contains(got["timeline.txt"], "click - capture_failed") {
		t.Errorf("timeline.txt:\n%s", got["timeline.txt"])
	}
	log := got["log.txt"]
	if !strings.Contains(log, "restored") || strings.Contains(log, "jane.doe@example.com") || (len(u.Username) > 2 && strings.Contains(log, u.Username)) {
		t.Errorf("log.txt not redacted:\n%s", log)
	}
}

func TestConcurrentFlushesSendEachBundleOnce(t *testing.T) {
	home(t)
	var mu sync.Mutex
	sent := map[string]int{}
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond)
		_ = r.ParseMultipartForm(1 << 20)
		mu.Lock()
		sent[r.FormValue("meta")]++
		mu.Unlock()
		_, _ = io.WriteString(w, `{"id":"OFS-1"}`)
	}))
	defer relay.Close()
	for i := range 5 {
		b, _ := Build(Request{Title: fmt.Sprintf("r%d", i), Manual: true})
		if _, err := Save(b); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() { _, _ = Flush(relay.URL, "osk_abcdefghijklmnopqrstuvwxyz") })
	}
	wg.Wait()
	if len(sent) != 5 {
		t.Errorf("sent %d distinct bundles, want 5", len(sent))
	}
	for m, n := range sent {
		if n != 1 {
			t.Errorf("sent %d times: %s", n, m)
		}
	}
	if left, _ := filepath.Glob(filepath.Join(QueueDir(), "*")); len(left) != 0 {
		t.Errorf("queue not empty: %v", left)
	}
}

func TestFlushReclaimsAStaleClaim(t *testing.T) {
	home(t)
	sent := 0
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent++
		_, _ = io.WriteString(w, `{"id":"OFS-2"}`)
	}))
	defer relay.Close()
	b, _ := Build(Request{Title: "orphan", Manual: true})
	dir, err := Save(b)
	if err != nil {
		t.Fatal(err)
	}
	claim := dir + sendingSuffix
	if err := os.Rename(dir, claim); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * staleClaim)
	_ = os.Chtimes(claim, old, old)
	if _, err := Flush(relay.URL, "osk_abcdefghijklmnopqrstuvwxyz"); err != nil {
		t.Fatal(err)
	}
	if sent != 1 {
		t.Errorf("sent %d, want the stale claim sent once", sent)
	}
}
