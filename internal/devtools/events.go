package devtools

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"
)

// Cursor identifies a position in one collector. It survives buffer reads, not a reconnect.
type Cursor struct {
	Source   string `json:"source"`
	Position uint64 `json:"position"`
	Lost     uint64 `json:"lost"`
	Started  uint64 `json:"started"`
}
type NetworkEvent struct {
	Sequence uint64    `json:"sequence"`
	At       time.Time `json:"at"`
	Request  Req       `json:"request"`
}

func (s *Session) cursor() Cursor {
	if s.eventSource == "" {
		s.eventSource = rand.Text()
	}
	return Cursor{Source: s.eventSource, Position: s.eventSeq, Lost: s.eventLost, Started: s.requestSeq}
}
func (s *Session) Cursor() Cursor { s.mu.Lock(); defer s.mu.Unlock(); return s.cursor() }

// Arm confirms network observation before the caller sends input.
func (s *Session) Arm(ctx context.Context) (Cursor, error) {
	for {
		if !s.Alive() {
			return Cursor{}, fmt.Errorf("collector disconnected")
		}
		s.mu.Lock()
		ids := make([]string, 0, len(s.pages))
		for id := range s.pages {
			ids = append(ids, id)
		}
		s.mu.Unlock()
		if len(ids) > 0 {
			for _, id := range ids {
				if _, err := s.c.Call(ctx, id, "Network.enable", nil); err != nil {
					return Cursor{}, err
				}
			}
			return s.Cursor(), nil
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Cursor{}, ctx.Err()
		case <-timer.C:
		}
	}
}

// complete runs under s.mu and retains an immutable completion record.
func (s *Session) complete(r *Req) {
	if r.completed {
		return
	}
	r.completed = true
	s.eventSeq++
	if len(s.events) >= MaxNetwork {
		s.events = s.events[1:]
	}
	s.events = append(s.events, NetworkEvent{Sequence: s.eventSeq, At: time.Now().UTC(), Request: *r})
}
func (s *Session) Events(after Cursor) ([]NetworkEvent, Cursor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.cursor()
	if after.Source != now.Source || after.Position > now.Position || after.Started > now.Started || after.Lost != now.Lost {
		return nil, now, fmt.Errorf("evidence gap: collector changed or requests were lost")
	}
	if len(s.events) > 0 && after.Position < s.events[0].Sequence-1 {
		return nil, now, fmt.Errorf("evidence gap: events expired")
	}
	out := []NetworkEvent{}
	for _, e := range s.events {
		if e.Sequence > after.Position {
			out = append(out, e)
		}
	}
	return out, now, nil
}

// Completed returns a snapshot for local evidence collection, including loss counts.
func (s *Session) Completed() ([]NetworkEvent, Cursor, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]NetworkEvent{}, s.events...), s.cursor(), s.eventLost + s.eventSeq - uint64(len(s.events))
}

// ConsoleEvidence keeps cumulative buffer-loss accounting across explicit clears.
func (s *Session) ConsoleEvidence() ([]Msg, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Msg{}, s.console...), s.consoleLost
}
