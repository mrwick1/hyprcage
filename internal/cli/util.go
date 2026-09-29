package cli

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hexadecimil/hyprcage/internal/lock"
	"github.com/hexadecimil/hyprcage/internal/screen"
)

// fail prints err and maps it to an exit code (cahier §6.1).
func (e *Env) fail(err error) int {
	fmt.Fprintf(e.Stderr, "hyprcage: %v\n", err)
	var se *screen.Error
	if errors.As(err, &se) {
		switch se.Code {
		case screen.CodeNotFound, screen.CodeDead, screen.CodeNotRecording, screen.CodeRecording:
			return ExitScreen
		case screen.CodeNotOwner:
			return ExitNotOwner
		case screen.CodeHyprland, screen.CodeCage, screen.CodeLocked, screen.CodeBrowserBusy, screen.CodeBrowserDown:
			return ExitDependency
		case screen.CodeAddress, screen.CodeUnsupported:
			return ExitUsage
		case screen.CodeTimeout:
			return ExitTimeout
		}
	}
	if errors.Is(err, lock.ErrBusy) {
		return ExitTimeout
	}
	return ExitUsage
}

func (e *Env) printJSON(v any) int {
	enc := json.NewEncoder(e.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return e.errorf("%v", err)
	}
	return ExitOK
}
