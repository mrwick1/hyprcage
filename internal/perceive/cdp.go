package perceive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/hexadecimil/hyprcage/internal/screen"
)

const cdpHint = "the DevTools port is closed; snapshot falls back to AT-SPI or OCR"

// cdpTargetTypes are the targets that hold a document.
var cdpTargetTypes = []string{"page", "iframe", "webview"}

// caller sends one CDP command on one session and returns its result.
type caller interface {
	Call(ctx context.Context, sessionID, method string, params any) (json.RawMessage, error)
}

// wsCaller sends the commands of every flattened session over the browser
// websocket and matches the responses by id.
type wsCaller struct {
	conn    *websocket.Conn
	mu      sync.Mutex
	next    int64
	pending map[int64]chan wsReply
	done    chan struct{}
	err     error // set before done closes
}

type wsReply struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (w *wsCaller) readLoop() {
	for {
		_, data, err := w.conn.Read(context.Background())
		if err != nil {
			w.err = err
			close(w.done)
			return
		}
		var r wsReply
		if json.Unmarshal(data, &r) != nil || r.ID == 0 {
			continue // an event
		}
		w.mu.Lock()
		ch := w.pending[r.ID]
		delete(w.pending, r.ID)
		w.mu.Unlock()
		if ch != nil {
			ch <- r
		}
	}
}

func (w *wsCaller) Call(ctx context.Context, sessionID, method string, params any) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ch := make(chan wsReply, 1)
	w.mu.Lock()
	w.next++
	id := w.next
	w.pending[id] = ch
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		delete(w.pending, id)
		w.mu.Unlock()
	}()
	msg, err := json.Marshal(struct {
		ID        int64  `json:"id"`
		Method    string `json:"method"`
		Params    any    `json:"params,omitempty"`
		SessionID string `json:"sessionId,omitempty"`
	}{id, method, params, sessionID})
	if err != nil {
		return nil, err
	}
	if err := w.conn.Write(ctx, websocket.MessageText, msg); err != nil {
		return nil, err
	}
	select {
	case r := <-ch:
		if r.Error != nil {
			return nil, fmt.Errorf("%s: %s", method, r.Error.Message)
		}
		return r.Result, nil
	case <-w.done:
		return nil, w.err
	case <-ctx.Done():
		return nil, fmt.Errorf("%s: %w", method, ctx.Err())
	}
}

type cdpSession struct {
	id   string
	dpr  float64
	w, h int
}

type cdpSource struct {
	c        caller
	types    []string
	closer   func() error
	mu       sync.Mutex
	sessions map[string]*cdpSession // by targetId
	last     map[string]Node        // by Key, from the last Nodes
}

// NewCDP dials the browser websocket that /json/version names on port.
func NewCDP(ctx context.Context, port int) (Source, error) {
	hc := http.Client{Timeout: 5 * time.Second}
	resp, err := hc.Get(fmt.Sprintf("http://127.0.0.1:%d/json/version", port))
	if err != nil {
		return nil, screen.Errf(screen.CodeCDP, cdpHint, "no DevTools answer on 127.0.0.1:%d: %v", port, err)
	}
	defer resp.Body.Close()
	var v struct {
		URL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil || v.URL == "" {
		return nil, screen.Errf(screen.CodeCDP, cdpHint, "no webSocketDebuggerUrl on 127.0.0.1:%d", port)
	}
	dctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(dctx, v.URL, nil)
	if err != nil {
		return nil, screen.Errf(screen.CodeCDP, cdpHint, "dial %s: %v", v.URL, err)
	}
	conn.SetReadLimit(-1) // a full AX tree is megabytes
	w := &wsCaller{conn: conn, pending: map[int64]chan wsReply{}, done: make(chan struct{})}
	go w.readLoop()
	s := newCDPWith(w, cdpTargetTypes).(*cdpSource)
	s.closer = conn.CloseNow
	return s, nil
}

// newCDPWith builds the source on c and attaches to the targets whose type
// is in targets.
func newCDPWith(c caller, targets []string) Source {
	return &cdpSource{c: c, types: targets, sessions: map[string]*cdpSession{}, last: map[string]Node{}}
}

func (s *cdpSource) Name() string { return "cdp" }

// call sends one command and turns every failure into CodeCDP.
func (s *cdpSource) call(ctx context.Context, session, method string, params, out any) error {
	raw, err := s.c.Call(ctx, session, method, params)
	if err != nil {
		var se *screen.Error
		if errors.As(err, &se) {
			return err
		}
		return screen.Errf(screen.CodeCDP, cdpHint, "%s: %v", method, err)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return screen.Errf(screen.CodeCDP, "", "%s: bad result: %v", method, err)
	}
	return nil
}

// attach returns the session of the target, attached once.
func (s *cdpSource) attach(ctx context.Context, target string) (*cdpSession, error) {
	if se := s.sessions[target]; se != nil {
		return se, nil
	}
	var a struct {
		SessionID string `json:"sessionId"`
	}
	if err := s.call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": target, "flatten": true}, &a); err != nil {
		return nil, err
	}
	var ev struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	err := s.call(ctx, a.SessionID, "Runtime.evaluate", map[string]any{
		"expression":    "JSON.stringify({dpr:devicePixelRatio,w:innerWidth,h:innerHeight})",
		"returnByValue": true,
	}, &ev)
	if err != nil {
		return nil, err
	}
	se := &cdpSession{id: a.SessionID}
	var vp struct {
		DPR  float64 `json:"dpr"`
		W, H int
	}
	if err := json.Unmarshal([]byte(ev.Result.Value), &vp); err != nil {
		return nil, screen.Errf(screen.CodeCDP, "", "viewport: %v", err)
	}
	se.dpr, se.w, se.h = vp.DPR, vp.W, vp.H
	if se.dpr == 0 {
		se.dpr = 1
	}
	s.sessions[target] = se
	return se, nil
}

type axValue struct {
	Value any `json:"value"`
}

type axNode struct {
	NodeID      string   `json:"nodeId"`
	Ignored     bool     `json:"ignored"`
	Role        *axValue `json:"role"`
	Name        *axValue `json:"name"`
	Value       *axValue `json:"value"`
	Description *axValue `json:"description"`
	Properties  []struct {
		Name  string  `json:"name"`
		Value axValue `json:"value"`
	} `json:"properties"`
	ParentID string `json:"parentId"`
	Backend  int    `json:"backendDOMNodeId"`
}

func (s *cdpSource) Nodes(ctx context.Context) ([]Node, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var tl struct {
		TargetInfos []struct {
			TargetID string `json:"targetId"`
			Type     string `json:"type"`
		} `json:"targetInfos"`
	}
	if err := s.call(ctx, "", "Target.getTargets", nil, &tl); err != nil {
		return nil, err
	}
	var targets []string
	for _, t := range tl.TargetInfos {
		if slices.Contains(s.types, t.Type) {
			targets = append(targets, t.TargetID)
		}
	}
	for id := range s.sessions {
		if !slices.Contains(targets, id) {
			delete(s.sessions, id)
		}
	}
	var out []Node
	for _, t := range targets {
		se, err := s.attach(ctx, t)
		if err != nil {
			return nil, err
		}
		var tree struct {
			Nodes []axNode `json:"nodes"`
		}
		if err := s.call(ctx, se.id, "Accessibility.getFullAXTree", nil, &tree); err != nil {
			return nil, err
		}
		nodes := s.convert(ctx, t, se, tree.Nodes)
		out = append(out, nodes...)
	}
	s.last = make(map[string]Node, len(out))
	for _, n := range out {
		s.last[n.Key] = n
	}
	return out, nil
}

// convert keeps the AX nodes that are not ignored and have a DOM node.
func (s *cdpSource) convert(ctx context.Context, target string, se *cdpSession, ax []axNode) []Node {
	byID := make(map[string]*axNode, len(ax))
	for i := range ax {
		byID[ax[i].NodeID] = &ax[i]
	}
	kept := func(n *axNode) bool { return !n.Ignored && n.Backend != 0 }
	key := func(backend int) string { return fmt.Sprintf("cdp:%s:%d", target, backend) }
	var out []Node
	for i := range ax {
		a := &ax[i]
		if !kept(a) {
			continue
		}
		n := Node{Key: key(a.Backend), Role: str(a.Role), Name: str(a.Name), Value: str(a.Value), Desc: str(a.Description)}
		// The parent is the nearest kept ancestor; the depth cap guards against a cycle.
		p := byID[a.ParentID]
		for j := 0; p != nil && !kept(p) && j <= len(ax); j++ {
			p = byID[p.ParentID]
		}
		if p != nil && kept(p) {
			n.Parent = key(p.Backend)
		}
		for _, pr := range a.Properties {
			v := pr.Value.Value
			switch pr.Name {
			case "focused", "expanded", "selected", "disabled", "required", "readonly":
				if truthy(v) {
					n.States = append(n.States, pr.Name)
				}
			case "checked":
				if truthy(v) || v == "mixed" {
					n.States = append(n.States, "checked")
				}
			case "invalid":
				if v != nil && v != false && v != "false" {
					n.States = append(n.States, "invalid")
				}
			case "level":
				if f, ok := v.(float64); ok {
					n.Level = int(f)
				}
			}
		}
		s.place(ctx, se, a.Backend, &n)
		out = append(out, n)
	}
	return out
}

// place sets the centre of n from its border box. A node without a box
// keeps (0,0) and is offscreen.
// ponytail: one DOM.getBoxModel per node; batch through DOMSnapshot if large trees get slow.
func (s *cdpSource) place(ctx context.Context, se *cdpSession, backend int, n *Node) {
	n.X, n.Y, n.Offscreen = 0, 0, true
	var box struct {
		Model struct {
			Border []float64 `json:"border"`
		} `json:"model"`
	}
	if s.call(ctx, se.id, "DOM.getBoxModel", map[string]any{"backendNodeId": backend}, &box) != nil || len(box.Model.Border) != 8 {
		return
	}
	var x, y float64
	for i := 0; i < 8; i += 2 {
		x += box.Model.Border[i] / 4
		y += box.Model.Border[i+1] / 4
	}
	n.X, n.Y = int(math.Round(x*se.dpr)), int(math.Round(y*se.dpr))
	n.Offscreen = x < 0 || y < 0 || x >= float64(se.w) || y >= float64(se.h)
}

func (s *cdpSource) Reveal(ctx context.Context, key string) (Node, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rest, ok := strings.CutPrefix(key, "cdp:")
	i := strings.LastIndexByte(rest, ':')
	backend, err := strconv.Atoi(rest[i+1:])
	if !ok || i < 0 || err != nil {
		return Node{}, screen.Errf(screen.CodeStaleRef, "take a new snapshot", "bad CDP key %q", key)
	}
	se := s.sessions[rest[:i]]
	if se == nil {
		return Node{}, screen.Errf(screen.CodeStaleRef, "take a new snapshot", "target of %q is gone", key)
	}
	if err := s.call(ctx, se.id, "DOM.scrollIntoViewIfNeeded", map[string]any{"backendNodeId": backend}, nil); err != nil {
		return Node{}, err
	}
	n, ok := s.last[key]
	if !ok {
		n = Node{Key: key}
	}
	s.place(ctx, se, backend, &n)
	s.last[key] = n
	return n, nil
}

func (s *cdpSource) Press(context.Context, string) error {
	return screen.Errf(screen.CodeUnsupported, "click the node with the pointer", "CDP has no press without the pointer")
}

func (s *cdpSource) Close() error {
	if s.closer == nil {
		return nil
	}
	return s.closer()
}

// str stringifies an AX value; a missing value is "".
func str(v *axValue) string {
	if v == nil || v.Value == nil {
		return ""
	}
	if s, ok := v.Value.(string); ok {
		return s
	}
	return fmt.Sprint(v.Value)
}

func truthy(v any) bool { return v == true || v == "true" }
