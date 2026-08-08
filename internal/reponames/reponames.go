package reponames

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/yarma/tsession/internal/repository"
)

const (
	dirName  = ".tsession"
	fileName = "repo-names.json"
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

// Load reads the repository alias file. A missing or malformed file returns an
// empty map.
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
	if m == nil {
		m = map[string]string{}
	}
	return m, nil
}

// Get returns the stored alias for a repository, or "" if none.
func Get(repositoryID string) (string, error) {
	m, err := Load()
	if err != nil {
		return "", err
	}
	return m[repository.Normalize(repositoryID)], nil
}

// Set stores an alias for the given repository identity. An empty alias
// removes the entry.
func Set(repositoryID, alias string) error {
	mu.Lock()
	defer mu.Unlock()

	m, err := Load()
	if err != nil {
		m = map[string]string{}
	}
	key := repository.Normalize(repositoryID)
	if key == "" {
		return nil
	}
	if alias == "" {
		delete(m, key)
	} else {
		m[key] = alias
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
