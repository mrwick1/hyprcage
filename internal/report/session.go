package report

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"

	"github.com/hexadecimil/offstage/internal/audit"
)

const (
	sessionTraceMax  = 500
	sessionBlocksMax = 200
)

// sessionTrace returns the last calls of one agent session, in order.
func sessionTrace(entries []audit.Entry, id string) []audit.Entry {
	if id == "" {
		return nil
	}
	var out []audit.Entry
	for _, e := range entries {
		if e.Session == id {
			out = append(out, e)
		}
	}
	return out[max(0, len(out)-sessionTraceMax):]
}

// sessionBlocks returns the last block log lines of one agent session.
func sessionBlocks(path, id string) []byte {
	if id == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var lines [][]byte
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var b Block
		if json.Unmarshal(sc.Bytes(), &b) == nil && b.Session == id {
			lines = append(lines, append([]byte(nil), sc.Bytes()...))
		}
	}
	if len(lines) == 0 {
		return nil
	}
	lines = lines[max(0, len(lines)-sessionBlocksMax):]
	return append(bytes.Join(lines, []byte("\n")), '\n')
}
