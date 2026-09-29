package perceive

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/hexadecimil/hyprcage/internal/screen"
)

// accessible is one AT-SPI object, as the walk needs it.
type accessible struct {
	Bus, Path, Parent string
	Role, Name        string
	States            []string
	Extents           [4]int // x, y, w, h in window coordinates; w == 0 when absent
	PID               int
}

// tree lists every object of the applications on the bus.
type tree interface {
	Walk(ctx context.Context) ([]accessible, error)
	Scroll(ctx context.Context, bus, path string) error // Component.ScrollTo
	DoAction(ctx context.Context, bus, path string, i int) error
}

const atspiHint = "start the a11y bus (at-spi-bus-launcher) or use source=ocr"

// atspiRoles maps AT-SPI role names to ARIA-style roles. Other roles keep
// their AT-SPI name with spaces replaced by "_".
var atspiRoles = map[string]string{
	"button": "button", "push button": "button", "toggle button": "button",
	"check box": "checkbox", "radio button": "radio",
	"text": "textbox", "entry": "textbox", "password text": "textbox",
	"combo box": "combobox", "list item": "option", "menu item": "menuitem",
	"page tab": "tab", "tree item": "treeitem", "heading": "heading",
	"dialog": "dialog", "link": "link", "slider": "slider", "spin button": "spinbutton",
}

// atspiStates maps AT-SPI state nicks to Node states.
var atspiStates = map[string]string{
	"focused": "focused", "checked": "checked", "expanded": "expanded", "selected": "selected",
	"required": "required", "invalid-entry": "invalid", "invalid": "invalid",
}

type atspiSource struct {
	t      tree
	pids   func() []int
	closer func() error
	mu     sync.Mutex
	last   map[string]accessible // by Key, from the last Nodes
}

// NewATSPI connects to the a11y bus of the session. The source keeps only
// the applications whose PID belongs to the screen.
func NewATSPI(ctx context.Context, screenName string) (Source, error) {
	conn, err := dialA11y(ctx)
	if err != nil {
		return nil, screen.Errf(screen.CodeNoSource, atspiHint, "a11y bus: %v", err)
	}
	pids := func() []int { return screen.ScreenProcesses(screenName) }
	s := &atspiSource{t: &dbusTree{conn: conn, pids: pids}, pids: pids, closer: conn.Close, last: map[string]accessible{}}
	return s, nil
}

// newATSPIWith builds the source on t, filtered by pids.
func newATSPIWith(t tree, pids []int) Source {
	return &atspiSource{t: t, pids: func() []int { return pids }, last: map[string]accessible{}}
}

// HasApps reports whether any application on the bus belongs to the screen.
func HasApps(ctx context.Context, screenName string) bool {
	pids := screen.ScreenProcesses(screenName)
	if len(pids) == 0 {
		return false
	}
	conn, err := dialA11y(ctx)
	if err != nil {
		return false
	}
	defer conn.Close()
	apps, err := (&dbusTree{conn: conn, pids: func() []int { return pids }}).apps(ctx)
	return err == nil && len(apps) > 0
}

func (s *atspiSource) Name() string { return "atspi" }

func atspiKey(bus, path string) string { return "atspi:" + bus + ":" + path }

// walk reads the tree and keeps the objects of the screen's processes, once each.
func (s *atspiSource) walk(ctx context.Context) ([]accessible, error) {
	objs, err := s.t.Walk(ctx)
	if err != nil {
		var se *screen.Error
		if errors.As(err, &se) {
			return nil, err
		}
		return nil, screen.Errf(screen.CodeNoSource, atspiHint, "a11y walk: %v", err)
	}
	// The guarantee: whatever Walk returns, a foreign PID never leaves here.
	pids := s.pids()
	seen := make(map[string]bool, len(objs))
	out := objs[:0]
	for _, o := range objs {
		k := atspiKey(o.Bus, o.Path)
		if !slices.Contains(pids, o.PID) || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, o)
	}
	return out, nil
}

func (s *atspiSource) Nodes(ctx context.Context) ([]Node, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	objs, err := s.walk(ctx)
	if err != nil {
		return nil, err
	}
	s.last = make(map[string]accessible, len(objs))
	for _, o := range objs {
		s.last[atspiKey(o.Bus, o.Path)] = o
	}
	return atspiNodes(objs), nil
}

// atspiNodes converts objs, which hold one entry per key.
func atspiNodes(objs []accessible) []Node {
	role := make(map[string]string, len(objs))
	parent := make(map[string]string, len(objs))
	for _, o := range objs {
		k := atspiKey(o.Bus, o.Path)
		role[k] = o.Role
		if o.Parent != "" {
			parent[k] = atspiKey(o.Bus, o.Parent)
		}
	}
	inTree := func(k string) bool {
		for i := 0; k != "" && i <= len(objs); i++ { // the cap guards against a cycle
			if r := role[k]; r == "tree" || r == "tree table" {
				return true
			}
			k = parent[k]
		}
		return false
	}
	nodes := make([]Node, 0, len(objs))
	for _, o := range objs {
		k := atspiKey(o.Bus, o.Path)
		n := Node{Key: k, Parent: parent[k], Name: o.Name}
		switch r, ok := atspiRoles[o.Role]; {
		case ok:
			n.Role = r
		case o.Role == "table cell" && inTree(parent[k]):
			n.Role = "treeitem"
		default:
			n.Role = strings.ReplaceAll(o.Role, " ", "_")
		}
		for _, st := range o.States {
			if m, ok := atspiStates[st]; ok && !slices.Contains(n.States, m) {
				n.States = append(n.States, m)
			}
		}
		if !slices.Contains(o.States, "enabled") && !slices.Contains(o.States, "sensitive") {
			n.States = append(n.States, "disabled")
		}
		if n.Role == "textbox" && !slices.Contains(o.States, "editable") {
			n.States = append(n.States, "readonly")
		}
		const hidden = -2147483648 // GTK3 extents of a widget that is not shown
		x, y, w, h := o.Extents[0], o.Extents[1], o.Extents[2], o.Extents[3]
		n.X, n.Y = x+w/2, y+h/2
		n.Offscreen = w <= 0 || x == hidden || y == hidden
		nodes = append(nodes, n)
	}
	return nodes
}

// known returns the object of key from the last Nodes.
func (s *atspiSource) known(key string) (accessible, error) {
	o, ok := s.last[key]
	if !ok {
		return o, screen.Errf(screen.CodeStaleRef, "take a new snapshot", "no AT-SPI object for %q", key)
	}
	return o, nil
}

// actErr maps an error of the object's application to stale_ref, and any
// other error to no_source.
func actErr(key string, err error) error {
	var de dbus.Error
	if errors.As(err, &de) {
		return screen.Errf(screen.CodeStaleRef, "take a new snapshot", "%s: %v", key, err)
	}
	return screen.Errf(screen.CodeNoSource, atspiHint, "%s: %v", key, err)
}

func (s *atspiSource) Reveal(ctx context.Context, key string) (Node, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, err := s.known(key)
	if err != nil {
		return Node{}, err
	}
	if err := s.t.Scroll(ctx, o.Bus, o.Path); err != nil {
		return Node{}, actErr(key, err)
	}
	// ponytail: tree has no single-object read, so Reveal walks everything; add one if Reveal is slow.
	objs, err := s.walk(ctx)
	if err != nil {
		return Node{}, err
	}
	for i, n := range atspiNodes(objs) {
		if n.Key == key {
			s.last[key] = objs[i]
			return n, nil
		}
	}
	delete(s.last, key)
	return Node{}, screen.Errf(screen.CodeStaleRef, "take a new snapshot", "%s is gone", key)
}

func (s *atspiSource) Press(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, err := s.known(key)
	if err != nil {
		return err
	}
	if err := s.t.DoAction(ctx, o.Bus, o.Path, 0); err != nil {
		return actErr(key, err)
	}
	return nil
}

func (s *atspiSource) Close() error {
	if s.closer == nil {
		return nil
	}
	return s.closer()
}

// dialA11y asks the session bus for the a11y bus address and connects to it.
func dialA11y(ctx context.Context) (*dbus.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	sess, err := dbus.SessionBusPrivate(dbus.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	defer sess.Close()
	if err := sess.Auth(nil); err != nil {
		return nil, err
	}
	if err := sess.Hello(); err != nil {
		return nil, err
	}
	var addr string
	if err := sess.Object("org.a11y.Bus", "/org/a11y/bus").CallWithContext(ctx, "org.a11y.Bus.GetAddress", 0).Store(&addr); err != nil {
		return nil, err
	}
	return dbus.Connect(addr, dbus.WithContext(ctx))
}

// AT-SPI constants, from /usr/include/at-spi-2.0/atspi/atspi-constants.h
// (at-spi2-core 2.60.6): AtspiStateType, AtspiCoordType, AtspiScrollType.
const (
	coordWindow    = 1
	scrollAnywhere = 6
	maxObjects     = 5000
	callTimeout    = 2 * time.Second
)

var stateBits = []struct {
	bit  uint
	nick string
}{
	{4, "checked"}, {7, "editable"}, {8, "enabled"}, {10, "expanded"}, {12, "focused"},
	{23, "selected"}, {24, "sensitive"}, {33, "required"}, {36, "invalid-entry"},
}

// dbusTree walks the real a11y bus.
type dbusTree struct {
	conn *dbus.Conn
	pids func() []int
}

type atspiRef struct {
	Bus  string
	Path dbus.ObjectPath
}

func (d *dbusTree) call(ctx context.Context, bus, path, method string, out any, args ...any) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	c := d.conn.Object(bus, dbus.ObjectPath(path)).CallWithContext(ctx, method, 0, args...)
	if out == nil {
		return c.Err
	}
	return c.Store(out)
}

type atspiApp struct {
	root atspiRef
	pid  int
}

// apps lists the applications on the bus whose PID is in d.pids. The
// others are skipped before any of their objects is read.
func (d *dbusTree) apps(ctx context.Context) ([]atspiApp, error) {
	var kids []atspiRef
	if err := d.call(ctx, "org.a11y.atspi.Registry", "/org/a11y/atspi/accessible/root",
		"org.a11y.atspi.Accessible.GetChildren", &kids); err != nil {
		return nil, err
	}
	pids := d.pids()
	var out []atspiApp
	for _, k := range kids {
		var pid uint32
		if d.call(ctx, "org.freedesktop.DBus", "/org/freedesktop/DBus",
			"org.freedesktop.DBus.GetConnectionUnixProcessID", &pid, k.Bus) != nil {
			continue
		}
		if slices.Contains(pids, int(pid)) {
			out = append(out, atspiApp{k, int(pid)})
		}
	}
	return out, nil
}

func (d *dbusTree) Walk(ctx context.Context) ([]accessible, error) {
	apps, err := d.apps(ctx)
	if err != nil {
		return nil, err
	}
	var out []accessible
	type item struct {
		ref    atspiRef
		parent string
	}
	for _, app := range apps {
		stack := []item{{app.root, ""}}
		for len(stack) > 0 && len(out) < maxObjects {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			it := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			o, kids, err := d.read(ctx, it.ref, app.pid)
			if err != nil {
				continue // ponytail: an object that vanished mid-walk is skipped with its subtree.
			}
			o.Parent = it.parent
			out = append(out, o)
			for i := len(kids) - 1; i >= 0; i-- {
				// ponytail: a child on another bus is another process; the PID check covers only this bus.
				if kids[i].Bus == app.root.Bus {
					stack = append(stack, item{kids[i], string(it.ref.Path)})
				}
			}
		}
	}
	return out, nil
}

// read reads one object and lists its children.
func (d *dbusTree) read(ctx context.Context, r atspiRef, pid int) (accessible, []atspiRef, error) {
	const acc = "org.a11y.atspi.Accessible"
	bus, path := r.Bus, string(r.Path)
	o := accessible{Bus: bus, Path: path, PID: pid}
	if err := d.call(ctx, bus, path, acc+".GetRoleName", &o.Role); err != nil {
		return o, nil, err
	}
	var name dbus.Variant
	if d.call(ctx, bus, path, "org.freedesktop.DBus.Properties.Get", &name, acc, "Name") == nil {
		o.Name, _ = name.Value().(string)
	}
	var st []uint32
	if d.call(ctx, bus, path, acc+".GetState", &st) == nil {
		for _, b := range stateBits {
			if int(b.bit/32) < len(st) && st[b.bit/32]&(1<<(b.bit%32)) != 0 {
				o.States = append(o.States, b.nick)
			}
		}
	}
	var ext struct{ X, Y, W, H int32 }
	if d.call(ctx, bus, path, "org.a11y.atspi.Component.GetExtents", &ext, uint32(coordWindow)) == nil {
		o.Extents = [4]int{int(ext.X), int(ext.Y), int(ext.W), int(ext.H)}
	}
	var kids []atspiRef
	_ = d.call(ctx, bus, path, acc+".GetChildren", &kids) // a leaf may not answer
	return o, kids, nil
}

func (d *dbusTree) Scroll(ctx context.Context, bus, path string) error {
	return d.call(ctx, bus, path, "org.a11y.atspi.Component.ScrollTo", nil, uint32(scrollAnywhere))
}

func (d *dbusTree) DoAction(ctx context.Context, bus, path string, i int) error {
	return d.call(ctx, bus, path, "org.a11y.atspi.Action.DoAction", nil, int32(i))
}
