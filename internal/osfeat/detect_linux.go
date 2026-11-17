//go:build linux

package osfeat

import "github.com/hexadecimil/offstage/internal/config"

// Detect returns the backends of this system for cfg.
func Detect(cfg config.Config) Set {
	return Set{Prompts: newPrompts(cfg.Guard.PromptClasses)}
}
