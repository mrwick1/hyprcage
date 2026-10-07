package screen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hexadecimil/hyprcage/internal/registry"
	"github.com/hexadecimil/hyprcage/internal/wl"
)

// conns caches one Wayland connection per screen. wl.Client is not safe for
// concurrent use; callers serialise (the MCP server runs one tool at a time).
var conns = struct {
	sync.Mutex
	m map[string]*wl.Client
}{m: map[string]*wl.Client{}}

// Open returns the Wayland connection to the screen's cage, opening it on
// first use and checking that the required protocols are there.
func Open(rec *registry.Screen) (*wl.Client, error) {
	conns.Lock()
	defer conns.Unlock()
	if cl, ok := conns.m[rec.Name]; ok {
		return cl, nil
	}
	if rec.InnerDisplay == "" {
		inner, err := registry.ReadInner(rec.Name)
		if err != nil {
			return nil, errf(CodeDead, "", "screen %s has no inner socket", rec.Name)
		}
		rec.InnerDisplay = inner["WAYLAND_DISPLAY"]
	}
	cl, err := wl.Connect(rec.InnerDisplay)
	if err != nil {
		return nil, errf(CodeDead, "hyprcage destroy "+rec.Name, "cannot connect to cage of %s: %v", rec.Name, err)
	}
	if missing := cl.Missing(); len(missing) > 0 {
		cl.Close()
		return nil, errf(CodeCage, "a newer cage is needed", "cage lacks %s", strings.Join(missing, ", "))
	}
	name := rec.Name
	cl.Glide = func() time.Duration {
		if recording(name) {
			return glideTime
		}
		return 0
	}
	cl.Input = func(kind string, x, y int) {
		if recording(name) {
			noteInput(name, kind, x, y)
		}
	}
	conns.m[rec.Name] = cl
	return cl, nil
}

// glideTime is how long the pointer takes to reach its target on a screen
// that is being recorded.
const glideTime = 400 * time.Millisecond

// RecordDir holds the recorders' files: <target>.json while one runs, and
// <target>.input, the pointer input it shows.
func RecordDir() string { return filepath.Join(registry.Dir(), "rec") }

// InputPath lists the pointer input on a recorded screen, one
// "unix-ms kind x y" line each; kind is move or click.
func InputPath(name string) string { return filepath.Join(RecordDir(), name+".input") }

func recording(name string) bool {
	_, err := os.Stat(filepath.Join(RecordDir(), name+".json"))
	return err == nil
}

// noteInput appends pointer input for the recorder. Input that cannot be
// noted only loses its ring, or the start of its glide, in the video.
func noteInput(name, kind string, x, y int) {
	f, err := os.OpenFile(InputPath(name), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	fmt.Fprintf(f, "%d %s %d %d\n", time.Now().UnixMilli(), kind, x, y)
	f.Close()
}

// CloseConn drops the cached connection of a screen, if any.
func CloseConn(name string) {
	conns.Lock()
	defer conns.Unlock()
	if cl, ok := conns.m[name]; ok {
		cl.Close()
		delete(conns.m, name)
	}
}

// CloseAll drops every cached connection.
func CloseAll() {
	conns.Lock()
	defer conns.Unlock()
	for name, cl := range conns.m {
		cl.Close()
		delete(conns.m, name)
	}
}
