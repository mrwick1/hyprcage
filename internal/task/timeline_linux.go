//go:build linux

package task

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/hexadecimil/offstage/internal/redact"
	"github.com/hexadecimil/offstage/internal/registry"
)

const MaxTimelineEvents = 512

type Event struct {
	Stream         string          `json:"stream,omitempty"`
	SourceSequence uint64          `json:"source_sequence,omitempty"`
	Sequence       uint64          `json:"sequence"`
	ID             string          `json:"id"`
	Source         string          `json:"source"`
	Kind           string          `json:"kind"`
	ActionID       string          `json:"action_id,omitempty"`
	ObservedAt     time.Time       `json:"observed_at"`
	SourceAt       time.Time       `json:"source_at,omitzero"`
	Data           json.RawMessage `json:"data,omitempty"`
	Truncated      bool            `json:"truncated,omitempty"`
}
type SourceState struct {
	Status  string `json:"status"` // available, partial, unavailable
	Dropped uint64 `json:"dropped,omitempty"`
	Path    string `json:"path,omitempty"`
	Note    string `json:"note,omitempty"`
}
type TimelineCursor struct {
	TaskID string `json:"task_id"`
	After  uint64 `json:"after"`
}
type timelineStore struct {
	Highwater map[string]uint64      `json:"highwater"`
	Sequence  uint64                 `json:"sequence"`
	Dropped   uint64                 `json:"dropped"`
	Events    []Event                `json:"events"`
	Sources   map[string]SourceState `json:"sources"`
}
type TimelinePage struct {
	Events  []Event                `json:"events"`
	Next    TimelineCursor         `json:"next"`
	More    bool                   `json:"more"`
	Gap     bool                   `json:"gap"`
	Dropped uint64                 `json:"dropped"`
	Sources map[string]SourceState `json:"sources"`
	Path    string                 `json:"path"`
}

func timelineRead(t *Task) (timelineStore, error) {
	var store timelineStore
	path := filepath.Join(t.Directory, "timeline.json")
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return timelineStore{Events: []Event{}, Sources: map[string]SourceState{}, Highwater: map[string]uint64{}}, nil
	}
	if err != nil {
		return store, err
	}
	if info.Size() > 8<<20 {
		return store, fmt.Errorf("timeline exceeds size limit")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return store, err
	}
	err = json.Unmarshal(b, &store)
	if store.Sources == nil {
		store.Sources = map[string]SourceState{}
	}
	if store.Highwater == nil {
		store.Highwater = map[string]uint64{}
	}
	return store, err
}
func scrubEvidence(v any) any {
	switch x := v.(type) {
	case string:
		return redact.Text(x)
	case map[string]any:
		for k, value := range x {
			switch k {
			case "password", "cookie", "cookies", "headers", "body", "env", "value":
				x[k] = "<redacted>"
			default:
				x[k] = scrubEvidence(value)
			}
		}
		return x
	case []any:
		for i := range x {
			x[i] = scrubEvidence(x[i])
		}
		return x
	default:
		return x
	}
}
func AppendEvents(ctx context.Context, taskID string, owner registry.Owner, events []Event, sources map[string]SourceState) error {
	t, release, err := loadContext(ctx, taskID, owner)
	if err != nil {
		return err
	}
	defer release()
	store, err := timelineRead(t)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, e := range store.Events {
		known[e.Source+"\x00"+e.ID] = true
	}
	for _, e := range events {
		stream := e.Source + "\x00" + e.Stream
		if e.SourceSequence > 0 && e.SourceSequence <= store.Highwater[stream] {
			continue
		}
		key := e.Source + "\x00" + e.ID
		if known[key] {
			continue
		}
		known[key] = true
		var value any
		if len(e.Data) > 0 {
			if err := json.Unmarshal(e.Data, &value); err != nil {
				return err
			}
			e.Data, err = json.Marshal(scrubEvidence(value))
			if err != nil {
				return err
			}
		}
		if len(e.Data) > 8192 {
			e.Truncated = true
			e.Data = json.RawMessage(`{"reason":"payload exceeded 8192 bytes"}`)
		}
		if len(e.Stream) > 256 || len(e.ID) > 256 || len(e.Source) > 512 || len(e.Kind) > 128 || len(e.ActionID) > 256 {
			return fmt.Errorf("timeline metadata exceeds limit")
		}
		if e.SourceSequence > 0 {
			store.Highwater[stream] = e.SourceSequence
		}
		e.Sequence = store.Sequence + 1
		store.Sequence = e.Sequence
		if e.ObservedAt.IsZero() {
			e.ObservedAt = time.Now().UTC()
		}
		store.Events = append(store.Events, e)
		if len(store.Events) > MaxTimelineEvents {
			store.Events = store.Events[1:]
			store.Dropped++
		}
	}
	for name, state := range sources {
		if len(name) > 512 || len(state.Path) > 4096 || len(state.Note) > 2048 {
			return fmt.Errorf("source metadata exceeds limit")
		}
		state.Note = redact.Text(state.Note)
		store.Sources[name] = state
	}
	// Source names are resource identities, not unbounded URLs.
	if len(store.Sources) > 256 || len(store.Highwater) > 256 {
		return fmt.Errorf("timeline source limit reached")
	}
	b, err := json.Marshal(store)
	if err != nil {
		return err
	}
	return registry.WriteAtomic(filepath.Join(t.Directory, "timeline.json"), b, 0600)
}
func ReadTimeline(ctx context.Context, taskID string, owner registry.Owner, after TimelineCursor, limit int, actionID string) (TimelinePage, error) {
	var page TimelinePage
	if after.TaskID != "" && after.TaskID != taskID {
		return page, fmt.Errorf("cursor belongs to another task")
	}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 200 {
		return page, fmt.Errorf("limit must be between 1 and 200")
	}
	t, release, err := loadContext(ctx, taskID, owner)
	if err != nil {
		return page, err
	}
	defer release()
	store, err := timelineRead(t)
	if err != nil {
		return page, err
	}
	if after.After > store.Sequence {
		return page, fmt.Errorf("cursor is ahead of timeline")
	}
	page = TimelinePage{Events: []Event{}, Next: TimelineCursor{TaskID: taskID, After: after.After}, Dropped: store.Dropped, Sources: store.Sources, Path: filepath.Join(t.Directory, "timeline.json")}
	if len(store.Events) > 0 && after.After < store.Events[0].Sequence-1 {
		page.Gap = true
	}
	for _, e := range store.Events {
		if e.Sequence <= after.After {
			continue
		}
		matches := actionID == "" || e.ActionID == actionID
		if matches && len(page.Events) == limit {
			page.More = true
			break
		}
		page.Next.After = e.Sequence
		if matches {
			page.Events = append(page.Events, e)
		}
	}
	return page, nil
}
