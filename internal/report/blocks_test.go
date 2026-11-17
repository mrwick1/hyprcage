package report

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestLogBlockRedactsAndTruncates(t *testing.T) {
	home(t)
	msg := "open /home/joel/app at http://app.local/x?tok=1 failed: token=abcdefghij " + strings.Repeat("x", 2*blockMessageMax)
	if err := LogBlock(Block{Tool: "click", Code: "capture_failed", Message: msg}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(BlocksPath())
	if err != nil {
		t.Fatal(err)
	}
	var b Block
	if err := json.Unmarshal(data, &b); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.Message, "joel") || strings.Contains(b.Message, "abcdefghij") || strings.Contains(b.Message, "tok=1") {
		t.Errorf("not redacted: %s", b.Message)
	}
	if len(b.Message) > blockMessageMax+len("…") {
		t.Errorf("message has %d bytes", len(b.Message))
	}
	if b.Time == "" || b.Tool != "click" || b.Code != "capture_failed" {
		t.Errorf("block %+v", b)
	}
}

func TestLogBlockRotatesAtTheLimit(t *testing.T) {
	home(t)
	if err := os.MkdirAll(QueueDir()+"/..", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(BlocksPath(), make([]byte, blocksMax), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := LogBlock(Block{Tool: "act", Code: "internal"}); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(BlocksPath() + ".1"); err != nil || st.Size() != blocksMax {
		t.Errorf("old log not kept: %v", err)
	}
	if data, _ := os.ReadFile(BlocksPath()); !strings.Contains(string(data), `"tool":"act"`) || len(data) > 1024 {
		t.Errorf("new log: %d bytes", len(data))
	}
}
