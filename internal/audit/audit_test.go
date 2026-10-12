package audit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hexadecimil/hyprcage/internal/screen"
)

func lines(t *testing.T) []map[string]any {
	t.Helper()
	f, err := os.Open(Path())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("not JSON: %q", sc.Text())
		}
		out = append(out, m)
	}
	return out
}

func TestPathUsesStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/s")
	if got := Path(); got != "/s/hyprcage/actions.jsonl" {
		t.Fatalf("got %s", got)
	}
}

func TestRecordAppendsOneLinePerCall(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	type clickIn struct {
		Screen string `json:"screen"`
		X, Y   int
	}
	Record("click", clickIn{Screen: "a", X: 1, Y: 2}, nil)
	Record("key", map[string]any{"screen": "a", "keys": []string{"Return"}}, nil)
	ls := lines(t)
	if len(ls) != 2 {
		t.Fatalf("%d lines", len(ls))
	}
	if ls[0]["tool"] != "click" || ls[0]["screen"] != "a" || ls[0]["time"] == "" {
		t.Fatalf("bad line %v", ls[0])
	}
	if _, ok := ls[0]["error"]; ok {
		t.Fatal("error set on success")
	}
	if _, ok := ls[0]["verdict"]; ok {
		t.Fatal("verdict must be absent when unset")
	}
}

func TestNeverLogsTypedText(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	secret := "hunter2-SECRET"
	Record("type", map[string]any{"screen": "a", "text": secret}, nil)
	Record("batch", map[string]any{"actions": []map[string]any{{"op": "type", "text": secret}, {"op": "key", "keys": []string{"Return"}}}}, nil)
	Record("clipboard_set", map[string]any{"text": secret}, nil)
	Record("devtools_eval", map[string]any{"expression": "x='" + secret + "'"}, nil)
	raw, _ := os.ReadFile(Path())
	if strings.Contains(string(raw), secret) {
		t.Fatalf("typed text leaked: %s", raw)
	}
	if n := len(lines(t)); n != 4 {
		t.Fatalf("%d lines", n)
	}
	if !strings.Contains(string(raw), `"Return"`) {
		t.Fatal("keys must stay in the log")
	}
}

func TestFailureLogsErrorCode(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	Record("click", map[string]any{}, screen.Errf(screen.CodeNotFound, "", "no screen"))
	if got := lines(t)[0]["error"]; got != "screen_not_found" {
		t.Fatalf("got %v", got)
	}
}

func TestVerdictAndAckAreLiftedWhenPresent(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	Record("click", map[string]any{"ack": "closing the dialog"}, nil, WithVerdict("changed"))
	l := lines(t)[0]
	if l["ack"] != "closing the dialog" || l["verdict"] != "changed" {
		t.Fatalf("got %v", l)
	}
}

func TestUnwritableDirDoesNotPanic(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file")
	os.WriteFile(f, nil, 0o600)
	t.Setenv("XDG_STATE_HOME", f) // a file, so MkdirAll fails
	Record("click", map[string]any{}, nil)
}
