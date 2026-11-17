//go:build linux

package osfeat

import "strings"

// promptClasses are parts of the app ids of the Linux agents that ask for a
// password or a passphrase: polkit agents (hyprpolkitagent,
// polkit-gnome-authentication-agent-1, lxpolkit and others), pinentry
// (pinentry-qt, pinentry-gtk), the GNOME keyring prompter, the KDE wallet
// and the ssh-askpass programs.
var promptClasses = []string{"polkit", "pinentry", "gcr-prompter", "ksecretd", "kwalletd", "ssh-askpass"}

type prompts struct{ classes []string }

// newPrompts returns the default classes plus extra, in lower case.
func newPrompts(extra []string) prompts {
	p := prompts{}
	for _, c := range append(append([]string{}, promptClasses...), extra...) {
		if c = strings.ToLower(strings.TrimSpace(c)); c != "" {
			p.classes = append(p.classes, c)
		}
	}
	return p
}

// IsPrompt reports whether the app id of w contains one of the classes.
func (p prompts) IsPrompt(w Window) bool {
	class := strings.ToLower(w.Class)
	for _, c := range p.classes {
		if strings.Contains(class, c) {
			return true
		}
	}
	return false
}
