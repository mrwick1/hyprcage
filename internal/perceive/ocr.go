package perceive

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/hexadecimil/hyprcage/internal/screen"
	"github.com/hexadecimil/hyprcage/internal/setup"
	"github.com/hexadecimil/hyprcage/internal/wl"
)

// OCRData is the path of the English language data.
const OCRData = setup.OCRData

const ocrHint = "run hyprcage setup"

// ocrMinConf is the confidence below which a word is dropped as noise.
const ocrMinConf = 60

// ocrSource reads the text of a screen with tesseract. It is the fallback for
// applications with no accessibility tree.
type ocrSource struct {
	cl    *wl.Client
	mu    sync.Mutex
	nodes map[string]Node // last Nodes result, by Key
}

// NewOCR returns the OCR source of the screen that cl is connected to.
func NewOCR(cl *wl.Client) Source { return &ocrSource{cl: cl} }

func (s *ocrSource) Name() string { return "ocr" }

func (s *ocrSource) Nodes(ctx context.Context) ([]Node, error) {
	nodes, err := s.read(ctx)
	s.store(nodes) // a failure clears the cache: old keys become stale
	return nodes, err
}

func (s *ocrSource) read(ctx context.Context) ([]Node, error) {
	if _, err := exec.LookPath("tesseract"); err != nil {
		return nil, screen.Errf(screen.CodeNoSource, ocrHint, "tesseract is not installed")
	}
	// Full size and PNG only: a downscaled or JPEG image reads worse, and
	// the node coordinates must be screen pixels.
	shot, err := screen.Shot(s.cl, screen.ShotOptions{Scale: 1, Format: "png", MaxSide: 1 << 20, MaxBytes: 1 << 30})
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
	scaleNodes(nodes, shot.Scale)
	return nodes, nil
}

// store replaces the cache that Reveal reads.
func (s *ocrSource) store(nodes []Node) {
	m := make(map[string]Node, len(nodes))
	for _, n := range nodes {
		m[n.Key] = n
	}
	s.mu.Lock()
	s.nodes = m
	s.mu.Unlock()
}

// scaleNodes turns image pixels into screen pixels when the capture was
// downscaled by scale.
func scaleNodes(nodes []Node, scale float64) {
	if scale <= 0 || scale == 1 {
		return
	}
	for i := range nodes {
		nodes[i].X = int(float64(nodes[i].X)/scale + 0.5)
		nodes[i].Y = int(float64(nodes[i].Y)/scale + 0.5)
	}
}

// Reveal returns the node unchanged: OCR only sees what is on screen.
func (s *ocrSource) Reveal(_ context.Context, key string) (Node, error) {
	s.mu.Lock()
	n, ok := s.nodes[key]
	s.mu.Unlock()
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
