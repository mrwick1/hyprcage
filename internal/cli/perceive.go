package cli

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/hexadecimil/hyprcage/internal/perceive"
	"github.com/hexadecimil/hyprcage/internal/registry"
	"github.com/hexadecimil/hyprcage/internal/wl"
)

const perceiveTimeout = 30 * time.Second

var actOps = []string{"click", "double_click", "type", "key", "hover", "scroll"}

// perceiveSource opens the screen and chooses its source. The caller closes it.
func perceiveSource(ctx context.Context, name, want string) (*registry.Screen, *wl.Client, perceive.Source, error) {
	rec, cl, err := openScreen(name)
	if err != nil {
		return nil, nil, nil, err
	}
	src, err := perceive.Choose(ctx, rec, cl, want)
	if err != nil {
		return nil, nil, nil, err
	}
	return rec, cl, src, nil
}

func runSnapshot(e *Env) int {
	fs := e.flags("snapshot")
	modeFlag := fs.String("mode", "interactive", "interactive or full")
	maxNodes := fs.Int("max", 0, "cap on the elements printed (default 300)")
	want := fs.String("source", "auto", "auto, cdp, atspi or ocr")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	if fs.NArg() > 1 {
		return e.errorf("usage: hyprcage snapshot [-mode interactive|full] [-max N] [-source auto|cdp|atspi|ocr] [screen]")
	}
	m, err := perceive.ParseMode(*modeFlag)
	if err != nil {
		return e.errorf("%v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), perceiveTimeout)
	defer cancel()
	_, _, src, err := perceiveSource(ctx, fs.Arg(0), *want)
	if err != nil {
		return e.fail(err)
	}
	defer src.Close()
	out, _, err := perceive.Snapshot(ctx, src, perceive.NewTable(), perceive.SnapOpts{Mode: m, MaxNodes: *maxNodes})
	if err != nil {
		return e.fail(err)
	}
	fmt.Fprintln(e.Stdout, out)
	return ExitOK
}

// runAct keeps no table between calls: it takes a fresh interactive
// snapshot first, so a ref from an earlier call names the same element only
// while the tree is unchanged.
func runAct(e *Env) int {
	fs := e.flags("act")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	args := fs.Args()
	name := ""
	if len(args) >= 3 && slices.Contains(actOps, args[2]) && !slices.Contains(actOps, args[1]) {
		name, args = args[0], args[1:]
	}
	if len(args) < 2 {
		return e.errorf("usage: hyprcage act [screen] <ref> <%s> [text or keys...]", strings.Join(actOps, "|"))
	}
	op := perceive.ActOp{Ref: args[0], Op: args[1]}
	switch op.Op {
	case "key":
		op.Keys = args[2:]
	case "type":
		op.Text = strings.Join(args[2:], " ")
	case "scroll":
		if len(args) > 2 {
			op.Direction = args[2]
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), perceiveTimeout)
	defer cancel()
	_, cl, src, err := perceiveSource(ctx, name, "auto")
	if err != nil {
		return e.fail(err)
	}
	defer src.Close()
	t := perceive.NewTable()
	if _, _, err := perceive.Snapshot(ctx, src, t, perceive.SnapOpts{}); err != nil {
		return e.fail(err)
	}
	d, err := perceive.Act(ctx, cl, src, t, op)
	if err != nil {
		return e.fail(err)
	}
	fmt.Fprintln(e.Stdout, d.String())
	return ExitOK
}

func runFind(e *Env) int {
	fs := e.flags("find")
	role := fs.String("role", "", "keep only this role")
	timeoutMs := fs.Int("timeout", 0, "wait up to this many milliseconds for a match")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	name, expr := "", fs.Arg(0)
	switch fs.NArg() {
	case 1:
	case 2:
		name, expr = fs.Arg(0), fs.Arg(1)
	default:
		return e.errorf("usage: hyprcage find [-role R] [-timeout ms] [screen] <regexp>")
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return e.errorf("invalid regular expression: %v", err)
	}
	timeout := time.Duration(max(*timeoutMs, 0)) * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), timeout+perceiveTimeout)
	defer cancel()
	_, _, src, err := perceiveSource(ctx, name, "auto")
	if err != nil {
		return e.fail(err)
	}
	defer src.Close()
	nodes, err := perceive.Find(ctx, src, perceive.NewTable(), re, *role, timeout)
	if err != nil {
		return e.fail(err)
	}
	fmt.Fprintln(e.Stdout, perceive.RenderMatches(nodes))
	return ExitOK
}
