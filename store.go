package main

import (
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"
)

// Store remembers which job IDs have been seen, so each posting is
// notified exactly once. It persists to a JSON file between runs.
type Store struct {
	mu   sync.Mutex
	path string
	data storeData
}

type storeData struct {
	// Companies that have completed at least one successful fetch. The first
	// fetch of a company only records existing jobs, without notifying.
	Initialized map[string]bool `json:"initialized"`
	// Job ID -> last time it was present in a listing.
	Seen map[string]time.Time `json:"seen"`
}

// Jobs missing from listings for this long are forgotten to keep the file small.
const forgetAfter = 30 * 24 * time.Hour

func LoadStore(path string) (*Store, error) {
	s := &Store{path: path, data: storeData{
		Initialized: map[string]bool{},
		Seen:        map[string]time.Time{},
	}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &s.data); err != nil {
		return nil, err
	}
	if s.data.Initialized == nil {
		s.data.Initialized = map[string]bool{}
	}
	if s.data.Seen == nil {
		s.data.Seen = map[string]time.Time{}
	}
	return s, nil
}

// Record marks all jobs as seen and returns the ones not seen before.
// It also reports whether this was the company's first successful fetch.
func (s *Store) Record(company string, jobs []Job) (fresh []Job, firstRun bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	firstRun = !s.data.Initialized[company]
	for _, j := range jobs {
		if _, ok := s.data.Seen[j.ID]; !ok {
			fresh = append(fresh, j)
		}
		s.data.Seen[j.ID] = now
	}
	s.data.Initialized[company] = true
	return fresh, firstRun
}

func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-forgetAfter)
	for id, t := range s.data.Seen {
		if t.Before(cutoff) {
			delete(s.data.Seen, id)
		}
	}
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	// Write to a temp file then rename, so a crash never leaves a corrupt store.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
