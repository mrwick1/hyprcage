package perceive

import (
	"strings"
	"testing"
)

func TestRenderLine(t *testing.T) {
	n := Node{Ref: "e12", Role: "button", Name: "Save", X: 640, Y: 388, States: []string{"focused"}}
	got := strings.Split(Render(Header{Source: "cdp", Nodes: 1}, []Node{n}), "\n")[1]
	if want := `[e12] button "Save" (640,388) focused`; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRenderHeader(t *testing.T) {
	out := Render(Header{Source: "cdp", Nodes: 2}, []Node{{Ref: "e1", Role: "button"}, {Ref: "e2", Role: "link"}})
	if first := strings.Split(out, "\n")[0]; first != "source=cdp nodes=2 truncated=false" {
		t.Fatalf("header %q", first)
	}
}

func TestRenderEscapes(t *testing.T) {
	out := Render(Header{Source: "cdp"}, []Node{{Ref: "e1", Role: "button", Name: "a \"b\"\nc"}})
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("node spans %d lines: %q", len(lines)-1, out)
	}
	if !strings.Contains(lines[1], `"a \"b\" c"`) {
		t.Fatalf("not escaped: %q", lines[1])
	}

	long := strings.Repeat("é", 130)
	line := strings.Split(Render(Header{}, []Node{{Ref: "e1", Role: "text", Name: long}}), "\n")[1]
	if want := `"` + strings.Repeat("é", 120) + `…"`; !strings.Contains(line, want) {
		t.Fatalf("not cut to 120 runes: %q", line)
	}

	for _, c := range []struct{ in, want string }{
		{`x\`, `"x\\"`},
		{"\x1b[31mred", `" [31mred"`},
		{"a\rb\tc", `"a b c"`},
		{"a\u2028b\u2029c", `"a b c"`},
		{strings.Repeat("x", 120), `"` + strings.Repeat("x", 120) + `"`},
	} {
		got := strings.Split(Render(Header{}, []Node{{Ref: "e1", Role: "text", Name: c.in}}), "\n")[1]
		if want := `[e1] text ` + c.want + ` (0,0)`; got != want {
			t.Errorf("name %q: got %q, want %q", c.in, got, want)
		}
	}

	line = strings.Split(Render(Header{}, []Node{{Ref: "e1", Role: "textbox", Value: strings.Repeat("v", 121)}}), "\n")[1]
	if want := ` value="` + strings.Repeat("v", 120) + `…"`; !strings.Contains(line, want) {
		t.Fatalf("value not cut to 120 runes: %q", line)
	}
}

func TestRenderTruncates(t *testing.T) {
	out := Render(Header{Source: "atspi", Nodes: 300, Truncated: true}, nil)
	if first := strings.Split(out, "\n")[0]; first != "source=atspi nodes=300 truncated=true" {
		t.Fatalf("header %q", first)
	}
}

func TestFilterInteractive(t *testing.T) {
	nodes := []Node{{Key: "g", Role: "generic"}, {Key: "b", Role: "button"}}
	if got := Filter(nodes, ModeInteractive, ""); len(got) != 1 || got[0].Key != "b" {
		t.Fatalf("interactive: %+v", got)
	}
	if got := Filter(nodes, ModeFull, ""); len(got) != 2 {
		t.Fatalf("full: %+v", got)
	}
}

func TestFilterRoot(t *testing.T) {
	nodes := []Node{
		{Key: "top", Role: "main"},
		{Key: "mid", Parent: "top", Role: "form"},
		{Key: "leaf", Parent: "mid", Role: "button"},
		{Key: "other", Parent: "top", Role: "link"},
	}
	got := Filter(nodes, ModeFull, "mid")
	if len(got) != 2 || got[0].Key != "mid" || got[1].Key != "leaf" {
		t.Fatalf("root filter: %+v", got)
	}
}

func TestDiff(t *testing.T) {
	before := []Node{{Key: "a", Ref: "e1", Role: "button", Name: "Save"}}
	after := []Node{
		{Key: "a", Ref: "e1", Role: "button", Name: "Saved"},
		{Key: "b", Ref: "e2", Role: "link", Name: "Home"},
	}
	d := DiffNodes(before, after)
	if len(d.Changed) != 1 || d.Changed[0].After.Key != "a" || len(d.Added) != 1 || d.Added[0].Key != "b" || len(d.Removed) != 0 {
		t.Fatalf("diff: %+v", d)
	}
	s := d.String()
	if !strings.Contains(s, "~ [") || !strings.Contains(s, "+ [") {
		t.Fatalf("string: %q", s)
	}
	if d.Empty() {
		t.Fatal("diff reported empty")
	}
}

func TestDiffIgnoresMovesAndStateOrder(t *testing.T) {
	before := []Node{{Key: "a", Role: "checkbox", X: 1, States: []string{"checked", "focused"}}}
	after := []Node{{Key: "a", Role: "checkbox", X: 9, States: []string{"focused", "checked"}}}
	d := DiffNodes(before, after)
	if !d.Empty() || d.String() != "no change" {
		t.Fatalf("diff: %+v", d)
	}
}
