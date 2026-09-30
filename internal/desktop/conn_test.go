package desktop

import (
	"testing"

	"github.com/hexadecimil/hyprcage/internal/hypr"
)

func TestVisible(t *testing.T) {
	mon := func(active, special int) hypr.Monitor {
		return hypr.Monitor{ActiveWorkspace: hypr.WorkspaceRef{ID: active}, SpecialWorkspace: hypr.WorkspaceRef{ID: special}}
	}
	cases := []struct {
		name string
		ws   int
		mons []hypr.Monitor
		want bool
	}{
		{"hidden workspace", 3, []hypr.Monitor{mon(1, 0)}, false},
		{"active workspace", 3, []hypr.Monitor{mon(3, 0)}, true},
		{"active on the second monitor", 3, []hypr.Monitor{mon(1, 0), mon(3, 0)}, true},
		{"open special workspace", -98, []hypr.Monitor{mon(1, -98)}, true},
		{"closed special workspace", -98, []hypr.Monitor{mon(1, 0)}, false},
	}
	for _, c := range cases {
		if got := visible(c.ws, c.mons); got != c.want {
			t.Errorf("%s: visible = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestHandle(t *testing.T) {
	if h, err := handle("0x55ebac116320"); err != nil || h != 0xac116320 {
		t.Errorf("handle = %#x %v, want 0xac116320", h, err)
	}
}
