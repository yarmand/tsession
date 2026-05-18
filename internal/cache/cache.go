// Package cache persists the merged session list to ~/.tsession/cache.json so
// list/browse/popup can render quickly while a `tsession watch` process keeps
// the file fresh in the background.
package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/yarma/tsession/internal/sessions"
)

const (
	dirName   = ".tsession"
	cacheFile = "cache.json"
	pidFile   = "watch.pid"
)

// File is the on-disk cache document. Interval is stored so readers can
// decide how stale is "too stale" without needing a flag.
type File struct {
	UpdatedAt time.Time          `json:"updated_at"`
	Interval  time.Duration      `json:"interval"`
	Sessions  []sessions.Session `json:"sessions"`
}

// Dir returns ~/.tsession, creating it if missing.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(home, dirName)
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", err
	}
	return d, nil
}

// Path returns the absolute path to the cache file (parent dir is created).
func Path() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, cacheFile), nil
}

// PidPath returns the absolute path to the watcher pidfile.
func PidPath() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, pidFile), nil
}

// Write atomically replaces the cache file with the given snapshot.
func Write(f File) error {
	path, err := Path()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".cache.*.tmp")
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
	return os.Rename(tmp.Name(), path)
}

// Read returns the cached snapshot, or an fs.ErrNotExist-wrapped error if absent.
func Read() (*File, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse cache: %w", err)
	}
	return &f, nil
}

// Fresh reports whether the cache is within tolerance of now.
func (f *File) Fresh(now time.Time, tolerance time.Duration) bool {
	if f == nil || f.UpdatedAt.IsZero() {
		return false
	}
	return now.Sub(f.UpdatedAt) <= tolerance
}

// IsNotExist returns true for "cache absent" errors so callers can choose
// to fall back to a live load without spelling out fs.ErrNotExist everywhere.
func IsNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }
