package screen

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtoi(t *testing.T) {
	for in, want := range map[string]int{"4242": 4242, "": 0, "12a": 0, "0": 0} {
		if got := atoi(in); got != want {
			t.Errorf("atoi(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestRemoveProfiles(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	mine := filepath.Join(tmp, "hc-chrome-hc-a-123")
	other := filepath.Join(tmp, "hc-chrome-hc-a-b-456")
	unrelated := filepath.Join(tmp, "hc-a-789")
	for _, d := range []string{mine, other, unrelated} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	removeProfiles("hc-a")
	if _, err := os.Stat(mine); !os.IsNotExist(err) {
		t.Error("the profile of hc-a is still there")
	}
	for _, d := range []string{other, unrelated} {
		if _, err := os.Stat(d); err != nil {
			t.Errorf("%s is gone: %v", d, err)
		}
	}
}
