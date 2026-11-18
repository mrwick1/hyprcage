package relay

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakePlane records what the relay sends.
type fakePlane struct {
	mu       sync.Mutex
	created  []map[string]any
	comments []string
	uploads  [][]byte
	seq      int
}

func (f *fakePlane) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	base := "/api/v1/workspaces/ws/projects/p"
	mux.HandleFunc(base+"/labels/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"results":[{"id":"l-agent","name":"agent-report"},{"id":"l-linux","name":"linux"},{"id":"l-crash","name":"crash"},{"id":"l-bug","name":"bug"},{"id":"l-doubt","name":"doubt"},{"id":"l-auto","name":"auto"}]}`)
	})
	mux.HandleFunc(base+"/states/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"results":[{"id":"s-new","name":"New"},{"id":"s-fixed","name":"Fixed in build"}]}`)
	})
	mux.HandleFunc(base+"/work-items/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		rest := strings.TrimPrefix(r.URL.Path, base+"/work-items/")
		switch {
		case rest == "" && r.Method == "POST":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.created = append(f.created, body)
			f.seq++
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "item-" + string(rune('0'+f.seq)), "sequence_id": f.seq, "state": "s-new"})
		case strings.HasSuffix(rest, "/comments/"):
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.comments = append(f.comments, body["comment_html"])
			w.WriteHeader(201)
		case strings.HasSuffix(rest, "/attachments/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"asset_id": "a1", "upload_data": map[string]any{"url": "http://" + r.Host + "/upload", "fields": map[string]string{"key": "k"}}})
		case strings.Contains(rest, "/attachments/"):
			w.WriteHeader(204)
		case r.Method == "GET":
			_, _ = io.WriteString(w, `{"id":"item-1","sequence_id":1,"state":"s-fixed"}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Errorf("upload: %v", err)
			return
		}
		data, _ := io.ReadAll(file)
		f.mu.Lock()
		f.uploads = append(f.uploads, data)
		f.mu.Unlock()
		w.WriteHeader(204)
	})
	return mux
}

func setup(t *testing.T) (*Server, *fakePlane) {
	t.Helper()
	fp := &fakePlane{}
	ps := httptest.NewServer(fp.handler(t))
	t.Cleanup(ps.Close)
	store, err := OpenStore(filepath.Join(t.TempDir(), "store.json"))
	if err != nil {
		t.Fatal(err)
	}
	return &Server{
		Plane:      &Plane{URL: ps.URL, Workspace: "ws", Project: "p", Token: "t"},
		Store:      store,
		Keys:       map[string]string{HashKey("osk_joel"): "joel", HashKey("osk_aswin"): "aswin"},
		Identifier: "OFS",
	}, fp
}

func zipOf(t *testing.T, files map[string]string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, _ := zw.Create(name)
		_, _ = io.WriteString(w, body)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func post(t *testing.T, s *Server, key string, meta Meta, bundle []byte) (int, map[string]any) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mj, _ := json.Marshal(meta)
	_ = mw.WriteField("meta", string(mj))
	if bundle != nil {
		fw, _ := mw.CreateFormFile("bundle", "bundle.zip")
		_, _ = fw.Write(bundle)
	}
	_ = mw.Close()
	req := httptest.NewRequest("POST", "/v1/reports", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func meta() Meta {
	return Meta{Title: "click did nothing", Platform: "linux", Kind: "agent", Area: "crash", Fingerprint: "deadbeef01", Version: "v0.10.0-beta.1", OS: "openSUSE Tumbleweed", Body: "steps: open /home/joel/x, then ws://127.0.0.1:9222/devtools?tok=1"}
}

func TestRejectsUnknownKey(t *testing.T) {
	s, fp := setup(t)
	for _, key := range []string{"", "osk_wrong"} {
		if code, _ := post(t, s, key, meta(), nil); code != 401 {
			t.Errorf("key %q: code %d, want 401", key, code)
		}
	}
	if len(fp.created) != 0 {
		t.Error("an unknown key filed a work item")
	}
}

func TestFilesRedactedItemWithLabelsAndBundle(t *testing.T) {
	s, fp := setup(t)
	bundle := zipOf(t, map[string]string{"trace.jsonl": `{"path":"/home/joel/a","url":"https://x.dev/p?session=abc"}`, "shot.png": "PNGDATA /home/joel"})
	code, out := post(t, s, "osk_joel", meta(), bundle)
	if code != 201 || out["id"] != "OFS-1" {
		t.Fatalf("code %d, out %v", code, out)
	}
	desc := fp.created[0]["description_html"].(string)
	if strings.Contains(desc, "/home/joel") || strings.Contains(desc, "tok=1") || !strings.Contains(desc, "Tester:</strong> joel") {
		t.Errorf("description not redacted or no tester: %s", desc)
	}
	labels, _ := json.Marshal(fp.created[0]["labels"])
	if string(labels) != `["l-agent","l-linux","l-crash","l-bug"]` {
		t.Errorf("labels %s", labels)
	}
	if len(fp.uploads) != 1 {
		t.Fatalf("%d uploads", len(fp.uploads))
	}
	zr, err := zip.NewReader(bytes.NewReader(fp.uploads[0]), int64(len(fp.uploads[0])))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		rc, _ := f.Open()
		data, _ := io.ReadAll(rc)
		rc.Close()
		switch f.Name {
		case "trace.jsonl":
			if strings.Contains(string(data), "joel") || strings.Contains(string(data), "session=") {
				t.Errorf("trace not redacted: %s", data)
			}
		case "shot.png":
			if string(data) != "PNGDATA /home/joel" {
				t.Errorf("binary file changed: %s", data)
			}
		}
	}
}

func TestSameFingerprintComments(t *testing.T) {
	s, fp := setup(t)
	post(t, s, "osk_joel", meta(), nil)
	code, out := post(t, s, "osk_aswin", meta(), nil)
	if code != 200 || out["id"] != "OFS-1" || out["duplicate"] != true {
		t.Fatalf("code %d, out %v", code, out)
	}
	if len(fp.created) != 1 || len(fp.comments) != 1 || !strings.Contains(fp.comments[0], "Seen again</strong> by aswin") {
		t.Errorf("created %d, comments %v", len(fp.created), fp.comments)
	}
}

func TestDailyLimit(t *testing.T) {
	s, _ := setup(t)
	for i := 0; i < DailyReports; i++ {
		m := meta()
		m.Fingerprint = ""
		if code, _ := post(t, s, "osk_joel", m, nil); code != 201 {
			t.Fatalf("report %d: code %d", i, code)
		}
	}
	if code, _ := post(t, s, "osk_joel", meta(), nil); code != 429 {
		t.Errorf("report over the limit: code %d, want 429", code)
	}
	if code, _ := post(t, s, "osk_aswin", meta(), nil); code != 201 {
		t.Errorf("another tester: code %d, want 201", code)
	}
}

func TestRejectsBadMetaAndBigBundle(t *testing.T) {
	s, fp := setup(t)
	bad := meta()
	bad.Platform = "mac"
	if code, _ := post(t, s, "osk_joel", bad, nil); code != 400 {
		t.Errorf("bad platform: code %d", code)
	}
	if code, _ := post(t, s, "osk_joel", meta(), bytes.Repeat([]byte("x"), MaxBundle+10)); code != 413 {
		t.Errorf("big bundle: code %d", code)
	}
	if code, _ := post(t, s, "osk_joel", meta(), []byte("not a zip")); code != 400 {
		t.Errorf("not a zip: code %d", code)
	}
	if len(fp.created) != 0 {
		t.Error("a rejected report filed a work item")
	}
}

func TestStatusOnlyForOwnReports(t *testing.T) {
	s, _ := setup(t)
	post(t, s, "osk_joel", meta(), nil)
	get := func(key, ref string) (int, map[string]any) {
		req := httptest.NewRequest("GET", "/v1/reports/"+ref, nil)
		req.Header.Set("Authorization", "Bearer "+key)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	if code, out := get("osk_joel", "OFS-1"); code != 200 || out["state"] != "Fixed in build" {
		t.Errorf("owner: code %d, out %v", code, out)
	}
	if code, _ := get("osk_aswin", "OFS-1"); code != 404 {
		t.Errorf("other tester: code %d, want 404", code)
	}
}

func TestTypeLabelTitleAndValidation(t *testing.T) {
	s, fp := setup(t)
	m := meta()
	m.Area, m.Fingerprint, m.Type = "", "", "doubt"
	if code, _ := post(t, s, "osk_joel", m, nil); code != 201 {
		t.Fatalf("doubt: code %d", code)
	}
	labels, _ := json.Marshal(fp.created[0]["labels"])
	if string(labels) != `["l-agent","l-linux","l-doubt"]` || fp.created[0]["name"] != "[doubt] click did nothing" {
		t.Errorf("labels %s, name %v", labels, fp.created[0]["name"])
	}
	m.Type = "suggestion" // no such label in the fake Plane: skipped, not an error
	if code, _ := post(t, s, "osk_joel", m, nil); code != 201 || fp.created[1]["name"] != "[suggestion] click did nothing" {
		t.Errorf("suggestion: code %d, name %v", code, fp.created[1]["name"])
	}
	m.Type = "rant"
	if code, out := post(t, s, "osk_joel", m, nil); code != 400 || !strings.Contains(out["error"].(string), "type must be") {
		t.Errorf("bad type: code %d, out %v", code, out)
	}
	if len(fp.created) != 2 {
		t.Error("a rejected type filed a work item")
	}
}

func TestAutoLabelFromTitlePrefix(t *testing.T) {
	s, fp := setup(t)
	m := meta()
	m.Title, m.Fingerprint = "[auto] click failed with internal", "a0000001"
	if code, _ := post(t, s, "osk_joel", m, nil); code != 201 {
		t.Fatalf("auto: code %d", code)
	}
	m.Title, m.Fingerprint = "click did nothing", "a0000002"
	if code, _ := post(t, s, "osk_joel", m, nil); code != 201 {
		t.Fatalf("plain: code %d", code)
	}
	m.Title, m.Fingerprint = "click [auto] did nothing", "a0000003"
	if code, _ := post(t, s, "osk_joel", m, nil); code != 201 {
		t.Fatalf("middle: code %d", code)
	}
	want := []string{`["l-agent","l-linux","l-crash","l-bug","l-auto"]`, `["l-agent","l-linux","l-crash","l-bug"]`, `["l-agent","l-linux","l-crash","l-bug"]`}
	for i, w := range want {
		labels, _ := json.Marshal(fp.created[i]["labels"])
		if string(labels) != w {
			t.Errorf("item %d labels %s, want %s", i, labels, w)
		}
	}
}

func TestAreaBareAcceptedUnknownRejected(t *testing.T) {
	s, fp := setup(t)
	m := meta()
	m.Area, m.Fingerprint = "bare", ""
	if code, _ := post(t, s, "osk_joel", m, nil); code != 201 {
		t.Fatalf("bare: code %d", code)
	}
	m.Area = "nonsense"
	if code, _ := post(t, s, "osk_joel", m, nil); code != 400 {
		t.Errorf("unknown area: code %d", code)
	}
	if len(fp.created) != 1 {
		t.Errorf("filed %d work items, want 1", len(fp.created))
	}
}

func TestPingRecordsLastSeen(t *testing.T) {
	s, _ := setup(t)
	ping := func(key, body string) int {
		req := httptest.NewRequest("POST", "/v1/ping", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+key)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := ping("osk_wrong", `{}`); code != 401 {
		t.Errorf("unknown key: code %d, want 401", code)
	}
	if code := ping("osk_joel", `not json`); code != 400 {
		t.Errorf("bad body: code %d, want 400", code)
	}
	if code := ping("osk_joel", `{"version":"0.11.0-beta.7","platform":"linux"}`); code != 204 {
		t.Fatalf("ping: code %d, want 204", code)
	}
	again, err := OpenStore(s.Store.path)
	if err != nil {
		t.Fatal(err)
	}
	got := again.d.Seen["joel"]
	if got.Version != "0.11.0-beta.7" || got.Platform != "linux" || got.At.IsZero() {
		t.Errorf("seen = %+v", got)
	}
	if _, ok := again.d.Seen["aswin"]; ok {
		t.Error("aswin seen without a ping")
	}
}

func TestHookGetsNewItemsOnly(t *testing.T) {
	s, _ := setup(t)
	var got []map[string]any
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var v map[string]any
		_ = json.NewDecoder(r.Body).Decode(&v)
		got = append(got, v)
	}))
	t.Cleanup(hs.Close)
	s.Hook = hs.URL
	m := meta()
	m.Title = "[auto] click failed"
	post(t, s, "osk_joel", m, nil)
	post(t, s, "osk_aswin", m, nil) // same fingerprint: a comment, no hook
	s.bg.Wait()
	if len(got) != 1 {
		t.Fatalf("hook calls %d, want 1", len(got))
	}
	want := `{"area":"crash","auto":true,"id":"OFS-1","kind":"agent","platform":"linux","project":"offstage","tester":"joel","type":"bug","version":"v0.10.0-beta.1"}`
	if b, _ := json.Marshal(got[0]); string(b) != want {
		t.Errorf("payload %s, want %s", b, want)
	}
	m.Area, m.Fingerprint = "bare", "beef0002"
	post(t, s, "osk_joel", m, nil)
	s.bg.Wait()
	if len(got) != 2 || got[1]["project"] != "bare" {
		t.Errorf("bare item: %v", got)
	}
}

// fastHook sets the retry delays to milliseconds for one test. Each test
// that sends to a hook must call s.bg.Wait() before it reads results.
func fastHook(t *testing.T) {
	t.Helper()
	old := hookDelays
	hookDelays = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { hookDelays = old })
}

func TestHookFailureStillFiles(t *testing.T) {
	s, _ := setup(t)
	fastHook(t)
	s.Hook = "http://127.0.0.1:1/hook-token-secret"
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	if code, out := post(t, s, "osk_joel", meta(), nil); code != 201 || out["id"] != "OFS-1" {
		t.Fatalf("code %d, out %v", code, out)
	}
	s.bg.Wait()
	if strings.Contains(logs.String(), "hook-token-secret") || !strings.Contains(logs.String(), "hook OFS-1") || !strings.Contains(logs.String(), "gave up") {
		t.Errorf("log %q", logs.String())
	}
}

func TestHookRetriesServerErrors(t *testing.T) {
	s, _ := setup(t)
	fastHook(t)
	var calls atomic.Int32
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	t.Cleanup(hs.Close)
	s.Hook = hs.URL
	if code, _ := post(t, s, "osk_joel", meta(), nil); code != 201 {
		t.Fatalf("code %d", code)
	}
	s.bg.Wait()
	if n := calls.Load(); n != 3 {
		t.Errorf("hook requests %d, want 3", n)
	}
}

func TestHookDoesNotRetryClientErrors(t *testing.T) {
	s, _ := setup(t)
	fastHook(t)
	var calls atomic.Int32
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(hs.Close)
	s.Hook = hs.URL
	if code, _ := post(t, s, "osk_joel", meta(), nil); code != 201 {
		t.Fatalf("code %d", code)
	}
	s.bg.Wait()
	if n := calls.Load(); n != 1 {
		t.Errorf("hook requests %d, want 1", n)
	}
}

func TestHookDoesNotDelayReply(t *testing.T) {
	s, _ := setup(t)
	arrived := make(chan struct{})
	release := make(chan struct{})
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(arrived)
		<-release // a slow hook: it answers only after the reply
	}))
	t.Cleanup(hs.Close)
	t.Cleanup(func() { close(release) }) // runs before hs.Close
	s.Hook = hs.URL

	replied := make(chan int, 1)
	go func() {
		code, _ := post(t, s, "osk_joel", meta(), nil)
		replied <- code
	}()
	select {
	case code := <-replied:
		if code != 201 {
			t.Errorf("code %d, want 201", code)
		}
	case <-time.After(5 * time.Second): // a failure guard, not a wait
		t.Fatal("the reply waited for the hook")
	}
	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("the hook never got the request")
	}
}
