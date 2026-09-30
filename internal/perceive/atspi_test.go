package perceive

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/hexadecimil/hyprcage/internal/screen"
)

const thunarPID = 587021

// fakeTree answers Walk from a fixed list and records the calls.
type fakeTree struct {
	objs    []accessible
	scrolls []string
	actions []string
	actIdx  []int
	actErr  error    // returned by Scroll and DoAction when set
	inserts []string // "<bus><path> <text>" per InsertText
}

func (f *fakeTree) Walk(context.Context, []int) ([]accessible, error) {
	return slices.Clone(f.objs), nil
}

func (f *fakeTree) Scroll(_ context.Context, bus, path string) error {
	f.scrolls = append(f.scrolls, bus+path)
	return f.actErr
}

func (f *fakeTree) DoAction(_ context.Context, bus, path string, i int) error {
	f.actions = append(f.actions, bus+path)
	f.actIdx = append(f.actIdx, i)
	return f.actErr
}

func (f *fakeTree) InsertText(_ context.Context, bus, path, text string) error {
	f.inserts = append(f.inserts, bus+path+" "+text)
	return nil
}

func thunarTree(t *testing.T) *fakeTree {
	t.Helper()
	raw, err := os.ReadFile("testdata/atspi_thunar.json")
	if err != nil {
		t.Fatal(err)
	}
	var objs []struct {
		Bus, Path, Parent, Role, Name string
		States                        []string
		Extents                       [4]int
		PID                           int
	}
	if err := json.Unmarshal(raw, &objs); err != nil {
		t.Fatal(err)
	}
	f := &fakeTree{}
	for _, o := range objs {
		f.objs = append(f.objs, accessible{Bus: o.Bus, Path: o.Path, Parent: o.Parent, Role: o.Role,
			Name: o.Name, States: o.States, Extents: o.Extents, PID: o.PID})
	}
	return f
}

// obj builds a showing object; the source drops objects that are not showing.
func obj(path, role, name string, ext [4]int, states ...string) accessible {
	return accessible{Bus: ":1.5", Path: path, Role: role, Name: name, Extents: ext,
		States: slices.Concat(states, []string{"showing"}), PID: 1}
}

func nodesOf(t *testing.T, f *fakeTree, pids ...int) []Node {
	t.Helper()
	nodes, err := newATSPIWith(f, pids, 1280, 800).Nodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return nodes
}

func TestATSPIFiltersForeignPIDs(t *testing.T) {
	f := thunarTree(t)
	f.objs = append(f.objs,
		accessible{Bus: ":1.99", Path: "/org/a11y/atspi/accessible/1", Role: "push button",
			Name: "Human's secret", States: []string{"showing"}, Extents: [4]int{0, 0, 10, 10}, PID: 999999},
		accessible{Bus: ":1.99", Path: "/org/a11y/atspi/accessible/2", Role: "push button",
			Name: "Human's secret", NoStates: true, Extents: [4]int{0, 0, 10, 10}, PID: 999999})
	nodes := nodesOf(t, f, thunarPID)
	if want := len(nodesOf(t, thunarTree(t), thunarPID)); len(nodes) != want {
		t.Fatalf("got %d nodes, want %d", len(nodes), want)
	}
	for _, n := range nodes {
		if n.Name == "Human's secret" {
			t.Fatalf("foreign node leaked: %+v", n)
		}
	}
	if n := nodesOf(t, f); len(n) != 0 {
		t.Fatalf("no pids must give no nodes, got %d", len(n))
	}
}

func TestATSPIRoleMap(t *testing.T) {
	f := &fakeTree{objs: []accessible{
		obj("/a", "push button", "A", [4]int{0, 0, 10, 10}),
		obj("/b", "button", "B", [4]int{0, 0, 10, 10}),
		obj("/c", "check box", "C", [4]int{0, 0, 10, 10}),
		obj("/d", "table column header", "D", [4]int{0, 0, 10, 10}),
		obj("/t", "tree table", "T", [4]int{0, 0, 10, 10}),
		obj("/e", "table cell", "E", [4]int{0, 0, 10, 10}),
		obj("/g", "table", "G", [4]int{0, 0, 10, 10}),
		obj("/h", "table cell", "H", [4]int{0, 0, 10, 10}),
	}}
	f.objs[5].Parent = "/t"
	f.objs[7].Parent = "/g"
	want := []string{"button", "button", "checkbox", "table_column_header", "tree_table", "treeitem", "table", "table_cell"}
	nodes := nodesOf(t, f, 1)
	for i, n := range nodes {
		if n.Role != want[i] {
			t.Errorf("%s: role %q, want %q", n.Name, n.Role, want[i])
		}
	}
}

func TestATSPIMenuRole(t *testing.T) {
	nodes := nodesOf(t, &fakeTree{objs: []accessible{
		obj("/m", "menu", "File", [4]int{0, 0, 10, 10}),
		obj("/c", "check menu item", "Hidden files", [4]int{0, 0, 10, 10}),
		obj("/r", "radio menu item", "List View", [4]int{0, 0, 10, 10}),
	}}, 1)
	got := Filter(nodes, ModeInteractive, "")
	want := []string{"menuitem", "menuitemcheckbox", "menuitemradio"}
	if len(got) != len(want) {
		t.Fatalf("interactive nodes %+v, want roles %v", got, want)
	}
	for i, n := range got {
		if n.Role != want[i] {
			t.Errorf("%s: role %q, want %q", n.Name, n.Role, want[i])
		}
	}
}

func TestATSPISkipsNotShowing(t *testing.T) {
	hidden := obj("/h", "button", "Hidden", [4]int{0, 0, 10, 10})
	hidden.States = []string{"enabled"}
	child := obj("/k", "button", "Child", [4]int{0, 0, 10, 10})
	child.Parent = "/h"
	unknown := accessible{Bus: ":1.5", Path: "/u", Role: "button", Name: "Unknown", NoStates: true, PID: 1}
	app := accessible{Bus: ":1.5", Path: "/app", Role: "application", Name: "app", PID: 1}
	nodes := nodesOf(t, &fakeTree{objs: []accessible{
		app, obj("/s", "button", "Shown", [4]int{0, 0, 10, 10}), hidden, child, unknown,
	}}, 1)
	var names []string
	for _, n := range nodes {
		names = append(names, n.Name)
	}
	if !slices.Equal(names, []string{"app", "Shown", "Unknown"}) {
		t.Fatalf("names %v, want [app Shown Unknown]", names)
	}
}

func TestATSPIHiddenCoordsZero(t *testing.T) {
	const hidden = -2147483648
	n := nodesOf(t, &fakeTree{objs: []accessible{
		obj("/a", "button", "A", [4]int{hidden, hidden, 1, 1}),
		obj("/b", "button", "B", [4]int{50, 60, 0, 0}),
	}}, 1)
	for _, x := range n {
		if x.X != 0 || x.Y != 0 || !x.Offscreen {
			t.Errorf("%s: got (%d,%d) offscreen=%v, want (0,0) offscreen", x.Name, x.X, x.Y, x.Offscreen)
		}
	}
}

func TestATSPIFixtureMenus(t *testing.T) {
	got := Filter(nodesOf(t, thunarTree(t), thunarPID), ModeInteractive, "")
	for _, name := range []string{"File", "Edit", "View", "Go", "Help"} {
		if !slices.ContainsFunc(got, func(n Node) bool { return n.Role == "menuitem" && n.Name == name }) {
			t.Errorf("no menuitem %q in the interactive snapshot", name)
		}
	}
	for _, n := range got {
		if n.Name == "Icon View" || n.Name == "List View" { // closed-menu items lack showing
			t.Errorf("non-showing %q in the snapshot", n.Name)
		}
	}
}

func TestATSPIPopupItemsActionOnly(t *testing.T) {
	bar := obj("/bar", "menu bar", "", [4]int{0, 0, 300, 20})
	goMenu := obj("/go", "menu", "Go", [4]int{250, 0, 30, 20})
	goMenu.Parent = "/bar"
	item := obj("/open", "menu item", "Open Parent", [4]int{100, 8, 70, 20})
	item.Parent = "/go"
	pop := obj("/pop", "popup menu", "", [4]int{0, 0, 200, 100})
	sub := obj("/sub", "menu", "Recent", [4]int{0, 0, 70, 20})
	sub.Parent = "/pop"
	got := map[string]bool{}
	for _, n := range nodesOf(t, &fakeTree{objs: []accessible{bar, goMenu, item, pop, sub}}, 1) {
		got[n.Name] = n.ActionOnly
	}
	if got["Go"] || !got["Open Parent"] || !got["Recent"] {
		t.Fatalf("ActionOnly by name: %v, want Go=false, Open Parent=true, Recent=true", got)
	}
}

func TestATSPIPressMenuHint(t *testing.T) {
	goMenu := obj("/go", "menu", "Go", [4]int{250, 0, 30, 20})
	item := obj("/open", "menu item", "Open Parent", [4]int{100, 8, 70, 20})
	item.Parent = "/go"
	f := &fakeTree{objs: []accessible{goMenu, item}, actErr: errRefused}
	src := newATSPIWith(f, []int{1}, 1280, 800)
	nodes, err := src.Nodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ key, hint string }{
		{nodes[1].Key, "use key to navigate the menu"},
		{nodes[0].Key, "this element has no position; try key navigation"},
	} {
		var se *screen.Error
		if err := src.Press(context.Background(), tc.key); !errors.As(err, &se) || se.Hint != tc.hint {
			t.Errorf("Press %s: %v, want hint %q", tc.key, err, tc.hint)
		}
	}
}

func TestATSPICentre(t *testing.T) {
	n := nodesOf(t, &fakeTree{objs: []accessible{obj("/a", "button", "A", [4]int{100, 200, 40, 20})}}, 1)
	if n[0].X != 120 || n[0].Y != 210 || n[0].Offscreen {
		t.Fatalf("got %+v", n[0])
	}
}

func TestATSPINoExtents(t *testing.T) {
	const hidden = -2147483648
	n := nodesOf(t, &fakeTree{objs: []accessible{
		obj("/a", "button", "A", [4]int{5, 5, 0, 0}),
		obj("/b", "button", "B", [4]int{hidden, hidden, 1, 1}),
	}}, 1)
	for _, x := range n {
		if !x.Offscreen {
			t.Fatalf("%s must be offscreen: %+v", x.Name, x)
		}
	}
}

func TestATSPIStates(t *testing.T) {
	n := nodesOf(t, &fakeTree{objs: []accessible{
		obj("/a", "check box", "A", [4]int{0, 0, 1, 1}, "enabled", "sensitive", "checked", "focused", "showing"),
		obj("/b", "button", "B", [4]int{0, 0, 1, 1}, "showing"),
		obj("/c", "text", "C", [4]int{0, 0, 1, 1}, "enabled", "invalid-entry", "required"),
		obj("/d", "text", "D", [4]int{0, 0, 1, 1}, "sensitive", "editable", "expanded", "selected"),
	}}, 1)
	want := [][]string{
		{"checked", "focused"},
		{"disabled"},
		{"invalid", "readonly", "required"},
		{"expanded", "selected"},
	}
	for i, x := range n {
		got := slices.Clone(x.States)
		slices.Sort(got)
		if !slices.Equal(got, want[i]) {
			t.Errorf("%s: states %v, want %v", x.Name, got, want[i])
		}
	}
}

func TestATSPIFixtureButtons(t *testing.T) {
	nodes := nodesOf(t, thunarTree(t), thunarPID)
	for name, at := range map[string][2]int{"Back": {22, 48}, "Home": {133, 48}} {
		i := slices.IndexFunc(nodes, func(n Node) bool { return n.Role == "button" && n.Name == name })
		if i < 0 {
			t.Fatalf("no button %q", name)
		}
		if nodes[i].X != at[0] || nodes[i].Y != at[1] {
			t.Errorf("%s at (%d,%d), want %v", name, nodes[i].X, nodes[i].Y, at)
		}
	}
}

func TestATSPIKeys(t *testing.T) {
	f := thunarTree(t)
	f.objs = append(f.objs, f.objs[3]) // a repeated object must not repeat its key
	nodes := nodesOf(t, f, thunarPID)
	seen := map[string]bool{}
	for _, n := range nodes {
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
	if nodes[1].Key != "atspi::fixture.587021:/org/a11y/atspi/accessible/89" ||
		nodes[1].Parent != "atspi::fixture.587021:/org/a11y/atspi/accessible/root" || nodes[0].Parent != "" {
		t.Fatalf("keys: %+v %+v", nodes[0], nodes[1])
	}
}

func TestATSPIPress(t *testing.T) {
	f := &fakeTree{objs: []accessible{obj("/a", "button", "A", [4]int{0, 0, 10, 10})}}
	src := newATSPIWith(f, []int{1}, 1280, 800)
	nodes, err := src.Nodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := src.Press(context.Background(), nodes[0].Key); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.actions, []string{":1.5/a"}) || !slices.Equal(f.actIdx, []int{0}) {
		t.Fatalf("DoAction calls %v %v", f.actions, f.actIdx)
	}
	var se *screen.Error
	if err := src.Press(context.Background(), "atspi::1.99:/x"); !errors.As(err, &se) || se.Code != screen.CodeStaleRef {
		t.Fatalf("unknown key: %v", err)
	}
}

func TestATSPIReveal(t *testing.T) {
	f := &fakeTree{objs: []accessible{obj("/a", "button", "A", [4]int{0, 0, 10, 10})}}
	src := newATSPIWith(f, []int{1}, 1280, 800)
	nodes, err := src.Nodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	f.objs[0].Extents = [4]int{100, 200, 40, 20} // scrolled
	n, err := src.Reveal(context.Background(), nodes[0].Key)
	if err != nil || n.X != 120 || n.Y != 210 || !slices.Equal(f.scrolls, []string{":1.5/a"}) {
		t.Fatalf("Reveal = %+v, %v, scrolls %v", n, err, f.scrolls)
	}
	f.objs = nil // gone
	var se *screen.Error
	if _, err := src.Reveal(context.Background(), nodes[0].Key); !errors.As(err, &se) || se.Code != screen.CodeStaleRef {
		t.Fatalf("gone object: %v", err)
	}
}

func TestATSPIUnknownStates(t *testing.T) {
	a := obj("/a", "text", "A", [4]int{0, 0, 1, 1})
	a.NoStates = true
	if n := nodesOf(t, &fakeTree{objs: []accessible{a}}, 1); len(n[0].States) != 0 {
		t.Fatalf("states %v, want none", n[0].States)
	}
}

func TestATSPIActErrors(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want screen.Code
	}{
		{errRefused, screen.CodeUnsupported},
		{dbus.MakeUnknownMethodError("DoAction"), screen.CodeUnsupported},
		{dbus.MakeUnknownInterfaceError("org.a11y.atspi.Action"), screen.CodeUnsupported},
		{dbus.MakeNoObjectError("/a"), screen.CodeStaleRef},
		{dbus.Error{Name: "org.freedesktop.DBus.Error.UnknownObject"}, screen.CodeStaleRef},
		{errors.New("broken pipe"), screen.CodeNoSource},
	} {
		f := &fakeTree{objs: []accessible{obj("/a", "button", "A", [4]int{0, 0, 10, 10})}}
		src := newATSPIWith(f, []int{1}, 1280, 800)
		nodes, err := src.Nodes(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		f.actErr = tc.err
		var se *screen.Error
		if err := src.Press(context.Background(), nodes[0].Key); !errors.As(err, &se) || se.Code != tc.want {
			t.Errorf("Press with %v: %v, want %s", tc.err, err, tc.want)
		}
		if _, err := src.Reveal(context.Background(), nodes[0].Key); !errors.As(err, &se) || se.Code != tc.want {
			t.Errorf("Reveal with %v: %v, want %s", tc.err, err, tc.want)
		}
	}
}

// TestATSPILive reads the real a11y bus. It runs only when
// HYPRCAGE_LIVE_ATSPI names a screen that shows Thunar.
func TestATSPILive(t *testing.T) {
	name := os.Getenv("HYPRCAGE_LIVE_ATSPI")
	if name == "" {
		t.Skip("set HYPRCAGE_LIVE_ATSPI to a screen that shows Thunar")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if !HasApps(ctx, name) {
		t.Fatalf("HasApps(%q) = false", name)
	}
	src, err := NewATSPI(ctx, name, 1280, 800)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := src.Close(); err != nil {
			t.Error(err)
		}
	}()
	nodes, err := src.Nodes(ctx)
	if err != nil || len(nodes) == 0 {
		t.Fatalf("Nodes = %d nodes, %v", len(nodes), err)
	}
	seen := map[string]bool{}
	for _, n := range nodes {
		if seen[n.Key] {
			t.Fatalf("duplicate key %q", n.Key)
		}
		seen[n.Key] = true
	}
	if !slices.ContainsFunc(nodes, func(n Node) bool { return n.Role == "button" && n.Name == "Back" }) {
		t.Fatal("no button named Back")
	}
}

// TestATSPIDialogOffset: cage centres a dialog, and AT-SPI gives extents
// relative to the dialog, so the source adds the centring offset.
func TestATSPIDialogOffset(t *testing.T) {
	for _, tc := range []struct {
		role         string
		win          [4]int
		wantX, wantY int
	}{
		{"dialog", [4]int{0, 0, 300, 130}, 590, 435},
		{"frame", [4]int{0, 0, 1280, 800}, 100, 100}, // maximized: no offset
	} {
		app := obj("/app", "application", "thunar", [4]int{})
		win := obj("/win", tc.role, "W", tc.win)
		win.Parent = "/app"
		btn := obj("/ok", "push button", "OK", [4]int{90, 90, 20, 20}, "enabled")
		btn.Parent = "/win"
		src, err := newATSPIWith(&fakeTree{objs: []accessible{app, win, btn}}, []int{1}, 1280, 800).Nodes(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		i := slices.IndexFunc(src, func(n Node) bool { return n.Name == "OK" })
		if i < 0 || src[i].X != tc.wantX || src[i].Y != tc.wantY {
			t.Errorf("%s: OK at %+v, want (%d,%d)", tc.role, src, tc.wantX, tc.wantY)
		}
	}
}
