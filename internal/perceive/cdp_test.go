package perceive

import (
	"cmp"
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
	"sync"
	"testing"

	"github.com/coder/websocket"
	"github.com/hexadecimil/hyprcage/internal/screen"
)

// fakeCaller answers from per-session trees and boxes. Session "S-<id>"
// belongs to target <id>. It is safe for concurrent use.
type fakeCaller struct {
	targets  string                                // JSON of targetInfos
	trees    map[string]json.RawMessage            // by session
	boxes    map[string]map[string]json.RawMessage // by session, then backend id
	owners   map[string]map[string]int             // by session, then frameId: owner backend id
	failTree map[string]error                      // by session
	failEval map[string]error                      // by session
	failBox  map[int]error                         // by backend id, any session
	scroll   error
	ownerErr error  // returned by DOM.getFrameOwner when set
	cancel   func() // called by DOM.getFrameOwner when set
	viewport string // Runtime.evaluate value; "" is 1280x800 with no browser toolbar

	mu       sync.Mutex
	detached []string
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
	f := &fakeCaller{
		targets: `[{"targetId":"T1","type":"page"},{"targetId":"W1","type":"service_worker"}]`,
		trees:   map[string]json.RawMessage{"S-T1": tree},
		boxes:   map[string]map[string]json.RawMessage{},
	}
	var boxes map[string]json.RawMessage
	if err := json.Unmarshal(raw, &boxes); err != nil {
		t.Fatal(err)
	}
	f.boxes["S-T1"] = boxes
	return f
}

var noLayout = &cdpError{Code: -32000, Message: "Could not compute box model."}

func (f *fakeCaller) Call(_ context.Context, session, method string, params any) (json.RawMessage, error) {
	b, _ := json.Marshal(params)
	var p struct {
		TargetID      string
		SessionID     string
		FrameID       string
		BackendNodeID int
	}
	json.Unmarshal(b, &p)
	switch method {
	case "Target.getTargets":
		return json.RawMessage(`{"targetInfos":` + f.targets + `}`), nil
	case "Target.attachToTarget":
		return json.RawMessage(`{"sessionId":"S-` + p.TargetID + `"}`), nil
	case "Target.detachFromTarget":
		f.mu.Lock()
		f.detached = append(f.detached, p.SessionID)
		f.mu.Unlock()
		return json.RawMessage(`{}`), nil
	case "Runtime.evaluate":
		if err := f.failEval[session]; err != nil {
			return nil, err
		}
		vp := cmp.Or(f.viewport, `{"dpr":1,"w":1280,"h":800,"ow":1280,"oh":800}`)
		return json.Marshal(map[string]any{"result": map[string]any{"type": "string", "value": vp}})
	case "Accessibility.getFullAXTree":
		if err := f.failTree[session]; err != nil {
			return nil, err
		}
		return f.trees[session], nil
	case "DOM.getFrameOwner":
		if f.cancel != nil {
			f.cancel()
			return nil, context.Canceled
		}
		if f.ownerErr != nil {
			return nil, f.ownerErr
		}
		if id, ok := f.owners[session][p.FrameID]; ok {
			return fmt.Appendf(nil, `{"backendNodeId":%d}`, id), nil
		}
		return nil, &cdpError{Code: -32000, Message: "Frame with the given id was not found."}
	case "DOM.scrollIntoViewIfNeeded":
		if f.scroll != nil {
			return nil, f.scroll
		}
		return json.RawMessage(`{}`), nil
	case "DOM.getBoxModel":
		if err := f.failBox[p.BackendNodeID]; err != nil {
			return nil, err
		}
		box, ok := f.boxes[session][fmt.Sprint(p.BackendNodeID)]
		if !ok {
			return nil, noLayout
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
	json.Unmarshal(newFake(t).trees["S-T1"], &fx)
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

func wantCode(t *testing.T, err error, code screen.Code) {
	t.Helper()
	var se *screen.Error
	if !errors.As(err, &se) || se.Code != code {
		t.Fatalf("err = %v, want code %s", err, code)
	}
}

func TestCDPBoxTransportErrorPropagates(t *testing.T) {
	f := newFake(t)
	f.failBox = map[int]error{929: errors.New("connection reset")}
	_, err := newCDPWith(f, cdpTargetTypes).Nodes(context.Background())
	wantCode(t, err, screen.CodeCDP)
}

func revealButton(t *testing.T, f *fakeCaller) (Node, error) {
	t.Helper()
	src := newCDPWith(f, cdpTargetTypes)
	if _, err := src.Nodes(context.Background()); err != nil {
		t.Fatal(err)
	}
	return src.Reveal(context.Background(), "cdp:T1:929")
}

func TestCDPRevealRemovedNode(t *testing.T) {
	f := newFake(t)
	f.scroll = &cdpError{Code: -32000, Message: "Node is detached from document"}
	_, err := revealButton(t, f)
	wantCode(t, err, screen.CodeStaleRef)

	f.scroll = errors.New("connection reset")
	_, err = revealButton(t, f)
	wantCode(t, err, screen.CodeCDP)
}

func TestCDPRevealBoxFailure(t *testing.T) {
	f := newFake(t)
	src := newCDPWith(f, cdpTargetTypes)
	if _, err := src.Nodes(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.failBox = map[int]error{929: noLayout}
	_, err := src.Reveal(context.Background(), "cdp:T1:929")
	wantCode(t, err, screen.CodeStaleRef)
	f.failBox = map[int]error{929: context.DeadlineExceeded}
	_, err = src.Reveal(context.Background(), "cdp:T1:929")
	wantCode(t, err, screen.CodeCDP)
}

func TestCDPIframeOffset(t *testing.T) {
	quad := func(x1, y1, x2, y2 int) json.RawMessage {
		q := fmt.Sprintf("[%d,%d,%d,%d,%d,%d,%d,%d]", x1, y1, x2, y1, x2, y2, x1, y2)
		return json.RawMessage(`{"model":{"content":` + q + `,"border":` + q + `}}`)
	}
	f := &fakeCaller{
		targets: `[{"targetId":"P","type":"page"},{"targetId":"F","type":"iframe"}]`,
		trees: map[string]json.RawMessage{
			"S-P": json.RawMessage(`{"nodes":[
				{"nodeId":"1","role":{"value":"RootWebArea"},"backendDOMNodeId":1},
				{"nodeId":"5","role":{"value":"Iframe"},"parentId":"1","backendDOMNodeId":5}]}`),
			"S-F": json.RawMessage(`{"nodes":[
				{"nodeId":"1","role":{"value":"RootWebArea"},"backendDOMNodeId":1},
				{"nodeId":"7","role":{"value":"button"},"name":{"value":"OK"},"parentId":"1","backendDOMNodeId":7}]}`),
		},
		boxes: map[string]map[string]json.RawMessage{
			"S-P": {"5": quad(100, 200, 400, 500)},
			"S-F": {"7": quad(0, 0, 20, 20)},
		},
		owners: map[string]map[string]int{"S-P": {"F": 5}},
	}
	nodes, err := newCDPWith(f, cdpTargetTypes).Nodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]Node{}
	for _, n := range nodes {
		byKey[n.Key] = n
	}
	if b := byKey["cdp:F:7"]; b.X != 110 || b.Y != 210 || b.Offscreen {
		t.Fatalf("iframe button = %+v, want (110,210)", b)
	}
	if r := byKey["cdp:F:1"]; r.Parent != "cdp:P:5" {
		t.Fatalf("iframe root parent = %q, want cdp:P:5", r.Parent)
	}

	f.owners = nil // no owner: the frame's nodes are skipped
	nodes, err = newCDPWith(f, cdpTargetTypes).Nodes(context.Background())
	if err != nil || slices.ContainsFunc(nodes, func(n Node) bool { return strings.HasPrefix(n.Key, "cdp:F:") }) {
		t.Fatalf("ownerless frame: %v, %+v", err, nodes)
	}
}

func TestCDPSkipsBrokenTarget(t *testing.T) {
	f := newFake(t)
	f.targets = `[{"targetId":"T1","type":"page"},{"targetId":"T2","type":"page"},{"targetId":"T3","type":"page"}]`
	f.failTree = map[string]error{"S-T2": &cdpError{Message: "boom"}}
	f.failEval = map[string]error{"S-T3": errors.New("connection reset")}
	src := newCDPWith(f, cdpTargetTypes)
	nodes, err := src.Nodes(context.Background())
	if err != nil || len(nodes) == 0 {
		t.Fatalf("Nodes = %d nodes, %v", len(nodes), err)
	}
	for _, n := range nodes {
		if !strings.HasPrefix(n.Key, "cdp:T1:") {
			t.Fatalf("node from a broken target: %q", n.Key)
		}
	}
	if !slices.Contains(f.detached, "S-T2") || !slices.Contains(f.detached, "S-T3") {
		t.Fatalf("detached = %v, want S-T2 and S-T3", f.detached)
	}
	if len(src.(*cdpSource).sessions) != 1 {
		t.Fatalf("sessions = %v, want only T1", src.(*cdpSource).sessions)
	}

	f.failTree["S-T1"] = errors.New("connection reset")
	_, err = newCDPWith(f, cdpTargetTypes).Nodes(context.Background())
	wantCode(t, err, screen.CodeCDP)
}

func TestCDPOriginTransportError(t *testing.T) {
	tree := json.RawMessage(`{"nodes":[{"nodeId":"1","role":{"value":"RootWebArea"},"backendDOMNodeId":1}]}`)
	f := &fakeCaller{
		targets:  `[{"targetId":"P","type":"page"},{"targetId":"F","type":"iframe"}]`,
		trees:    map[string]json.RawMessage{"S-P": tree, "S-F": tree},
		ownerErr: errors.New("connection reset"),
	}
	src := newCDPWith(f, cdpTargetTypes)
	nodes, err := src.Nodes(context.Background())
	if err != nil || len(nodes) != 1 || nodes[0].Key != "cdp:P:1" {
		t.Fatalf("Nodes = %+v, %v; want only the page node", nodes, err)
	}
	if !slices.Contains(f.detached, "S-F") || src.(*cdpSource).sessions["F"] != nil {
		t.Fatalf("iframe not evicted: detached = %v", f.detached)
	}

	// Two iframes and no page: both fail to place, so every target failed.
	f.targets = `[{"targetId":"F","type":"iframe"},{"targetId":"G","type":"iframe"}]`
	f.trees["S-G"] = tree
	_, err = newCDPWith(f, cdpTargetTypes).Nodes(context.Background())
	wantCode(t, err, screen.CodeCDP)

	ctx, cancel := context.WithCancel(context.Background())
	f.targets = `[{"targetId":"P","type":"page"},{"targetId":"F","type":"iframe"}]`
	f.cancel = cancel
	_, err = newCDPWith(f, cdpTargetTypes).Nodes(ctx)
	wantCode(t, err, screen.CodeCDP)
}

func TestCDPBrowserToolbarOffset(t *testing.T) {
	f := &fakeCaller{
		targets: `[{"targetId":"P","type":"page"}]`,
		trees: map[string]json.RawMessage{"S-P": json.RawMessage(`{"nodes":[
			{"nodeId":"1","role":{"value":"RootWebArea"},"backendDOMNodeId":1},
			{"nodeId":"2","role":{"value":"link"},"name":{"value":"Learn more"},"parentId":"1","backendDOMNodeId":2}]}`)},
		boxes: map[string]map[string]json.RawMessage{"S-P": {
			"2": json.RawMessage(`{"model":{"border":[600,393,680,393,680,413,600,413]}}`),
		}},
		viewport: `{"dpr":1,"w":1280,"h":713,"ow":1280,"oh":800}`,
	}
	nodes, err := newCDPWith(f, cdpTargetTypes).Nodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(nodes, func(n Node) bool { return n.Name == "Learn more" })
	if i < 0 || nodes[i].X != 640 || nodes[i].Y != 490 || nodes[i].Offscreen {
		t.Fatalf("nodes = %+v, want the link at (640,490) on screen", nodes)
	}
}

func TestCDPToolbarOffsetZoomed(t *testing.T) {
	at := func(viewport string) Node {
		t.Helper()
		f := &fakeCaller{
			targets: `[{"targetId":"P","type":"page"}]`,
			trees: map[string]json.RawMessage{"S-P": json.RawMessage(`{"nodes":[
				{"nodeId":"2","role":{"value":"link"},"name":{"value":"x"},"backendDOMNodeId":2}]}`)},
			boxes:    map[string]map[string]json.RawMessage{"S-P": {"2": json.RawMessage(`{"model":{"border":[90,90,110,90,110,110,90,110]}}`)}},
			viewport: viewport,
		}
		nodes, err := newCDPWith(f, cdpTargetTypes).Nodes(context.Background())
		if err != nil || len(nodes) != 1 {
			t.Fatalf("Nodes = %+v, %v", nodes, err)
		}
		return nodes[0]
	}
	// Chrome at 150% zoom with an 87 px toolbar. innerWidth and innerHeight
	// are whole CSS pixels, so the offset can be off by half a pixel.
	if n := at(`{"dpr":1.5,"w":853,"h":475,"ow":1280,"oh":800}`); abs(n.X-150) > 1 || abs(n.Y-237) > 1 || n.Offscreen {
		t.Fatalf("zoomed Chrome: %+v, want (150,237)", n)
	}
	// Electron at 150% zoom: no toolbar.
	if n := at(`{"dpr":1.5,"w":853,"h":533,"ow":1280,"oh":800}`); abs(n.X-150) > 1 || abs(n.Y-150) > 1 || n.Offscreen {
		t.Fatalf("zoomed Electron: %+v, want (150,150)", n)
	}
}
