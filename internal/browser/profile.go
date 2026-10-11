package browser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/hexadecimil/hyprcage/internal/screen"
)

var profileName = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)

// ProfilesDir is where named browser profiles live:
// $XDG_DATA_HOME/hyprcage/profiles, else ~/.local/share/hyprcage/profiles.
func ProfilesDir() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" || !filepath.IsAbs(base) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "hyprcage", "profiles"), nil
}

// ProfilePath checks name and returns the profile directory. The name
// check keeps the path inside ProfilesDir.
func ProfilePath(name string) (string, error) {
	if !profileName.MatchString(name) {
		return "", screen.Errf(screen.CodeInvalidName, "use 1 to 40 characters from a-z, 0-9 and -", "invalid profile name %q", name)
	}
	dir, err := ProfilesDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

// lockProfile takes an exclusive flock on a file inside the profile, so two
// browser_open calls cannot both pass the in-use check before either Chrome
// has made its SingletonLock. It fails at once when another process holds it.
func lockProfile(path string) (unlock func(), err error) {
	f, err := os.OpenFile(filepath.Join(path, ".hyprcage.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil // closing the file releases the lock
}

// ProfileInUse reports whether a running Chrome holds the profile at path,
// or another process is opening or removing it.
func ProfileInUse(path string) bool {
	unlock, err := lockProfile(path)
	if err != nil {
		return true
	}
	defer unlock()
	return chromeHolds(path)
}

// chromeHolds reports whether a running Chrome has path as its user data
// dir. Chrome keeps SingletonLock in the profile, a symlink to "host-pid". A
// second Chrome on the same user-data-dir does not start: it hands its
// arguments to the first one and exits, so the second DevTools port never
// opens. A lock left by a dead Chrome does not count. The argv element must
// match exactly, so profile "a" does not match profile "a2".
func chromeHolds(path string) bool {
	link, err := os.Readlink(filepath.Join(path, "SingletonLock"))
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(link[strings.LastIndex(link, "-")+1:])
	if err != nil || pid <= 0 {
		return false
	}
	argv, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		return false
	}
	for _, a := range strings.Split(string(argv), "\x00") {
		if a == "--user-data-dir="+path {
			return true
		}
	}
	return false
}

// openProfile creates the profile directory when it is missing (mode 0700)
// and refuses one that a running Chrome holds. The caller keeps the profile
// locked until Chrome has started, then calls unlock.
func openProfile(name string) (path string, unlock func(), err error) {
	if path, err = ProfilePath(name); err != nil {
		return "", nil, err
	}
	if err = os.MkdirAll(path, 0o700); err != nil {
		return "", nil, err
	}
	if err = os.Chmod(path, 0o700); err != nil {
		return "", nil, err
	}
	busy := screen.Errf(screen.CodeBrowserBusy, "use another profile name, or close the screen that holds it",
		"profile %q is in use", name)
	if unlock, err = lockProfile(path); err != nil {
		return "", nil, busy
	}
	if chromeHolds(path) {
		unlock()
		return "", nil, busy
	}
	if err = keepSession(path); err != nil {
		unlock()
		return "", nil, err
	}
	return path, unlock, nil
}

// keepSession sets Chrome's "continue where you left off" startup option
// (session.restore_on_startup = 1) in the profile. Chrome drops session
// cookies, those with no expiry, at exit unless it restores the session at
// the next start, and many sites log in with a session cookie.
func keepSession(path string) error {
	file := filepath.Join(path, "Default", "Preferences")
	prefs := map[string]any{}
	if b, err := os.ReadFile(file); err == nil {
		if json.Unmarshal(b, &prefs) != nil {
			return nil // Chrome owns a file we cannot read; leave it
		}
	}
	sess, _ := prefs["session"].(map[string]any)
	if sess == nil {
		sess = map[string]any{}
	}
	if v, _ := sess["restore_on_startup"].(float64); v == 1 {
		return nil
	}
	sess["restore_on_startup"] = 1
	prefs["session"] = sess
	b, err := json.Marshal(prefs)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	return os.WriteFile(file, b, 0o600)
}

// ProfileInfo describes one saved profile.
type ProfileInfo struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	InUse bool   `json:"in_use"`
}

// ListProfiles returns the saved profiles by name.
func ListProfiles() ([]ProfileInfo, error) {
	dir, err := ProfilesDir()
	if err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []ProfileInfo
	for _, e := range ents {
		if p := filepath.Join(dir, e.Name()); e.IsDir() && profileName.MatchString(e.Name()) {
			out = append(out, ProfileInfo{Name: e.Name(), Path: p, InUse: ProfileInUse(p)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// RemoveProfile deletes a saved profile, unless a running Chrome holds it.
func RemoveProfile(name string) error {
	path, err := ProfilePath(name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		return screen.Errf(screen.CodeNotFound, "run: hyprcage profile list", "no profile %q", name)
	}
	unlock, err := lockProfile(path)
	if err == nil {
		defer unlock()
	}
	if err != nil || chromeHolds(path) {
		return screen.Errf(screen.CodeBrowserBusy, "destroy the screen that holds it first", "profile %q is in use", name)
	}
	return os.RemoveAll(path)
}
