package perceive

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/hexadecimil/hyprcage/internal/desktop"
	"github.com/hexadecimil/hyprcage/internal/hypr"
	"github.com/hexadecimil/hyprcage/internal/registry"
	"github.com/hexadecimil/hyprcage/internal/screen"
	"github.com/hexadecimil/hyprcage/internal/session"
)

// Target is what a desktop snapshot reads: one window, or every visible one.
type Target struct {
	Windows []hypr.Client // one entry with a window address, else the visible ones
	Named   bool          // the caller named the one window of Windows
}

// The probes of ChooseDesktop, replaced in tests.
var (
	devToolsPort    = DevToolsPort
	pidsHaveApps    = PIDsHaveApps
	newDesktopATSPI = NewDesktopATSPI
)

// ChooseDesktop picks CDP when the window's PID owns a listening DevTools
// port, then AT-SPI filtered to the windows' PIDs, then OCR on the window
// image (screencopy without a window). want is "auto", "cdp", "atspi" or "ocr".
// CDP needs a single window: its coordinates are relative to that window.
func ChooseDesktop(ctx context.Context, c *desktop.Conn, t Target, want string) (Source, error) {
	port := 0
	pids := make([]int, 0, len(t.Windows))
	for _, w := range t.Windows {
		pids = append(pids, w.PID)
	}
	if len(t.Windows) == 1 {
		port = devToolsPort(t.Windows[0].PID)
	}
	// ponytail: CDP reads every page target of the process, so a Chromium
	// with several windows shows the pages of all of them.
	cdp := func() (Source, error) {
		src, err := newCDP(ctx, port)
		if err != nil {
			return nil, err
		}
		return shifted{src, t.Windows[0].At}, nil
	}
	ocrOK := func() bool { _, err := os.Stat(ocrData); return err == nil }
	ocr := func() Source {
		addr := ""
		if len(t.Windows) == 1 {
			addr = t.Windows[0].Address
		}
		return newOCR(func(o screen.ShotOptions) (*screen.ShotResult, error) { return c.Shot(addr, o) })
	}
	switch want {
	case "cdp":
		if port == 0 {
			return nil, screen.Errf(screen.CodeNoSource, "pass the window of an app started with --remote-debugging-port", "no single window with a listening DevTools port")
		}
		return cdp()
	case "atspi":
		return newDesktopATSPI(ctx, t.Windows, t.Named)
	case "ocr":
		if !ocrOK() {
			return nil, screen.Errf(screen.CodeNoSource, "run hyprcage setup", "no OCR data at %s", ocrData)
		}
		return ocr(), nil
	case "auto":
		cdpNote := ""
		if port > 0 {
			src, err := cdp()
			if err == nil {
				return src, nil
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			cdpNote = fmt.Sprintf(" (CDP: %v)", err)
		}
		if pidsHaveApps(ctx, pids) {
			if src, err := newDesktopATSPI(ctx, t.Windows, t.Named); err == nil {
				return src, nil
			}
		}
		if ocrOK() {
			return ocr(), nil
		}
		return nil, screen.Errf(screen.CodeNoSource, "run hyprcage setup for OCR", "the desktop windows have no DevTools answer, no AT-SPI application and no OCR data%s", cdpNote)
	}
	return nil, fmt.Errorf("unknown source %q (auto, cdp, atspi, ocr)", want)
}

// DevToolsPort returns the port of the --remote-debugging-port argument of
// pid when pid itself listens on it, or 0.
func DevToolsPort(pid int) int {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return 0
	}
	for _, arg := range strings.Split(string(raw), "\x00") {
		v, ok := strings.CutPrefix(arg, "--remote-debugging-port=")
		if !ok {
			continue
		}
		port, _ := strconv.Atoi(v)
		st, err := session.ProcStart(pid)
		if port <= 0 || err != nil || !ownsPort(&registry.Screen{DebugPort: port, DebugPID: pid, DebugPIDStart: st}) {
			return 0
		}
		return port
	}
	return 0
}

// NewDesktopATSPI connects to the a11y bus of the session and keeps only
// the applications of the windows' PIDs. With named, it keeps only the
// top-level object of the one window.
func NewDesktopATSPI(ctx context.Context, wins []hypr.Client, named bool) (Source, error) {
	conn, err := dialA11y(ctx)
	if err != nil {
		return nil, screen.Errf(screen.CodeNoSource, atspiHint, "a11y bus: %v", err)
	}
	s := newDesktopATSPIWith(&dbusTree{conn: conn}, wins, named).(*atspiSource)
	s.closer = conn.Close
	return s, nil
}

// newDesktopATSPIWith builds the desktop source on t. AT-SPI extents are
// window-relative on Hyprland, so each node gets its window's position.
func newDesktopATSPIWith(t tree, wins []hypr.Client, named bool) Source {
	pids := make([]int, 0, len(wins))
	for _, w := range wins {
		pids = append(pids, w.PID)
	}
	s := &atspiSource{t: t, pids: func() []int { return pids }, last: map[string]accessible{}, place: windowAt(wins)}
	if named && len(wins) == 1 {
		s.only = func(objs []accessible) ([]accessible, error) { return windowTree(objs, wins[0]) }
	}
	return s
}

// windowRoles are the top-level roles that are Hyprland windows of their
// own. Other top-levels (a GTK popup menu has role "window") have no
// Hyprland window, so they stay with every window of their app.
var windowRoles = map[string]bool{"frame": true, "dialog": true, "alert": true, "file chooser": true}

// windowTree keeps the application objects, the popups, and the subtree of
// the top-level object of w: the child of the application whose name is w's
// title. When no title matches, a single top-level stays and several are
// refused.
func windowTree(objs []accessible, w hypr.Client) ([]accessible, error) {
	role := make(map[string]string, len(objs))
	for _, o := range objs {
		role[atspiKey(o.Bus, o.Path)] = o.Role
	}
	tops, match := 0, ""
	for _, o := range objs {
		if o.Parent != "" && role[atspiKey(o.Bus, o.Parent)] == "application" && windowRoles[o.Role] {
			tops++
			if match == "" && o.Name == w.Title {
				match = atspiKey(o.Bus, o.Path)
			}
		}
	}
	if match == "" {
		if tops > 1 {
			return nil, screen.Errf(screen.CodeUnsupported, "several windows of this app; use source ocr", "no AT-SPI window of pid %d has the title %q", w.PID, w.Title)
		}
		return objs, nil
	}
	keep := map[string]bool{match: true}
	out := make([]accessible, 0, len(objs))
	for _, o := range objs {
		k := atspiKey(o.Bus, o.Path)
		popup := role[atspiKey(o.Bus, o.Parent)] == "application" && !windowRoles[o.Role]
		if k == match || popup || (o.Parent != "" && keep[atspiKey(o.Bus, o.Parent)]) {
			keep[k] = true
			out = append(out, o)
		} else if o.Role == "application" {
			out = append(out, o)
		}
	}
	return out, nil
}

// windowAt returns the position of the window of a top-level object: the
// window of its PID with its title, else the first window of its PID.
// ponytail: two windows of one PID with the same title share the first position.
func windowAt(wins []hypr.Client) func(accessible) (int, int) {
	return func(top accessible) (int, int) {
		at, found := [2]int{}, false
		for _, w := range wins {
			if w.PID != top.PID {
				continue
			}
			if w.Title == top.Name {
				return w.At[0], w.At[1]
			}
			if !found {
				at, found = w.At, true
			}
		}
		return at[0], at[1]
	}
}

// shifted moves the nodes of a window-relative source by the window's
// position.
type shifted struct {
	Source
	at [2]int
}

func (s shifted) move(n Node) Node {
	if !n.Offscreen {
		n.X, n.Y = n.X+s.at[0], n.Y+s.at[1]
	}
	return n
}

func (s shifted) Nodes(ctx context.Context) ([]Node, error) {
	nodes, err := s.Source.Nodes(ctx)
	for i := range nodes {
		nodes[i] = s.move(nodes[i])
	}
	return nodes, err
}

func (s shifted) Reveal(ctx context.Context, key string) (Node, error) {
	n, err := s.Source.Reveal(ctx, key)
	return s.move(n), err
}

func (s shifted) HitTest(ctx context.Context, key string) error {
	if h, ok := s.Source.(hitTester); ok {
		return h.HitTest(ctx, key)
	}
	return nil
}
