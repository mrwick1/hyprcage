package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hexadecimil/offstage/internal/audit"
	"github.com/hexadecimil/offstage/internal/redact"
)

// Block is one failed tool call in the block log. The log stays on this
// machine; automatic reports read it.
type Block struct {
	Time     string `json:"time"`
	Session  string `json:"session,omitempty"`
	Client   string `json:"client,omitempty"`
	ActionID string `json:"action_id,omitempty"`
	Tool     string `json:"tool"`
	Screen   string `json:"screen,omitempty"`
	Code     string `json:"code,omitempty"`
	Message  string `json:"message,omitempty"`
}

const (
	blockMessageMax = 500
	blocksMax       = 5 << 20 // the log moves to .1 beyond this size
)

// BlocksPath is the block log, beside the action log.
func BlocksPath() string { return filepath.Join(filepath.Dir(audit.Path()), "blocks.jsonl") }

// LogBlock appends b to the block log. The message is redacted and cut
// before it is written.
func LogBlock(b Block) error {
	if b.Time == "" {
		b.Time = time.Now().UTC().Format(time.RFC3339Nano)
	}
	b.Message = redact.Text(b.Message)
	if len(b.Message) > blockMessageMax {
		b.Message = strings.ToValidUTF8(b.Message[:blockMessageMax], "") + "…"
	}
	line, err := json.Marshal(b)
	if err != nil {
		return err
	}
	p := BlocksPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	if st, err := os.Stat(p); err == nil && st.Size() >= blocksMax {
		_ = os.Rename(p, p+".1")
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}
