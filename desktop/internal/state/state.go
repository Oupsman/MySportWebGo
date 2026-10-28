// Package state records the files already synchronized, not to read them again from the devices
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"MySportWebSync/internal/config"
	"MySportWebSync/internal/device"
)

type Result string

const (
	Uploaded  Result = "uploaded"
	Duplicate Result = "duplicate" // the server already had the activity
	Rejected  Result = "rejected"  // the server refused the file, e.g. not an activity: tried again once changed
)

type Record struct {
	Result     Result    `json:"result"`
	Message    string    `json:"message,omitempty"`
	ActivityID string    `json:"activity_id,omitempty"`
	Hash       string    `json:"hash"`
	At         time.Time `json:"at"`
}

// State maps the files of the devices, and their contents, to their result. A file is known by its
// device, path, size and date, without reading it; its content by its SHA-256, for the same file read
// from another device or another mode (MTP, mass storage).
type State struct {
	mu     sync.Mutex
	path   string
	Files  map[string]Record `json:"files"`
	Hashes map[string]Record `json:"hashes"`
}

func Key(deviceID string, f device.File) string {
	return fmt.Sprintf("%s|%s|%d|%d", deviceID, f.Path, f.Size, f.ModTime.Unix())
}

func Load() (*State, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, err
	}
	s := &State{path: filepath.Join(dir, "state.json"), Files: map[string]Record{}, Hashes: map[string]Record{}}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("%s is corrupted, remove it to synchronize again: %w", s.path, err)
	}
	return s, nil
}

func (s *State) File(key string) (Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.Files[key]
	return r, ok
}

// Content returns the record of a content uploaded or found on the server, not of a rejected one:
// the same content from another device is refused again, but cheaply
func (s *State) Content(hash string) (Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.Hashes[hash]
	return r, ok && r.Result != Rejected
}

// Put records the result of a file and saves the state
func (s *State) Put(key string, r Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Files[key] = r
	if r.Hash != "" {
		s.Hashes[r.Hash] = r
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return config.WriteFile(s.path, data)
}
