// Package perceive turns accessibility trees into text lines with refs, so
// that an agent acts by ref instead of by screenshot.
package perceive

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// Mode selects which nodes a snapshot keeps.
type Mode string

const (
	ModeInteractive Mode = "interactive"
	ModeFull        Mode = "full"
)

// Node is one element, whatever the source.
type Node struct {
	Key       string // source identity: "cdp:<target>:<backendId>", "atspi:<bus>:<path>", "ocr:<n>"
	Parent    string // Key of the parent, "" at the root
	Ref       string // set by Table.Assign
	Role      string // ARIA-style role name
	Name      string
	Value     string
	Desc      string
	Level     int
	States    []string // subset of: focused checked expanded selected disabled required invalid readonly
	X, Y      int      // centre, screen pixels
	Offscreen bool
}

// Header is the first line of a snapshot.
type Header struct {
	Source    string
	Nodes     int
	Truncated bool
}

var interactive = map[string]bool{
	"button": true, "link": true, "textbox": true, "searchbox": true, "checkbox": true,
	"radio": true, "combobox": true, "listbox": true, "option": true, "menuitem": true,
	"menuitemcheckbox": true, "menuitemradio": true, "tab": true, "treeitem": true,
	"switch": true, "slider": true, "spinbutton": true, "heading": true, "banner": true,
	"navigation": true, "main": true, "complementary": true, "contentinfo": true,
	"dialog": true, "alertdialog": true, "search": true, "form": true, "region": true,
}

// Interactive reports whether role is a control, a landmark or a heading.
func Interactive(role string) bool { return interactive[role] }

// Filter keeps the node rootKey and its descendants when rootKey is not
// empty, then keeps only the interactive nodes in ModeInteractive.
func Filter(nodes []Node, m Mode, rootKey string) []Node {
	parent := make(map[string]string, len(nodes))
	for _, n := range nodes {
		parent[n.Key] = n.Parent
	}
	under := func(key string) bool {
		// ponytail: the depth cap guards against a cycle in bad source data.
		for i := 0; key != "" && i <= len(nodes); i++ {
			if key == rootKey {
				return true
			}
			key = parent[key]
		}
		return false
	}
	var out []Node
	for _, n := range nodes {
		if rootKey != "" && !under(n.Key) {
			continue
		}
		if m == ModeInteractive && !Interactive(n.Role) {
			continue
		}
		out = append(out, n)
	}
	return out
}

// Render writes the header line, then one line per node.
func Render(h Header, nodes []Node) string {
	var b strings.Builder
	fmt.Fprintf(&b, "source=%s nodes=%d truncated=%t", h.Source, h.Nodes, h.Truncated)
	for _, n := range nodes {
		b.WriteByte('\n')
		b.WriteString(line(n))
	}
	return b.String()
}

func line(n Node) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%s] %s %s", n.Ref, n.Role, quote(n.Name))
	if n.Value != "" {
		b.WriteString(" value=" + quote(n.Value))
	}
	fmt.Fprintf(&b, " (%d,%d)", n.X, n.Y)
	for _, s := range n.States {
		b.WriteString(" " + s)
	}
	if n.Level > 0 {
		fmt.Fprintf(&b, " level=%d", n.Level)
	}
	if n.Offscreen {
		b.WriteString(" offscreen")
	}
	return b.String()
}

// quote cuts s to 120 runes and escapes it so that it stays on one line.
// A backslash or a quote gets a backslash. A control character, U+2028 or
// U+2029 becomes one space.
func quote(s string) string {
	if r := []rune(s); len(r) > 120 {
		s = string(r[:120]) + "…"
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '\\' || r == '"':
			b.WriteByte('\\')
			b.WriteRune(r)
		case unicode.IsControl(r) || r == '\u2028' || r == '\u2029':
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// Change is one node whose name, value or states differ between snapshots.
type Change struct{ Before, After Node }

// Diff is the difference between two snapshots.
type Diff struct {
	Added, Removed []Node
	Changed        []Change
}

// DiffNodes matches nodes by Key. A move alone is not a change.
func DiffNodes(before, after []Node) Diff {
	old := make(map[string]Node, len(before))
	for _, n := range before {
		old[n.Key] = n
	}
	seen := make(map[string]bool, len(after))
	var d Diff
	for _, n := range after {
		seen[n.Key] = true
		b, ok := old[n.Key]
		switch {
		case !ok:
			d.Added = append(d.Added, n)
		case b.Name != n.Name || b.Value != n.Value || !sameStates(b.States, n.States):
			d.Changed = append(d.Changed, Change{Before: b, After: n})
		}
	}
	for _, n := range before {
		if !seen[n.Key] {
			d.Removed = append(d.Removed, n)
		}
	}
	return d
}

func sameStates(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

// Empty reports whether nothing was added, removed or changed.
func (d Diff) Empty() bool {
	return len(d.Added) == 0 && len(d.Removed) == 0 && len(d.Changed) == 0
}

// String writes removed, added, then changed nodes, one line each.
func (d Diff) String() string {
	if d.Empty() {
		return "no change"
	}
	var lines []string
	for _, n := range d.Removed {
		lines = append(lines, "- "+line(n))
	}
	for _, n := range d.Added {
		lines = append(lines, "+ "+line(n))
	}
	for _, c := range d.Changed {
		lines = append(lines, "~ "+line(c.After))
	}
	return strings.Join(lines, "\n")
}
