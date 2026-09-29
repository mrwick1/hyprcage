package cli

import (
	"testing"
	"time"

	"github.com/hexadecimil/hyprcage/internal/perceive"
	"github.com/hexadecimil/hyprcage/internal/registry"
)

func TestTableFileFollowsScreenInstance(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	rec := &registry.Screen{Name: "hc-1", CreatedAt: time.Unix(100, 5)}
	tb := perceive.NewTable()
	tb.Assign([]perceive.Node{{Key: "cdp:a", Role: "button", Name: "OK"}})
	tb.SetSource("ocr")
	if err := saveTable(rec, tb); err != nil {
		t.Fatal(err)
	}
	got := loadTable(rec)
	if n, ok := got.Lookup("e1"); !ok || n.Name != "OK" || got.SourceName() != "ocr" {
		t.Errorf("loaded %+v %v source %q", n, ok, got.SourceName())
	}
	reused := &registry.Screen{Name: "hc-1", CreatedAt: time.Unix(200, 0)}
	if _, ok := loadTable(reused).Lookup("e1"); ok {
		t.Error("a reused screen name inherited the old refs")
	}
}
