// Package names persists user-defined session display names in
// ~/.tsession/names.json. When a session has a stored name it is shown
// in place of the repository/CWD in list and browse output.
package names

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

const (
	dirName  = ".tsession"
	fileName = "names.json"
)

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
	return filepath.Join(d, fileName), nil
}

// Load reads the names file. Returns an empty map if the file doesn't exist.
func Load() (map[string]string, error) {
	p, err := path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]string{}, nil
	}
	return m, nil
}

// Set stores a name for the given session ID. An empty name removes the entry.
func Set(sessionID, name string) error {
	mu.Lock()
	defer mu.Unlock()

	m, err := Load()
	if err != nil {
		m = map[string]string{}
	}
	if name == "" {
		delete(m, sessionID)
	} else {
		m[sessionID] = name
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	p, err := path()
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

// Get returns the stored name for a session, or "" if none.
func Get(sessionID string) string {
	m, err := Load()
	if err != nil {
		return ""
	}
	return m[sessionID]
}
