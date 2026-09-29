package perceive

import (
	"fmt"
	"os"
	"strings"
	"testing"
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
