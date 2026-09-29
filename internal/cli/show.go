package cli

import (
	"github.com/hexadecimil/hyprcage/internal/hypr"
	"github.com/hexadecimil/hyprcage/internal/registry"
)

// runShow switches the human to a screen's mirror workspace. It is the
// only command that moves the human on purpose: they asked for it.
func runShow(e *Env) int {
	if len(e.Args) != 1 {
		return e.errorf("usage: hyprcage show <screen>")
	}
	rec, err := registry.Load(e.Args[0])
	if err != nil {
		return e.fail(err)
	}
	if rec.WorkspaceMirror == 0 {
		return e.errorf("%s has no mirror window (%s); open one with: hyprcage mirror %s", rec.Name, rec.MirrorNote, rec.Name)
	}
	inst, err := hypr.Discover()
	if err != nil {
		return e.fail(err)
	}
	if err := inst.Command(inst.Driver().WorkspaceCmd(rec.WorkspaceMirror)); err != nil {
		return e.fail(err)
	}
	return ExitOK
}
