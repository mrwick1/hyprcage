package config

import (
	"errors"
	"os"
	"path/filepath"
)

// Template is the configuration file written on installation: every key at
// its default, so that changing a setting is editing a line, not
// discovering a key. It must parse to Default(), which a test checks.
const Template = `# hyprcage configuration. Every key is optional and shown at its default;
# a key hyprcage does not know or a value out of range makes it refuse the
# whole file rather than run on the defaults in silence. The file is read
# again by every command, so a change applies to the next screen or mirror
# without restarting anything.

[screen]
width = 1280          # a new screen is this wide, and so is every screenshot of it
height = 800          # and this tall, 1280x800 being what vision models read best
max_width = 3840      # refuse a wider screen, whichever agent asks for it
max_height = 2160     # refuse a taller one
max_per_session = 4   # screens one agent session may hold at once, 0 lifts the limit
mirror = true         # open the window you watch a screen through, unless the agent says otherwise
notify = true         # tell you, through your desktop notifications, when a screen opens or closes

[workspaces]
mirror = [6, 9]       # the mirror windows land here, never on a workspace holding something else

[mirror]
group = "session"     # session: one workspace per agent session, its mirrors tiled side by side
                      # pack: mirrors of every session share a workspace until it is full
                      # screen: one workspace per mirror, each fullscreen
per_workspace = 4     # mirrors a workspace holds before the next one is used (session and pack)
fps = 30              # most frames per second the mirror asks for, and it asks none when idle or hidden

[lifecycle]
safety_timer = "15m"  # close a screen this long after the session that opened it died

[cage]
renderer = "auto"     # auto tries the GPU first and falls back, gles or pixman forces one
render_device = ""    # DRM node cage renders on, empty to follow your compositor's GPU

[record]
dir = "~/Videos/agent" # recordings of screens and of the desktop go here
max = "30m"           # a recording stops by itself after this long
fps = 30              # frames per second of a recording

[browser]
command = ""          # Chrome binary for browser_open, empty to look one up on PATH
`

// WriteDefault writes Template at Path() when no file is there yet and
// reports whether it did. An existing file is never touched.
func WriteDefault() (path string, written bool, err error) {
	path = Path()
	if _, err := os.Stat(path); err == nil {
		return path, false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return path, false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return path, false, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return path, false, nil
		}
		return path, false, err
	}
	if _, err := f.WriteString(Template); err != nil {
		f.Close()
		return path, false, err
	}
	return path, true, f.Close()
}
