// Package report builds a beta report bundle and sends it to the beta relay.
//
// A bundle holds the tool calls on the screen (from the action log), the
// environment, the tail of the screen's logs and, from the MCP tool, the
// screen's element tree and a screenshot. Every text file passes
// internal/redact, and the user name and host name are replaced, before the
// bundle leaves the machine. The relay redacts again.
package report

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/hexadecimil/offstage/internal/audit"
	"github.com/hexadecimil/offstage/internal/bare"
	"github.com/hexadecimil/offstage/internal/config"
	"github.com/hexadecimil/offstage/internal/headless"
	"github.com/hexadecimil/offstage/internal/redact"
	"github.com/hexadecimil/offstage/internal/relay"
	"github.com/hexadecimil/offstage/internal/screen"
	"github.com/hexadecimil/offstage/internal/version"
)

// Limits on what a bundle carries.
const (
	maxZip       = 4 << 20 // under the relay's 5 MB, with room for the form
	logLines     = 200
	bodySteps    = 80    // steps written in the report text; trace.jsonl has all of them
	maxBody      = 18000 // under the relay's 20000 characters
	maxLogFiles  = 6
	recentEvents = 60 // tool calls kept when the report names no screen

	// The files added for a maintainer stay under 256 KiB together:
	// log.txt 200, timeline.txt 40, the Chrome and config lines of env.txt the rest.
	maxLogTxt     = 200 << 10
	maxTimeline   = 40 << 10
	timelineLines = 200
	recentLogs    = 300
	recentWindow  = 10 * time.Minute
)

// Request is what the caller knows about the problem.
type Request struct {
	Title    string
	Expected string
	Actual   string
	Screen   string // the agent screen, empty when the problem is not tied to one
	Area     string // crash, isolation-leak, install, desktop, bare, or empty
	Type     string // bug (or empty), doubt, suggestion or feedback
	Manual   bool   // filed by a human, not by an agent
	Extra    map[string][]byte
}

// reportType maps the default type to empty, so a bug looks the same as
// it did before types existed.
func reportType(t string) string {
	if t == "bug" {
		return ""
	}
	return t
}

// Bundle is a built report.
type Bundle struct {
	Meta  relay.Meta
	Zip   []byte
	Files []string // names inside the zip, for the preview
	// Crashes are the crash files in the bundle. Remove them with
	// ClearCrashes once the report is sent or queued.
	Crashes []string
}

// Build collects and redacts a bundle.
func Build(r Request) (*Bundle, error) {
	if strings.TrimSpace(r.Title) == "" {
		return nil, errors.New("a report needs a title")
	}
	switch r.Type {
	case "", "bug", "doubt", "suggestion", "feedback":
	default:
		return nil, errors.New("type must be bug, doubt, suggestion or feedback")
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		return nil, fmt.Errorf("reports are for linux and windows, not %s", runtime.GOOS)
	}
	entries := readEntries(audit.Path())
	trace := traceOf(entries, r.Screen)
	files := map[string][]byte{
		"trace.jsonl": joinLines(trace),
		"env.txt":     []byte(Env()),
	}
	id, _ := audit.Session()
	if st := sessionTrace(entries, id); len(st) > 0 {
		files["session-trace.jsonl"] = joinLines(st)
	}
	if bl := sessionBlocks(BlocksPath(), id); len(bl) > 0 {
		files["blocks.jsonl"] = bl
	}
	for name, data := range Logs(screen.LogDir(), r.Screen) {
		files["logs/"+name] = data
	}
	if log := RecentLog(screen.LogDir(), r.Screen, time.Now()); log != "" {
		files["log.txt"] = []byte(log)
	}
	if tl := Timeline(trace); tl != "" {
		files["timeline.txt"] = []byte(tl)
	}
	for name, data := range r.Extra {
		files[name] = data
	}
	var crashes []string
	fingerprint := Fingerprint(trace)
	if r.Area == "crash" {
		for _, p := range Crashes() {
			data, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			files["crashes/"+filepath.Base(p)] = data
			crashes = append(crashes, p)
			if fp := crashFingerprint(string(data)); fp != "" {
				fingerprint = fp
			}
		}
	}
	if t := reportType(r.Type); t != "" && fingerprint != "" {
		sum := sha256.Sum256([]byte(fingerprint + "|" + t))
		fingerprint = hex.EncodeToString(sum[:8])
	}
	kind := "agent"
	if r.Manual {
		kind = "manual"
	}
	m := relay.Meta{
		Title:       r.Title,
		Platform:    runtime.GOOS,
		Kind:        kind,
		Area:        r.Area,
		Type:        reportType(r.Type),
		Fingerprint: fingerprint,
		Version:     version.Version,
		OS:          osName(),
		Body:        body(r, trace),
	}
	scrub := scrubber()
	m.Title, m.OS, m.Body = scrub(m.Title), scrub(m.OS), scrub(m.Body)
	for name, data := range files {
		if isText(name) {
			files[name] = []byte(scrub(string(data)))
		}
	}
	z, names, err := zipFiles(files)
	if err != nil {
		return nil, err
	}
	return &Bundle{Meta: m, Zip: z, Files: names, Crashes: crashes}, nil
}

// scrubber returns the redaction for this machine: internal/redact, then
// the user name and the host name.
func scrubber() func(string) string {
	var names []string
	if u, err := user.Current(); err == nil {
		names = append(names, u.Username)
		if i := strings.LastIndex(u.Username, `\`); i >= 0 { // DOMAIN\user on Windows
			names = append(names, u.Username[i+1:])
		}
	}
	host, _ := os.Hostname()
	return func(s string) string {
		s = redact.Text(s)
		if host != "" && len(host) > 2 {
			s = strings.ReplaceAll(s, host, "<host>")
		}
		for _, n := range names {
			if len(n) > 2 {
				s = strings.ReplaceAll(s, n, "<user>")
			}
		}
		return s
	}
}

func isText(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".txt", ".json", ".jsonl", ".log", ".md", ".toml":
		return true
	}
	return false
}

// zipFiles writes the files in name order. Over the size limit it drops the
// screenshot first, then the logs, the session trace and the block log.
func zipFiles(files map[string][]byte) ([]byte, []string, error) {
	for _, drop := range []string{"", "screenshot.png", "logs/", "session-trace.jsonl", "blocks.jsonl"} {
		if drop != "" {
			for name := range files {
				if name == drop || (strings.HasSuffix(drop, "/") && strings.HasPrefix(name, drop)) {
					delete(files, name)
				}
			}
			files["dropped.txt"] = append(files["dropped.txt"], []byte("dropped "+drop+" to fit the size limit\n")...)
		}
		names := make([]string, 0, len(files))
		for n := range files {
			names = append(names, n)
		}
		sort.Strings(names)
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		for _, n := range names {
			w, err := zw.Create(n)
			if err != nil {
				return nil, nil, err
			}
			if _, err := w.Write(files[n]); err != nil {
				return nil, nil, err
			}
		}
		if err := zw.Close(); err != nil {
			return nil, nil, err
		}
		if buf.Len() <= maxZip {
			return buf.Bytes(), names, nil
		}
	}
	return nil, nil, errors.New("the bundle is over 4 MB even without the screenshot, the logs, the session trace and the block log")
}

// Trace returns the action log lines of a screen, from the screen_create
// call before its first line. With no screen it returns the last calls.
func Trace(logPath, name string) []audit.Entry { return traceOf(readEntries(logPath), name) }

// readEntries returns every line of the action log that parses.
func readEntries(logPath string) []audit.Entry {
	f, err := os.Open(logPath)
	if err != nil {
		return nil
	}
	defer f.Close()
	var all []audit.Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var e audit.Entry
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			all = append(all, e)
		}
	}
	return all
}

func traceOf(all []audit.Entry, name string) []audit.Entry {
	if name == "" {
		return all[max(0, len(all)-recentEvents):]
	}
	first := -1
	for i, e := range all {
		if e.Screen == name {
			first = i
			break
		}
	}
	if first < 0 {
		return nil
	}
	// A screen's name is in the result of screen_create, not in its line:
	// take the last screen_create before the screen's first call.
	start := first
	for i := first - 1; i >= 0 && i >= first-20; i-- {
		if all[i].Tool == "screen_create" {
			start = i
			break
		}
	}
	var out []audit.Entry
	for i, e := range all[start:] {
		if i == 0 && e.Tool == "screen_create" || e.Screen == name {
			out = append(out, e)
		}
	}
	return out
}

// Fingerprint names a problem by the last failed call: its tool, its error
// code and the platform. A report with no failed call has none, so it is
// never merged with another.
func Fingerprint(trace []audit.Entry) string {
	for i := len(trace) - 1; i >= 0; i-- {
		if e := trace[i]; e.Error != "" {
			sum := sha256.Sum256([]byte(e.Tool + "|" + e.Error + "|" + runtime.GOOS))
			return hex.EncodeToString(sum[:8])
		}
	}
	return ""
}

func body(r Request, trace []audit.Entry) string {
	var b strings.Builder
	if t := reportType(r.Type); t != "" {
		fmt.Fprintf(&b, "Type: %s\n\n", t)
	}
	if reportType(r.Type) == "" || strings.TrimSpace(r.Expected) != "" {
		fmt.Fprintf(&b, "Expected:\n%s\n\n", strings.TrimSpace(r.Expected))
	}
	if reportType(r.Type) == "" || strings.TrimSpace(r.Actual) != "" {
		fmt.Fprintf(&b, "Actual:\n%s\n", strings.TrimSpace(r.Actual))
	}
	if r.Screen != "" {
		fmt.Fprintf(&b, "\nScreen: %s\n", r.Screen)
	}
	if len(trace) > 0 {
		b.WriteString("\nSteps (from the action log; typed text is not logged):\n")
		first := max(0, len(trace)-bodySteps)
		if first > 0 {
			fmt.Fprintf(&b, "(%d earlier steps are in trace.jsonl)\n", first)
		}
		var prev time.Time
		for i, e := range trace {
			if i < first {
				continue
			}
			t, _ := time.Parse(time.RFC3339Nano, e.Time)
			gap := ""
			if !prev.IsZero() {
				gap = fmt.Sprintf(" +%dms", t.Sub(prev).Milliseconds())
			}
			prev = t
			args, _ := json.Marshal(e.Args)
			if e.Args == nil {
				args = []byte("{}")
			}
			fmt.Fprintf(&b, "%d.%s %s %s", i+1, gap, e.Tool, args)
			if e.Error != "" {
				fmt.Fprintf(&b, " -> error %s", e.Error)
			}
			if v, ok := e.Verdict.(map[string]any); ok && v["verdict"] != nil {
				fmt.Fprintf(&b, " -> %v", v["verdict"])
			} else if e.Verdict != nil {
				v, _ := json.Marshal(e.Verdict)
				fmt.Fprintf(&b, " -> verdict %s", v)
			}
			b.WriteString("\n")
		}
	}
	if out := b.String(); len(out) > maxBody {
		return out[:maxBody] + "\n(cut; the full steps are in trace.jsonl)\n"
	}
	return b.String()
}

func joinLines(trace []audit.Entry) []byte {
	var b bytes.Buffer
	for _, e := range trace {
		line, _ := json.Marshal(e)
		b.Write(line)
		b.WriteByte('\n')
	}
	return b.Bytes()
}

// Logs returns the last lines of a screen's log files, newest files first.
func Logs(dir, name string) map[string][]byte {
	out := map[string][]byte{}
	if name == "" {
		return out
	}
	paths, _ := filepath.Glob(filepath.Join(dir, name+"-*.log"))
	sort.Slice(paths, func(i, j int) bool { return modTime(paths[i]).After(modTime(paths[j])) })
	for _, p := range paths[:min(len(paths), maxLogFiles)] {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		out[filepath.Base(p)] = []byte(strings.Join(lines[max(0, len(lines)-logLines):], "\n") + "\n")
	}
	return out
}

// RecentLog returns the state-dir logs that are not tied to one launch: the
// restore log, and the recording log of the screen. It keeps the last
// recentLogs lines, or the lines of the last recentWindow when the log is
// timestamped and that is fewer. A line with no timestamp counts as recent.
func RecentLog(dir, name string, now time.Time) string {
	names := []string{"restore.log"}
	if name != "" {
		names = append(names, "record-"+name+".log")
	}
	var b strings.Builder
	for _, n := range names {
		data, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		var keep []string
		for _, l := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
			if ts, _, _ := strings.Cut(l, " "); ts != "" {
				if t, err := time.Parse(time.RFC3339Nano, ts); err == nil && now.Sub(t) > recentWindow {
					continue
				}
			}
			keep = append(keep, l)
		}
		keep = keep[max(0, len(keep)-recentLogs):]
		if len(keep) > 0 {
			fmt.Fprintf(&b, "== %s ==\n%s\n", n, strings.Join(keep, "\n"))
		}
	}
	return tail(b.String(), maxLogTxt)
}

// tail keeps the last n bytes of s, from the start of a line.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[len(s)-n:]
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	return "(cut)\n" + s
}

// Timeline returns one line per call of the trace: time, tool, duration
// and result. It keeps the last timelineLines calls.
func Timeline(trace []audit.Entry) string {
	var b strings.Builder
	for _, e := range trace[max(0, len(trace)-timelineLines):] {
		res := "ok"
		if e.Error != "" {
			res = e.Error
		}
		ms := "-"
		if e.Ms > 0 {
			ms = fmt.Sprintf("%dms", e.Ms)
		}
		fmt.Fprintf(&b, "%s %s %s %s\n", e.Time, e.Tool, ms, res)
	}
	return tail(b.String(), maxTimeline)
}

func modTime(p string) time.Time {
	fi, err := os.Stat(p)
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

var chromeVersionDir = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$`)

// Env describes the machine: versions, OS, desktop and the config file.
func Env() string {
	var b strings.Builder
	fmt.Fprintf(&b, "offstage %s\n%s/%s\nos: %s\ngo: %s\n", version.Version, runtime.GOOS, runtime.GOARCH, osName(), runtime.Version())
	if p, err := headless.FindChrome(); err == nil && runtime.GOOS == "windows" {
		// chrome.exe --version opens a window; the install folder holds a
		// directory named after the version.
		v := "version unreadable"
		if ents, err := os.ReadDir(filepath.Dir(p)); err == nil {
			for _, d := range ents {
				if d.IsDir() && chromeVersionDir.MatchString(d.Name()) {
					v = d.Name()
				}
			}
		}
		fmt.Fprintf(&b, "chrome: %s %s\n", filepath.Base(p), v)
	} else if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		out, err := exec.CommandContext(ctx, p, "--version").Output()
		cancel()
		if err == nil {
			fmt.Fprintf(&b, "chrome: %s\n", firstLine(string(out)))
		} else {
			fmt.Fprintf(&b, "chrome: %s (version unreadable: %v)\n", filepath.Base(p), err)
		}
	} else {
		b.WriteString("chrome: not found\n")
	}
	cfg, cerr := config.Load()
	if cerr != nil {
		cfg = config.Default()
		cfg.Path = config.Path()
	}
	if runtime.GOOS == "linux" {
		if k, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
			fmt.Fprintf(&b, "kernel: %s", k)
		}
		if out, err := exec.Command("Hyprland", "--version").Output(); err == nil {
			fmt.Fprintf(&b, "hyprland: %s\n", firstLine(string(out)))
		}
		if out, err := exec.Command("cage", "-v").CombinedOutput(); err == nil {
			fmt.Fprintf(&b, "cage: %s\n", firstLine(string(out)))
		}
		gpus, _ := filepath.Glob("/sys/class/drm/card*/device/uevent")
		for _, g := range gpus {
			if data, err := os.ReadFile(g); err == nil {
				for _, l := range strings.Split(string(data), "\n") {
					if strings.HasPrefix(l, "PCI_ID=") || strings.HasPrefix(l, "DRIVER=") {
						fmt.Fprintf(&b, "gpu %s: %s\n", filepath.Base(filepath.Dir(filepath.Dir(g))), l)
					}
				}
			}
		}
	}
	if _, client := audit.Session(); client != "" {
		fmt.Fprintf(&b, "client: %s\n", client)
	}
	if runtime.GOOS == "linux" {
		r := screen.KnownRenderer(cfg.Renderer)
		if r == "" {
			r = "default"
		}
		fmt.Fprintf(&b, "cage renderer: %s\n", r)
	}
	// bare-mcp has no --version flag: it prints its usage line, so only the path is shown.
	if p, err := bare.Find(); err == nil {
		fmt.Fprintf(&b, "bare-mcp: %s\n", p)
	} else {
		b.WriteString("bare-mcp: not found\n")
	}
	if cerr != nil {
		fmt.Fprintf(&b, "\nconfig error: %v\n", cerr)
	}
	b.WriteString("\neffective config:\n")
	b.WriteString(render(reflect.ValueOf(cfg), ""))
	if data, err := os.ReadFile(cfg.Path); err == nil {
		fmt.Fprintf(&b, "\nconfig (%s):\n%s", cfg.Path, data)
	} else {
		b.WriteString("\nconfig: defaults\n")
	}
	return b.String()
}

// render writes the fields of a struct as key = value lines, nested
// structs as prefix.key.
func render(v reflect.Value, prefix string) string {
	var b strings.Builder
	for i := 0; i < v.NumField(); i++ {
		f := v.Type().Field(i)
		if f.Type.Kind() == reflect.Struct {
			b.WriteString(render(v.Field(i), prefix+f.Name+"."))
			continue
		}
		fmt.Fprintf(&b, "%s%s = %v\n", prefix, f.Name, v.Field(i).Interface())
	}
	return b.String()
}

func osName() string {
	if runtime.GOOS == "linux" {
		data, _ := os.ReadFile("/etc/os-release")
		for _, l := range strings.Split(string(data), "\n") {
			if v, ok := strings.CutPrefix(l, "PRETTY_NAME="); ok {
				return strings.Trim(v, `"`)
			}
		}
	}
	if runtime.GOOS == "windows" {
		if out, err := exec.Command("cmd", "/c", "ver").Output(); err == nil {
			return strings.TrimSpace(string(out))
		}
	}
	return runtime.GOOS
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return l
}

// KeyPath is the file that holds the beta key, next to the config file.
func KeyPath() string { return filepath.Join(filepath.Dir(config.Path()), "beta-key") }

// Key returns the beta key: $OFFSTAGE_BETA_KEY, else the key file.
func Key() string {
	if k := os.Getenv("OFFSTAGE_BETA_KEY"); k != "" {
		return strings.TrimSpace(k)
	}
	data, _ := os.ReadFile(KeyPath())
	return strings.TrimSpace(string(data))
}

// Login stores a beta key, readable by the user only.
func Login(key string) error {
	key = strings.TrimSpace(key)
	if !strings.HasPrefix(key, "osk_") || len(key) < 20 || strings.ContainsAny(key, " \t\r\n") {
		return errors.New("a beta key starts with osk_; ask Arjun for yours")
	}
	if err := os.MkdirAll(filepath.Dir(KeyPath()), 0o700); err != nil {
		return err
	}
	return os.WriteFile(KeyPath(), []byte(key+"\n"), 0o600)
}

// Result is the relay's answer to a report.
type Result struct {
	ID        string `json:"id"`
	Duplicate bool   `json:"duplicate"`
	Warning   string `json:"warning,omitempty"`
}

var client = &http.Client{Timeout: 90 * time.Second}

// Send posts a bundle to the relay.
func Send(url, key string, b *Bundle) (Result, error) {
	var res Result
	if key == "" {
		return res, errors.New("no beta key; run: offstage report login")
	}
	meta, err := json.Marshal(b.Meta)
	if err != nil {
		return res, err
	}
	var form bytes.Buffer
	mw := multipart.NewWriter(&form)
	_ = mw.WriteField("meta", string(meta))
	if len(b.Zip) > 0 {
		fw, err := mw.CreateFormFile("bundle", "bundle.zip")
		if err != nil {
			return res, err
		}
		_, _ = fw.Write(b.Zip)
	}
	_ = mw.Close()
	req, err := http.NewRequest("POST", strings.TrimRight(url, "/")+"/v1/reports", &form)
	if err != nil {
		return res, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+key)
	err = call(req, &res)
	return res, err
}

// Status asks the relay for the state of a report this key filed.
func Status(url, key, id string) (string, error) {
	req, err := http.NewRequest("GET", strings.TrimRight(url, "/")+"/v1/reports/"+id, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	var out struct{ State string }
	err = call(req, &out)
	return out.State, err
}

// RelayError is an answer from the relay that says no. Retrying the same
// bundle does not help, except for 429 and 5xx.
type RelayError struct {
	Code int
	Msg  string
}

func (e *RelayError) Error() string { return fmt.Sprintf("relay: %d %s", e.Code, e.Msg) }

// Retry reports whether a later attempt can succeed.
func Retry(err error) bool {
	var re *RelayError
	return !errors.As(err, &re) || re.Code == 429 || re.Code >= 500
}

func call(req *http.Request, out any) error {
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode >= 300 {
		var e struct{ Error string }
		_ = json.Unmarshal(data, &e)
		return &RelayError{Code: resp.StatusCode, Msg: e.Error}
	}
	return json.Unmarshal(data, out)
}

// QueueDir holds bundles that could not be sent yet.
func QueueDir() string { return filepath.Join(filepath.Dir(audit.Path()), "reports") }

// PreviewDir holds bundles built with --dry-run. Flush never reads it.
func PreviewDir() string { return filepath.Join(filepath.Dir(audit.Path()), "report-previews") }

// Save queues a bundle and returns its directory.
func Save(b *Bundle) (string, error) { return SaveTo(QueueDir(), b) }

// SaveTo writes a bundle under root and returns its directory.
func SaveTo(root string, b *Bundle) (string, error) {
	dir := filepath.Join(root, time.Now().UTC().Format("20060102-150405.000000000"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	meta, _ := json.MarshalIndent(b.Meta, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), meta, 0o600); err != nil {
		return "", err
	}
	return dir, os.WriteFile(filepath.Join(dir, "bundle.zip"), b.Zip, 0o600)
}

// A flush claims a bundle by renaming it with sendingSuffix, so that two
// processes never send the same bundle. A claim older than staleClaim was
// left by a process that died while sending; the next flush takes it back.
const (
	sendingSuffix = ".sending"
	staleClaim    = 5 * time.Minute
)

// Flush sends the queued bundles, oldest first. It stops at the first
// bundle that fails for a reason a retry can fix.
func Flush(url, key string) ([]Result, error) {
	claims, _ := filepath.Glob(filepath.Join(QueueDir(), "*"+sendingSuffix))
	for _, c := range claims {
		if st, err := os.Stat(c); err == nil && time.Since(st.ModTime()) > staleClaim {
			_ = os.Rename(c, strings.TrimSuffix(c, sendingSuffix))
		}
	}
	dirs, _ := filepath.Glob(filepath.Join(QueueDir(), "*"))
	sort.Strings(dirs)
	var sent []Result
	for _, d := range dirs {
		if strings.HasSuffix(d, ".rejected") || strings.HasSuffix(d, sendingSuffix) {
			continue
		}
		claim := d + sendingSuffix
		if os.Rename(d, claim) != nil {
			continue // another flush took it
		}
		now := time.Now()
		_ = os.Chtimes(claim, now, now)
		var b Bundle
		meta, err := os.ReadFile(filepath.Join(claim, "meta.json"))
		if err != nil || json.Unmarshal(meta, &b.Meta) != nil {
			_ = os.Rename(claim, d)
			continue
		}
		b.Zip, _ = os.ReadFile(filepath.Join(claim, "bundle.zip"))
		res, err := Send(url, key, &b)
		if err != nil {
			if Retry(err) {
				_ = os.Rename(claim, d)
				return sent, err
			}
			_ = os.Rename(claim, d+".rejected")
			continue
		}
		sent = append(sent, res)
		_ = os.RemoveAll(claim)
	}
	return sent, nil
}

// Deliver sends the queued bundles, then this one, and queues it when the
// relay cannot take it now. The text says what happened.
func Deliver(url string, b *Bundle) (string, error) {
	key := Key()
	if key == "" {
		dir, err := Save(b)
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("no beta key, so the report waits in %s; run offstage report login, then offstage report flush", dir)
	}
	_, _ = Flush(url, key)
	res, err := Send(url, key, b)
	if err != nil {
		if !Retry(err) {
			return "", err
		}
		dir, serr := Save(b)
		if serr != nil {
			return "", fmt.Errorf("%v; and queueing failed: %v", err, serr)
		}
		return fmt.Sprintf("the relay did not take the report (%v); it waits in %s and goes with the next report", err, dir), nil
	}
	msg := "filed " + res.ID
	if res.Duplicate {
		msg = "known problem " + res.ID + ": this report was added to it"
	}
	if res.Warning != "" {
		msg += " (" + res.Warning + ")"
	}
	return msg, nil
}
