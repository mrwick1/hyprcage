package screen

import (
	"os"
	"path/filepath"
	"strings"
)

// cage's renderer. Its GLES renderer needs a working EGL on the render node;
// in a VM without 3D acceleration (Hyprland itself on kms_swrast) it never
// maps a window, while the pixman renderer does. No probe tells the two
// apart reliably (Hyprland advertises linux-dmabuf either way), so the
// first creation tries the default and falls back to pixman, and the answer
// is remembered across sessions.

const (
	rendererGLES   = "gles2"
	rendererPixman = "pixman"
)

// RendererStatePath is where the learnt renderer is kept, so that doctor can
// tell the human which file to delete.
func RendererStatePath() string { return rendererStatePath() }

func rendererStatePath() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		base = filepath.Join(os.Getenv("HOME"), ".local", "state")
	}
	return filepath.Join(base, "offstage", "renderer")
}

// KnownRenderer returns the renderer learnt by a previous creation ("" when
// the default GLES renderer works or nothing is known yet).
func KnownRenderer(configured string) string {
	if v := os.Getenv("OFFSTAGE_RENDERER"); v != "" {
		return strings.ToLower(strings.TrimSpace(v))
	}
	if configured != "" && configured != "auto" {
		return configured
	}
	data, err := os.ReadFile(rendererStatePath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// forgetRenderer drops what a previous creation learnt. A remembered
// renderer that then fails is a wrong memory: a busy machine can time a
// probe out, and keeping the answer would hold every later screen on the
// slow renderer. Forgetting it makes the next creation try both again.
func forgetRenderer() {
	_ = os.Remove(rendererStatePath())
}

func rememberRenderer(r string) {
	p := rendererStatePath()
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	_ = os.WriteFile(p, []byte(r+"\n"), 0o600)
}

// rendererCandidates lists what to try, in order: the learnt renderer alone,
// else the GPU renderer followed by pixman. Names are wlroots' own, passed
// as WLR_RENDERER: never "auto", whose silent fallback to software would
// hide from the probe which renderer actually runs.
func rendererCandidates(configured string) []string {
	switch KnownRenderer(configured) {
	case rendererPixman:
		return []string{rendererPixman}
	case rendererGLES, "gles", "default":
		return []string{rendererGLES}
	}
	return []string{rendererGLES, rendererPixman}
}
