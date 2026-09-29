package record

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/hexadecimil/hyprcage/internal/config"
	"github.com/hexadecimil/hyprcage/internal/desktop"
	"github.com/hexadecimil/hyprcage/internal/registry"
	"github.com/hexadecimil/hyprcage/internal/screen"
	"github.com/hexadecimil/hyprcage/internal/wl"
)

// Desktop is the target name of the human's own screen.
const Desktop = "desktop"

// lockedFn reports whether the human's session is locked.
var lockedFn = func() bool { return desktop.Locked("/proc") }

// State is the record of one running recorder.
type State struct {
	Target  string    `json:"target"`
	PID     int       `json:"pid"`
	Path    string    `json:"path"`
	Started time.Time `json:"started"`
}

// StatePath is the state file of the recorder of target. It lives in a
// rec/ subdirectory, so that registry.List never reads it as a screen.
func StatePath(target string) string {
	return filepath.Join(registry.Dir(), "rec", target+".json")
}

func save(s State) error {
	if _, err := registry.EnsureDir(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(StatePath(s.Target)), 0o700); err != nil {
		return err
	}
	data, _ := json.Marshal(s)
	return os.WriteFile(StatePath(s.Target), data, 0o600)
}

func alive(pid int) bool { return pid > 0 && syscall.Kill(pid, 0) == nil }

// Load returns the state of target's recorder, or not_recording.
func Load(target string) (State, error) {
	var s State
	data, err := os.ReadFile(StatePath(target))
	if err == nil {
		err = json.Unmarshal(data, &s)
	}
	if err != nil || !alive(s.PID) {
		_ = os.Remove(StatePath(target))
		return State{}, screen.Errf(screen.CodeNotRecording, "record_start first", "no recording of %s", target)
	}
	return s, nil
}

// List returns the live recorders and removes the files of dead ones.
func List() ([]State, error) {
	files, err := filepath.Glob(filepath.Join(registry.Dir(), "rec", "*.json"))
	if err != nil {
		return nil, err
	}
	out := []State{}
	for _, f := range files {
		target := strings.TrimSuffix(filepath.Base(f), ".json")
		if s, err := Load(target); err == nil {
			out = append(out, s)
		}
	}
	return out, nil
}

// Start runs `exe _record target out` detached and waits for its state
// file. A screen's recorder carries HYPRCAGE_SCREEN, so destroying the
// screen sends it SIGTERM and the file is finalised.
func Start(exe, target string, isScreen bool, cfg config.Config) (State, error) {
	if s, err := Load(target); err == nil {
		return s, screen.Errf(screen.CodeRecording, "record_stop first", "%s is already recording to %s", target, s.Path)
	}
	if !isScreen && lockedFn() {
		return State{}, screen.Errf(screen.CodeLocked, "wait until the human unlocks", "the desktop is locked")
	}
	dir := config.ExpandHome(cfg.RecordDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return State{}, err
	}
	out := filepath.Join(dir, fmt.Sprintf("%s-%s.mp4", target, time.Now().Format("20060102-150405")))
	cmd := exec.Command(exe, "_record", target, out)
	cmd.Env = os.Environ()
	if isScreen {
		cmd.Env = append(cmd.Env, "HYPRCAGE_SCREEN="+target)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	// The child's own errors go to its log; O_APPEND keeps them and
	// ffmpeg's output, which RunChild appends to the same file.
	logPath := filepath.Join(screen.LogDir(), "record-"+target+".log")
	if err := os.MkdirAll(screen.LogDir(), 0o700); err != nil {
		return State{}, err
	}
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_APPEND, 0o600)
	if err != nil {
		return State{}, err
	}
	cmd.Stdout, cmd.Stderr = logf, logf
	err = cmd.Start()
	logf.Close()
	if err != nil {
		return State{}, err
	}
	go func() { _ = cmd.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s, err := Load(target); err == nil {
			return s, nil
		}
		if !alive(cmd.Process.Pid) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	return State{}, screen.Errf(screen.CodeCapture, "see "+logPath, "the recorder of %s did not start", target)
}

// Stop sends SIGTERM to target's recorder and waits until ffmpeg has
// written the file.
func Stop(target string) (State, error) {
	s, err := Load(target)
	if err != nil {
		return s, err
	}
	_ = syscall.Kill(s.PID, syscall.SIGTERM)
	for i := 0; i < 150 && alive(s.PID); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if alive(s.PID) {
		return s, screen.Errf(screen.CodeTimeout, "", "the recorder of %s did not stop within 15 s", target)
	}
	return s, nil
}

// RunChild is the body of `hyprcage _record`: capture display, pipe the
// frames to ffmpeg, stop on ctx, max or a capture error.
func RunChild(ctx context.Context, target, display, out string, fps int, max time.Duration) error {
	_ = os.MkdirAll(screen.LogDir(), 0o700)
	cl, err := wl.ConnectCapture(display)
	if err != nil {
		return err
	}
	defer cl.Close()
	// ponytail: the capture connection binds the first wl_output, so on a
	// desktop with several monitors only one is recorded; add an output
	// choice when a second monitor is in use.
	first, err := cl.Capture(true)
	if err != nil {
		return err
	}
	encs, _ := exec.Command("ffmpeg", "-hide_banner", "-encoders").Output()
	b := first.Bounds()
	argv := FFmpegArgs(b.Dx(), b.Dy(), fps, Encoder(string(encs)), out)
	ff := exec.Command(argv[0], argv[1:]...)
	// ffmpeg must not carry HYPRCAGE_SCREEN: destroy signals the recorder,
	// which closes ffmpeg's input so that the file is finalised.
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "HYPRCAGE_SCREEN=") {
			ff.Env = append(ff.Env, kv)
		}
	}
	logf, _ := os.OpenFile(filepath.Join(screen.LogDir(), "record-"+target+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if logf != nil {
		defer logf.Close()
		ff.Stdout, ff.Stderr = logf, logf
	}
	stdin, err := ff.StdinPipe()
	if err != nil {
		return err
	}
	if err := ff.Start(); err != nil {
		return err
	}
	if err := save(State{Target: target, PID: os.Getpid(), Path: out, Started: time.Now()}); err != nil {
		_ = ff.Process.Kill()
		return err
	}
	defer os.Remove(StatePath(target))
	_, loopErr := Loop(ctx, cl, stdin, fps, max, first)
	_ = stdin.Close()
	ffErr := ff.Wait()
	return errors.Join(ignoreClosedPipe(loopErr), ffErr)
}

func ignoreClosedPipe(err error) error {
	if errors.Is(err, io.ErrClosedPipe) || errors.Is(err, syscall.EPIPE) {
		return nil
	}
	return err
}
