package relay

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// Store keeps what the relay must remember across restarts: which work item
// holds each fingerprint, which tester filed each report, and the daily
// count of each key. It is one JSON file.
type Store struct {
	path string
	mu   sync.Mutex
	d    storeData
}

type storeData struct {
	Fingerprints map[string]string   `json:"fingerprints"` // fingerprint -> work item id
	Reports      map[string]report   `json:"reports"`      // work item id -> report
	Daily        map[string]dayCount `json:"daily"`        // tester -> count today
}

type report struct {
	Ref     string   `json:"ref"` // OFS-12
	Testers []string `json:"testers"`
}

type dayCount struct {
	Day   string `json:"day"`
	Count int    `json:"count"`
}

// OpenStore loads the store, or starts an empty one.
func OpenStore(path string) (*Store, error) {
	s := &Store{path: path}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &s.d); err != nil {
			return nil, err
		}
	}
	if s.d.Fingerprints == nil {
		s.d.Fingerprints = map[string]string{}
	}
	if s.d.Reports == nil {
		s.d.Reports = map[string]report{}
	}
	if s.d.Daily == nil {
		s.d.Daily = map[string]dayCount{}
	}
	return s, nil
}

// save writes the file atomically. The caller holds mu.
func (s *Store) save() error {
	data, err := json.MarshalIndent(s.d, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Take counts one report for the tester today. It returns false when the
// tester is at the limit.
func (s *Store) Take(tester, day string, limit int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.d.Daily[tester]
	if c.Day != day {
		c = dayCount{Day: day}
	}
	if c.Count >= limit {
		return false, nil
	}
	c.Count++
	s.d.Daily[tester] = c
	return true, s.save()
}

// Item returns the work item that holds a fingerprint.
func (s *Store) Item(fingerprint string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.d.Fingerprints[fingerprint]
	return id, ok
}

// Record links a work item to a fingerprint and a tester.
func (s *Store) Record(fingerprint, id, ref, tester string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if fingerprint != "" {
		s.d.Fingerprints[fingerprint] = id
	}
	r := s.d.Reports[id]
	if ref != "" {
		r.Ref = ref
	}
	for _, t := range r.Testers {
		if t == tester {
			s.d.Reports[id] = r
			return s.save()
		}
	}
	r.Testers = append(r.Testers, tester)
	s.d.Reports[id] = r
	return s.save()
}

// Lookup finds the work item id of a report ref, and whether the tester
// filed it.
func (s *Store) Lookup(ref, tester string) (id string, mine bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, r := range s.d.Reports {
		if r.Ref == ref {
			for _, t := range r.Testers {
				if t == tester {
					return id, true
				}
			}
			return id, false
		}
	}
	return "", false
}

// Ref returns the report ref of a work item.
func (s *Store) Ref(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d.Reports[id].Ref
}
