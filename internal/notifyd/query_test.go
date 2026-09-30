package notifyd

import (
	"regexp"
	"slices"
	"testing"
	"time"
)

func TestListFoldsClosed(t *testing.T) {
	now := time.Now()
	got := List([]Entry{
		{ID: 1, Event: "notify", Summary: "hi", Time: now},
		{ID: 1, Event: "closed", Time: now.Add(time.Second)},
		{ID: 1, Event: "closed", Time: now.Add(time.Second)}, // swaync sends it twice
	}, time.Time{}, "", nil)
	if len(got) != 1 || got[0].ID != 1 || !got[0].Closed {
		t.Fatalf("got %+v, want one closed notification", got)
	}
}

func TestListFilters(t *testing.T) {
	now := time.Now()
	entries := []Entry{
		{ID: 1, Event: "notify", App: "mail", Summary: "invoice", Body: "due", Time: now.Add(-3 * time.Hour)},
		{ID: 2, Event: "notify", App: "mail", Summary: "hello", Body: "invoice attached", Time: now.Add(-time.Hour)},
		{ID: 3, Event: "notify", App: "chat", Summary: "invoice", Time: now},
	}
	ids := func(ns []Notification) (out []uint32) {
		for _, n := range ns {
			out = append(out, n.ID)
		}
		return out
	}
	for _, c := range []struct {
		since time.Time
		app   string
		match *regexp.Regexp
		want  []uint32
	}{
		{time.Time{}, "", nil, []uint32{3, 2, 1}},
		{time.Time{}, "mail", nil, []uint32{2, 1}},
		{time.Time{}, "", regexp.MustCompile("attached"), []uint32{2}},
		{time.Time{}, "mail", regexp.MustCompile("invoice"), []uint32{2, 1}},
		{now.Add(-2 * time.Hour), "", nil, []uint32{3, 2}},
	} {
		if got := ids(List(entries, c.since, c.app, c.match)); !slices.Equal(got, c.want) {
			t.Errorf("since=%v app=%q match=%v: got %v, want %v", c.since, c.app, c.match, got, c.want)
		}
	}
}

func TestLatestSkipsClosed(t *testing.T) {
	now := time.Now()
	n, ok := Latest([]Entry{
		{ID: 1, Event: "notify", Time: now},
		{ID: 2, Event: "notify", Time: now.Add(time.Second)},
		{ID: 2, Event: "closed", Time: now.Add(2 * time.Second)},
	})
	if !ok || n.ID != 1 {
		t.Fatalf("got %+v %v, want ID 1", n, ok)
	}
	if _, ok := Latest([]Entry{{ID: 1, Event: "notify"}, {ID: 1, Event: "closed"}}); ok {
		t.Fatal("Latest found a notification when all are closed")
	}
}
