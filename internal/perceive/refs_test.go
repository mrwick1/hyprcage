package perceive

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/hexadecimil/hyprcage/internal/screen"
)

func TestAssignStable(t *testing.T) {
	tb := NewTable()
	first := []Node{{Key: "k1"}, {Key: "k2"}}
	tb.Assign(first)
	if first[0].Ref != "e1" || first[1].Ref != "e2" {
		t.Fatalf("first: %+v", first)
	}
	second := []Node{{Key: "k2"}, {Key: "k3"}}
	tb.Assign(second)
	if second[0].Ref != "e2" || second[1].Ref != "e3" {
		t.Fatalf("second: %+v", second)
	}
}

func TestAssignOCRPrefix(t *testing.T) {
	tb := NewTable()
	nodes := []Node{{Key: "ocr:0"}, {Key: "cdp:t:1"}}
	tb.Assign(nodes)
	if nodes[0].Ref != "o1" || nodes[1].Ref != "e1" {
		t.Fatalf("refs: %+v", nodes)
	}
}

func TestLookup(t *testing.T) {
	tb := NewTable()
	tb.Assign([]Node{{Key: "k1", Role: "button", Name: "Save"}})
	if n, ok := tb.Lookup("e1"); !ok || n.Name != "Save" {
		t.Fatalf("e1: %+v %v", n, ok)
	}
	if _, ok := tb.Lookup("e99"); ok {
		t.Fatal("e99 found")
	}
}

func TestReResolveUnique(t *testing.T) {
	old := Node{Key: "k1", Role: "button", Name: "Save"}
	fresh := []Node{{Key: "k7", Role: "button", Name: "Cancel"}, {Key: "k8", Role: "button", Name: "Save"}}
	n, err := ReResolve(old, fresh)
	if err != nil || n.Key != "k8" {
		t.Fatalf("got %+v, %v", n, err)
	}
}

func stale(t *testing.T, err error) {
	t.Helper()
	var e *screen.Error
	if !errors.As(err, &e) || e.Code != screen.CodeStaleRef {
		t.Fatalf("want stale_ref, got %v", err)
	}
}

func TestReResolveAmbiguous(t *testing.T) {
	old := Node{Key: "k1", Role: "button", Name: "Close"}
	fresh := []Node{{Key: "k2", Role: "button", Name: "Close"}, {Key: "k3", Role: "button", Name: "Close"}}
	_, err := ReResolve(old, fresh)
	stale(t, err)
}

func TestReResolveMissing(t *testing.T) {
	_, err := ReResolve(Node{Role: "button", Name: "Close"}, []Node{{Role: "link", Name: "Close"}})
	stale(t, err)
}

func TestReResolveUnnamed(t *testing.T) {
	_, err := ReResolve(Node{Role: "button"}, []Node{{Key: "k2", Role: "button"}})
	stale(t, err)
}

func TestTableJSONRoundTrip(t *testing.T) {
	tb := NewTable()
	tb.Assign([]Node{{Key: "cdp:a", Role: "button", Name: "OK"}, {Key: "ocr:1", Role: "text", Name: "hi"}})
	tb.SetSource("cdp")
	data, err := json.Marshal(tb)
	if err != nil {
		t.Fatal(err)
	}
	got := NewTable()
	if err := json.Unmarshal(data, got); err != nil {
		t.Fatal(err)
	}
	if n, ok := got.Lookup("e1"); !ok || n.Key != "cdp:a" || n.Name != "OK" {
		t.Errorf("Lookup e1 = %+v, %v", n, ok)
	}
	if got.SourceName() != "cdp" {
		t.Errorf("source %q", got.SourceName())
	}
	nodes := []Node{{Key: "cdp:a"}, {Key: "cdp:b"}, {Key: "ocr:2"}}
	got.Assign(nodes)
	if nodes[0].Ref != "e1" || nodes[1].Ref != "e2" || nodes[2].Ref != "o2" {
		t.Errorf("refs after round trip: %s %s %s", nodes[0].Ref, nodes[1].Ref, nodes[2].Ref)
	}
}

func TestTableSourceDefaultsToAuto(t *testing.T) {
	if got := NewTable().SourceName(); got != "auto" {
		t.Errorf("empty table source %q, want auto", got)
	}
}

func TestNodeAt(t *testing.T) {
	tb := NewTable()
	tb.Assign([]Node{
		{Key: "a", Name: "Save", X: 100, Y: 100},
		{Key: "b", Name: "Delete", X: 160, Y: 100},
		{Key: "c", Name: "", X: 100, Y: 140},
		{Key: "d", Name: "Hidden", X: 300, Y: 300, Offscreen: true},
		{Key: "e", Name: "Popup item", X: 5, Y: 5, ActionOnly: true},
	})
	for _, c := range []struct {
		x, y int
		want string // "" means no node
	}{
		{100, 100, "Save"},
		{150, 104, "Delete"}, // nearest of two within the radius
		{100, 140, ""},       // the node there has no name
		{300, 300, ""},       // offscreen
		{5, 5, ""},           // coordinates not reliable
		{500, 500, ""},       // nothing near
	} {
		n, ok := tb.NodeAt(c.x, c.y, 30)
		if got := n.Name; ok != (c.want != "") || got != c.want {
			t.Errorf("NodeAt(%d,%d) = %q,%v, want %q", c.x, c.y, got, ok, c.want)
		}
	}
}

func TestNodeAtPrefersTheBoxUnderThePoint(t *testing.T) {
	tb := NewTable()
	tb.Assign([]Node{
		{Key: "a", Name: "Delete", X: 200, Y: 100, W: 200, H: 40}, // wide, spans x 100..300
		{Key: "b", Name: "Save", X: 120, Y: 100, W: 20, H: 20},    // the smaller box that contains (125,100)
		{Key: "c", Name: "Close", X: 140, Y: 100, W: 10, H: 20},   // nearer by centre to (131,100), does not contain it
	})
	// (131,100): Close centre is 9 away, Delete box contains the point, Save box (110..130) does not.
	if n, ok := tb.NodeAt(131, 100, 30); !ok || n.Name != "Delete" {
		t.Errorf("NodeAt(131,100) = %q,%v, want Delete (the box under the pointer)", n.Name, ok)
	}
	// Two boxes contain (125,100): the smaller one wins.
	if n, ok := tb.NodeAt(125, 100, 30); !ok || n.Name != "Save" {
		t.Errorf("NodeAt(125,100) = %q,%v, want Save (smallest box)", n.Name, ok)
	}
}
