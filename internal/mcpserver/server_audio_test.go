package mcpserver

import (
	"errors"
	"strings"
	"testing"

	"github.com/hexadecimil/offstage/internal/osfeat"
)

type fakeAudio struct {
	osfeat.Audio
	st  osfeat.AudioState
	err error
}

func (f fakeAudio) Observe(string) (osfeat.AudioState, error) { return f.st, f.err }

func TestObserveAudio(t *testing.T) {
	yes, no := true, false
	playing := fakeAudio{st: osfeat.AudioState{Sink: "offstage_hc-a", State: "RUNNING", Streams: 1}}
	quiet := fakeAudio{st: osfeat.AudioState{Sink: "offstage_hc-a", State: "IDLE"}}
	for _, tc := range []struct {
		name string
		a    osfeat.Audio
		want *bool
		hit  bool
		st   string
	}{
		{"playing wanted, playing", playing, &yes, true, ""},
		{"playing wanted, quiet", quiet, &yes, false, ""},
		{"silence wanted, quiet", quiet, &no, true, ""},
		{"no backend", nil, &yes, false, "unsupported"},
		{"pactl fails", fakeAudio{err: errors.New("boom")}, &yes, false, "unsupported"},
	} {
		hit, obs := observeAudio(tc.a, "hc-a", *tc.want)
		if hit != tc.hit || obs.Status != tc.st {
			t.Errorf("%s: hit %v status %q", tc.name, hit, obs.Status)
		}
	}
}

func TestAudioPredicateValidation(t *testing.T) {
	yes := true
	ok := expectIn{Screen: "hc-a", Success: []predicate{{Kind: "audio", Playing: &yes}}}
	if _, _, _, err := compilePredicates(ok); err != nil {
		t.Errorf("valid audio predicate: %v", err)
	}
	for name, p := range map[string]predicate{
		"no playing":         {Kind: "audio"},
		"text on audio":      {Kind: "audio", Playing: &yes, Text: "x"},
		"playing on element": {Kind: "element", Playing: &yes},
	} {
		_, _, _, err := compilePredicates(expectIn{Screen: "hc-a", Success: []predicate{p}})
		if err == nil || !strings.Contains(err.Error(), "audio") && !strings.Contains(err.Error(), "playing") {
			t.Errorf("%s: %v", name, err)
		}
	}
}
