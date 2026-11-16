package mcpserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hexadecimil/hyprcage/internal/config"
	"github.com/hexadecimil/hyprcage/internal/proc"
	"github.com/hexadecimil/hyprcage/internal/record"
	"github.com/hexadecimil/hyprcage/internal/registry"
	"github.com/hexadecimil/hyprcage/internal/screen"
)

const motionPage = `<!doctype html><title>m</title><body style="margin:0;background:#fff">
<style>@keyframes m{from{left:0}to{left:1000px}}</style>
<div style="position:absolute;top:300px;width:200px;height:200px;background:#c00;animation:m 1s linear infinite alternate"></div>`

// recordSetup makes a headless screen that shows motionPage, and a server
// whose recorder is the hyprcage binary, at 20 frames per second.
func recordSetup(t *testing.T) (cs *mcp.ClientSession, rec *registry.Screen) {
	t.Helper()
	requireChrome(t)
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip("no " + tool)
		}
	}
	// The recorder is `hyprcage _record`, and the test binary is not it.
	exe := filepath.Join(t.TempDir(), "hyprcage")
	if out, err := exec.Command("go", "build", "-o", exe, "github.com/hexadecimil/hyprcage/cmd/hyprcage").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	old := executable
	executable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { executable = old })

	// The recorder reads its frame rate from the config file.
	recDir, cfgDir := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	if err := os.MkdirAll(filepath.Join(cfgDir, "hyprcage"), 0o700); err != nil {
		t.Fatal(err)
	}
	conf := fmt.Sprintf("[record]\ndir = %q\nfps = 20\n", recDir)
	if err := os.WriteFile(filepath.Join(cfgDir, "hyprcage", "config.toml"), []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
	_, cs = guardServer(t, func(c *config.Config) { c.RecordDir, c.RecordFPS = recDir, 20 })
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, motionPage) }))
	t.Cleanup(web.Close)
	rec = createHeadlessScreen(t, cs)
	mustCall(t, cs, "browser_open", map[string]any{"url": web.URL})

	return cs, rec
}

func TestRecordHeadlessScreen(t *testing.T) {
	cs, rec := recordSetup(t)
	var started struct{ Path, Crop string }
	if err := json.Unmarshal([]byte(text(mustCall(t, cs, "record_start", map[string]any{}))), &started); err != nil {
		t.Fatal(err)
	}
	if started.Crop != "none" {
		t.Errorf("crop = %q, want none", started.Crop)
	}
	if res := hcall(t, cs, "record_start", map[string]any{}); !res.IsError || !strings.Contains(text(res), "recording_active") {
		t.Errorf("second record_start: %s", text(res))
	}
	begin := time.Now()
	time.Sleep(3 * time.Second)
	waited := time.Since(begin).Seconds()
	mustCall(t, cs, "record_stop", map[string]any{})

	out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-count_frames",
		"-show_entries", "stream=width,height,r_frame_rate,nb_read_frames:format=duration", "-of", "json", started.Path).Output()
	if err != nil {
		t.Fatalf("ffprobe %s: %v", started.Path, err)
	}
	var p struct {
		Streams []struct {
			Width, Height int
			RFrameRate    string `json:"r_frame_rate"`
			Frames        string `json:"nb_read_frames"`
		}
		Format struct{ Duration string }
	}
	if err := json.Unmarshal(out, &p); err != nil || len(p.Streams) != 1 {
		t.Fatalf("ffprobe output %s: %v", out, err)
	}
	dur, _ := strconv.ParseFloat(p.Format.Duration, 64)
	st := p.Streams[0]
	t.Logf("waited %.2f s; video %.2f s, %dx%d, %s fps, %s frames", waited, dur, st.Width, st.Height, st.RFrameRate, st.Frames)
	if dur < waited-0.5 || dur > waited+0.5 {
		t.Errorf("duration %.2f s, waited %.2f s", dur, waited)
	}
	if st.RFrameRate != "20/1" {
		t.Errorf("frame rate %s, want 20/1", st.RFrameRate)
	}
	if st.Width != rec.Width || st.Height != rec.Height {
		t.Errorf("size %dx%d, screen %dx%d", st.Width, st.Height, rec.Width, rec.Height)
	}

	// The recorder detached: Chrome still runs and answers.
	if proc.Kill(rec.DebugPID, 0) != nil {
		t.Fatal("record_stop killed the screen's Chrome")
	}
	mustCall(t, cs, "snapshot", nil)
	if _, err := record.Load(rec.Name); err == nil {
		t.Error("the recorder still runs after record_stop")
	}

	mustCall(t, cs, "screen_destroy", map[string]any{"screen": rec.Name})
	if _, err := registry.Load(rec.Name); err != registry.ErrNotFound {
		t.Errorf("record after destroy: %v", err)
	}
	if left := chromeProcs(screen.ProfilePrefix(rec.Name)); len(left) > 0 {
		t.Errorf("Chrome processes left: %v", left)
	}
}

// Destroying the screen while it records ends the recorder, and the file
// stays readable.
func TestDestroyHeadlessScreenWhileRecording(t *testing.T) {
	cs, rec := recordSetup(t)
	var started struct{ Path string }
	if err := json.Unmarshal([]byte(text(mustCall(t, cs, "record_start", map[string]any{}))), &started); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)
	mustCall(t, cs, "screen_destroy", map[string]any{"screen": rec.Name})
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := record.Load(rec.Name); err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the recorder still runs 10 s after the screen was destroyed")
		}
		time.Sleep(100 * time.Millisecond)
	}
	out, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", started.Path).Output()
	if err != nil {
		t.Fatalf("ffprobe: %v", err)
	}
	dur, _ := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	t.Logf("video %.2f s after destroy at 1.5 s", dur)
	if dur < 1 {
		t.Errorf("duration %.2f s", dur)
	}
}
