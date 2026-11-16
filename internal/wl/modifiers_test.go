//go:build linux

package wl

import "testing"

func TestHoldModifiers(t *testing.T) {
	c, s := startFake(t, fullGlobals())
	s.reset()
	if err := c.HoldModifiers([]string{"ctrl", "shift"}); err != nil {
		t.Fatalf("HoldModifiers: %v", err)
	}
	if err := c.PressButton(ButtonLeft, true); err != nil {
		t.Fatal(err)
	}
	if err := c.PressButton(ButtonLeft, false); err != nil {
		t.Fatal(err)
	}
	if err := c.HoldModifiers(nil); err != nil {
		t.Fatalf("release: %v", err)
	}
	recs := s.log()
	mods := requestsNamed(recs, "zwp_virtual_keyboard_v1.modifiers")
	if len(mods) != 2 || mods[0].args[0].(uint32) != modControl|modShift || mods[1].args[0].(uint32) != 0 {
		t.Fatalf("modifiers requests: %s", formatLog(recs))
	}
	keys := requestsNamed(recs, "zwp_virtual_keyboard_v1.key")
	if len(keys) != 4 {
		t.Fatalf("%d key requests, want 4 (2 presses, 2 releases): %s", len(keys), formatLog(recs))
	}
	// Presses in order, releases in reverse order, around the button events.
	if keys[0].args[1].(uint32) != kcControl-8 || keys[1].args[1].(uint32) != kcShift-8 ||
		keys[2].args[1].(uint32) != kcShift-8 || keys[3].args[1].(uint32) != kcControl-8 {
		t.Errorf("key order: %s", formatLog(recs))
	}
	bi := indexOf(recs, "zwlr_virtual_pointer_v1.button")
	if bi < 0 || bi < indexOf(recs, "zwp_virtual_keyboard_v1.key") {
		t.Errorf("button must come after the modifier presses: %s", formatLog(recs))
	}
	if err := c.HoldModifiers([]string{"hyper"}); err == nil {
		t.Error("unknown modifier accepted")
	}
}
