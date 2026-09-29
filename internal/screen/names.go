package screen

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strings"
)

var nameRE = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

// ValidateName enforces cahier F1: the name crosses Lua, sh and Hyprland's
// IPC, so only a strict alphabet is accepted.
func ValidateName(name string) error {
	if !nameRE.MatchString(name) {
		return errf(CodeInvalidName, "use lowercase letters, digits and dashes", "%q must match ^[a-z0-9-]{1,32}$", name)
	}
	if name == "desktop" {
		return errf(CodeInvalidName, "choose another name", "%q is the record target of the human's desktop", name)
	}
	return nil
}

// Prefixed is the screen name for a name a caller chose. The configured
// prefix is forced on because it is what tells an agent output from one of
// the human's monitors everywhere else, from the restoration of the human's
// workspaces to the collection of orphan outputs.
func Prefixed(name, prefix string) string {
	if prefix == "" || strings.HasPrefix(name, prefix) {
		return name
	}
	return prefix + name
}

// NewName returns prefix + 6 random hex characters.
func NewName(prefix string) string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}
