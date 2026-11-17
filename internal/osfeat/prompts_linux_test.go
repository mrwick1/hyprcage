//go:build linux

package osfeat

import "testing"

func TestIsPrompt(t *testing.T) {
	p := newPrompts([]string{" My-Askpass "})
	for class, want := range map[string]bool{
		"hyprpolkitagent":                           true,
		"polkit-gnome-authentication-agent-1":       true,
		"org.kde.polkit-kde-authentication-agent-1": true,
		"pinentry-qt":                               true,
		"gcr-prompter":                              true,
		"org.kde.ksecretd":                          true,
		"my-askpass":                                true,
		"firefox":                                   false,
		"kitty":                                     false,
		"":                                          false,
	} {
		if got := p.IsPrompt(Window{Class: class}); got != want {
			t.Errorf("%q: got %v", class, got)
		}
	}
}
