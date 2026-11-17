package mcpserver

import (
	"github.com/hexadecimil/offstage/internal/condition"
	"github.com/hexadecimil/offstage/internal/osfeat"
	"github.com/hexadecimil/offstage/internal/redact"
)

// observeAudio reports whether the screen's audio output is in the wanted
// state: playing or silent.
func observeAudio(a osfeat.Audio, screen string, playing bool) (bool, condition.Observation) {
	if a == nil {
		return false, condition.Observation{Status: "unsupported", Evidence: map[string]string{"reason": "audio conditions need pactl and [audio] enabled"}}
	}
	st, err := a.Observe(screen)
	if err != nil {
		return false, condition.Observation{Status: "unsupported", Evidence: map[string]string{"reason": redact.Text(err.Error())}}
	}
	return st.Playing() == playing, condition.Observation{Evidence: st}
}
