package cli

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hexadecimil/offstage/internal/audit"
	"github.com/hexadecimil/offstage/internal/mcpserver"
)

const replayUsage = `usage: offstage replay [--pace D] [--keep] <bundle.zip | bundle dir | trace.jsonl>`

func runReplay(e *Env) int {
	fs := e.flags("replay")
	pace := fs.Duration("pace", 2*time.Second, "longest pause kept between two steps")
	keep := fs.Bool("keep", false, "keep the screens the replay created")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	if fs.NArg() != 1 {
		return e.errorf("%s", replayUsage)
	}
	trace, err := readTrace(fs.Arg(0))
	if err != nil {
		return e.errorf("%v", err)
	}
	steps, err := mcpserver.Replay(context.Background(), trace, mcpserver.ReplayOptions{Pace: *pace, Keep: *keep})
	for _, st := range steps {
		mark := "  "
		if !st.Same() {
			mark = "!="
		}
		got := st.Got
		if got == "" {
			got = "skipped"
		}
		line := fmt.Sprintf("%3d %s %-16s was %-12s got %s", st.N, mark, st.Tool, st.Was, got)
		if st.Note != "" {
			line += "  (" + st.Note + ")"
		}
		fmt.Fprintln(e.Stdout, line)
	}
	if err != nil {
		return e.errorf("%v", err)
	}
	if mcpserver.Reproduced(steps) {
		fmt.Fprintln(e.Stdout, "reproduced")
	} else {
		fmt.Fprintln(e.Stdout, "needs-repro: the last failure of the trace did not happen again")
	}
	return ExitOK
}

// readTrace reads trace.jsonl from a report bundle: a zip, a directory of
// the queue that holds bundle.zip, or an unpacked bundle. Any other file is
// read as the trace itself.
func readTrace(path string) ([]audit.Entry, error) {
	if zp := filepath.Join(path, "bundle.zip"); fileExists(zp) {
		path = zp
	}
	var data []byte
	fi, err := os.Stat(path)
	switch {
	case err != nil:
		return nil, err
	case fi.IsDir():
		data, err = os.ReadFile(filepath.Join(path, "trace.jsonl"))
	case strings.HasSuffix(path, ".zip"):
		var zr *zip.ReadCloser
		if zr, err = zip.OpenReader(path); err != nil {
			return nil, err
		}
		defer zr.Close()
		var rc io.ReadCloser
		if rc, err = zr.Open("trace.jsonl"); err != nil {
			return nil, err
		}
		defer rc.Close()
		data, err = io.ReadAll(rc)
	default:
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}
	var out []audit.Entry
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var en audit.Entry
		if err := json.Unmarshal(sc.Bytes(), &en); err != nil {
			return nil, fmt.Errorf("%s: line %d: %v", path, len(out)+1, err)
		}
		out = append(out, en)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: the trace is empty", path)
	}
	return out, sc.Err()
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
