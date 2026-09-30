package perceive

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/hexadecimil/hyprcage/internal/screen"
)

func fixtureTSV(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/ocr_lines.tsv")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseTSVLines(t *testing.T) {
	for _, n := range parseTSV(fixtureTSV(t)) {
		if n.Name != "Continue without Signing In" {
			continue
		}
		if n.Role != "text" || n.Parent != "" || n.Offscreen {
			t.Fatalf("node %+v", n)
		}
		if dx, dy := n.X-990, n.Y-650; dx*dx+dy*dy > 100 {
			t.Fatalf("centre (%d,%d), want within 10 px of (990,650)", n.X, n.Y)
		}
		return
	}
	t.Fatal(`no node named "Continue without Signing In"`)
}

func TestParseTSVLowConfidence(t *testing.T) {
	tsv := "level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\n" +
		"5\t1\t1\t1\t1\t1\t10\t10\t40\t10\t95.0\tSave\n" +
		"5\t1\t1\t1\t1\t2\t60\t10\t40\t10\t30.0\tgarbage\n" +
		"5\t1\t1\t1\t2\t1\t10\t30\t40\t10\t30.0\tnoise\n"
	nodes := parseTSV([]byte(tsv))
	if len(nodes) != 1 || nodes[0].Name != "Save" {
		t.Fatalf("got %+v, want one node \"Save\"", nodes)
	}
	for _, n := range parseTSV(fixtureTSV(t)) {
		if strings.Contains(n.Name, "garbage") {
			t.Fatalf("low-confidence word in %q", n.Name)
		}
	}
}

func TestParseTSVKeys(t *testing.T) {
	nodes := parseTSV(fixtureTSV(t))
	if len(nodes) < 2 {
		t.Fatalf("only %d nodes", len(nodes))
	}
	for i, n := range nodes {
		if want := fmt.Sprintf("ocr:%d", i); n.Key != want {
			t.Fatalf("node %d key %q, want %q", i, n.Key, want)
		}
	}
}

func TestScaleNodes(t *testing.T) {
	nodes := []Node{{X: 100, Y: 50}, {X: 3, Y: 7}}
	scaleNodes(nodes, 0.5)
	if nodes[0].X != 200 || nodes[0].Y != 100 || nodes[1].X != 6 || nodes[1].Y != 14 {
		t.Fatalf("scale 0.5: %+v", nodes)
	}
	scaleNodes(nodes, 1)
	if nodes[0].X != 200 || nodes[0].Y != 100 {
		t.Fatalf("scale 1 changed %+v", nodes[0])
	}
}

func TestOCRRevealAfterFailureIsStale(t *testing.T) {
	s := &ocrSource{}
	s.store(parseTSV(fixtureTSV(t)))
	if _, err := s.Reveal(context.Background(), "ocr:0"); err != nil {
		t.Fatalf("Reveal after store: %v", err)
	}
	s.store(nil) // what a failed Nodes does
	_, err := s.Reveal(context.Background(), "ocr:0")
	var se *screen.Error
	if !errors.As(err, &se) || se.Code != screen.CodeStaleRef {
		t.Fatalf("Reveal after a failure: %v, want stale_ref", err)
	}
}

// TestOCRCacheConcurrent fails under -race without the cache mutex.
func TestOCRCacheConcurrent(t *testing.T) {
	s := &ocrSource{}
	nodes := parseTSV(fixtureTSV(t))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); s.store(nodes) }()
		go func() { defer wg.Done(); _, _ = s.Reveal(context.Background(), "ocr:0") }()
	}
	wg.Wait()
}

// TestCrashedAfterOutput: a tesseract killed by a signal after its TSV is a
// read; a crash before any TSV, or a plain non-zero exit, stays an error.
func TestCrashedAfterOutput(t *testing.T) {
	run := func(script string) ([]byte, error) { return exec.Command("sh", "-c", script).Output() }
	for _, c := range []struct {
		script string
		want   bool
	}{
		{`printf 'level\tpage_num\n1\t1\n'; kill -SEGV $$`, true},
		{`kill -SEGV $$`, false},
		{`printf 'level\tpage_num\n'; exit 1`, false},
	} {
		out, err := run(c.script)
		if got := crashedAfterOutput(err, out); got != c.want {
			t.Errorf("%s: got %v, want %v (err %v)", c.script, got, c.want, err)
		}
	}
}
