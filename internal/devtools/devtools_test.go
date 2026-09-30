package devtools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hexadecimil/hyprcage/internal/cdp"
	"github.com/hexadecimil/hyprcage/internal/screen"
)

// fake answers commands from reply and records them. Its onCall runs
// before the reply, the way the browser sends events before a result.
type fake struct {
	mu     sync.Mutex
	calls  []string
	reply  func(method string, params any) (any, error)
	onCall func(method string)
	done   chan struct{}
}

func (f *fake) Call(ctx context.Context, sid, method string, params any) (json.RawMessage, error) {
	return f.CallFor(ctx, time.Second, sid, method, params)
}

func (f *fake) CallFor(_ context.Context, _ time.Duration, sid, method string, params any) (json.RawMessage, error) {
	f.mu.Lock()
	f.calls = append(f.calls, sid+" "+method)
	f.mu.Unlock()
	if f.onCall != nil {
		f.onCall(method)
	}
	var out any = map[string]any{}
	if f.reply != nil {
		r, err := f.reply(method, params)
		if err != nil {
			return nil, err
		}
		if r != nil {
			out = r
		}
	}
	return json.Marshal(out)
}

func (f *fake) Done() <-chan struct{} { return f.done }
func (f *fake) Close() error          { return nil }

func newFake() (*Session, *fake) {
	f := &fake{done: make(chan struct{})}
	return &Session{c: f, pages: map[string]target{}}, f
}

func ev(s *Session, sid, method, params string) {
	s.event(cdp.Event{Method: method, SessionID: sid, Params: json.RawMessage(params)})
}

func attach(s *Session, sid, id, url string) {
	ev(s, "", "Target.attachedToTarget", `{"sessionId":"`+sid+`","targetInfo":{"targetId":"`+id+`","type":"page","url":"`+url+`"}}`)
}

func TestConsoleCollectsMessagesExceptionsAndLogEntries(t *testing.T) {
	s, _ := newFake()
	attach(s, "S1", "T1", "https://a.test/")
	ev(s, "S1", "Runtime.consoleAPICalled", `{"type":"log","args":[{"type":"string","value":"hi"},{"type":"number","value":2},{"type":"object","description":"Object"},{"type":"undefined"}],"stackTrace":{"callFrames":[{"url":"https://a.test/app.js","lineNumber":9}]}}`)
	ev(s, "S1", "Runtime.exceptionThrown", `{"exceptionDetails":{"text":"Uncaught","url":"https://a.test/app.js","lineNumber":0,"exception":{"type":"object","description":"TypeError: x is not a function"}}}`)
	ev(s, "S1", "Log.entryAdded", `{"entry":{"level":"error","text":"Failed to load resource: 404","url":"https://a.test/x.png"}}`)

	got, dropped := s.Console(false)
	want := []Msg{
		{Level: "log", Text: "hi 2 Object undefined", Source: "https://a.test/app.js:10", Page: "https://a.test/"},
		{Level: "exception", Text: "TypeError: x is not a function", Source: "https://a.test/app.js:1", Page: "https://a.test/"},
		{Level: "error", Text: "Failed to load resource: 404", Source: "https://a.test/x.png", Page: "https://a.test/"},
	}
	if dropped != 0 || len(got) != len(want) {
		t.Fatalf("got %+v dropped %d", got, dropped)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("msg %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
	if got, _ := s.Console(true); len(got) != 3 {
		t.Fatalf("clear read: %d", len(got))
	}
	if got, _ := s.Console(false); len(got) != 0 {
		t.Fatalf("after clear: %+v", got)
	}
}

func TestConsoleDropsOldestPastTheCap(t *testing.T) {
	s, _ := newFake()
	for i := 0; i < MaxConsole+5; i++ {
		ev(s, "S1", "Runtime.consoleAPICalled", `{"type":"log","args":[{"type":"number","value":`+itoa(i)+`}]}`)
	}
	got, dropped := s.Console(false)
	if len(got) != MaxConsole || dropped != 5 || got[0].Text != "5" {
		t.Fatalf("len %d dropped %d first %q", len(got), dropped, got[0].Text)
	}
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }

func TestPageSelection(t *testing.T) {
	s, _ := newFake()
	if _, err := s.page(""); codeOf(err) != screen.CodeCDP {
		t.Fatalf("no page: %v", err)
	}
	attach(s, "S1", "T1", "https://a.test/")
	if sid, err := s.page(""); err != nil || sid != "S1" {
		t.Fatalf("one page: %q %v", sid, err)
	}
	attach(s, "S2", "T2", "https://b.test/")
	if _, err := s.page(""); codeOf(err) != screen.CodeUnsupported || !strings.Contains(err.Error(), "b.test") {
		t.Fatalf("two pages, no match: %v", err)
	}
	if sid, _ := s.page("b.test"); sid != "S2" {
		t.Fatalf("match: %q", sid)
	}
	ev(s, "", "Target.targetInfoChanged", `{"targetInfo":{"targetId":"T2","url":"https://c.test/"}}`)
	if sid, _ := s.page("c.test"); sid != "S2" {
		t.Fatalf("navigated: %q", sid)
	}
	ev(s, "", "Target.detachedFromTarget", `{"sessionId":"S2"}`)
	if sid, err := s.page(""); err != nil || sid != "S1" {
		t.Fatalf("after detach: %q %v", sid, err)
	}
}

func TestEval(t *testing.T) {
	s, f := newFake()
	f.reply = func(method string, params any) (any, error) {
		if method != "Runtime.evaluate" {
			return nil, nil
		}
		switch params.(map[string]any)["expression"] {
		case "1+1":
			return map[string]any{"result": map[string]any{"type": "number", "value": 2}}, nil
		case "undefined":
			return map[string]any{"result": map[string]any{"type": "undefined"}}, nil
		case "NaN":
			return map[string]any{"result": map[string]any{"type": "number", "unserializableValue": "NaN"}}, nil
		default:
			return map[string]any{"result": map[string]any{"type": "object"},
				"exceptionDetails": map[string]any{"text": "Uncaught", "exception": map[string]any{"description": "ReferenceError: nope is not defined"}}}, nil
		}
	}
	attach(s, "S1", "T1", "https://a.test/")
	for expr, want := range map[string]string{"1+1": "2", "undefined": `"undefined"`, "NaN": `"NaN"`} {
		got, err := s.Eval(context.Background(), "", expr, time.Second)
		if err != nil || string(got) != want {
			t.Errorf("%s: got %s %v, want %s", expr, got, err, want)
		}
	}
	if _, err := s.Eval(context.Background(), "", "nope", time.Second); err == nil || !strings.Contains(err.Error(), "ReferenceError") {
		t.Fatalf("exception: %v", err)
	}
}

func TestHeapSnapshotWritesChunks(t *testing.T) {
	s, f := newFake()
	f.onCall = func(method string) {
		if method == "HeapProfiler.takeHeapSnapshot" {
			ev(s, "S1", "HeapProfiler.addHeapSnapshotChunk", `{"chunk":"{\"snapshot\":"}`)
			ev(s, "S1", "HeapProfiler.addHeapSnapshotChunk", `{"chunk":"1}"}`)
		}
	}
	attach(s, "S1", "T1", "https://a.test/")
	path := filepath.Join(t.TempDir(), "sub", "h.heapsnapshot")
	n, err := s.HeapSnapshot(context.Background(), "", path)
	data, _ := os.ReadFile(path)
	if err != nil || n != 14 || string(data) != `{"snapshot":1}` {
		t.Fatalf("n %d err %v data %q", n, err, data)
	}
	// A chunk after the snapshot has nowhere to go.
	ev(s, "S1", "HeapProfiler.addHeapSnapshotChunk", `{"chunk":"late"}`)
}

func TestTraceStartStop(t *testing.T) {
	s, f := newFake()
	reads := []map[string]any{
		{"data": base64.StdEncoding.EncodeToString([]byte(`{"traceEvents":[`)), "base64Encoded": true, "eof": false},
		{"data": `]}`, "eof": true},
	}
	f.onCall = func(method string) {
		if method == "Tracing.end" {
			ev(s, "", "Tracing.tracingComplete", `{"stream":"H1"}`)
		}
	}
	f.reply = func(method string, _ any) (any, error) {
		if method == "IO.read" {
			r := reads[0]
			reads = reads[1:]
			return r, nil
		}
		return nil, nil
	}
	path := filepath.Join(t.TempDir(), "t.json")
	if _, err := s.StopTrace(context.Background(), path); codeOf(err) != screen.CodeUnsupported {
		t.Fatalf("stop before start: %v", err)
	}
	if err := s.StartTrace(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.StartTrace(context.Background()); codeOf(err) != screen.CodeLimit {
		t.Fatalf("second start: %v", err)
	}
	n, err := s.StopTrace(context.Background(), path)
	data, _ := os.ReadFile(path)
	if err != nil || n != int64(len(`{"traceEvents":[]}`)) || string(data) != `{"traceEvents":[]}` {
		t.Fatalf("n %d err %v data %q", n, err, data)
	}
	if s.Tracing() {
		t.Fatal("still tracing after stop")
	}
	if last := f.calls[len(f.calls)-1]; last != " IO.close" {
		t.Fatalf("last call %q", last)
	}
}

func codeOf(err error) screen.Code {
	if se, ok := err.(*screen.Error); ok {
		return se.Code
	}
	return ""
}
