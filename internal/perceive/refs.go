package perceive

import (
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
// No match or more than one match gives a stale_ref error.
func ReResolve(old Node, fresh []Node) (Node, error) {
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
