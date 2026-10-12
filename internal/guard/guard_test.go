package guard

import (
	"errors"
	"strings"
	"testing"

	"github.com/hexadecimil/hyprcage/internal/config"
	"github.com/hexadecimil/hyprcage/internal/screen"
)

func policy(mut func(*config.Guard)) Policy {
	g := config.Default().Guard
	if mut != nil {
		mut(&g)
	}
	return New(g)
}

func blocked(t *testing.T, err error) string {
	t.Helper()
	var se *screen.Error
	if !errors.As(err, &se) || se.Code != screen.CodeAckRequired {
		t.Fatalf("got %v, want an ack_required error", err)
	}
	return se.Msg
}

func TestBlockedCombos(t *testing.T) {
	p := policy(nil)
	for _, k := range []string{"ctrl+w", "Ctrl+W", "control+w", "ctrl+q", "alt+F4", "Alt+f4", "ctrl+W"} {
		if err := p.Check(Action{Desktop: true, Keys: []string{"Return", k}}, ""); err == nil {
			t.Errorf("%s was not blocked", k)
		} else if msg := blocked(t, err); !strings.Contains(msg, k) {
			t.Errorf("reason %q does not name %s", msg, k)
		}
	}
}

func TestOtherCombosPass(t *testing.T) {
	p := policy(nil)
	for _, k := range []string{"ctrl+l", "ctrl+shift+w", "Return", "alt+F5", "ctrl+shift+q"} {
		if err := p.Check(Action{Desktop: true, Keys: []string{k}}, ""); err != nil {
			t.Errorf("%s: %v", k, err)
		}
	}
}

func TestModifierOrderDoesNotMatter(t *testing.T) {
	p := policy(func(g *config.Guard) { g.Combos = []string{"Ctrl+Shift+Q"} })
	blocked(t, p.Check(Action{Desktop: true, Keys: []string{"shift+ctrl+q"}}, ""))
}

func TestKeywordLabel(t *testing.T) {
	p := policy(nil)
	for _, l := range []string{"Delete", "delete file", "Empty trash and REMOVE all", "Uninstall app", "Close all tabs", "Shut down", "Format disk", "Erase", "Quit"} {
		blocked(t, p.Check(Action{Desktop: true, Label: l}, ""))
	}
}

func TestKeywordMatchesInflections(t *testing.T) {
	p := policy(nil)
	for _, l := range []string{"Deleted", "Deleting", "Removed", "Removing items", "Uninstalling", "Erased", "Formatting disk"} {
		blocked(t, p.Check(Action{Desktop: true, Label: l}, ""))
	}
}

func TestKeywordNeedsAWholeWord(t *testing.T) {
	p := policy(nil)
	for _, l := range []string{"Information", "Reformat", "Save", "Close", "Close tab", ""} {
		if err := p.Check(Action{Desktop: true, Label: l}, ""); err != nil {
			t.Errorf("%q: %v", l, err)
		}
	}
}

func TestAckLetsTheActionRun(t *testing.T) {
	p := policy(func(g *config.Guard) { g.Apps = []string{"kate"} })
	for _, a := range []Action{
		{Desktop: true, Keys: []string{"ctrl+w"}},
		{Desktop: true, Label: "Delete"},
		{Desktop: true, App: "firefox"},
	} {
		blocked(t, p.Check(a, ""))
		blocked(t, p.Check(a, "  "))
		if err := p.Check(a, "closing the scratch tab I opened"); err != nil {
			t.Errorf("%+v with ack: %v", a, err)
		}
	}
}

func TestAgentScreenIsNotGuardedByDefault(t *testing.T) {
	p := policy(func(g *config.Guard) { g.Apps = []string{"kate"} })
	if err := p.Check(Action{Keys: []string{"ctrl+w"}, Label: "Delete", App: "firefox"}, ""); err != nil {
		t.Fatal(err)
	}
}

func TestEnabledAllGuardsAgentScreens(t *testing.T) {
	p := policy(func(g *config.Guard) { g.Enabled = "all" })
	blocked(t, p.Check(Action{Keys: []string{"ctrl+w"}}, ""))
	blocked(t, p.Check(Action{Label: "Delete"}, ""))
}

func TestEnabledOff(t *testing.T) {
	p := policy(func(g *config.Guard) { g.Enabled = "off"; g.Apps = []string{"kate"} })
	if err := p.Check(Action{Desktop: true, Keys: []string{"ctrl+w"}, Label: "Delete", App: "firefox"}, ""); err != nil {
		t.Fatal(err)
	}
}

func TestAllowlist(t *testing.T) {
	p := policy(func(g *config.Guard) { g.Apps = []string{"kate", "Org.Gnome.Nautilus"} })
	if err := p.Check(Action{Desktop: true, App: "KATE"}, ""); err != nil {
		t.Errorf("listed app: %v", err)
	}
	if err := p.Check(Action{Desktop: true, App: "org.gnome.nautilus"}, ""); err != nil {
		t.Errorf("listed app, other case: %v", err)
	}
	if msg := blocked(t, p.Check(Action{Desktop: true, App: "firefox"}, "")); !strings.Contains(msg, "firefox") {
		t.Errorf("reason %q does not name the app", msg)
	}
}

func TestAllowlistBlocksAnUnknownDesktopApp(t *testing.T) {
	p := policy(func(g *config.Guard) { g.Apps = []string{"kate"} })
	blocked(t, p.Check(Action{Desktop: true}, ""))
}

func TestNoAllowlistMeansAnyApp(t *testing.T) {
	if err := policy(nil).Check(Action{Desktop: true, App: "firefox"}, ""); err != nil {
		t.Fatal(err)
	}
}

func TestAllowlistBlocksAnAgentScreenAction(t *testing.T) {
	p := policy(func(g *config.Guard) { g.Enabled = "all"; g.Apps = []string{"kate"} })
	blocked(t, p.Check(Action{}, ""))
	if err := p.Check(Action{}, "the task needs it"); err != nil {
		t.Errorf("ack: %v", err)
	}
	// Without an allowlist an agent screen action still passes.
	if err := policy(func(g *config.Guard) { g.Enabled = "all" }).Check(Action{}, ""); err != nil {
		t.Errorf("no allowlist: %v", err)
	}
}
