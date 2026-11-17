package cli

import (
	"fmt"

	"github.com/hexadecimil/offstage/internal/agentrule"
	"github.com/hexadecimil/offstage/internal/config"
)

const agentRuleUsage = "usage: offstage agent-rule [install | remove | print]"

func runAgentRule(e *Env) int {
	fs := e.flags("agent-rule")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	f := agentrule.Install
	switch fs.Arg(0) {
	case "", "install":
	case "remove":
		f = agentrule.Remove
	case "print":
		fmt.Fprint(e.Stdout, agentrule.Block())
		return ExitOK
	default:
		return e.errorf("%s", agentRuleUsage)
	}
	paths := agentrule.Paths()
	if len(paths) == 0 {
		fmt.Fprintln(e.Stdout, "no Claude Code or Codex configuration directory found")
		return ExitOK
	}
	code := ExitOK
	for _, p := range paths {
		st, err := f(p)
		switch {
		case err != nil:
			code = e.errorf("%s: %v", p, err)
		case st == agentrule.Symlink && fs.Arg(0) != "remove":
			fmt.Fprintf(e.Stdout, "%s: %s; add the block from `offstage agent-rule print` by hand\n", p, st)
		default:
			fmt.Fprintf(e.Stdout, "%s: %s\n", p, st)
		}
	}
	return code
}

// syncAgentRule applies report.agent_rule at session start. It prints
// nothing: a SessionStart hook's output goes into the agent's context.
func syncAgentRule() {
	f := agentrule.Install
	if !config.Fallback().Report.AgentRule {
		f = agentrule.Remove
	}
	for _, p := range agentrule.Paths() {
		_, _ = f(p)
	}
}
