package screen

import (
	"errors"
	"testing"

	"github.com/hexadecimil/offstage/internal/config"
	"github.com/hexadecimil/offstage/internal/registry"
)

// create adds the prefix to the name it is given, so the same name without
// the prefix must find the screen.
func TestGetFindsTheNameWithoutPrefix(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	if _, err := registry.EnsureDir(); err != nil {
		t.Fatal(err)
	}
	if err := registry.Save(&registry.Screen{Name: "hc-foo", Version: RecordVersion}); err != nil {
		t.Fatal(err)
	}
	c := &Ctx{Cfg: config.Default()}
	for _, name := range []string{"foo", "hc-foo"} {
		rec, err := Get(c, name)
		var se *Error
		if rec == nil || rec.Name != "hc-foo" || (errors.As(err, &se) && se.Code == CodeNotFound) {
			t.Errorf("Get(%q) = %v, %v; want screen hc-foo", name, rec, err)
		}
	}
	if _, err := Get(c, "bar"); !errors.As(err, new(*Error)) {
		t.Errorf("Get(bar) = %v, want screen_not_found", err)
	}
}
