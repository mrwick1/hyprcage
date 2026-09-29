package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

// tableFile is the CLI's ref table of one screen, kept between two calls.
type tableFile struct {
	CreatedAt time.Time       `json:"created_at"`
	Table     *perceive.Table `json:"table"`
}

func tablePath(name string) string {
	return filepath.Join(registry.Dir(), "perceive", name+".json")
}

// loadTable returns the stored table of rec, or a new one when there is
// none or it belongs to an older screen of the same name.
func loadTable(rec *registry.Screen) *perceive.Table {
	f := tableFile{Table: perceive.NewTable()}
	data, err := os.ReadFile(tablePath(rec.Name))
	if err != nil || json.Unmarshal(data, &f) != nil || !f.CreatedAt.Equal(rec.CreatedAt) {
		return perceive.NewTable()
	}
	return f.Table
}

// saveTable stores t for the next call.
// ponytail: last writer wins when two CLI calls run on one screen at once.
func saveTable(rec *registry.Screen, t *perceive.Table) error {
	path := tablePath(rec.Name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(tableFile{CreatedAt: rec.CreatedAt, Table: t})
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// perceiveSource opens the screen, loads its table and chooses a source:
// want, or the source of the table's last read when want is empty. The
// caller closes the source.
func perceiveSource(ctx context.Context, name, want string) (*registry.Screen, *wl.Client, perceive.Source, *perceive.Table, error) {
	rec, cl, err := openScreen(name)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	t := loadTable(rec)
	if want == "" {
		want = t.SourceName()
	}
	src, err := perceive.Choose(ctx, rec, cl, want)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return rec, cl, src, t, nil
}

func runSnapshot(e *Env) int {
	fs := e.flags("snapshot")
	modeFlag := fs.String("mode", "interactive", "interactive or full")
	maxNodes := fs.Int("max", 0, "cap on the elements printed (default 300)")
	want := fs.String("source", "auto", "auto, cdp, atspi or ocr")
	root := fs.String("root", "", "ref of a subtree to read instead of the whole screen")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	if fs.NArg() > 1 {
		return e.errorf("usage: hyprcage snapshot [-mode interactive|full] [-max N] [-root REF] [-source auto|cdp|atspi|ocr] [screen]")
	}
	m, err := perceive.ParseMode(*modeFlag)
	if err != nil {
		return e.errorf("%v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), perceiveTimeout)
	defer cancel()
	rec, _, src, t, err := perceiveSource(ctx, fs.Arg(0), *want)
	if err != nil {
		return e.fail(err)
	}
	defer src.Close()
	out, _, err := perceive.Snapshot(ctx, src, t, perceive.SnapOpts{Mode: m, RootRef: *root, MaxNodes: *maxNodes})
	if err != nil {
		return e.fail(err)
	}
	if err := saveTable(rec, t); err != nil {
		return e.fail(err)
	}
	fmt.Fprintln(e.Stdout, out)
	return ExitOK
}

// runAct resolves the ref against the table that the last snapshot or find
// of this screen stored, like the MCP tool does.
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
	if !perceive.ValidOp(op.Op) {
		return e.errorf("unknown op %q (%s)", op.Op, strings.Join(actOps, ", "))
	}
	rec, cl, src, t, err := perceiveSource(ctx, name, "")
	if err != nil {
		return e.fail(err)
	}
	defer src.Close()
	d, err := perceive.Act(ctx, cl, src, t, op)
	if err != nil {
		return e.fail(err)
	}
	if err := saveTable(rec, t); err != nil {
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
	rec, cl, err := openScreen(name)
	if err != nil {
		return e.fail(err)
	}
	t := loadTable(rec)
	var nodes []perceive.Node
	if want := t.SourceName(); want != "auto" {
		var src perceive.Source
		if src, err = perceive.Choose(ctx, rec, cl, want); err != nil {
			return e.fail(err)
		}
		defer src.Close()
		nodes, err = perceive.Find(ctx, src, t, re, *role, timeout)
	} else {
		// No recorded source: the app may not be on the a11y bus yet, so choose on every poll.
		choose := func(ctx context.Context) (perceive.Source, error) { return perceive.Choose(ctx, rec, cl, "auto") }
		nodes, err = perceive.FindAuto(ctx, choose, t, re, *role, timeout)
	}
	if err != nil {
		return e.fail(err)
	}
	if err := saveTable(rec, t); err != nil {
		return e.fail(err)
	}
	fmt.Fprintln(e.Stdout, perceive.RenderMatches(nodes))
	return ExitOK
}
