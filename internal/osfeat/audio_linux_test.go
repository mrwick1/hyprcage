//go:build linux

package osfeat

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hexadecimil/offstage/internal/config"
)

// fakePactl puts a pactl on PATH that logs its arguments and prints modules.
func fakePactl(t *testing.T, modules string) string {
	t.Helper()
	dir := t.TempDir()
	catPath, err := exec.LookPath("cat")
	if err != nil {
		t.Fatal(err)
	}
	calls := filepath.Join(dir, "calls")
	list := filepath.Join(dir, "modules")
	if err := os.WriteFile(list, []byte(modules), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\necho \"$*\" >> " + calls + "\n" +
		"case \"$*\" in \"list short modules\") " + catPath + " " + list + ";; \"load-module\"*) echo 99;; esac\n"
	if err := os.WriteFile(filepath.Join(dir, "pactl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return calls
}

func readCalls(t *testing.T, path string) []string {
	t.Helper()
	b, _ := os.ReadFile(path)
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

const modules = "1\tmodule-alsa-card\tdevice_id=0\n" +
	"7\tmodule-null-sink\tsink_name=offstage_hc-a sink_properties=device.description=offstage_hc-a\n" +
	"8\tmodule-null-sink\tsink_name=offstage_hc-ab\n" +
	"9\tmodule-null-sink\tsink_name=offstage_hc-gone\n" +
	"10\tmodule-null-sink\tsink_name=human_sink\n"

func TestAudioLaunchEnvCreatesTheSinkOnce(t *testing.T) {
	calls := fakePactl(t, modules)
	s := Detect(config.Default())
	if s.Audio == nil {
		t.Fatal("no audio backend with pactl on PATH")
	}
	if env := s.LaunchEnv("hc-a"); env["PULSE_SINK"] != "offstage_hc-a" {
		t.Errorf("existing sink: %v", env)
	}
	if env := s.LaunchEnv("hc-new"); env["PULSE_SINK"] != "offstage_hc-new" || env["PIPEWIRE_NODE"] != "offstage_hc-new" {
		t.Errorf("new sink: %v", env)
	}
	var loads []string
	for _, c := range readCalls(t, calls) {
		if strings.HasPrefix(c, "load-module") {
			loads = append(loads, c)
		}
	}
	if len(loads) != 1 || !strings.Contains(loads[0], "sink_name=offstage_hc-new") {
		t.Errorf("loads: %v", loads)
	}
}

func TestAudioRemoveAndPrune(t *testing.T) {
	calls := fakePactl(t, modules)
	a := Detect(config.Default()).Audio
	if err := a.Remove("hc-a"); err != nil {
		t.Fatal(err)
	}
	if err := a.Prune(map[string]bool{"hc-a": true, "hc-ab": true}); err != nil {
		t.Fatal(err)
	}
	var unloads []string
	for _, c := range readCalls(t, calls) {
		if strings.HasPrefix(c, "unload-module") {
			unloads = append(unloads, c)
		}
	}
	// Remove takes 7 only, not 8 (hc-ab). Prune takes 9 only: 10 is not ours.
	if strings.Join(unloads, ",") != "unload-module 7,unload-module 9" {
		t.Errorf("unloads: %v", unloads)
	}
}

func TestAudioOff(t *testing.T) {
	fakePactl(t, "")
	cfg := config.Default()
	cfg.AudioEnabled = false
	if Detect(cfg).Audio != nil {
		t.Error("audio enabled = false still gives a backend")
	}
	t.Setenv("PATH", t.TempDir())
	if Detect(config.Default()).Audio != nil {
		t.Error("no pactl still gives a backend")
	}
}

func TestAudioObserve(t *testing.T) {
	dir := t.TempDir()
	catPath, err := exec.LookPath("cat")
	if err != nil {
		t.Fatal(err)
	}
	sinks := filepath.Join(dir, "sinks")
	inputs := filepath.Join(dir, "inputs")
	os.WriteFile(sinks, []byte("60\talsa_output.x\tPipeWire\ts16le 2ch 48000Hz\tRUNNING\n107\toffstage_hc-a\tPipeWire\tfloat32le 2ch 48000Hz\tRUNNING\n108\toffstage_hc-b\tPipeWire\tfloat32le 2ch 48000Hz\tIDLE\n"), 0o644)
	os.WriteFile(inputs, []byte("5\t107\t40\tPipeWire\ts16le 2ch 44100Hz\n6\t107\t41\tPipeWire\ts16le 2ch 44100Hz\n7\t60\t42\tPipeWire\ts16le 2ch 44100Hz\n"), 0o644)
	script := "#!/bin/sh\ncase \"$*\" in \"list short sinks\") " + catPath + " " + sinks + ";; \"list short sink-inputs\") " + catPath + " " + inputs + ";; esac\n"
	os.WriteFile(filepath.Join(dir, "pactl"), []byte(script), 0o755)
	t.Setenv("PATH", dir)
	a := Detect(config.Default()).Audio
	for screen, want := range map[string]AudioState{
		"hc-a": {Sink: "offstage_hc-a", State: "RUNNING", Streams: 2},
		"hc-b": {Sink: "offstage_hc-b", State: "IDLE", Streams: 0},
		"hc-c": {Sink: "offstage_hc-c", State: "absent", Streams: 0},
	} {
		got, err := a.Observe(screen)
		if err != nil || got != want {
			t.Errorf("%s: got %+v, %v; want %+v", screen, got, err, want)
		}
		if got.Playing() != (screen == "hc-a") {
			t.Errorf("%s: Playing() = %v", screen, got.Playing())
		}
	}
}
