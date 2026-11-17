//go:build linux

package mcpserver

import (
	"testing"

	"github.com/hexadecimil/offstage/internal/config"
	"github.com/hexadecimil/offstage/internal/hypr"
	"github.com/hexadecimil/offstage/internal/osfeat"
)

func TestPromptTarget(t *testing.T) {
	p := osfeat.Detect(config.Default()).Prompts
	if p == nil {
		t.Fatal("no prompts backend on linux")
	}
	editor := hypr.Client{Address: "0xa", Class: "kitty", FocusHistoryID: 0, At: [2]int{0, 0}, Size: [2]int{1000, 1000}}
	prompt := hypr.Client{Address: "0xb", Class: "hyprpolkitagent", FocusHistoryID: 1, At: [2]int{400, 400}, Size: [2]int{200, 100}}
	focused := prompt
	focused.FocusHistoryID = 0
	editor2 := editor
	editor2.FocusHistoryID = 1
	at := func(x, y int) *[2]int { return &[2]int{x, y} }
	for _, tc := range []struct {
		name string
		wins []hypr.Client
		c    guardCall
		want string
	}{
		{"key to the prompt", []hypr.Client{editor, prompt}, guardCall{Window: "0xb"}, "0xb"},
		{"key to another window", []hypr.Client{editor, prompt}, guardCall{Window: "0xa"}, ""},
		{"click on the prompt", []hypr.Client{editor, prompt}, guardCall{Window: "0xa", At: at(450, 420)}, "0xb"},
		{"click beside the prompt", []hypr.Client{editor, prompt}, guardCall{Window: "0xa", At: at(10, 10)}, ""},
		{"focused prompt", []hypr.Client{editor2, focused}, guardCall{Window: "0xa"}, "0xb"},
		{"no prompt", []hypr.Client{editor}, guardCall{At: at(450, 420)}, ""},
	} {
		got := ""
		if w := promptTarget(p, tc.wins, tc.c); w != nil {
			got = w.Address
		}
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
