// Package donestate persists a per-session marker indicating that a Copilot
// session has just transitioned from "working" to an idle/active state. The
// marker stays in place until the session's tmux pane is activated, at which
// point callers should call Clear to remove it.
package donestate

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	dirName     = ".tsession"
	runtimeFile = "runtime.json"
)

// Entry tracks the last computed state for a session (used to detect
// transitions) and the time at which it became "done".
type Entry struct {
	LastState string    `json:"last_state,omitempty"`
	DoneSince time.Time `json:"done_since,omitempty"`
}

// File is the on-disk runtime document.
type File struct {
	Entries map[string]Entry `json:"entries"`
}

var mu sync.Mutex

func path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(home, dirName)
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(d, runtimeFile), nil
}

// Load reads the runtime file. A missing file returns an empty File.
func Load() (*File, error) {
	p, err := path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &File{Entries: map[string]Entry{}}, nil
		}
		return nil, err
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return &File{Entries: map[string]Entry{}}, nil
	}
	if f.Entries == nil {
		f.Entries = map[string]Entry{}
	}
	return &f, nil
}

// Save atomically writes the runtime file.
func Save(f *File) error {
	mu.Lock()
	defer mu.Unlock()
	p, err := path()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".runtime.*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(f); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

// Clear removes the done marker for a single session and persists the
// updated file. The session's LastState is preserved so transitions remain
// detectable on the next refresh.
func Clear(id string) error {
	f, err := Load()
	if err != nil {
		return err
	}
	e, ok := f.Entries[id]
	if !ok {
		return nil
	}
	if e.DoneSince.IsZero() {
		return nil
	}
	e.DoneSince = time.Time{}
	f.Entries[id] = e
	return Save(f)
}
