//go:build linux

// Package task owns task resources independently of an MCP server process.
package task

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/hexadecimil/offstage/internal/audit"
	"github.com/hexadecimil/offstage/internal/redact"
	"github.com/hexadecimil/offstage/internal/registry"
	"github.com/hexadecimil/offstage/internal/sysd"
	"golang.org/x/sys/unix"
)

type Resource struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Unit      string    `json:"unit,omitempty"`
	Screen    string    `json:"screen,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	State     string    `json:"state"`
	ExitCode  *int      `json:"exit_code,omitempty"`
	Error     string    `json:"error,omitempty"`
	Log       string    `json:"log,omitempty"`
	LogState  string    `json:"log_state,omitempty"`
}
type Task struct {
	Version   int            `json:"version"`
	ID        string         `json:"id"`
	Owner     registry.Owner `json:"owner"`
	CreatedAt time.Time      `json:"created_at"`
	ExpiresAt time.Time      `json:"expires_at,omitempty"`
	State     string         `json:"state"`
	Directory string         `json:"directory"`
	Resources []Resource     `json:"resources"`
}

var collectors sync.Map // unit -> completion channel

var valid = regexp.MustCompile(`^task-[0-9a-f]{32}$`)

func root() string { return filepath.Join(filepath.Dir(audit.Path()), "tasks") }
func id(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(b)
}
func save(t *Task) error {
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return registry.WriteAtomic(filepath.Join(t.Directory, "manifest.json"), b, 0600)
}
func load(taskID string, owner registry.Owner) (*Task, func(), error) {
	return loadContext(context.Background(), taskID, owner)
}
func loadContext(ctx context.Context, taskID string, owner registry.Owner) (*Task, func(), error) {
	if !valid.MatchString(taskID) {
		return nil, nil, fmt.Errorf("invalid task ID")
	}
	dir := filepath.Join(root(), taskID)
	f, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_RDWR, 0600)
	if err != nil {
		return nil, nil, err
	}
	for {
		if err = ctx.Err(); err != nil {
			f.Close()
			return nil, nil, err
		}
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			f.Close()
			return nil, nil, err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			f.Close()
			return nil, nil, ctx.Err()
		case <-timer.C:
		}
	}
	release := func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN); f.Close() }
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		release()
		return nil, nil, err
	}
	var t Task
	if err = json.Unmarshal(b, &t); err != nil {
		release()
		return nil, nil, err
	}
	sameSession := owner.SessionID != "" && t.Owner.SessionID == owner.SessionID
	if t.ID != taskID || (t.Owner != owner && !sameSession) {
		release()
		return nil, nil, fmt.Errorf("task owner mismatch")
	}
	t.Owner = owner
	t.Directory = dir
	return &t, release, nil
}
func Create(owner registry.Owner) (*Task, error) {
	if err := prune(); err != nil {
		return nil, err
	}
	t := &Task{Version: 1, ID: id("task-"), Owner: owner, CreatedAt: time.Now().UTC(), State: "active", Resources: []Resource{}}
	t.Directory = filepath.Join(root(), t.ID)
	if err := os.MkdirAll(t.Directory, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(t.Directory, ".lock"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	f.Close()
	if err := save(t); err != nil {
		return nil, err
	}
	return t, nil
}

// serviceState uses an unguessable unit name recorded before process creation.
// Process exit results are persisted by the launcher when available.
func serviceState(unit string) (string, *int, error) {
	return serviceStateContext(context.Background(), unit)
}
func serviceStateContext(ctx context.Context, unit string) (string, *int, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", "--user", "show", unit, "--property=LoadState,ActiveState,ExecMainCode,ExecMainStatus").Output()
	if err != nil {
		return "unknown", nil, err
	}
	fields := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		k, v, _ := strings.Cut(line, "=")
		fields[k] = v
	}
	if fields["LoadState"] == "not-found" {
		return "missing", nil, nil
	}
	var code int
	if fields["ExecMainCode"] != "0" && fields["ExecMainCode"] != "" {
		_, err = fmt.Sscan(fields["ExecMainStatus"], &code)
		if err != nil {
			return "unknown", nil, err
		}
		return "exited", &code, nil
	}
	return fields["ActiveState"], nil, nil
}
func Inspect(taskID string, owner registry.Owner) (*Task, error) {
	return InspectContext(context.Background(), taskID, owner)
}

// InspectContext permits cancellation during lock acquisition and service queries.
func InspectContext(ctx context.Context, taskID string, owner registry.Owner) (*Task, error) {
	t, release, err := loadContext(ctx, taskID, owner)
	if err != nil {
		return nil, err
	}
	defer release()
	for i := range t.Resources {
		r := &t.Resources[i]
		if r.State == "closed" {
			continue
		}
		if r.Kind == "process" {
			if _, ok := collectors.Load(r.Unit); !ok && r.LogState == "collecting" {
				r.LogState = "unavailable"
			}
			state, code, e := serviceStateContext(ctx, r.Unit)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if state != "missing" || r.ExitCode == nil {
				r.State = state
				if code != nil {
					r.ExitCode = code
				}
			}
			if e != nil {
				r.Error = "service state unavailable"
			}
		} else if r.Kind == "screen" {
			rec, e := registry.Load(r.Screen)
			if errors.Is(e, registry.ErrNotFound) || (e == nil && !rec.CreatedAt.Equal(r.CreatedAt)) {
				r.State = "closed"
			} else if e != nil {
				r.State = "unknown"
				r.Error = "screen state unavailable"
			}
		}
	}
	if err := save(t); err != nil {
		return nil, err
	}
	return t, nil
}

// logWriter persists redacted complete lines. Overlong lines are discarded.
// One MiB per log bounds both storage and collection work per resource.
type logWriter struct {
	mu        sync.Mutex
	f         *os.File
	pending   []byte
	discard   bool
	written   int
	truncated bool
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	for _, c := range p {
		if c == '\n' {
			if !w.discard {
				w.line(w.pending)
			}
			w.pending = nil
			w.discard = false
		} else if !w.discard {
			if len(w.pending) >= 16384 {
				w.pending = nil
				w.discard = true
				w.truncated = true
			} else {
				w.pending = append(w.pending, c)
			}
		}
	}
	return n, nil
}
func (w *logWriter) line(p []byte) {
	b := []byte(redact.Text(string(p)) + "\n")
	if w.written+len(b) > 1<<20 {
		w.truncated = true
		return
	}
	n, err := w.f.Write(b)
	w.written += n
	if err != nil {
		w.truncated = true
	}
}
func (w *logWriter) close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pending) > 0 && !w.discard {
		w.line(w.pending)
	}
	if w.truncated {
		_ = os.WriteFile(w.f.Name()+".incomplete", []byte("log truncated or write failed\n"), 0600)
	}
	w.f.Close()
}
func Launch(taskID string, owner registry.Owner, command []string, cwd string, env map[string]string) (*Resource, error) {
	if len(command) == 0 || command[0] == "" {
		return nil, fmt.Errorf("command must contain an executable")
	}
	if !sysd.Available() {
		return nil, fmt.Errorf("task_launch requires Linux user systemd for durable process ownership")
	}
	name := command[0]
	if strings.ContainsRune(name, os.PathSeparator) && !filepath.IsAbs(name) && cwd != "" {
		name = filepath.Join(cwd, name)
	}
	executable, err := exec.LookPath(name)
	if err != nil {
		return nil, err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return nil, err
	}
	if cwd != "" {
		st, e := os.Stat(cwd)
		if e != nil {
			return nil, e
		}
		if !st.IsDir() {
			return nil, fmt.Errorf("cwd is not a directory")
		}
	}
	t, release, err := load(taskID, owner)
	if err != nil {
		return nil, err
	}
	defer release()
	if t.State != "active" {
		return nil, fmt.Errorf("task is %s", t.State)
	}
	// 64 bounded logs keep task process artifacts below 100 MiB.
	if len(t.Resources) >= 64 {
		return nil, fmt.Errorf("task resource limit reached (64)")
	}
	r := Resource{ID: id("process-"), Kind: "process", CreatedAt: time.Now().UTC(), State: "starting"}
	r.Unit = "offstage-" + t.ID + "-" + r.ID + ".service"
	r.LogState = "collecting"
	r.Log = filepath.Join(t.Directory, r.ID+".log")
	f, err := os.OpenFile(r.Log, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	w := &logWriter{f: f}
	t.Resources = append(t.Resources, r)
	if err = save(t); err != nil {
		w.close()
		return nil, err
	}
	args := []string{"--user", "--quiet", "--pipe", "--unit=" + r.Unit, "--service-type=exec", "--property=KillMode=control-group", "--property=TimeoutStopSec=3s", "--property=StandardInput=null"}
	if cwd != "" {
		abs, e := filepath.Abs(cwd)
		if e != nil {
			w.close()
			return nil, e
		}
		args = append(args, "--working-directory="+abs)
	}
	for k, v := range env {
		if strings.ContainsAny(k, "=\x00") || k == "" || strings.ContainsRune(v, 0) {
			w.close()
			return nil, fmt.Errorf("invalid environment entry")
		}
		args = append(args, "--setenv="+k+"="+v)
	}
	if _, provided := env["PATH"]; !provided {
		args = append(args, "--setenv=PATH="+os.Getenv("PATH"))
	}
	args = append(args, "--", executable)
	args = append(args, command[1:]...)
	cmd := exec.Command("systemd-run", args...)
	cmd.Stdout = w
	cmd.Stderr = w
	if err = cmd.Start(); err != nil {
		w.close()
		t.Resources[len(t.Resources)-1].State = "failed"
		_ = save(t)
		return nil, err
	}
	finished := make(chan struct{})
	collectors.Store(r.Unit, finished)
	done := make(chan error, 1)
	resourceID, unit := r.ID, r.Unit
	go func() {
		defer close(finished)
		defer collectors.Delete(unit)
		e := cmd.Wait()
		w.close()
		done <- e
		current, unlock, le := load(taskID, owner)
		if le != nil {
			return
		}
		defer unlock()
		for i := range current.Resources {
			if current.Resources[i].ID == resourceID {
				code := cmd.ProcessState.ExitCode()
				if current.Resources[i].ExitCode == nil {
					current.Resources[i].ExitCode = &code
				}
				current.Resources[i].LogState = "complete"
				if w.truncated {
					current.Resources[i].LogState = "incomplete"
				}
				if current.Resources[i].State != "closed" {
					current.Resources[i].State = "exited"
				}
			}
		}
		_ = save(current)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		state, _, e := serviceState(r.Unit)
		if e == nil && (state == "active" || state == "exited") {
			r.State = state
			break
		}
		select {
		case <-done:
			if cmd.ProcessState.ExitCode() < 0 {
				r.State = "failed"
				t.Resources[len(t.Resources)-1] = r
				_ = save(t)
				return nil, fmt.Errorf("service launch failed; inspect %s", r.Log)
			}
			r.State = "exited"
			code := cmd.ProcessState.ExitCode()
			r.ExitCode = &code
		default:
		}
		if time.Now().After(deadline) {
			_ = sysd.StopUnit(r.Unit)
			r.State = "unknown"
			t.Resources[len(t.Resources)-1] = r
			_ = save(t)
			return nil, fmt.Errorf("service launch timed out; resource retained for cleanup")
		}
		if r.State == "exited" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Resources[len(t.Resources)-1] = r
	if err = save(t); err != nil {
		_ = sysd.StopUnit(r.Unit)
		return nil, err
	}
	return &r, nil
}

// Attach must run after validating the task and before returning a created screen.
func Attach(taskID string, owner registry.Owner, rec *registry.Screen) error {
	t, release, err := load(taskID, owner)
	if err != nil {
		return err
	}
	defer release()
	if t.State != "active" {
		return fmt.Errorf("task is %s", t.State)
	}
	if len(t.Resources) >= 64 {
		return fmt.Errorf("task resource limit reached (64)")
	}
	t.Resources = append(t.Resources, Resource{ID: id("screen-"), Kind: "screen", Screen: rec.Name, CreatedAt: rec.CreatedAt, State: "active"})
	return save(t)
}

// Close invokes screenClose only for the exact recorded screen instance.
func Close(taskID string, owner registry.Owner, screenClose func(*registry.Screen) error) error {
	t, release, err := load(taskID, owner)
	if err != nil {
		return err
	}
	defer func() {
		release()
		for _, r := range t.Resources {
			if c, ok := collectors.Load(r.Unit); ok {
				select {
				case <-c.(chan struct{}):
				case <-time.After(5 * time.Second):
				}
			}
		}
	}()
	if t.State == "closed" {
		return nil
	}
	t.State = "closing"
	if err = save(t); err != nil {
		return err
	}
	var failures []error
	for i := range t.Resources {
		r := &t.Resources[i]
		if r.State == "closed" {
			continue
		}
		var e error
		switch r.Kind {
		case "process":
			state, code, se := serviceState(r.Unit)
			if code != nil {
				r.ExitCode = code
			}
			if se != nil {
				e = se
			} else if state != "missing" {
				e = sysd.StopUnit(r.Unit)
				if e == nil {
					sysd.ResetFailed(r.Unit)
				}
				if e == nil {
					state, _, e = serviceState(r.Unit)
					if e == nil && state != "exited" && state != "inactive" && state != "missing" && state != "failed" {
						e = fmt.Errorf("service still %s", state)
					}
				}
			}
		case "screen":
			rec, re := registry.Load(r.Screen)
			if errors.Is(re, registry.ErrNotFound) {
				break
			}
			if re != nil {
				e = re
				break
			}
			if !rec.CreatedAt.Equal(r.CreatedAt) {
				break
			}
			if rec.Owner != owner {
				e = fmt.Errorf("screen owner changed")
				break
			}
			if screenClose == nil {
				e = fmt.Errorf("screen cleanup unavailable")
			} else {
				e = screenClose(rec)
			}
		default:
			e = fmt.Errorf("unknown resource kind")
		}
		if e != nil {
			r.State = "cleanup_failed"
			r.Error = "resource cleanup failed"
			failures = append(failures, fmt.Errorf("%s: %w", r.ID, e))
		} else {
			r.State = "closed"
			r.Error = ""
		}
	}
	t.State = "closed"
	if len(failures) > 0 {
		t.State = "cleanup_failed"
	} else {
		t.ExpiresAt = time.Now().UTC().Add(7 * 24 * time.Hour)
	}
	return errors.Join(errors.Join(failures...), save(t))
}

var _ io.Writer = (*logWriter)(nil)

// prune removes only expired closed task directories. It never stops resources.
func prune() error {
	entries, err := os.ReadDir(root())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !valid.MatchString(entry.Name()) {
			continue
		}
		dir := filepath.Join(root(), entry.Name())
		f, e := os.OpenFile(filepath.Join(dir, ".lock"), os.O_RDWR, 0600)
		if e != nil {
			continue
		}
		if unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) != nil {
			f.Close()
			continue
		}
		b, e := os.ReadFile(filepath.Join(dir, "manifest.json"))
		var t Task
		if e == nil && json.Unmarshal(b, &t) == nil && t.ID == entry.Name() && t.State == "closed" && !t.ExpiresAt.IsZero() && time.Now().After(t.ExpiresAt) {
			e = os.RemoveAll(dir)
		}
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		f.Close()
		if e != nil {
			return e
		}
	}
	return nil
}
