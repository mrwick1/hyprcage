package perceive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/sync/errgroup"

	"github.com/hexadecimil/hyprcage/internal/cdp"
	"github.com/hexadecimil/hyprcage/internal/screen"
)

const cdpHint = "the DevTools port is closed; snapshot falls back to AT-SPI or OCR"

// cdpTargetTypes are the targets that hold a document.
var cdpTargetTypes = []string{"page", "iframe", "webview"}

// caller sends one CDP command on one session and returns its result.
type caller interface {
	Call(ctx context.Context, sessionID, method string, params any) (json.RawMessage, error)
}

// cdpError is an "error" reply: the browser answered, the command failed.
// For DOM calls it means the node has no layout or is gone.
type cdpError = cdp.Error

// isProto reports whether err is a CDP error reply, not a transport failure.
func isProto(err error) bool {
	var ce *cdpError
	return errors.As(err, &ce)
}

type cdpSession struct {
	id   string
	typ  string  // target type: page, iframe or webview
	dpr  float64 // viewport, read on every Nodes call
	w, h int
	// Browser chrome above and left of the viewport, in window pixels:
	// outer minus inner times dpr, because page zoom shrinks inner, not outer.
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
	c, err := cdp.Dial(ctx, port, nil)
	if err != nil {
		return nil, screen.Errf(screen.CodeCDP, cdpHint, "%v", err)
	}
	s := newCDPWith(c, cdpTargetTypes).(*cdpSource)
	s.closer = c.Close
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
	if se.dpr == 0 {
		se.dpr = 1
	}
	// ponytail: all of outer minus inner goes above and left of the viewport, as with Chrome's top toolbar; side borders or a bottom panel would shift nodes.
	se.chromeX = max(float64(vp.OW)-float64(vp.W)*se.dpr, 0)
	se.chromeY = max(float64(vp.OH)-float64(vp.H)*se.dpr, 0)
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
			place(&f.nodes[i], f.boxes[i], ox, oy, root, s.sessions[f.target])
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
	id, backend, err := s.owner(ctx, target)
	if err != nil {
		return 0, 0, nil, "", err
	}
	q, err := s.quad(ctx, s.sessions[id].id, backend, "content")
	if err != nil {
		return 0, 0, nil, "", err
	}
	px, py, root, _, err := s.origin(ctx, id, depth+1)
	if err != nil {
		return 0, 0, nil, "", err
	}
	return px + q[0], py + q[1], root, key(id, backend), nil
}

// owner finds the attached target whose document holds the frame of target,
// and the backend id of the owner element. No owner gives errNoOwner.
func (s *cdpSource) owner(ctx context.Context, target string) (string, int, error) {
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
			return "", 0, err
		}
		return id, fo.BackendNodeID, nil
	}
	return "", 0, errNoOwner
}

// place sets the centre of n in window pixels: page pixels plus the
// browser toolbar of the top page. c is in the CSS pixels of own, the
// session of the node's target. A node without a box keeps (0,0) and is
// offscreen. Offscreen is judged against the viewport of the top page and
// against own's viewport, which clips a node inside an iframe.
func place(n *Node, c *[2]float64, ox, oy float64, root, own *cdpSession) {
	n.X, n.Y, n.Offscreen = 0, 0, true
	if c == nil {
		return
	}
	out := func(x, y float64, se *cdpSession) bool {
		return x < 0 || y < 0 || x >= float64(se.w) || y >= float64(se.h)
	}
	x, y := ox+c[0], oy+c[1]
	n.X, n.Y = int(math.Round(x*root.dpr+root.chromeX)), int(math.Round(y*root.dpr+root.chromeY))
	// ponytail: only the node's own frame clips; an iframe nested in a clipped iframe is not checked.
	n.Offscreen = out(x, y, root) || out(c[0], c[1], own)
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

// node returns the session and the backend id of the CDP Key k.
func (s *cdpSource) node(k string) (*cdpSession, int, error) {
	t := target(k)
	backend, err := strconv.Atoi(strings.TrimPrefix(k, "cdp:"+t+":"))
	se := s.sessions[t]
	if !strings.HasPrefix(k, "cdp:") || err != nil || se == nil {
		return nil, 0, screen.Errf(screen.CodeStaleRef, "take a new snapshot", "no CDP target for %q", k)
	}
	return se, backend, nil
}

// nodeErr maps an error reply about the node k to stale_ref.
func nodeErr(k string, err error) error {
	if isProto(err) {
		return staleErr(k, err)
	}
	return unreachable(err)
}

func (s *cdpSource) Reveal(ctx context.Context, k string) (Node, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	se, backend, err := s.node(k)
	if err != nil {
		return Node{}, err
	}
	if err := s.call(ctx, se.id, "DOM.scrollIntoViewIfNeeded", map[string]any{"backendNodeId": backend}, nil); err != nil {
		return Node{}, nodeErr(k, err)
	}
	c, err := s.centre(ctx, se.id, backend)
	if err != nil {
		return Node{}, nodeErr(k, err)
	}
	ox, oy, root, _, err := s.origin(ctx, target(k), 0)
	if err != nil {
		return Node{}, nodeErr(k, err)
	}
	n, ok := s.last[k]
	if !ok {
		n = Node{Key: k}
	}
	place(&n, c, ox, oy, root, se)
	s.last[k] = n
	return n, nil
}

// HitTest checks that the element at the node's centre is the node or a
// descendant of it, so that a dialog over the node never takes the click.
// A CDP error reply skips the check: it must not block the act.
func (s *cdpSource) HitTest(ctx context.Context, k string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := target(k)
	backend, err := strconv.Atoi(strings.TrimPrefix(k, "cdp:"+t+":"))
	se := s.sessions[t]
	if err != nil || se == nil {
		return screen.Errf(screen.CodeStaleRef, "take a new snapshot", "no CDP target for %q", k)
	}
	// The node, then the <iframe> element of each parent frame up to the page.
	for depth := 0; ; depth++ {
		covered, err := s.covered(ctx, se.id, backend)
		switch {
		case isProto(err):
			return nil
		case err != nil:
			return unreachable(err)
		case covered:
			return screen.Errf(screen.CodeRefOccluded, "another element covers it; close the dialog or act on the covering element",
				"%s is covered by another element", k)
		}
		if se.typ == "page" || depth > len(s.sessions) {
			return nil
		}
		if t, backend, err = s.owner(ctx, t); err != nil {
			if isProto(err) {
				return nil
			}
			return unreachable(err)
		}
		se = s.sessions[t]
	}
}

// covered hit-tests the node's centre in its own session, in CSS pixels.
func (s *cdpSource) covered(ctx context.Context, session string, backend int) (bool, error) {
	c, err := s.centre(ctx, session, backend)
	if err != nil {
		return false, err
	}
	var hit struct {
		BackendNodeID int `json:"backendNodeId"`
	}
	if err := s.call(ctx, session, "DOM.getNodeForLocation", map[string]any{
		"x": int(math.Round(c[0])), "y": int(math.Round(c[1])),
		"includeUserAgentShadowDOM": true, "ignorePointerEventsNone": false,
	}, &hit); err != nil {
		return false, err
	}
	if hit.BackendNodeID == backend {
		return false, nil
	}
	const group = "hyprcage-hit"
	defer s.call(ctx, session, "Runtime.releaseObjectGroup", map[string]any{"objectGroup": group}, nil)
	var ids [2]string
	for i, b := range []int{backend, hit.BackendNodeID} {
		var r struct {
			Object struct {
				ObjectID string `json:"objectId"`
			} `json:"object"`
		}
		if err := s.call(ctx, session, "DOM.resolveNode", map[string]any{"backendNodeId": b, "objectGroup": group}, &r); err != nil {
			return false, err
		}
		ids[i] = r.Object.ObjectID
	}
	var res struct {
		Result struct {
			Value bool `json:"value"`
		} `json:"result"`
		Exception json.RawMessage `json:"exceptionDetails"`
	}
	if err := s.call(ctx, session, "Runtime.callFunctionOn", map[string]any{
		"objectId": ids[0],
		// Walk the composed tree: the hit may sit in a (user-agent) shadow root of the node.
		"functionDeclaration": "function(h){for(;h;h=h.parentNode||h.host){if(h===this)return true}return false}",
		"arguments":           []map[string]any{{"objectId": ids[1]}},
		"returnByValue":       true,
	}, &res); err != nil {
		return false, err
	}
	if res.Exception != nil {
		return false, nil // the check threw: do not block the act
	}
	return !res.Result.Value, nil
}

// Mouse sends CDP mouse events at the centre of the node k: a click with
// count presses, a hover (op "hover") or a wheel turn (op "scroll").
func (s *cdpSource) Mouse(ctx context.Context, k, op string, count int, direction string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	se, backend, err := s.node(k)
	if err != nil {
		return err
	}
	c, err := s.centre(ctx, se.id, backend)
	if err != nil {
		return nodeErr(k, err)
	}
	ox, oy, root, _, err := s.origin(ctx, target(k), 0)
	if err != nil {
		return nodeErr(k, err)
	}
	// The coordinates are CSS pixels of the top page's viewport.
	ev := func(typ string, extra map[string]any) error {
		p := map[string]any{"type": typ, "x": ox + c[0], "y": oy + c[1]}
		maps.Copy(p, extra)
		return unreachableIf(s.call(ctx, root.id, "Input.dispatchMouseEvent", p, nil))
	}
	if err := ev("mouseMoved", nil); err != nil || op == "hover" {
		return err
	}
	if op == "scroll" {
		d := map[string][2]int{"": {0, 300}, "down": {0, 300}, "up": {0, -300}, "right": {300, 0}, "left": {-300, 0}}
		delta, ok := d[strings.ToLower(direction)]
		if !ok {
			return fmt.Errorf("unknown direction %q (up, down, left, right)", direction)
		}
		return ev("mouseWheel", map[string]any{"deltaX": delta[0], "deltaY": delta[1]})
	}
	for i := 1; i <= count; i++ {
		for _, typ := range []string{"mousePressed", "mouseReleased"} {
			if err := ev(typ, map[string]any{"button": "left", "clickCount": i}); err != nil {
				return err
			}
		}
	}
	return nil
}

// InsertText focuses the node k and inserts text as if typed.
func (s *cdpSource) InsertText(ctx context.Context, k, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	se, backend, err := s.node(k)
	if err != nil {
		return err
	}
	if err := s.call(ctx, se.id, "DOM.focus", map[string]any{"backendNodeId": backend}, nil); err != nil {
		return nodeErr(k, err)
	}
	return unreachableIf(s.call(ctx, se.id, "Input.insertText", map[string]any{"text": text}, nil))
}

// cdpKeys maps a key name to its DOM key, Windows virtual key code and text.
var cdpKeys = map[string]struct {
	key  string
	vk   int
	text string
}{
	"return": {"Enter", 13, "\r"}, "enter": {"Enter", 13, "\r"}, "tab": {"Tab", 9, ""},
	"escape": {"Escape", 27, ""}, "backspace": {"Backspace", 8, ""}, "delete": {"Delete", 46, ""},
	"space": {" ", 32, " "}, "left": {"ArrowLeft", 37, ""}, "up": {"ArrowUp", 38, ""},
	"right": {"ArrowRight", 39, ""}, "down": {"ArrowDown", 40, ""}, "home": {"Home", 36, ""},
	"end": {"End", 35, ""}, "page_up": {"PageUp", 33, ""}, "page_down": {"PageDown", 34, ""},
}

// cdpMods are the modifier bits of Input.dispatchKeyEvent.
var cdpMods = map[string]int{"alt": 1, "ctrl": 2, "super": 4, "shift": 8}

// Keys presses combinations such as ctrl+a on the one page of the source.
// Every combination is parsed first: an unknown key sends nothing and
// answers unsupported, so that the caller can use another path.
func (s *cdpSource) Keys(ctx context.Context, combos []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var page *cdpSession
	for _, se := range s.sessions {
		if se.typ == "page" {
			if page != nil {
				return screen.Errf(screen.CodeUnsupported, "", "several pages: CDP cannot tell which one has the focus")
			}
			page = se
		}
	}
	if page == nil {
		return screen.Errf(screen.CodeUnsupported, "", "no page")
	}
	var events []map[string]any
	for _, combo := range combos {
		parts := strings.Split(combo, "+")
		mods := 0
		for _, m := range parts[:len(parts)-1] {
			bit, ok := cdpMods[strings.ToLower(m)]
			if !ok {
				return screen.Errf(screen.CodeUnsupported, "ctrl, shift, alt or super", "bad modifier %q in %q", m, combo)
			}
			mods |= bit
		}
		name := parts[len(parts)-1]
		k, ok := cdpKeys[strings.ToLower(name)]
		if r := []rune(name); len(r) == 1 && r[0] < 0x80 && (unicode.IsLetter(r[0]) || unicode.IsDigit(r[0])) {
			k.key, k.vk, ok = name, int(unicode.ToUpper(r[0])), true
			if mods&cdpMods["shift"] != 0 {
				k.key = strings.ToUpper(name)
			}
			k.text = k.key
		}
		if !ok {
			return screen.Errf(screen.CodeUnsupported, "", "no CDP key for %q", combo)
		}
		if mods&^cdpMods["shift"] != 0 {
			k.text = "" // a shortcut, not text
		}
		down := map[string]any{"type": "rawKeyDown", "key": k.key, "windowsVirtualKeyCode": k.vk, "modifiers": mods}
		if k.text != "" {
			down["type"], down["text"] = "keyDown", k.text
		}
		events = append(events, down, map[string]any{"type": "keyUp", "key": k.key, "windowsVirtualKeyCode": k.vk, "modifiers": mods})
	}
	for _, ev := range events {
		if err := s.call(ctx, page.id, "Input.dispatchKeyEvent", ev, nil); err != nil {
			return unreachable(err)
		}
	}
	return nil
}

// unreachableIf is unreachable for a non-nil err.
func unreachableIf(err error) error {
	if err != nil {
		return unreachable(err)
	}
	return nil
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
