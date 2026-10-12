package mcpserver

import (
	"strings"

	"github.com/hexadecimil/hyprcage/internal/desktop"
	"github.com/hexadecimil/hyprcage/internal/guard"
	"github.com/hexadecimil/hyprcage/internal/perceive"
)

// labelRadius is how far from a click the guard looks for the element that
// the click lands on, in screen pixels. The snapshot keeps the centre of an
// element only.
const labelRadius = 30

// guardCall is what a tool is about to do, as far as the guard needs it.
type guardCall struct {
	Screen string
	Window string   // desktop window address, empty when unknown
	At     *[2]int  // click position
	Ref    string   // element of the last snapshot
	Op     string   // act op that goes with Ref
	Keys   []string // key combinations to press
	Ack    string
}

// guard applies the [guard] table to a call before it acts. It reads the
// label of the target from the last snapshot of the screen, so it adds no
// perception call: without a snapshot the guard does not block on keywords.
func (s *Server) guard(c guardCall) error {
	onDesktop := desktop.IsDesktop(c.Screen)
	if s.cfg.Guard.Enabled == "off" || (!onDesktop && s.cfg.Guard.Enabled != "all") {
		return nil
	}
	a := guard.Action{Desktop: onDesktop, Keys: c.Keys}
	if onDesktop && len(s.cfg.Guard.Apps) > 0 {
		a.App = s.windowClass(c.Window)
	}
	labels := []string{""}
	if t := s.guardTable(c); t != nil {
		switch {
		case c.Ref != "" && (c.Op == "click" || c.Op == "double_click"):
			n, _ := t.Lookup(c.Ref)
			labels = []string{n.Name}
		case c.At != nil:
			// A box that contains the point can be a container, and the
			// nearest centre can be the real target. Block on either.
			for _, n := range t.NodesAt(c.At[0], c.At[1], labelRadius) {
				labels = append(labels, n.Name)
			}
		}
	}
	g := guard.New(s.cfg.Guard)
	for _, l := range labels {
		a.Label = l
		if err := g.Check(a, c.Ack); err != nil {
			return err
		}
	}
	return nil
}

// guardBatch checks every key and click of a batch on an agent screen.
func (s *Server) guardBatch(in batchIn) error {
	for _, a := range in.Actions {
		c := guardCall{Screen: in.Screen, Ack: in.Ack}
		switch strings.ToLower(a.Op) {
		case "key":
			c.Keys = a.Keys
		case "click", "double_click":
			c.At = &[2]int{a.X, a.Y}
		default:
			continue
		}
		if err := s.guard(c); err != nil {
			return err
		}
	}
	return nil
}

// guardTable returns the ref table of the last snapshot of the target, nil
// when the screen does not exist.
func (s *Server) guardTable(c guardCall) *perceive.Table {
	if desktop.IsDesktop(c.Screen) {
		return s.tableAt(desktopKey(c.Window))
	}
	rec, _, err := s.resolve(c.Screen, false)
	if err != nil {
		return nil
	}
	return s.table(rec)
}

// windowClass returns the app id of a desktop window, empty when it is
// unknown.
func (s *Server) windowClass(address string) string {
	if address == "" {
		return ""
	}
	d, err := s.desktop()
	if err != nil {
		return ""
	}
	wins, err := d.Windows()
	if err != nil {
		return ""
	}
	for _, w := range wins {
		if w.Address == address {
			return w.Class
		}
	}
	return ""
}
