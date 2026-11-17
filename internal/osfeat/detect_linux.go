//go:build linux

package osfeat

import (
	"os/exec"

	"github.com/hexadecimil/offstage/internal/config"
)

// Detect returns the backends of this system for cfg.
func Detect(cfg config.Config) Set {
	s := Set{Prompts: newPrompts(cfg.Guard.PromptClasses)}
	if cfg.Dialogs != "host" {
		s.Dialogs = dialogs{}
	}
	if _, err := exec.LookPath("pactl"); err == nil && cfg.AudioEnabled {
		s.Audio = pulseAudio{}
	}
	return s
}
