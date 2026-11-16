package cli

import (
	"fmt"
	"time"

	"github.com/hexadecimil/offstage/internal/config"
	"github.com/hexadecimil/offstage/internal/registry"
	"github.com/hexadecimil/offstage/internal/screen"
	"github.com/hexadecimil/offstage/internal/session"
)

func runList(e *Env) int {
	fs := e.flags("list")
	all := fs.Bool("all", false, "screens of every session, not only mine")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := e.parse(fs); err != nil {
		return ExitUsage
	}
	cfg, err := config.Load()
	if err != nil {
		return e.fail(err)
	}
	recs, err := registry.List()
	if err != nil {
		return e.fail(err)
	}
	id := session.Current()
	type row struct {
		*registry.Screen
		Alive     bool      `json:"alive"`
		Mine      bool      `json:"mine"`
		Heartbeat time.Time `json:"heartbeat"`
	}
	rows := []row{}
	for _, r := range recs {
		mine := screen.CheckOwner(r, id) == nil
		if !*all && !mine {
			continue
		}
		hb, _ := registry.Heartbeat(r.Name)
		rows = append(rows, row{r, session.IsAlive(r.Owner, hb, cfg.SessionGrace), mine, hb})
	}
	if *asJSON {
		return e.printJSON(rows)
	}
	if len(rows) == 0 {
		fmt.Fprintln(e.Stdout, "no screen")
		return ExitOK
	}
	for _, r := range rows {
		fmt.Fprintf(e.Stdout, "%-12s %dx%d  mirror=%d  state=%s  owner=%s/%d  alive=%v  mine=%v\n",
			r.Name, r.Width, r.Height, r.WorkspaceMirror, r.State, r.Owner.Client, r.Owner.PID, r.Alive, r.Mine)
	}
	return ExitOK
}
