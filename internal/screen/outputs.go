package screen

import (
	"strings"

	"github.com/hexadecimil/offstage/internal/registry"
)

// isAgentOutput reports whether an output belongs to offstage rather than to
// the human. Two sources answer it. The configured prefix covers every name
// offstage builds, and Create forces it on the names its callers pass. The
// registry covers a screen recorded before that rule existed, whose name can
// be anything.
//
// Getting this wrong is not cosmetic. The human's state is captured from the
// outputs left over, so an agent output taken for one of the human's monitors
// makes screen_destroy restore a workspace that only ever lived on the agent
// screen: the human's monitor lands on an empty workspace.
func isAgentOutput(name, prefix string, known map[string]bool) bool {
	return known[name] || (prefix != "" && strings.HasPrefix(name, prefix))
}

// knownScreens is the set of output names the registry holds.
func knownScreens() map[string]bool {
	out := map[string]bool{}
	recs, err := registry.List()
	if err != nil {
		return out
	}
	for _, r := range recs {
		out[r.Name] = true
	}
	return out
}
