//go:build linux

package task

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestTimelineOwnershipPaginationAndRedaction(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	a, _ := Create(owner())
	b, _ := Create(owner())
	for i := 0; i < 3; i++ {
		if err := AppendEvents(context.Background(), a.ID, owner(), []Event{{ID: time.Now().String(), Source: "fixture", Kind: "request", Data: json.RawMessage(`{"url":"https://example.test/?token=secret","password":"secret-value"}`)}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	first, err := ReadTimeline(context.Background(), a.ID, owner(), TimelineCursor{}, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Events) != 2 || !first.More {
		t.Fatal(first)
	}
	bytes, _ := json.Marshal(first)
	if strings.Contains(string(bytes), "secret-value") || strings.Contains(string(bytes), "token=secret") {
		t.Fatal("secret persisted")
	}
	second, err := ReadTimeline(context.Background(), a.ID, owner(), first.Next, 2, "")
	if err != nil || len(second.Events) != 1 {
		t.Fatal(second, err)
	}
	if _, err := ReadTimeline(context.Background(), b.ID, owner(), first.Next, 2, ""); err == nil {
		t.Fatal("foreign cursor accepted")
	}
}
func TestTimelineGapDedupAndBounds(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	a, _ := Create(owner())
	events := []Event{}
	for i := 0; i < MaxTimelineEvents+3; i++ {
		events = append(events, Event{ID: fmt.Sprint(i), SourceSequence: uint64(i + 1), Source: "fixture", Kind: "log", Data: json.RawMessage(`"line"`)})
	}
	if err := AppendEvents(context.Background(), a.ID, owner(), events, nil); err != nil {
		t.Fatal(err)
	}
	page, err := ReadTimeline(context.Background(), a.ID, owner(), TimelineCursor{}, 200, "")
	if err != nil || page.Dropped != 3 || !page.Gap {
		t.Fatal(page, err)
	}
	if err := AppendEvents(context.Background(), a.ID, owner(), events, nil); err != nil {
		t.Fatal(err)
	}
	next, _ := ReadTimeline(context.Background(), a.ID, owner(), TimelineCursor{}, 200, "")
	if next.Dropped != 3 {
		t.Fatal("duplicate appended")
	}
}

func TestTimelineWriteFailurePreservesEvidence(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission failure requires a non-root user")
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	a, _ := Create(owner())
	first := Event{ID: "first", Source: "fixture", Kind: "action"}
	if err := AppendEvents(context.Background(), a.ID, owner(), []Event{first}, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(a.Directory, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(a.Directory, 0700)
	err := AppendEvents(context.Background(), a.ID, owner(), []Event{{ID: "second", Source: "fixture", Kind: "action"}}, nil)
	if err == nil {
		t.Fatal("expected write failure")
	}
	page, err := ReadTimeline(context.Background(), a.ID, owner(), TimelineCursor{}, 50, "")
	if err != nil || len(page.Events) != 1 || page.Events[0].ID != "first" {
		t.Fatal(page, err)
	}
}
