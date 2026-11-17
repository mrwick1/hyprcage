// Package update installs the newest beta build next to the running binary.
// It downloads, verifies the SHA256 against the release's SHA256SUMS and
// replaces the files in place. It never restarts anything: the MCP server
// decides when it is idle (docs/self-update.md).
package update

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/hexadecimil/offstage/internal/lock"
)

const (
	pluginURL = "https://raw.githubusercontent.com/mrwick1/offstage/beta/.claude-plugin/plugin.json"
	releases  = "https://github.com/mrwick1/offstage/releases/download"
	bareBase  = "https://github.com/mrwick1/bare/releases/latest/download"
)

// Options says what to check. Base, when set, serves plugin.json, the
// release assets with their SHA256SUMS, and the bare assets under bare/ (tests).
type Options struct {
	Exe     string // the binary to replace
	Current string // the running version, for example v0.11.0-beta.12
	Base    string
	Client  *http.Client
}

// Result says what a check installed.
type Result struct {
	Version string // the new offstage version, empty when none
	Bare    bool   // bare-mcp and bare-drive were replaced
}

var release = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.]+)?$`)

// Eligible reports whether v is a release tag. A build from a checkout
// (vX.Y.Z-N-gHASH) and "dev" are not: they never update themselves.
func Eligible(v string) bool { return release.MatchString(v) }

// Newer reports whether release b ranks above a in semver order: a release
// ranks above its prereleases, numeric identifiers compare as numbers.
func Newer(a, b string) bool {
	if !Eligible(a) || !Eligible(b) {
		return false
	}
	ca, pa, _ := strings.Cut(strings.TrimPrefix(a, "v"), "-")
	cb, pb, _ := strings.Cut(strings.TrimPrefix(b, "v"), "-")
	x, y := strings.Split(ca, "."), strings.Split(cb, ".")
	for i := range x {
		p, _ := strconv.Atoi(x[i])
		q, _ := strconv.Atoi(y[i])
		if p != q {
			return p < q
		}
	}
	if pa == pb {
		return false
	}
	if pa == "" || pb == "" {
		return pa != "" // only a prerelease ranks below
	}
	p, q := strings.Split(pa, "."), strings.Split(pb, ".")
	for i := 0; i < len(p) && i < len(q); i++ {
		if p[i] == q[i] {
			continue
		}
		m, e1 := strconv.Atoi(p[i])
		n, e2 := strconv.Atoi(q[i])
		if e1 == nil && e2 == nil {
			return m < n
		}
		return p[i] < q[i]
	}
	return len(p) < len(q)
}

func (o Options) client() *http.Client {
	if o.Client != nil {
		return o.Client
	}
	return &http.Client{Timeout: 5 * time.Minute}
}

func (o Options) urls(tag string) (plugin, rel, bare string) {
	if o.Base != "" {
		b := strings.TrimRight(o.Base, "/")
		return b + "/plugin.json", b, b + "/bare"
	}
	return pluginURL, releases + "/" + tag, bareBase
}

func (o Options) get(url string) (io.ReadCloser, error) {
	resp, err := o.client().Get(url)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return resp.Body, nil
}

// platform returns the asset suffix of this machine, for example linux-amd64.
func platform() (string, error) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "linux/amd64", "linux/arm64", "windows/amd64":
		return runtime.GOOS + "-" + runtime.GOARCH, nil
	}
	return "", fmt.Errorf("no release for %s/%s", runtime.GOOS, runtime.GOARCH)
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// sums reads a SHA256SUMS file into name -> hash.
func (o Options) sums(url string) (map[string]string, error) {
	body, err := o.get(url)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, 1<<20))
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	for _, l := range strings.Split(string(data), "\n") {
		if f := strings.Fields(l); len(f) == 2 {
			m[strings.TrimPrefix(f[1], "*")] = f[0]
		}
	}
	return m, nil
}

// fetch downloads url into a new file in dir and checks its SHA256 against
// want. It returns the path, or removes the file and fails.
func (o Options) fetch(url, dir, want string) (string, error) {
	body, err := o.get(url)
	if err != nil {
		return "", err
	}
	defer body.Close()
	f, err := os.CreateTemp(dir, ".offstage-update-*")
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h), body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && hex.EncodeToString(h.Sum(nil)) != want {
		err = fmt.Errorf("checksum mismatch for %s: not installing", url)
	}
	if err == nil {
		err = os.Chmod(f.Name(), 0o755)
	}
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// place moves tmp over path. Windows cannot overwrite a running exe but can
// rename it: the old file goes to path.old-<ticks> (Cleanup removes it later).
func place(tmp, path string) error {
	if runtime.GOOS != "windows" {
		return os.Rename(tmp, path)
	}
	old := fmt.Sprintf("%s.old-%d", path, time.Now().UnixNano())
	if err := os.Rename(path, old); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Rename(old, path)
		return err
	}
	return nil
}

// Cleanup removes what earlier Windows updates left: path.old-*. A file
// still running cannot be removed; the next start tries again.
func Cleanup(exe string) {
	dir := filepath.Dir(exe)
	for _, n := range []string{filepath.Base(exe), "bare-mcp" + exeSuffix(), "bare-drive" + exeSuffix()} {
		old, _ := filepath.Glob(filepath.Join(dir, n+".old-*"))
		for _, f := range old {
			_ = os.Remove(f)
		}
	}
}

func fileSum(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// diskVersion asks the binary on disk for its version.
func diskVersion(exe string) string {
	out, err := exec.Command(exe, "version").Output()
	if err != nil {
		return ""
	}
	v, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return v
}

// Check looks for a newer beta and installs it. An error leaves every file
// as it was.
func Check(o Options) (Result, error) {
	var res Result
	plat, err := platform()
	if err != nil {
		return res, err
	}
	pURL, _, _ := o.urls("")
	body, err := o.get(pURL)
	if err != nil {
		return res, err
	}
	var manifest struct{ Version string }
	err = json.NewDecoder(io.LimitReader(body, 1<<20)).Decode(&manifest)
	body.Close()
	if err != nil {
		return res, err
	}
	tag := "v" + manifest.Version
	_, relURL, bareURL := o.urls(tag)
	dir := filepath.Dir(o.Exe)
	wantOffstage := Newer(o.Current, tag)
	// Bare comes with the offstage install; one of one's own stays alone.
	mcp, drive := filepath.Join(dir, "bare-mcp"+exeSuffix()), filepath.Join(dir, "bare-drive"+exeSuffix())
	_, e1 := os.Stat(mcp)
	_, e2 := os.Stat(drive)
	haveBare := e1 == nil && e2 == nil
	if !wantOffstage && !haveBare {
		return res, nil
	}
	release, err := lock.File(o.Exe+".update.lock", 3*time.Minute)
	if err != nil {
		return res, err
	}
	defer release()
	if wantOffstage {
		// Another server may have installed it while this one waited.
		if v := diskVersion(o.Exe); v == "" || Newer(v, tag) {
			asset := "offstage-" + plat + exeSuffix()
			s, err := o.sums(relURL + "/SHA256SUMS")
			if err != nil {
				return res, err
			}
			if s[asset] == "" {
				return res, fmt.Errorf("no checksum for %s in SHA256SUMS", asset)
			}
			tmp, err := o.fetch(relURL+"/"+asset, dir, s[asset])
			if err != nil {
				return res, err
			}
			if err := place(tmp, o.Exe); err != nil {
				os.Remove(tmp)
				return res, err
			}
			res.Version = tag
		}
	}
	if haveBare {
		b, err := o.sums(bareURL + "/SHA256SUMS")
		if err != nil {
			return res, err
		}
		mAsset, dAsset := "bare-mcp-"+plat+exeSuffix(), "bare-drive-"+plat+exeSuffix()
		if b[mAsset] == "" || b[dAsset] == "" {
			return res, fmt.Errorf("no checksum for %s or %s in SHA256SUMS", mAsset, dAsset)
		}
		if fileSum(mcp) != b[mAsset] || fileSum(drive) != b[dAsset] {
			t1, err := o.fetch(bareURL+"/"+mAsset, dir, b[mAsset])
			if err != nil {
				return res, err
			}
			t2, err := o.fetch(bareURL+"/"+dAsset, dir, b[dAsset])
			if err != nil {
				os.Remove(t1)
				return res, err
			}
			if err := place(t1, mcp); err != nil {
				os.Remove(t1)
				os.Remove(t2)
				return res, err
			}
			if err := place(t2, drive); err != nil {
				os.Remove(t2)
				return res, err
			}
			res.Bare = true
		}
	}
	return res, nil
}

// Watch notices that the files of an install changed on disk since it was
// made: another server updated them, or the launcher did.
type Watch struct{ exe, sig string }

// NewWatch records the state of the binary and of bare next to it.
func NewWatch(exe string) *Watch { return &Watch{exe, signature(exe)} }

// Changed reports whether any of the files differs from the start.
func (w *Watch) Changed() bool { return signature(w.exe) != w.sig }

func signature(exe string) string {
	dir := filepath.Dir(exe)
	var b strings.Builder
	for _, p := range []string{exe, filepath.Join(dir, "bare-mcp"+exeSuffix()), filepath.Join(dir, "bare-drive"+exeSuffix())} {
		if fi, err := os.Stat(p); err == nil {
			fmt.Fprintf(&b, "%d:%d;", fi.ModTime().UnixNano(), fi.Size())
		} else {
			b.WriteString("-;")
		}
	}
	return b.String()
}
