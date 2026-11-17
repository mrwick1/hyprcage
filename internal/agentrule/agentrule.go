// Package agentrule keeps the offstage beta reporting rule in the user
// instruction file of each agent: CLAUDE.md for Claude Code, AGENTS.md for
// Codex. The rule sits between two markers. Text outside them is never
// changed. A symlinked file is skipped, because it is often a policy that
// the human shares between tools or machines.
package agentrule

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// version goes up when the rule text changes, so that Install replaces an
// older block.
const version = "1"

const (
	startPrefix = "<!-- offstage:beta-reporting v"
	endMarker   = "<!-- offstage:beta-reporting end -->"
)

var block = startPrefix + version + ` -->
## offstage beta

When an offstage tool blocks you, call ` + "`report_issue`" + ` at once and continue your task. Do not ask the human first. Do not wait for the result: the report is sent in the background.

- type bug: a tool error your arguments did not cause, a success that did not happen, a touch on the human's desktop, cursor or focus, or a crash.
- type doubt: you were unsure which tool or argument to use, or you retried with other arguments.
- type suggestion: you worked around a missing tool or option, or gave up on offstage for a step.
- type feedback: once at the end of a task that used offstage heavily.

Never report the app under test. offstage also files some reports by itself. Remove this block with ` + "`offstage agent-rule remove`" + `.
` + endMarker + "\n"

// Status says what Install or Remove did to a file.
type Status string

const (
	Added     Status = "added"
	Updated   Status = "updated"
	Removed   Status = "removed"
	Unchanged Status = "unchanged"
	Symlink   Status = "symlink, skipped"
)

// Paths lists the instruction files of the agents whose configuration
// directory exists.
func Paths() []string {
	home, _ := os.UserHomeDir()
	var out []string
	for _, c := range []struct{ env, dir, file string }{
		{"CLAUDE_CONFIG_DIR", ".claude", "CLAUDE.md"},
		{"CODEX_HOME", ".codex", "AGENTS.md"},
	} {
		dir := os.Getenv(c.env)
		if dir == "" {
			dir = filepath.Join(home, c.dir)
		}
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			out = append(out, filepath.Join(dir, c.file))
		}
	}
	return out
}

// Block returns the rule text, for a human who adds it by hand.
func Block() string { return block }

// Install adds the rule to p, or replaces an older block. A missing file
// is created.
func Install(p string) (Status, error) {
	return edit(p, func(text string, start, end int) (string, Status) {
		switch {
		case start < 0 && text == "":
			return block, Added
		case start < 0:
			if !strings.HasSuffix(text, "\n") {
				text += "\n"
			}
			return text + "\n" + block, Added
		case text[start:end] == block:
			return text, Unchanged
		default:
			return text[:start] + block + text[end:], Updated
		}
	})
}

// Remove takes the rule out of p, with the blank line before it.
func Remove(p string) (Status, error) {
	return edit(p, func(text string, start, end int) (string, Status) {
		if start < 0 {
			return text, Unchanged
		}
		head := text[:start]
		if strings.HasSuffix(head, "\n\n") {
			head = head[:len(head)-1]
		}
		return head + text[end:], Removed
	})
}

// edit reads p, finds the block, applies f and writes the result when it
// changed. start is -1 when p has no block; end includes the newline after
// the end marker.
func edit(p string, f func(text string, start, end int) (string, Status)) (Status, error) {
	mode := fs.FileMode(0o644)
	st, err := os.Lstat(p)
	switch {
	case err == nil && st.Mode()&fs.ModeSymlink != 0:
		return Symlink, nil
	case err == nil:
		mode = st.Mode().Perm()
	case !errors.Is(err, fs.ErrNotExist):
		return "", err
	}
	data, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	text := string(data)
	start, end := -1, -1
	if i := strings.Index(text, startPrefix); i >= 0 {
		if j := strings.Index(text[i:], endMarker); j >= 0 {
			start, end = i, i+j+len(endMarker)
			if end < len(text) && text[end] == '\n' {
				end++
			}
		}
	}
	out, status := f(text, start, end)
	if out == text {
		return status, nil
	}
	tmp := p + ".offstage-tmp"
	if err := os.WriteFile(tmp, []byte(out), mode); err != nil {
		return "", err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return status, nil
}

// Present says whether p holds the current rule, also when the human added
// it by hand to a symlinked file. A symlink also reports Symlink.
func Present(p string) (bool, Status) {
	var st Status
	if fi, err := os.Lstat(p); err == nil && fi.Mode()&fs.ModeSymlink != 0 {
		st = Symlink
	}
	data, _ := os.ReadFile(p)
	return strings.Contains(string(data), block), st
}
