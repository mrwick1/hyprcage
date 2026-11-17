// Package relay is the beta report relay. Testers send reports to it with a
// beta key. It files each report as a work item in a private Plane project,
// so testers never hold a Plane token and Plane stays private.
package relay

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/hexadecimil/offstage/internal/redact"
)

// Limits.
const (
	MaxBundle     = 5 << 20 // the Plane attachment limit on the instance
	MaxBody       = 20000   // characters of report text
	DailyReports  = 10      // reports per tester per day
	HourlyCalls   = 60      // requests per tester per hour
	maxRequest    = MaxBundle + 256<<10
	bundleName    = "bundle.zip"
	fingerprintRe = `^[a-f0-9]{8,64}$`
)

var (
	validFingerprint = regexp.MustCompile(fingerprintRe)
	validRef         = regexp.MustCompile(`^[A-Z]{1,10}-[0-9]{1,9}$`)
	areas            = map[string]bool{"crash": true, "isolation-leak": true, "install": true, "desktop": true, "bare": true}
	types            = map[string]bool{"bug": true, "doubt": true, "suggestion": true, "feedback": true}
	textExt          = map[string]bool{".txt": true, ".json": true, ".jsonl": true, ".log": true, ".md": true, ".toml": true}
)

// Meta is the JSON part of a report.
type Meta struct {
	Title       string `json:"title"`
	Platform    string `json:"platform"`              // linux or windows
	Kind        string `json:"kind"`                  // agent or manual
	Area        string `json:"area,omitempty"`        // crash, isolation-leak, install, desktop, bare
	Type        string `json:"type,omitempty"`        // bug, doubt, suggestion, feedback; empty means bug
	Fingerprint string `json:"fingerprint,omitempty"` // hex hash from the client
	Version     string `json:"version"`
	OS          string `json:"os"`
	Body        string `json:"body"` // expected, actual, steps; plain text
}

// Server is the relay's HTTP handler.
type Server struct {
	Plane      *Plane
	Store      *Store
	Keys       map[string]string // sha256 hex of a key -> tester name
	Identifier string            // project identifier, for example OFS
	Now        func() time.Time

	mu    sync.Mutex
	calls map[string][]time.Time
}

// LoadKeys reads "name sha256hex" lines. Blank lines and # comments are skipped.
func LoadKeys(file string) (map[string]string, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	keys := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, hash, ok := strings.Cut(line, " ")
		hash = strings.TrimSpace(hash)
		if !ok || len(hash) != 64 {
			return nil, fmt.Errorf("%s: bad line %q", file, line)
		}
		keys[strings.ToLower(hash)] = name
	}
	return keys, sc.Err()
}

// HashKey returns the stored form of a beta key.
func HashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/healthz" && r.Method == http.MethodGet:
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok\n")
	case r.URL.Path == "/v1/reports" && r.Method == http.MethodPost:
		s.withTester(w, r, s.file)
	case strings.HasPrefix(r.URL.Path, "/v1/reports/") && r.Method == http.MethodGet:
		s.withTester(w, r, s.status)
	case r.URL.Path == "/v1/ping" && r.Method == http.MethodPost:
		s.withTester(w, r, s.ping)
	default:
		reply(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

// withTester checks the beta key and the hourly limit.
func (s *Server) withTester(w http.ResponseWriter, r *http.Request, next func(http.ResponseWriter, *http.Request, string)) {
	key, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	tester, known := s.Keys[HashKey(strings.TrimSpace(key))]
	if !ok || !known {
		reply(w, http.StatusUnauthorized, map[string]string{"error": "unknown beta key"})
		return
	}
	if !s.allow(tester) {
		reply(w, http.StatusTooManyRequests, map[string]string{"error": "too many requests; try again in an hour"})
		return
	}
	next(w, r, tester)
}

func (s *Server) allow(tester string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.calls == nil {
		s.calls = map[string][]time.Time{}
	}
	cut := s.now().Add(-time.Hour)
	kept := s.calls[tester][:0]
	for _, t := range s.calls[tester] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= HourlyCalls {
		s.calls[tester] = kept
		return false
	}
	s.calls[tester] = append(kept, s.now())
	return true
}

// ping records that a tester's agent started a session today.
func (s *Server) ping(w http.ResponseWriter, r *http.Request, tester string) {
	var p struct {
		Version  string `json:"version"`
		Platform string `json:"platform"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&p); err != nil || len(p.Version) > 64 || len(p.Platform) > 16 {
		reply(w, http.StatusBadRequest, map[string]string{"error": "want {\"version\":...,\"platform\":...}"})
		return
	}
	if err := s.Store.See(tester, s.now().UTC(), p.Version, p.Platform); err != nil {
		s.fail(w, err)
		return
	}
	log.Printf("ping %s %s %s", tester, p.Version, p.Platform)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) file(w http.ResponseWriter, r *http.Request, tester string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequest)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		reply(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "the report is over 5 MB or not multipart"})
		return
	}
	var m Meta
	if err := json.Unmarshal([]byte(r.FormValue("meta")), &m); err != nil {
		reply(w, http.StatusBadRequest, map[string]string{"error": "meta is not valid JSON"})
		return
	}
	if msg := m.check(); msg != "" {
		reply(w, http.StatusBadRequest, map[string]string{"error": msg})
		return
	}
	var bundle []byte
	if f, _, err := r.FormFile("bundle"); err == nil {
		raw, _ := io.ReadAll(io.LimitReader(f, MaxBundle+1))
		f.Close()
		if len(raw) > MaxBundle {
			reply(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "the bundle is over 5 MB"})
			return
		}
		if bundle, err = redactZip(raw); err != nil {
			reply(w, http.StatusBadRequest, map[string]string{"error": "the bundle is not a zip file"})
			return
		}
	}
	ok, err := s.Store.Take(tester, s.now().UTC().Format("2006-01-02"), DailyReports)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !ok {
		reply(w, http.StatusTooManyRequests, map[string]string{"error": fmt.Sprintf("%d reports a day per tester; the rest waits until tomorrow", DailyReports)})
		return
	}
	m.redact()

	if id, dup := s.Store.Item(m.Fingerprint); m.Fingerprint != "" && dup {
		if err := s.Plane.Comment(id, m.html(tester, true)); err != nil {
			s.fail(w, err)
			return
		}
		if err := s.attach(id, bundle); err != nil {
			s.fail(w, err)
			return
		}
		ref := s.Store.Ref(id)
		if err := s.Store.Record(m.Fingerprint, id, "", tester); err != nil {
			s.fail(w, err)
			return
		}
		reply(w, http.StatusOK, map[string]any{"id": ref, "duplicate": true})
		return
	}

	labels, err := s.Plane.Labels()
	if err != nil {
		s.fail(w, err)
		return
	}
	var ids []string
	for _, name := range []string{map[string]string{"agent": "agent-report", "manual": "manual"}[m.Kind], m.Platform, m.Area, m.kindOfReport(), m.autoLabel()} {
		if id := labels[name]; id != "" {
			ids = append(ids, id)
		}
	}
	it, err := s.Plane.Create(m.planeTitle(), m.html(tester, false), ids)
	if err != nil {
		s.fail(w, err)
		return
	}
	ref := fmt.Sprintf("%s-%d", s.Identifier, it.Sequence)
	if err := s.Store.Record(m.Fingerprint, it.ID, ref, tester); err != nil {
		s.fail(w, err)
		return
	}
	if err := s.attach(it.ID, bundle); err != nil {
		// The work item exists; the tester still gets its id.
		log.Printf("attach %s: %v", ref, err)
		reply(w, http.StatusCreated, map[string]any{"id": ref, "duplicate": false, "warning": "the bundle did not upload"})
		return
	}
	reply(w, http.StatusCreated, map[string]any{"id": ref, "duplicate": false})
}

func (s *Server) attach(id string, bundle []byte) error {
	if len(bundle) == 0 {
		return nil
	}
	name := fmt.Sprintf("bundle-%s.zip", s.now().UTC().Format("20060102-150405"))
	return s.Plane.Attach(id, name, "application/zip", bundle)
}

func (s *Server) status(w http.ResponseWriter, r *http.Request, tester string) {
	ref := strings.TrimPrefix(r.URL.Path, "/v1/reports/")
	id, mine := s.Store.Lookup(ref, tester)
	if !validRef.MatchString(ref) || !mine {
		reply(w, http.StatusNotFound, map[string]string{"error": "no report " + ref + " filed with this key"})
		return
	}
	it, err := s.Plane.Get(id)
	if err != nil {
		s.fail(w, err)
		return
	}
	states, err := s.Plane.States()
	if err != nil {
		s.fail(w, err)
		return
	}
	name := "unknown"
	for n, sid := range states {
		if sid == it.State {
			name = n
		}
	}
	reply(w, http.StatusOK, map[string]string{"id": ref, "state": name})
}

// fail logs the cause and gives the tester no detail about Plane.
func (s *Server) fail(w http.ResponseWriter, err error) {
	log.Printf("error: %v", err)
	reply(w, http.StatusBadGateway, map[string]string{"error": "the relay could not file the report; keep the bundle and retry later"})
}

func reply(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// kindOfReport is the report type; an old client sends none, which means bug.
func (m *Meta) kindOfReport() string {
	if m.Type == "" {
		return "bug"
	}
	return m.Type
}

// planeTitle marks a report that is not a bug.
func (m *Meta) planeTitle() string {
	if t := m.kindOfReport(); t != "bug" {
		return "[" + t + "] " + m.Title
	}
	return m.Title
}

// autoLabel names the label for a report the client filed automatically.
func (m *Meta) autoLabel() string {
	if strings.HasPrefix(m.Title, "[auto] ") {
		return "auto"
	}
	return ""
}

func (m *Meta) check() string {
	switch {
	case strings.TrimSpace(m.Title) == "" || len(m.Title) > 200:
		return "title must be 1 to 200 characters"
	case m.Platform != "linux" && m.Platform != "windows":
		return "platform must be linux or windows"
	case m.Kind != "agent" && m.Kind != "manual":
		return "kind must be agent or manual"
	case m.Area != "" && !areas[m.Area]:
		return "area must be crash, isolation-leak, install, desktop or bare"
	case m.Type != "" && !types[m.Type]:
		return "type must be bug, doubt, suggestion or feedback"
	case m.Fingerprint != "" && !validFingerprint.MatchString(m.Fingerprint):
		return "fingerprint must be 8 to 64 lowercase hex characters"
	case len(m.Body) > MaxBody:
		return fmt.Sprintf("body is over %d characters", MaxBody)
	case len(m.Version) > 100 || len(m.OS) > 200:
		return "version or os is too long"
	}
	return ""
}

func (m *Meta) redact() {
	m.Title = redact.Text(m.Title)
	m.Version = redact.Text(m.Version)
	m.OS = redact.Text(m.OS)
	m.Body = redact.Text(m.Body)
}

func (m *Meta) html(tester string, again bool) string {
	var b strings.Builder
	if again {
		fmt.Fprintf(&b, "<p><strong>Seen again</strong> by %s.</p>", html.EscapeString(tester))
	}
	fmt.Fprintf(&b, "<p><strong>Tester:</strong> %s<br><strong>Platform:</strong> %s<br><strong>Version:</strong> %s<br><strong>OS:</strong> %s</p>",
		html.EscapeString(tester), html.EscapeString(m.Platform), html.EscapeString(m.Version), html.EscapeString(m.OS))
	fmt.Fprintf(&b, "<pre>%s</pre>", html.EscapeString(m.Body))
	if m.Fingerprint != "" {
		fmt.Fprintf(&b, "<p>fingerprint: <code>%s</code></p>", m.Fingerprint)
	}
	return b.String()
}

// redactZip rewrites a zip with every text file redacted. Other files (the
// screenshots) pass through.
func redactZip(raw []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || strings.Contains(f.Name, "..") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(rc, MaxBundle*4))
		rc.Close()
		if err != nil {
			return nil, err
		}
		if textExt[strings.ToLower(path.Ext(f.Name))] {
			data = []byte(redact.Text(string(data)))
		}
		w, err := zw.Create(f.Name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
