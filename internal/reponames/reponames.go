package reponames

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/yarma/tsession/internal/repository"
)

const (
	dirName      = ".tsession"
	fileName     = "repo-names.json"
	lockFileName = "repo-names.lock"
)

var (
	mu         sync.Mutex
	readFile   = os.ReadFile
	writeFile  = os.WriteFile
	renameFile = os.Rename
	openFile   = os.OpenFile
)

func dir() (string, error) {
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

func path() (string, error) {
	d, err := dir()
	if err != nil {
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
	return loadFromPath(p)
}

func loadFromPath(p string) (map[string]string, error) {
	data, err := readFile(p)
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

func lock(path string) (func(), error) {
	f, err := openFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

func writeAtomically(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := writeFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := renameFile(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
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
	key := repository.Normalize(repositoryID)
	if key == "" {
		return nil
	}

	mu.Lock()
	defer mu.Unlock()

	d, err := dir()
	if err != nil {
		return err
	}
	unlock, err := lock(filepath.Join(d, lockFileName))
	if err != nil {
		return err
	}
	defer unlock()

	p := filepath.Join(d, fileName)
	m, err := loadFromPath(p)
	if err != nil {
		return err
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
	return writeAtomically(p, data)
}
