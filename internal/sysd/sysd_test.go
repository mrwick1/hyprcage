package sysd

import "testing"

func TestUnitNames(t *testing.T) {
	if got := SliceName("hc-1a2b3c"); got != "offstage-hc_1a2b3c.slice" {
		t.Errorf("SliceName = %s", got)
	}
	if got := UnitName("hc-1a2b3c", "cage"); got != "offstage-hc_1a2b3c-cage" {
		t.Errorf("UnitName = %s", got)
	}
	if got := GCTimerName("hc-1a2b3c"); got != "offstage-gc-hc_1a2b3c" {
		t.Errorf("GCTimerName = %s", got)
	}
	if got := ScreenFromUnit("hc_1a2b3c"); got != "hc-1a2b3c" {
		t.Errorf("ScreenFromUnit = %s", got)
	}
}
