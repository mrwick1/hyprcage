package perceive

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/hexadecimil/hyprcage/internal/screen"
)

const thunarPID = 587021

// fakeTree answers Walk from a fixed list and records the calls.
type fakeTree struct {
	objs    []accessible
	scrolls []string
	actions []string
	actIdx  []int
}

func (f *fakeTree) Walk(context.Context) ([]accessible, error) { return slices.Clone(f.objs), nil }

func (f *fakeTree) Scroll(_ context.Context, bus, path string) error {
	f.scrolls = append(f.scrolls, bus+path)
	return nil
}

func (f *fakeTree) DoAction(_ context.Context, bus, path string, i int) error {
	f.actions = append(f.actions, bus+path)
	f.actIdx = append(f.actIdx, i)
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

func obj(path, role, name string, ext [4]int, states ...string) accessible {
	return accessible{Bus: ":1.5", Path: path, Role: role, Name: name, Extents: ext, States: states, PID: 1}
}

func nodesOf(t *testing.T, f *fakeTree, pids ...int) []Node {
	t.Helper()
	nodes, err := newATSPIWith(f, pids).Nodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return nodes
}

func TestATSPIFiltersForeignPIDs(t *testing.T) {
	f := thunarTree(t)
	f.objs = append(f.objs, accessible{Bus: ":1.99", Path: "/org/a11y/atspi/accessible/1", Role: "push button",
		Name: "Human's secret", Extents: [4]int{0, 0, 10, 10}, PID: 999999})
	nodes := nodesOf(t, f, thunarPID)
	if len(nodes) != len(f.objs)-1 {
		t.Fatalf("got %d nodes, want %d", len(nodes), len(f.objs)-1)
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
	src := newATSPIWith(f, []int{1})
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
	src := newATSPIWith(f, []int{1})
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
