package perceive

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/hexadecimil/hyprcage/internal/screen"
)

// Table gives each node Key a stable ref and keeps the last snapshot.
type Table struct {
	mu   sync.Mutex
	e, o int               // last e<n> and o<n> handed out
	refs map[string]string // Key to ref
	last map[string]Node   // ref to node, from the last Assign
	src  string            // source name of the last Snapshot or Find, "" before
}

// SetSource records the name of the source that the last read came from.
func (t *Table) SetSource(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.src = name
}

// SourceName returns the source of the last Snapshot or Find, or "auto"
// when there was none. Act and Find choose this source so that the refs
// they resolve come from the same source.
func (t *Table) SourceName() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.src == "" {
		return "auto"
	}
	return t.src
}

// fillRefs sets Ref on each node whose Key has one, without handing out
// new refs and without touching the last snapshot.
func (t *Table) fillRefs(nodes []Node) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := range nodes {
		nodes[i].Ref = t.refs[nodes[i].Key]
	}
}

// tableJSON is the stored form of a Table, for the CLI between two calls.
type tableJSON struct {
	Source string            `json:"source,omitempty"`
	E      int               `json:"e"`
	O      int               `json:"o"`
	Refs   map[string]string `json:"refs"`
	Last   map[string]Node   `json:"last"`
}

// MarshalJSON stores the refs, both counters, the last snapshot and the source.
func (t *Table) MarshalJSON() ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return json.Marshal(tableJSON{Source: t.src, E: t.e, O: t.o, Refs: t.refs, Last: t.last})
}

// UnmarshalJSON restores what MarshalJSON stored.
func (t *Table) UnmarshalJSON(data []byte) error {
	var j tableJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.src, t.e, t.o, t.refs, t.last = j.Source, j.E, j.O, j.Refs, j.Last
	if t.refs == nil {
		t.refs = map[string]string{}
	}
	if t.last == nil {
		t.last = map[string]Node{}
	}
	return nil
}

// NewTable returns an empty table.
func NewTable() *Table {
	return &Table{refs: map[string]string{}, last: map[string]Node{}}
}

// Assign sets Ref on each node in place. A known Key keeps its ref. A new
// Key gets the next e<n>, or o<n> when the Key starts with "ocr:". Assign
// replaces the table's last snapshot with nodes.
func (t *Table) Assign(nodes []Node) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.last = make(map[string]Node, len(nodes))
	for i := range nodes {
		ref, ok := t.refs[nodes[i].Key]
		if !ok {
			if strings.HasPrefix(nodes[i].Key, "ocr:") {
				t.o++
				ref = fmt.Sprintf("o%d", t.o)
			} else {
				t.e++
				ref = fmt.Sprintf("e%d", t.e)
			}
			t.refs[nodes[i].Key] = ref
		}
		nodes[i].Ref = ref
		t.last[ref] = nodes[i]
	}
}

// Lookup returns the node that ref named in the last snapshot.
func (t *Table) Lookup(ref string) (Node, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	n, ok := t.last[ref]
	return n, ok
}

// ReResolve looks in fresh for exactly one node with old.Role and old.Name.
// No match, more than one match, or an unnamed old node gives a stale_ref
// error, so that act never guesses between look-alike controls.
func ReResolve(old Node, fresh []Node) (Node, error) {
	if old.Name == "" {
		return Node{}, screen.Errf(screen.CodeStaleRef, "take a new snapshot",
			"%s has no name to re-resolve by", old.Role)
	}
	var found []Node
	for _, n := range fresh {
		if n.Role == old.Role && n.Name == old.Name {
			found = append(found, n)
		}
	}
	if len(found) == 1 {
		return found[0], nil
	}
	return Node{}, screen.Errf(screen.CodeStaleRef, "take a new snapshot",
		"%s %q matches %d nodes, want 1", old.Role, old.Name, len(found))
}
