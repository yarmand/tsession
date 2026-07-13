// Package remote's update_state.go persists per-remote binary update
// metadata to ~/.tsession/remote-update/<name>.json so runtime/version
// checks are throttled to at most once per TTL (default 24h).
package remote

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

const updateStateDirName = "remote-update"

// UpdateState is the last-known runtime/version resolution for a remote.
type UpdateState struct {
	LastCheckedAt time.Time `json:"last_checked_at"`
	Runtime       string    `json:"runtime"`
	Version       string    `json:"version"`
	AssetName     string    `json:"asset_name"`
}

// NeedsRefresh reports whether a fresh runtime/version resolution should be
// performed: forced explicitly, no prior state recorded, or the TTL interval
// has elapsed since the last check.
func NeedsRefresh(s UpdateState, now time.Time, interval time.Duration, force bool) bool {
	if force || s.LastCheckedAt.IsZero() {
		return true
	}
	return now.Sub(s.LastCheckedAt) >= interval
}

func updateStateDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(home, ".tsession", updateStateDirName)
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", err
	}
	return d, nil
}

func updateStatePath(remoteName string) (string, error) {
	d, err := updateStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, remoteName+".json"), nil
}

// LoadUpdateState reads persisted update state for a remote. A missing file
// returns a zero-value UpdateState (no error), so callers naturally trigger a
// refresh via NeedsRefresh.
func LoadUpdateState(remoteName string) (UpdateState, error) {
	path, err := updateStatePath(remoteName)
	if err != nil {
		return UpdateState{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return UpdateState{}, nil
		}
		return UpdateState{}, err
	}
	var state UpdateState
	if err := json.Unmarshal(data, &state); err != nil {
		return UpdateState{}, err
	}
	return state, nil
}

// SaveUpdateState persists update state for a remote.
func SaveUpdateState(remoteName string, state UpdateState) error {
	path, err := updateStatePath(remoteName)
	if err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
