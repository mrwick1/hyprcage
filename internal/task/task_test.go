//go:build linux

package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hexadecimil/offstage/internal/registry"
	"github.com/hexadecimil/offstage/internal/session"
	"github.com/hexadecimil/offstage/internal/sysd"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func owner() registry.Owner { return session.Current().Owner() }
func TestLifecycle(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	a, err := Create(owner())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(a.ID, registry.Owner{PID: -1}); err == nil {
		t.Fatal("foreign owner accepted")
	}
	if _, err := Inspect("../escape", owner()); err == nil {
		t.Fatal("invalid id accepted")
	}
	if err := Close(a.ID, owner(), nil); err != nil {
		t.Fatal(err)
	}
	if err := Close(a.ID, owner(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Launch(a.ID, owner(), []string{"true"}, "", nil); err == nil {
		t.Fatal("closed task accepted launch")
	}
}
func TestOwnedProcesses(t *testing.T) {
	if !sysd.Available() {
		t.Skip("user systemd unavailable")
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	a, _ := Create(owner())
	b, _ := Create(owner())
	t.Cleanup(func() { _ = Close(a.ID, owner(), nil); _ = Close(b.ID, owner(), nil) })
	ar, err := Launch(a.ID, owner(), []string{"/bin/sh", "-c", "echo ready; sleep 120 & wait"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	br, err := Launch(b.ID, owner(), []string{"/bin/sleep", "120"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := Close(a.ID, owner(), nil); err != nil {
		t.Fatal(err)
	}
	if state, _, err := serviceState(ar.Unit); err != nil || state == "active" {
		t.Fatalf("closed service: %s %v", state, err)
	}
	if state, _, err := serviceState(br.Unit); err != nil || state != "active" {
		t.Fatalf("other task: %s %v", state, err)
	}
}
func TestExitAndLaunchFailure(t *testing.T) {
	if !sysd.Available() {
		t.Skip("user systemd unavailable")
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	a, _ := Create(owner())
	t.Cleanup(func() { _ = Close(a.ID, owner(), nil) })
	r, err := Launch(a.ID, owner(), []string{"/bin/sh", "-c", "echo hello; exit 7"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		a, err = Inspect(a.ID, owner())
		if err != nil {
			t.Fatal(err)
		}
		if a.Resources[0].ExitCode != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("exit result not recorded")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if *a.Resources[0].ExitCode != 7 {
		t.Fatal(a.Resources[0])
	}
	data, err := os.ReadFile(filepath.Join(a.Directory, r.ID+".log"))
	if err != nil || string(data) != "hello\n" {
		t.Fatalf("log %q %v", data, err)
	}
	if _, err := Launch(a.ID, owner(), []string{"/no/such/executable"}, "", nil); err == nil {
		t.Fatal("missing executable accepted")
	}
	if err := Close(a.ID, owner(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestScreenIdentityAndCleanupRetry(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	a, _ := Create(owner())
	rec := &registry.Screen{Name: "test-screen", CreatedAt: time.Now(), Owner: owner()}
	if err := registry.Save(rec); err != nil {
		t.Fatal(err)
	}
	if err := Attach(a.ID, owner(), rec); err != nil {
		t.Fatal(err)
	}
	if err := Close(a.ID, owner(), nil); err == nil {
		t.Fatal("missing cleanup accepted")
	}
	got, _ := Inspect(a.ID, owner())
	if got.State != "cleanup_failed" {
		t.Fatal(got.State)
	}
	// Reuse the same name; retry must not touch the new screen instance.
	rec.CreatedAt = rec.CreatedAt.Add(time.Second)
	if err := registry.Save(rec); err != nil {
		t.Fatal(err)
	}
	if err := Close(a.ID, owner(), func(*registry.Screen) error { t.Fatal("new screen touched"); return nil }); err != nil {
		t.Fatal(err)
	}
}
func TestLogRedactionAndBounds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "output.log")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := &logWriter{f: f}
	_, _ = w.Write([]byte("password=super"))
	_, _ = w.Write([]byte("secret123\n"))
	_, _ = w.Write([]byte(strings.Repeat("x", 17000) + "\n"))
	w.close()
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "supersecret") || !strings.Contains(string(data), "<redacted>") {
		t.Fatalf("unredacted log: %q", data)
	}
	if len(data) > 100 {
		t.Fatal("overlong line persisted")
	}
	if _, err := os.Stat(path + ".incomplete"); err != nil {
		t.Fatal(err)
	}
}
func TestRetention(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	a, _ := Create(owner())
	active, _ := Create(owner())
	if err := Close(a.ID, owner(), nil); err != nil {
		t.Fatal(err)
	}
	a, release, err := load(a.ID, owner())
	if err != nil {
		t.Fatal(err)
	}
	a.ExpiresAt = time.Now().Add(-time.Hour)
	if err := save(a); err != nil {
		t.Fatal(err)
	}
	release()
	if _, err := Create(owner()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a.Directory); !os.IsNotExist(err) {
		t.Fatal("expired directory retained", err)
	}
	if _, err := Inspect(active.ID, owner()); err != nil {
		t.Fatal("active task removed", err)
	}
}

func TestLaunchHelper(t *testing.T) {
	if os.Getenv("OFFSTAGE_TASK_HELPER") == "" {
		return
	}
	var o registry.Owner
	if err := json.Unmarshal([]byte(os.Getenv("OFFSTAGE_TASK_OWNER")), &o); err != nil {
		os.Exit(2)
	}
	if _, err := Launch(os.Getenv("OFFSTAGE_TASK_HELPER"), o, []string{"/bin/sleep", "120"}, "", nil); err != nil {
		os.Exit(3)
	}
	os.Exit(0)
}
func TestRecoveryAfterLauncherExit(t *testing.T) {
	if !sysd.Available() {
		t.Skip("user systemd unavailable")
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	a, _ := Create(owner())
	t.Cleanup(func() { _ = Close(a.ID, owner(), nil) })
	data, _ := json.Marshal(owner())
	cmd := exec.Command(os.Args[0], "-test.run=^TestLaunchHelper$")
	cmd.Env = append(os.Environ(), "OFFSTAGE_TASK_HELPER="+a.ID, "OFFSTAGE_TASK_OWNER="+string(data))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("helper: %v %s", err, out)
	}
	got, err := Inspect(a.ID, owner())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Resources) != 1 || got.Resources[0].State != "active" || got.Resources[0].LogState != "unavailable" {
		t.Fatalf("recovered: %+v", got.Resources)
	}
	if err := Close(a.ID, owner(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPHelper(t *testing.T) {
	if os.Getenv("OFFSTAGE_TASK_HTTP_HELPER") == "" {
		return
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(2)
	}
	fmt.Printf("http://%s\n", listener.Addr())
	_ = http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ready")) }))
	os.Exit(0)
}
func TestCloseReleasesPortAndPreservesExternalServer(t *testing.T) {
	if !sysd.Available() {
		t.Skip("user systemd unavailable")
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer external.Close()
	a, _ := Create(owner())
	t.Cleanup(func() { _ = Close(a.ID, owner(), nil) })
	// The HTTP listener is a child of the launched shell.
	resource, err := Launch(a.ID, owner(), []string{"/bin/sh", "-c", `"$1" -test.run=^TestHTTPHelper$ & wait`, "server", os.Args[0]}, "", map[string]string{"OFFSTAGE_TASK_HTTP_HELPER": "1"})
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 200 * time.Millisecond}
	var address string
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, _ := os.ReadFile(resource.Log)
		address = strings.TrimSpace(string(data))
		if strings.HasPrefix(address, "http://") {
			resp, e := client.Get(address)
			if e == nil {
				resp.Body.Close()
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("listener did not start", address)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := Close(a.ID, owner(), nil); err != nil {
		t.Fatal(err)
	}
	if resp, err := client.Get(address); err == nil {
		resp.Body.Close()
		t.Fatal("owned child listener survived")
	}
	resp, err := client.Get(external.URL)
	if err != nil {
		t.Fatal("external server stopped", err)
	}
	resp.Body.Close()
}

func TestInspectionCancellationWhileLocked(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	a, err := Create(owner())
	if err != nil {
		t.Fatal(err)
	}
	_, release, err := load(a.ID, owner())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := InspectContext(ctx, a.ID, owner()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("lock ignored cancellation", err)
	}
}
