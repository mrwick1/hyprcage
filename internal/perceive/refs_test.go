package perceive

import (
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
