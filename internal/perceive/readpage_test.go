package perceive

import (
	"context"
	"strings"
	"testing"
)

func pageNode(key string, y int) Node {
	return Node{Key: key, Role: "link", Name: key, X: 10, Y: y}
}

// A page of six links, three visible per viewport, scrolling to the end.
func scrolledPage() *fakeSource {
	return &fakeSource{name: "cdp", reads: [][]Node{
		{pageNode("a", 10), pageNode("b", 20), pageNode("c", 30)},
		{pageNode("b", 10), pageNode("c", 20), pageNode("d", 30), pageNode("e", 40)},
		{pageNode("d", 10), pageNode("e", 20), pageNode("f", 30)},
		{pageNode("d", 10), pageNode("e", 20), pageNode("f", 30)},
	}}
}

func TestReadPageMergesBelowTheFirstViewport(t *testing.T) {
	src, tb := scrolledPage(), NewTable()
	scrolls := 0
	out, nodes, err := ReadPage(context.Background(), src, tb, ReadOpts{}, func(context.Context) error { scrolls++; return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, n := range nodes {
		keys = append(keys, n.Key)
	}
	if got := strings.Join(keys, ""); got != "abcdef" {
		t.Errorf("keys = %q, want abcdef (each once, in page order)", got)
	}
	if scrolls != 3 {
		t.Errorf("scrolls = %d, want 3", scrolls)
	}
	if !strings.HasPrefix(out, "source=cdp nodes=6 truncated=false") {
		t.Errorf("header: %q", out)
	}
	for _, n := range nodes {
		if r, ok := tb.Lookup(n.Ref); !ok || r.Key != n.Key {
			t.Errorf("ref %s of %s not in the table", n.Ref, n.Key)
		}
	}
}

func TestReadPageStopsOnAPageThatDoesNotScroll(t *testing.T) {
	src := &fakeSource{name: "cdp", reads: [][]Node{{pageNode("a", 10), pageNode("b", 20)}}}
	scrolls := 0
	_, nodes, err := ReadPage(context.Background(), src, NewTable(), ReadOpts{}, func(context.Context) error { scrolls++; return nil }, nil)
	if err != nil || len(nodes) != 2 {
		t.Fatalf("nodes=%d err=%v", len(nodes), err)
	}
	if scrolls != 1 {
		t.Errorf("scrolls = %d, want 1", scrolls)
	}
}

func TestReadPageStopsAtMaxSteps(t *testing.T) {
	src := &changing{fakeSource{name: "cdp"}}
	scrolls := 0
	_, _, err := ReadPage(context.Background(), src, NewTable(), ReadOpts{MaxSteps: 4}, func(context.Context) error { scrolls++; return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if scrolls != 3 {
		t.Errorf("scrolls = %d, want 3 (4 snapshots)", scrolls)
	}
}

func TestReadPageCapsNodes(t *testing.T) {
	out, nodes, err := ReadPage(context.Background(), scrolledPage(), NewTable(), ReadOpts{MaxNodes: 4}, func(context.Context) error { return nil }, nil)
	if err != nil || len(nodes) != 4 || !strings.Contains(out, "truncated=true") {
		t.Errorf("nodes=%d err=%v out=%q", len(nodes), err, out)
	}
}

func TestReadPageRefusesOCR(t *testing.T) {
	src := &fakeSource{name: "ocr", reads: [][]Node{{pageNode("ocr:1", 1)}}}
	if _, _, err := ReadPage(context.Background(), src, NewTable(), ReadOpts{}, func(context.Context) error { return nil }, nil); err == nil {
		t.Error("ocr keys are per snapshot: want an error")
	}
}

// pagedSource shows three links per viewport and starts at viewport 2.
type pagedSource struct {
	fakeSource
	pos int
}

var pagedLinks = []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}

func (p *pagedSource) Nodes(context.Context) ([]Node, error) {
	var out []Node
	for i, k := range pagedLinks[p.pos*3 : p.pos*3+3] {
		out = append(out, pageNode(k, 10*(i+1)))
	}
	return out, nil
}

func TestReadPageScrollsToTheTopFirst(t *testing.T) {
	src := &pagedSource{fakeSource: fakeSource{name: "cdp"}, pos: 2}
	down := func(context.Context) error { src.pos = min(src.pos+1, 2); return nil }
	up := func(context.Context) error { src.pos = max(src.pos-1, 0); return nil }
	_, nodes, err := ReadPage(context.Background(), src, NewTable(), ReadOpts{}, down, up)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, n := range nodes {
		keys = append(keys, n.Key)
	}
	if got := strings.Join(keys, ""); got != "abcdefghi" {
		t.Errorf("keys = %q, want abcdefghi", got)
	}
}
