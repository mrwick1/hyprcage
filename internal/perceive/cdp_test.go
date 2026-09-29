package perceive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"github.com/hexadecimil/hyprcage/internal/screen"
)

// fakeCaller answers from the recorded VS Code fixtures.
type fakeCaller struct {
	tree  json.RawMessage
	boxes map[string]json.RawMessage
}

func newFake(t *testing.T) *fakeCaller {
	t.Helper()
	tree, err := os.ReadFile("testdata/cdp_vscode_axtree.json")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/cdp_vscode_boxes.json")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeCaller{tree: tree}
	if err := json.Unmarshal(raw, &f.boxes); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fakeCaller) Call(_ context.Context, _, method string, params any) (json.RawMessage, error) {
	switch method {
	case "Target.getTargets":
		return json.RawMessage(`{"targetInfos":[{"targetId":"T1","type":"page"},{"targetId":"W1","type":"service_worker"}]}`), nil
	case "Target.attachToTarget":
		return json.RawMessage(`{"sessionId":"S1"}`), nil
	case "Runtime.evaluate":
		return json.RawMessage(`{"result":{"type":"string","value":"{\"dpr\":1,\"w\":1280,\"h\":800}"}}`), nil
	case "Accessibility.getFullAXTree":
		return f.tree, nil
	case "DOM.getBoxModel", "DOM.scrollIntoViewIfNeeded":
		b, _ := json.Marshal(params)
		var p struct{ BackendNodeID int }
		json.Unmarshal(b, &p)
		box, ok := f.boxes[fmt.Sprint(p.BackendNodeID)]
		if !ok {
			return nil, errors.New("Could not compute box model.")
		}
		if method == "DOM.scrollIntoViewIfNeeded" {
			return json.RawMessage(`{}`), nil
		}
		return box, nil
	}
	return nil, fmt.Errorf("unexpected method %s", method)
}

func fixtureNodes(t *testing.T) []Node {
	t.Helper()
	nodes, err := newCDPWith(newFake(t), cdpTargetTypes).Nodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return nodes
}

func TestCDPNodesFromFixture(t *testing.T) {
	for _, n := range fixtureNodes(t) {
		if n.Role == "button" && n.Name == "Continue without Signing In" {
			if abs(n.X-991) > 2 || abs(n.Y-650) > 2 || n.Offscreen {
				t.Fatalf("button at (%d,%d) offscreen=%t, want (991,650)", n.X, n.Y, n.Offscreen)
			}
			return
		}
	}
	t.Fatal("button not found")
}

func abs(n int) int { return max(n, -n) }

func TestCDPIgnoredDropped(t *testing.T) {
	var fx struct {
		Nodes []struct {
			Ignored bool `json:"ignored"`
			Backend int  `json:"backendDOMNodeId"`
		} `json:"nodes"`
	}
	json.Unmarshal(newFake(t).tree, &fx)
	ignored := map[string]bool{}
	for _, n := range fx.Nodes {
		if n.Ignored {
			ignored[fmt.Sprintf("cdp:T1:%d", n.Backend)] = true
		}
	}
	if len(ignored) == 0 {
		t.Fatal("fixture has no ignored node")
	}
	for _, n := range fixtureNodes(t) {
		if ignored[n.Key] {
			t.Fatalf("ignored node returned: %+v", n)
		}
		if ignored[n.Parent] {
			t.Fatalf("parent is an ignored node: %+v", n)
		}
	}
}

func TestCDPStates(t *testing.T) {
	for _, n := range fixtureNodes(t) {
		if slices.Contains(n.States, "focused") {
			return
		}
	}
	t.Fatal("no node has the focused state")
}

func TestCDPKeys(t *testing.T) {
	nodes := fixtureNodes(t)
	seen := map[string]bool{}
	for _, n := range nodes {
		if !strings.HasPrefix(n.Key, "cdp:T1:") {
			t.Fatalf("bad key %q", n.Key)
		}
		if seen[n.Key] {
			t.Fatalf("duplicate key %q", n.Key)
		}
		seen[n.Key] = true
	}
	for _, n := range nodes {
		if n.Parent != "" && !seen[n.Parent] {
			t.Fatalf("parent %q of %q is not in the result", n.Parent, n.Key)
		}
	}
}

func TestCDPReveal(t *testing.T) {
	src := newCDPWith(newFake(t), cdpTargetTypes)
	nodes, err := src.Nodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(nodes, func(n Node) bool { return n.Name == "Continue without Signing In" && n.Role == "button" })
	n, err := src.Reveal(context.Background(), nodes[i].Key)
	if err != nil || n.Name != nodes[i].Name || n.X != nodes[i].X || n.Y != nodes[i].Y {
		t.Fatalf("Reveal = %+v, %v", n, err)
	}
	if src.Press(context.Background(), n.Key) == nil {
		t.Fatal("Press must fail on CDP")
	}
}

func TestCDPUnreachable(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	_, err = NewCDP(context.Background(), port)
	var se *screen.Error
	if !errors.As(err, &se) || se.Code != screen.CodeCDP {
		t.Fatalf("err = %v, want code %s", err, screen.CodeCDP)
	}
}

func TestCDPWebsocketCaller(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/version" {
			fmt.Fprintf(w, `{"webSocketDebuggerUrl":"ws://%s/ws"}`, r.Host)
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		for {
			_, data, err := c.Read(r.Context())
			if err != nil {
				return
			}
			var m struct {
				ID        int    `json:"id"`
				Method    string `json:"method"`
				SessionID string `json:"sessionId"`
			}
			json.Unmarshal(data, &m)
			c.Write(r.Context(), websocket.MessageText, []byte(`{"method":"Some.event","params":{}}`))
			if m.Method == "Bad.method" {
				c.Write(r.Context(), websocket.MessageText, fmt.Appendf(nil, `{"id":%d,"error":{"message":"nope"}}`, m.ID))
				continue
			}
			c.Write(r.Context(), websocket.MessageText, fmt.Appendf(nil, `{"id":%d,"result":{"s":%q}}`, m.ID, m.SessionID))
		}
	}))
	defer ts.Close()
	port := ts.Listener.Addr().(*net.TCPAddr).Port
	src, err := NewCDP(context.Background(), port)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	w := src.(*cdpSource).c
	res, err := w.Call(context.Background(), "S9", "Any.method", nil)
	if err != nil || string(res) != `{"s":"S9"}` {
		t.Fatalf("Call = %s, %v", res, err)
	}
	if _, err := w.Call(context.Background(), "", "Bad.method", nil); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("error reply: %v", err)
	}
}
