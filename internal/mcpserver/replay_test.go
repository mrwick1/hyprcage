package mcpserver

import (
	"encoding/json"
	"testing"

	"github.com/hexadecimil/offstage/internal/audit"
)

func TestFillPutsPlaceholders(t *testing.T) {
	var args map[string]any
	_ = json.Unmarshal([]byte(`{"text_len":3,"actions":[{"op":"type","text_len":2},{"op":"key","keys":["<char>","Return"]}]}`), &args)
	fill(args)
	got, _ := json.Marshal(args)
	want := `{"actions":[{"op":"type","text":"xx"},{"keys":["x","Return"],"op":"key"}],"text":"xxx"}`
	if string(got) != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
}

func TestSkipsTheHumansDesktop(t *testing.T) {
	for _, e := range []audit.Entry{{Tool: "click", Screen: "desktop"}, {Tool: "desktop_key"}, {Tool: "notify_act"}, {Tool: "report_issue"}, {Tool: "devtools_eval"}} {
		if skipReason(e) == "" {
			t.Errorf("%s on %q replays", e.Tool, e.Screen)
		}
	}
	if r := skipReason(audit.Entry{Tool: "click", Screen: "a"}); r != "" {
		t.Errorf("click on an agent screen skipped: %s", r)
	}
}

func TestOutcome(t *testing.T) {
	cases := map[string]audit.Entry{
		"stale_ref": {Error: "stale_ref", Verdict: map[string]any{"verdict": "NO_OP"}},
		"NO_OP":     {Verdict: map[string]any{"verdict": "NO_OP"}},
		"ok":        {},
	}
	for want, e := range cases {
		if got := outcome(e); got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	}
}

func TestReproducedWantsTheLastFailure(t *testing.T) {
	steps := []ReplayStep{
		{Tool: "act", Was: "stale_ref", Fail: true, Got: "ok"},
		{Tool: "click", Was: "NO_OP", Got: "CONFIRMED"},
		{Tool: "wait", Was: "timeout", Fail: true, Got: "timeout"},
	}
	if !Reproduced(steps) {
		t.Error("the last failure happened again: want reproduced")
	}
	steps[2].Got = "ok"
	if Reproduced(steps) {
		t.Error("the last failure did not happen again")
	}
	if Reproduced([]ReplayStep{{Tool: "click", Was: "ok", Got: "ok"}}) {
		t.Error("a trace with no failure cannot reproduce")
	}
}

func TestSameIgnoresAOneSidedVerdict(t *testing.T) {
	cases := []struct {
		st   ReplayStep
		same bool
	}{
		{ReplayStep{Was: "ok", Got: "CONFIRMED"}, true},
		{ReplayStep{Was: "NO_OP", Got: "CONFIRMED"}, false},
		{ReplayStep{Was: "ok", Got: "timeout", GotFail: true}, false},
		{ReplayStep{Was: "stale_ref", Fail: true, Got: "ok"}, false},
		{ReplayStep{Was: "stale_ref", Fail: true}, true}, // skipped
	}
	for _, c := range cases {
		if c.st.Same() != c.same {
			t.Errorf("%+v: Same %v", c.st, !c.same)
		}
	}
}
