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
	"golang.org/x/sync/errgroup"

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
	Error  *cdpError       `json:"error"`
}

// cdpError is an "error" reply: the browser answered, the command failed.
// For DOM calls it means the node has no layout or is gone.
type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *cdpError) Error() string { return fmt.Sprintf("cdp error %d: %s", e.Code, e.Message) }

// isProto reports whether err is a CDP error reply, not a transport failure.
func isProto(err error) bool {
	var ce *cdpError
	return errors.As(err, &ce)
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
			return nil, fmt.Errorf("%s: %w", method, r.Error)
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
	typ  string  // target type: page, iframe or webview
	dpr  float64 // viewport, read on every Nodes call
	w, h int
	// Browser chrome above and left of the viewport: outer minus inner size, in CSS pixels.
	chromeX, chromeY float64
}

type cdpSource struct {
	c        caller
	types    []string
	closer   func() error
	mu       sync.Mutex
	sessions map[string]*cdpSession // by targetId
	last     map[string]Node        // by Key, from the last Nodes
}

// errNoOwner means that no attached session owns the frame of a child target.
var errNoOwner = &cdpError{Message: "no attached session owns the frame"}

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

// call sends one command and decodes its result into out. It returns the
// caller's error unchanged, so that isProto still sees a *cdpError.
func (s *cdpSource) call(ctx context.Context, session, method string, params, out any) error {
	raw, err := s.c.Call(ctx, session, method, params)
	if err != nil || out == nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s: bad result: %w", method, err)
	}
	return nil
}

// unreachable turns err into *screen.Error with CodeCDP.
func unreachable(err error) error {
	var se *screen.Error
	if errors.As(err, &se) {
		return err
	}
	return screen.Errf(screen.CodeCDP, cdpHint, "%v", err)
}

// staleErr is the error for a node or frame that the browser no longer has.
func staleErr(key string, err error) error {
	return screen.Errf(screen.CodeStaleRef, "take a new snapshot", "%s: %v", key, err)
}

// session returns the session of the target, attached once, with a fresh
// viewport. A session that fails here is detached and forgotten.
func (s *cdpSource) session(ctx context.Context, target, typ string) (*cdpSession, error) {
	se := s.sessions[target]
	if se == nil {
		var a struct {
			SessionID string `json:"sessionId"`
		}
		if err := s.call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": target, "flatten": true}, &a); err != nil {
			return nil, err
		}
		se = &cdpSession{id: a.SessionID, typ: typ}
		s.sessions[target] = se
	}
	var ev struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	var vp struct {
		DPR    float64 `json:"dpr"`
		W, H   int
		OW, OH int
	}
	err := s.call(ctx, se.id, "Runtime.evaluate", map[string]any{
		"expression":    "JSON.stringify({dpr:devicePixelRatio,w:innerWidth,h:innerHeight,ow:outerWidth,oh:outerHeight})",
		"returnByValue": true,
	}, &ev)
	if err == nil {
		if err = json.Unmarshal([]byte(ev.Result.Value), &vp); err != nil {
			err = fmt.Errorf("viewport: %w", err)
		}
	}
	if err != nil {
		s.evict(ctx, target)
		return nil, err
	}
	se.dpr, se.w, se.h = vp.DPR, vp.W, vp.H
	// ponytail: all of outer minus inner goes above and left of the viewport, as with Chrome's top toolbar; side borders or a bottom panel would shift nodes.
	se.chromeX, se.chromeY = float64(max(vp.OW-vp.W, 0)), float64(max(vp.OH-vp.H, 0))
	if se.dpr == 0 {
		se.dpr = 1
	}
	return se, nil
}

// evict detaches the session of target and forgets it.
func (s *cdpSource) evict(ctx context.Context, target string) {
	if se := s.sessions[target]; se != nil {
		s.call(ctx, "", "Target.detachFromTarget", map[string]any{"sessionId": se.id}, nil)
		delete(s.sessions, target)
	}
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

// frame is the nodes of one target, with centres in the target's own CSS pixels.
type frame struct {
	target  string
	nodes   []Node
	backend []int
	boxes   []*[2]float64 // nil: no box
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
		return nil, unreachable(err)
	}
	live := map[string]bool{}
	var frames []*frame
	var lastErr error
	tried, failed := 0, 0
	for _, t := range tl.TargetInfos {
		if !slices.Contains(s.types, t.Type) {
			continue
		}
		live[t.TargetID] = true
		tried++
		f, err := s.read(ctx, t.TargetID, t.Type)
		if ctx.Err() != nil {
			return nil, unreachable(ctx.Err())
		}
		if err != nil {
			// One broken target does not fail the snapshot.
			s.evict(ctx, t.TargetID)
			lastErr = err
			failed++
			continue
		}
		frames = append(frames, f)
	}
	for id := range s.sessions {
		if !live[id] {
			s.evict(ctx, id)
		}
	}
	var out []Node
	var broken []string           // placed after the loop, so that every frame asks the same sessions
	owners := map[string]string{} // child target → Key of its owner node
	for _, f := range frames {
		ox, oy, root, owner, err := s.origin(ctx, f.target, 0)
		if ctx.Err() != nil {
			return nil, unreachable(ctx.Err())
		}
		if isProto(err) {
			continue // ponytail: a frame without a known owner has no page coordinates; skip it.
		}
		if err != nil {
			broken = append(broken, f.target)
			lastErr = err
			failed++
			continue
		}
		owners[f.target] = owner
		for i := range f.nodes {
			place(&f.nodes[i], f.boxes[i], ox, oy, root)
		}
		out = append(out, f.nodes...)
	}
	for _, t := range broken {
		s.evict(ctx, t)
	}
	if tried > 0 && failed == tried {
		return nil, unreachable(lastErr)
	}
	keys := make(map[string]bool, len(out))
	for _, n := range out {
		keys[n.Key] = true
	}
	s.last = make(map[string]Node, len(out))
	for i, n := range out {
		if owner := owners[target(n.Key)]; n.Parent == "" && keys[owner] {
			out[i].Parent = owner
		}
		s.last[n.Key] = out[i]
	}
	return out, nil
}

// read reads the AX tree of one target and the box of each kept node.
func (s *cdpSource) read(ctx context.Context, target, typ string) (*frame, error) {
	se, err := s.session(ctx, target, typ)
	if err != nil {
		return nil, err
	}
	var tree struct {
		Nodes []axNode `json:"nodes"`
	}
	if err := s.call(ctx, se.id, "Accessibility.getFullAXTree", nil, &tree); err != nil {
		return nil, err
	}
	f := convert(target, tree.Nodes)
	f.boxes = make([]*[2]float64, len(f.nodes))
	// ponytail: one DOM.getBoxModel per node, 16 in flight; DOMSnapshot.captureSnapshot is the next step if this is still slow.
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(16)
	for i, b := range f.backend {
		g.Go(func() error {
			c, err := s.centre(gctx, se.id, b)
			if isProto(err) {
				return nil // no layout: no box
			}
			f.boxes[i] = c
			return err
		})
	}
	return f, g.Wait()
}

// centre returns the mean of the border quad of the node, in CSS pixels.
func (s *cdpSource) centre(ctx context.Context, session string, backend int) (*[2]float64, error) {
	q, err := s.quad(ctx, session, backend, "border")
	if err != nil {
		return nil, err
	}
	c := [2]float64{(q[0] + q[2] + q[4] + q[6]) / 4, (q[1] + q[3] + q[5] + q[7]) / 4}
	return &c, nil
}

// quad returns one quad ("border" or "content") of the node's box model.
func (s *cdpSource) quad(ctx context.Context, session string, backend int, which string) ([]float64, error) {
	var box struct {
		Model map[string]json.RawMessage `json:"model"`
	}
	if err := s.call(ctx, session, "DOM.getBoxModel", map[string]any{"backendNodeId": backend}, &box); err != nil {
		return nil, err
	}
	var q []float64
	if json.Unmarshal(box.Model[which], &q) != nil || len(q) != 8 {
		return nil, &cdpError{Message: "box model without a " + which + " quad"}
	}
	return q, nil
}

// origin returns the top-left of the target's frame in the CSS pixels of
// its top page, that page's session, and the Key of the frame's owner node
// ("" for a page). A child target without an owner gives errNoOwner.
func (s *cdpSource) origin(ctx context.Context, target string, depth int) (x, y float64, root *cdpSession, owner string, err error) {
	se := s.sessions[target]
	if se == nil || depth > len(s.sessions) {
		return 0, 0, nil, "", errNoOwner
	}
	if se.typ == "page" {
		return 0, 0, se, "", nil
	}
	for id, o := range s.sessions {
		if id == target {
			continue
		}
		var fo struct {
			BackendNodeID int `json:"backendNodeId"`
		}
		err := s.call(ctx, o.id, "DOM.getFrameOwner", map[string]any{"frameId": target}, &fo)
		if isProto(err) {
			continue
		}
		if err != nil {
			return 0, 0, nil, "", err
		}
		q, err := s.quad(ctx, o.id, fo.BackendNodeID, "content")
		if err != nil {
			return 0, 0, nil, "", err
		}
		px, py, root, _, err := s.origin(ctx, id, depth+1)
		if err != nil {
			return 0, 0, nil, "", err
		}
		return px + q[0], py + q[1], root, key(id, fo.BackendNodeID), nil
	}
	return 0, 0, nil, "", errNoOwner
}

// place sets the centre of n in window pixels: page pixels plus the
// browser toolbar of the top page. A node without a box keeps (0,0) and is
// offscreen. Offscreen is judged against the viewport of the top page.
func place(n *Node, c *[2]float64, ox, oy float64, root *cdpSession) {
	n.X, n.Y, n.Offscreen = 0, 0, true
	if c == nil {
		return
	}
	x, y := ox+c[0], oy+c[1]
	n.X, n.Y = int(math.Round((x+root.chromeX)*root.dpr)), int(math.Round((y+root.chromeY)*root.dpr))
	n.Offscreen = x < 0 || y < 0 || x >= float64(root.w) || y >= float64(root.h)
}

func key(target string, backend int) string { return fmt.Sprintf("cdp:%s:%d", target, backend) }

// target returns the target part of a CDP Key.
func target(key string) string {
	rest := strings.TrimPrefix(key, "cdp:")
	if i := strings.LastIndexByte(rest, ':'); i >= 0 {
		return rest[:i]
	}
	return rest
}

// convert keeps the AX nodes that are not ignored and have a DOM node.
func convert(target string, ax []axNode) *frame {
	byID := make(map[string]*axNode, len(ax))
	for i := range ax {
		byID[ax[i].NodeID] = &ax[i]
	}
	kept := func(n *axNode) bool { return !n.Ignored && n.Backend != 0 }
	f := &frame{target: target}
	for i := range ax {
		a := &ax[i]
		if !kept(a) {
			continue
		}
		n := Node{Key: key(target, a.Backend), Role: str(a.Role), Name: str(a.Name), Value: str(a.Value), Desc: str(a.Description)}
		// The parent is the nearest kept ancestor; the depth cap guards against a cycle.
		p := byID[a.ParentID]
		for j := 0; p != nil && !kept(p) && j <= len(ax); j++ {
			p = byID[p.ParentID]
		}
		if p != nil && kept(p) {
			n.Parent = key(target, p.Backend)
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
		f.nodes = append(f.nodes, n)
		f.backend = append(f.backend, a.Backend)
	}
	return f
}

func (s *cdpSource) Reveal(ctx context.Context, k string) (Node, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := target(k)
	backend, err := strconv.Atoi(strings.TrimPrefix(k, "cdp:"+t+":"))
	se := s.sessions[t]
	if !strings.HasPrefix(k, "cdp:") || err != nil || se == nil {
		return Node{}, screen.Errf(screen.CodeStaleRef, "take a new snapshot", "no CDP target for %q", k)
	}
	fail := func(err error) (Node, error) {
		if isProto(err) {
			return Node{}, staleErr(k, err)
		}
		return Node{}, unreachable(err)
	}
	if err := s.call(ctx, se.id, "DOM.scrollIntoViewIfNeeded", map[string]any{"backendNodeId": backend}, nil); err != nil {
		return fail(err)
	}
	c, err := s.centre(ctx, se.id, backend)
	if err != nil {
		return fail(err)
	}
	ox, oy, root, _, err := s.origin(ctx, t, 0)
	if err != nil {
		return fail(err)
	}
	n, ok := s.last[k]
	if !ok {
		n = Node{Key: k}
	}
	place(&n, c, ox, oy, root)
	s.last[k] = n
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
