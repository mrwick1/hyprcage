//go:build linux

package osfeat

import (
	"os/exec"
	"strings"
)

// sinkPrefix starts the name of every sink that offstage makes.
const sinkPrefix = "offstage_"

// pulseAudio gives each screen a null sink through pactl, which PulseAudio
// and pipewire-pulse both serve. An app plays into the sink, not into the
// human's speakers: PulseAudio clients through PULSE_SINK, native PipeWire
// and ALSA clients through PIPEWIRE_NODE.
type pulseAudio struct{}

func sinkName(screen string) string { return sinkPrefix + screen }

func (pulseAudio) Env(screen string) map[string]string {
	return map[string]string{"PULSE_SINK": sinkName(screen), "PIPEWIRE_NODE": sinkName(screen)}
}

// Ensure loads the screen's sink unless it exists.
func (pulseAudio) Ensure(screen string) error {
	mods, err := nullSinks()
	if err != nil {
		return err
	}
	name := sinkName(screen)
	for _, s := range mods {
		if s == name {
			return nil
		}
	}
	return exec.Command("pactl", "load-module", "module-null-sink",
		"sink_name="+name, "sink_properties=device.description="+name).Run()
}

// Remove unloads the screen's sink.
func (pulseAudio) Remove(screen string) error {
	return unload(func(sink string) bool { return sink == sinkName(screen) })
}

// Prune unloads the sinks of screens that are not in live.
func (pulseAudio) Prune(live map[string]bool) error {
	return unload(func(sink string) bool {
		s, ok := strings.CutPrefix(sink, sinkPrefix)
		return ok && !live[s]
	})
}

func unload(match func(sink string) bool) error {
	mods, err := nullSinks()
	if err != nil {
		return err
	}
	var first error
	for id, sink := range mods {
		if !match(sink) {
			continue
		}
		if err := exec.Command("pactl", "unload-module", id).Run(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// nullSinks maps the module ID of each null sink to its sink name.
func nullSinks() (map[string]string, error) {
	out, err := exec.Command("pactl", "list", "short", "modules").Output()
	if err != nil {
		return nil, err
	}
	mods := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.SplitN(line, "\t", 3)
		if len(f) < 3 || f[1] != "module-null-sink" {
			continue
		}
		for _, arg := range strings.Fields(f[2]) {
			if v, ok := strings.CutPrefix(arg, "sink_name="); ok {
				mods[f[0]] = v
			}
		}
	}
	return mods, nil
}
