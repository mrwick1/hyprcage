package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hexadecimil/hyprcage/internal/perceive"
	"github.com/hexadecimil/hyprcage/internal/registry"
	"github.com/hexadecimil/hyprcage/internal/screen"
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

func TestCDPCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{}`) }))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	// the test process answers but belongs to no screen
	if st, d := cdpCheck(&registry.Screen{Name: "hc-1", DebugPort: port}); st != "warn" || !strings.Contains(d, "no longer holds it") {
		t.Errorf("foreign listener: %s %s", st, d)
	}
	free, _ := screen.FreePort()
	if st, d := cdpCheck(&registry.Screen{Name: "hc-1", DebugPort: free}); st != "warn" || !strings.Contains(d, "no answer") {
		t.Errorf("closed port: %s %s", st, d)
	}
}
