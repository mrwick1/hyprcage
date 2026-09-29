package perceive

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/hexadecimil/hyprcage/internal/screen"
	"github.com/hexadecimil/hyprcage/internal/setup"
	"github.com/hexadecimil/hyprcage/internal/wl"
)

// OCRData is the path of the English language data.
const OCRData = setup.OCRData

const ocrHint = "install tesseract-data-eng"

// ocrMinConf is the confidence below which a word is dropped as noise.
const ocrMinConf = 60

// ocrSource reads the text of a screen with tesseract. It is the fallback for
// applications with no accessibility tree.
type ocrSource struct {
	cl    *wl.Client
	nodes map[string]Node // last Nodes result, by Key
}

// NewOCR returns the OCR source of the screen that cl is connected to.
func NewOCR(cl *wl.Client) Source { return &ocrSource{cl: cl} }

func (s *ocrSource) Name() string { return "ocr" }

func (s *ocrSource) Nodes(ctx context.Context) ([]Node, error) {
	if _, err := exec.LookPath("tesseract"); err != nil {
		return nil, screen.Errf(screen.CodeNoSource, ocrHint, "tesseract is not installed")
	}
	shot, err := screen.Shot(s.cl, screen.ShotOptions{Scale: 1, Format: "png"})
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "tesseract", "-", "-", "-l", "eng", "tsv")
	cmd.Stdin = bytes.NewReader(shot.Data)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// ponytail: every tesseract failure is reported as no_source; the
		// usual one is the missing eng data, and stderr says which.
		return nil, screen.Errf(screen.CodeNoSource, ocrHint, "tesseract: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	nodes := parseTSV(out)
	s.nodes = make(map[string]Node, len(nodes))
	for _, n := range nodes {
		s.nodes[n.Key] = n
	}
	return nodes, nil
}

// Reveal returns the node unchanged: OCR only sees what is on screen.
func (s *ocrSource) Reveal(_ context.Context, key string) (Node, error) {
	n, ok := s.nodes[key]
	if !ok {
		return Node{}, screen.Errf(screen.CodeStaleRef, "take a new snapshot", "no OCR node %q", key)
	}
	return n, nil
}

func (s *ocrSource) Press(context.Context, string) error {
	return screen.Errf(screen.CodeUnsupported, "click the node with the pointer", "OCR has no press without the pointer")
}

func (s *ocrSource) Close() error { return nil }

// parseTSV turns tesseract TSV into one node per line, in reading order.
// Words of one (block_num, par_num, line_num) with a confidence of at least
// ocrMinConf are joined with spaces; the node sits at the centre of the
// union of their boxes.
func parseTSV(tsv []byte) []Node {
	type line struct {
		words                  []string
		left, top, right, bott int
	}
	var order []string
	lines := map[string]*line{}
	for _, row := range strings.Split(string(tsv), "\n") {
		f := strings.Split(strings.TrimRight(row, "\r"), "\t")
		if len(f) < 12 || f[0] != "5" {
			continue // header, non-word level, or short row
		}
		text := strings.TrimSpace(f[11])
		conf, err := strconv.ParseFloat(f[10], 64)
		if text == "" || err != nil || conf < ocrMinConf {
			continue
		}
		var box [4]int
		bad := false
		for i := range box {
			if box[i], err = strconv.Atoi(f[6+i]); err != nil {
				bad = true
			}
		}
		if bad {
			continue
		}
		l, t, r, b := box[0], box[1], box[0]+box[2], box[1]+box[3]
		id := f[1] + "/" + f[2] + "/" + f[3] + "/" + f[4] // page/block/par/line
		ln := lines[id]
		if ln == nil {
			ln = &line{left: l, top: t, right: r, bott: b}
			lines[id] = ln
			order = append(order, id)
		}
		ln.words = append(ln.words, text)
		ln.left, ln.top = min(ln.left, l), min(ln.top, t)
		ln.right, ln.bott = max(ln.right, r), max(ln.bott, b)
	}
	nodes := make([]Node, 0, len(order))
	for i, id := range order {
		ln := lines[id]
		nodes = append(nodes, Node{
			Key:  fmt.Sprintf("ocr:%d", i),
			Role: "text",
			Name: strings.Join(ln.words, " "),
			X:    (ln.left + ln.right) / 2,
			Y:    (ln.top + ln.bott) / 2,
		})
	}
	return nodes
}
