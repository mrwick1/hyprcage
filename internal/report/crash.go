package report

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hexadecimil/offstage/internal/audit"
	"github.com/hexadecimil/offstage/internal/session"
)

// CrashDir holds the crash files that wait for a report: crash-*.txt, and
// live-<pid>.log, where a running server writes a fatal crash.
func CrashDir() string { return filepath.Join(filepath.Dir(audit.Path()), "crashes") }

// CatchCrashes keeps the fatal crash output of this process (a panic in any
// goroutine, or a fatal runtime error) in a file. It first turns the files
// of servers that died into crash files. Call the returned function on a
// clean exit.
func CatchCrashes() func() {
	dir := CrashDir()
	if os.MkdirAll(dir, 0o700) != nil {
		return func() {}
	}
	collect(dir)
	p := filepath.Join(dir, fmt.Sprintf("live-%d.log", os.Getpid()))
	f, err := os.OpenFile(p, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return func() {}
	}
	defer f.Close() // SetCrashOutput keeps its own copy of the descriptor
	if debug.SetCrashOutput(f, debug.CrashOptions{}) != nil {
		os.Remove(p)
		return func() {}
	}
	return func() {
		_ = debug.SetCrashOutput(nil, debug.CrashOptions{})
		os.Remove(p)
	}
}

// collect renames the non-empty live file of each dead server to a crash
// file, and removes the empty ones.
func collect(dir string) {
	lives, _ := filepath.Glob(filepath.Join(dir, "live-*.log"))
	for _, p := range lives {
		pid, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(p), "live-"), ".log"))
		if err != nil || session.PIDAlive(pid, 0) {
			continue
		}
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		if fi.Size() == 0 {
			os.Remove(p)
			continue
		}
		os.Rename(p, filepath.Join(dir, fmt.Sprintf("crash-%s-%d.txt", fi.ModTime().UTC().Format("20060102-150405"), pid)))
	}
}

// RecordPanic writes a crash file for a panic that a tool handler recovered,
// and returns its path, empty when the write failed.
func RecordPanic(tool string, v any, stack []byte) string {
	dir := CrashDir()
	if os.MkdirAll(dir, 0o700) != nil {
		return ""
	}
	p := filepath.Join(dir, fmt.Sprintf("crash-%s-%d.txt", time.Now().UTC().Format("20060102-150405.000"), os.Getpid()))
	data := fmt.Sprintf("panic: %v [recovered in tool %s]\n\n%s", v, tool, stack)
	if os.WriteFile(p, []byte(data), 0o600) != nil {
		return ""
	}
	return p
}

// Crashes returns the crash files that wait for a report, oldest first.
func Crashes() []string {
	out, _ := filepath.Glob(filepath.Join(CrashDir(), "crash-*.txt"))
	sort.Strings(out)
	return out
}

// ClearCrashes removes crash files after a report took them.
func ClearCrashes(paths []string) {
	for _, p := range paths {
		os.Remove(p)
	}
}

var (
	hexArgs  = regexp.MustCompile(`\([^()]*\)$`)
	crashMsg = regexp.MustCompile(`^(panic|fatal error): `)
)

// crashFingerprint names a crash by its message and the first offstage
// frame, so the same crash from two testers merges into one report.
func crashFingerprint(text string) string {
	var msg, frame string
	for _, l := range strings.Split(text, "\n") {
		switch {
		case msg == "" && crashMsg.MatchString(l):
			msg = l
		case msg != "" && frame == "" && strings.HasPrefix(l, "github.com/hexadecimil/offstage/") && !strings.Contains(l, "report.RecordPanic") && !strings.Contains(l, "mcpserver.add"):
			frame = hexArgs.ReplaceAllString(l, "")
		}
	}
	if msg == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("crash|" + msg + "|" + frame + "|" + runtime.GOOS))
	return hex.EncodeToString(sum[:8])
}
