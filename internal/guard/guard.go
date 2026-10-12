// Package guard decides whether an action needs an acknowledgement first.
// A blocked action fails with the error code ack_required. The caller
// repeats it with ack set to a reason, and the audit log keeps the reason.
package guard

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/hexadecimil/hyprcage/internal/config"
	"github.com/hexadecimil/hyprcage/internal/screen"
)

// Action describes what a tool call is about to do.
type Action struct {
	Desktop bool     // the target is a window of the human's desktop
	App     string   // app id (window class) of the target, empty when unknown
	Keys    []string // key combinations to press
	Label   string   // label of the click target, empty when the perception layer has none
}

// Policy is the [guard] table, ready to check.
type Policy struct {
	enabled  string
	apps     map[string]bool
	combos   map[string]bool
	keywords *regexp.Regexp // nil when there are no keywords
}

// New builds a Policy from the configuration.
func New(g config.Guard) Policy {
	p := Policy{enabled: g.Enabled, apps: map[string]bool{}, combos: map[string]bool{}}
	for _, a := range g.Apps {
		p.apps[strings.ToLower(strings.TrimSpace(a))] = true
	}
	for _, c := range g.Combos {
		p.combos[normalize(c)] = true
	}
	var words []string
	for _, k := range g.Keywords {
		// A final "e" is optional, so "delete" also matches "deleting" and "removing".
		w := strings.TrimSpace(k)
		if stem, ok := strings.CutSuffix(w, "e"); ok && stem != "" {
			words = append(words, regexp.QuoteMeta(stem)+"e?")
		} else {
			words = append(words, regexp.QuoteMeta(w))
		}
	}
	if len(words) > 0 {
		p.keywords = regexp.MustCompile(`(?i)\b(?:` + strings.Join(words, "|") + `)`)
	}
	return p
}

// Check returns nil when the action may run. A non-blank ack lets any
// guarded action run.
func (p Policy) Check(a Action, ack string) error {
	if p.enabled == "off" || (p.enabled != "all" && !a.Desktop) {
		return nil
	}
	reason := p.reason(a)
	if reason == "" || strings.TrimSpace(ack) != "" {
		return nil
	}
	return screen.Errf(screen.CodeAckRequired, `repeat the call with ack set to the reason for this action`, "%s", reason)
}

func (p Policy) reason(a Action) string {
	if len(p.apps) > 0 {
		switch {
		case a.App != "" && !p.apps[strings.ToLower(a.App)]:
			return fmt.Sprintf("app %q is not in the guard allowlist", a.App)
		case a.App == "" && a.Desktop:
			return "the app of the target is unknown and the guard has an allowlist"
		}
	}
	for _, k := range a.Keys {
		if p.combos[normalize(k)] {
			return fmt.Sprintf("key combination %q is blocked by the guard", k)
		}
	}
	if p.keywords != nil && a.Label != "" {
		if w := p.keywords.FindString(a.Label); w != "" {
			return fmt.Sprintf("the target %q matches the destructive word %q", a.Label, strings.ToLower(w))
		}
	}
	return ""
}

var modAlias = map[string]string{"control": "ctrl", "ctl": "ctrl", "meta": "super", "win": "super", "logo": "super", "option": "alt"}

// normalize makes "Shift+Ctrl+Q" and "ctrl+shift+q" the same string.
func normalize(combo string) string {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(combo)), "+")
	key := parts[len(parts)-1]
	mods := parts[:len(parts)-1]
	for i, m := range mods {
		m = strings.TrimSpace(m)
		if a, ok := modAlias[m]; ok {
			m = a
		}
		mods[i] = m
	}
	slices.Sort(mods)
	return strings.Join(append(mods, strings.TrimSpace(key)), "+")
}
