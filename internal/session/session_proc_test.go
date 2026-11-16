//go:build !windows

package session

import "testing"

func TestParseStartTime(t *testing.T) {
	// Field 22 is 1234567; the command name contains a space and parentheses.
	stat := "4242 (my (odd) cmd) S 1 4242 4242 0 -1 4194560 100 0 0 0 5 3 0 0 20 0 1 0 1234567 8192 100 18446744073709551615 0 0 0 0 0 0 0 0 0 0 0 0 17 3 0 0 0 0 0"
	got, err := parseStartTime(stat)
	if err != nil || got != 1234567 {
		t.Fatalf("got %d, %v", got, err)
	}
	if _, err := parseStartTime("garbage"); err == nil {
		t.Error("expected an error")
	}
}
