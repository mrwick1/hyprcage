package screen

import "testing"

// fakeClip is an in-memory clipboard; copy models what ctrl+c does.
type fakeClip struct {
	text      string
	selection string
	sets      []string
}

func (f *fakeClip) get() (string, error) { return f.text, nil }
func (f *fakeClip) set(s string) error   { f.text = s; f.sets = append(f.sets, s); return nil }
func (f *fakeClip) copy(string) error {
	if f.selection != "" {
		f.text = f.selection
	}
	return nil
}

func TestReadSelectionReturnsTextAndRestoresClipboard(t *testing.T) {
	f := &fakeClip{text: "earlier", selection: "exact\ttext\n  with spaces"}
	got, err := readSelection(f.get, f.set, f.copy)
	if err != nil {
		t.Fatal(err)
	}
	if got != "exact\ttext\n  with spaces" {
		t.Errorf("selection = %q", got)
	}
	if f.text != "earlier" {
		t.Errorf("clipboard = %q, want earlier", f.text)
	}
}

func TestReadSelectionNoSelectionIsEmpty(t *testing.T) {
	f := &fakeClip{text: "earlier"}
	got, err := readSelection(f.get, f.set, f.copy)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("selection = %q, want empty (not the old clipboard)", got)
	}
	if f.text != "earlier" {
		t.Errorf("clipboard = %q, want earlier", f.text)
	}
}
