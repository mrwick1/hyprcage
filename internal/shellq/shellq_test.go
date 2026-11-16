package shellq

import "testing"

func TestQuote(t *testing.T) {
	cases := map[string]string{
		"":                  "''",
		"cage":              "cage",
		"/usr/bin/cage":     "/usr/bin/cage",
		"a b":               "'a b'",
		"it's":              `'it'\''s'`,
		"$HOME":             "'$HOME'",
		"x;y":               "'x;y'",
		"TimeoutStopSec=3s": "TimeoutStopSec=3s",
	}
	for in, want := range cases {
		if got := Quote(in); got != want {
			t.Errorf("Quote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestJoin(t *testing.T) {
	got := Join([]string{"systemd-run", "--user", "--", "env", "OFFSTAGE_SCREEN=hc-1a2b3c", "cage", "-d", "--", "/opt/x y/offstage", "_holder", "hc-1a2b3c"})
	want := "systemd-run --user -- env OFFSTAGE_SCREEN=hc-1a2b3c cage -d -- '/opt/x y/offstage' _holder hc-1a2b3c"
	if got != want {
		t.Errorf("Join = %s\nwant %s", got, want)
	}
}
