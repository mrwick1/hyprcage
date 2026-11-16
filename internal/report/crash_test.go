package report

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const stackA = `panic: runtime error: index out of range [3] with length 2 [recovered in tool click]

goroutine 7 [running]:
runtime/debug.Stack()
github.com/hexadecimil/offstage/internal/mcpserver.add[...].func1.1()
panic({0x9a4b60?, 0xc000012345?})
github.com/hexadecimil/offstage/internal/screen.Click(0xc0001a2000, 0x3)
	/build/internal/screen/input.go:42 +0x1d
`

func TestCrashFingerprintIgnoresAddresses(t *testing.T) {
	b := strings.ReplaceAll(stackA, "0xc0001a2000", "0xc000777000")
	if crashFingerprint(stackA) == "" || crashFingerprint(stackA) != crashFingerprint(b) {
		t.Fatal("the same crash at other addresses must give the same fingerprint")
	}
	c := strings.Replace(stackA, "screen.Click", "screen.Drag", 1)
	if crashFingerprint(stackA) == crashFingerprint(c) {
		t.Fatal("a crash in another function must give another fingerprint")
	}
	if crashFingerprint("no panic here") != "" {
		t.Fatal("text with no panic has no fingerprint")
	}
}

func TestCollectKeepsOnlyCrashesOfDeadServers(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := CrashDir()
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "live-999999999.log"), []byte("fatal error: concurrent map writes\n"), 0o600)
	os.WriteFile(filepath.Join(dir, "live-999999998.log"), nil, 0o600)
	own := filepath.Join(dir, "live-"+itoa(os.Getpid())+".log")
	os.WriteFile(own, []byte("x"), 0o600)
	collect(dir)
	if got := Crashes(); len(got) != 1 || !strings.HasSuffix(got[0], "-999999999.txt") {
		t.Fatalf("crashes %v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "live-999999998.log")); err == nil {
		t.Error("an empty live file of a dead server stays")
	}
	if _, err := os.Stat(own); err != nil {
		t.Error("the live file of a running server was moved")
	}
}

func TestCrashReportCarriesRedactedCrashFiles(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	p := RecordPanic("click", "boom in /home/someone/x", []byte(stackA))
	if p == "" {
		t.Fatal("no crash file")
	}
	b, err := Build(Request{Title: "server crashed", Area: "crash"})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Crashes) != 1 || b.Meta.Fingerprint != crashFingerprint("panic: boom in /home/someone/x [recovered in tool click]\n\n"+stackA) {
		t.Fatalf("crashes %v, fingerprint %s", b.Crashes, b.Meta.Fingerprint)
	}
	zr, _ := zip.NewReader(bytes.NewReader(b.Zip), int64(len(b.Zip)))
	found := false
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "crashes/") {
			rc, _ := f.Open()
			data, _ := io.ReadAll(rc)
			rc.Close()
			found = true
			if strings.Contains(string(data), "/home/someone") {
				t.Errorf("crash file not redacted: %s", data)
			}
		}
	}
	if !found {
		t.Fatal("no crash file in the bundle")
	}
	if other, _ := Build(Request{Title: "slow click"}); len(other.Crashes) != 0 {
		t.Error("a report with another area took the crash files")
	}
	ClearCrashes(b.Crashes)
	if len(Crashes()) != 0 {
		t.Error("crash files stay after ClearCrashes")
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestFatalCrashOfAServerBecomesACrashFile(t *testing.T) {
	if os.Getenv("OFFSTAGE_CRASH_CHILD") == "1" {
		CatchCrashes()
		done := make(chan struct{})
		go func() { panic("child goroutine died") }()
		<-done
		return
	}
	state := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestFatalCrashOfAServerBecomesACrashFile$")
	cmd.Env = append(os.Environ(), "OFFSTAGE_CRASH_CHILD=1", "XDG_STATE_HOME="+state)
	if err := cmd.Run(); err == nil {
		t.Fatal("the child did not crash")
	}
	t.Setenv("XDG_STATE_HOME", state)
	collect(CrashDir())
	got := Crashes()
	if len(got) != 1 {
		t.Fatalf("crash files %v", got)
	}
	data, _ := os.ReadFile(got[0])
	if !strings.Contains(string(data), "panic: child goroutine died") {
		t.Fatalf("crash file: %s", data)
	}
}
