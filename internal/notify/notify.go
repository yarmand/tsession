// Package notify fires desktop notifications when a tracked session enters a
// "done" or "question" state. State is diffed against a persisted snapshot
// (~/.tsession/notify.json) under a cross-process file lock so each transition
// fires exactly once across the separate processes that observe sessions over
// time (the watch daemon and the repeated `list` reloads driven by
// `browse --watch`).
package notify

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/yarma/tsession/internal/sessions"
)

const (
	dirName      = ".tsession"
	snapshotFile = "notify.json"
	lockFile     = "notify.lock"
)

// fireFunc is the platform notification sender. It is a package variable so
// tests can capture invocations. It returns an error when the notification
// could not be shown so Process can surface the failure rather than crashing.
var fireFunc = fire

type message struct {
	text  string
	sound string
}

// notifiableState maps a session state to "done", "question", or "" (a state
// that should never produce a notification).
func notifiableState(s sessions.State) string {
	switch s {
	case sessions.StateDone:
		return "done"
	case sessions.StateWaiting:
		return "question"
	default:
		return ""
	}
}

// messageFor returns the notification text and sound for a notifiable state.
func messageFor(state, label string) (message, bool) {
	switch state {
	case "done":
		return message{text: "[" + label + "] done!", sound: "Tink"}, true
	case "question":
		return message{text: "[" + label + "] needs your input", sound: "Funk"}, true
	default:
		return message{}, false
	}
}

// displayLabel resolves the human-facing session label using the same priority
// as the UI: user-defined Name, then Summary, then basename(CWD), then ID.
func displayLabel(s sessions.Session) string {
	if s.Name != "" {
		return s.Name
	}
	if s.Summary != "" {
		return strings.ReplaceAll(strings.ReplaceAll(s.Summary, "\n", " "), "\r", " ")
	}
	if s.CWD != "" {
		return filepath.Base(s.CWD)
	}
	return s.ID
}

// escapeAppleScript escapes a string for use inside an AppleScript double-
// quoted literal. Backslash must be escaped first, then the double-quote.
func escapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// snapshot is the on-disk last-notified-state map: session ID -> "done" |
// "question" | "".
type snapshot struct {
	Entries map[string]string `json:"entries"`
}

// loadSnapshot reads the snapshot file. A missing or corrupt file yields an
// empty (non-nil) map so callers can treat every session as a first sighting.
func loadSnapshot(path string) snapshot {
	data, err := os.ReadFile(path)
	if err != nil {
		return snapshot{Entries: map[string]string{}}
	}
	var s snapshot
	if err := json.Unmarshal(data, &s); err != nil || s.Entries == nil {
		return snapshot{Entries: map[string]string{}}
	}
	return s
}

// saveSnapshot atomically writes the snapshot via a temp file + rename.
func saveSnapshot(path string, s snapshot) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".notify.*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s); err != nil {
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

// dir returns ~/.tsession, creating it if missing.
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

// lock acquires an exclusive advisory lock on path and returns a release func.
// It serializes the read-modify-write of the snapshot across the watch daemon
// and any concurrent `list --notify` processes so each transition fires once.
func lock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

// Process diffs ss against the persisted snapshot, firing a notification for
// each session that has just entered the done/question state, then persists the
// updated snapshot. The first time a session ID is seen its state is recorded
// silently (no notification) to avoid a flood for sessions already idle when
// observation begins. Sessions absent from ss are pruned from the snapshot.
//
// Errors are returned but are intended to be non-fatal to callers: a failure to
// notify must never break list/browse/watch.
func Process(ss []sessions.Session) error {
	d, err := dir()
	if err != nil {
		return err
	}
	snapPath := filepath.Join(d, snapshotFile)

	unlock, err := lock(filepath.Join(d, lockFile))
	if err != nil {
		return err
	}
	defer unlock()

	snap := loadSnapshot(snapPath)
	seen := make(map[string]bool, len(ss))
	var fireErrs []error

	for _, s := range ss {
		seen[s.ID] = true
		cur := notifiableState(s.State)
		prev, known := snap.Entries[s.ID]
		if !known {
			snap.Entries[s.ID] = cur
			continue
		}
		if cur == prev {
			continue
		}
		// Advance the snapshot before firing so a notification that cannot
		// be shown (permission denied, headless, osascript missing) degrades
		// gracefully: it is reported once but not retried on every cycle.
		snap.Entries[s.ID] = cur
		if msg, ok := messageFor(cur, displayLabel(s)); ok {
			if err := fireFunc(msg.text, msg.sound); err != nil {
				fireErrs = append(fireErrs, err)
			}
		}
	}

	for id := range snap.Entries {
		if !seen[id] {
			delete(snap.Entries, id)
		}
	}

	saveErr := saveSnapshot(snapPath, snap)
	if len(fireErrs) > 0 {
		// Collapse repeated identical failures (e.g. the same permission
		// error for every session) into a single user-visible message.
		fireErr := fmt.Errorf("could not show %d notification(s): %w",
			len(fireErrs), errors.Join(dedupeErrs(fireErrs)...))
		return errors.Join(fireErr, saveErr)
	}
	return saveErr
}

// dedupeErrs removes duplicate error messages while preserving order so a
// failure affecting many sessions at once is reported only once.
func dedupeErrs(errs []error) []error {
	seen := make(map[string]bool, len(errs))
	out := make([]error, 0, len(errs))
	for _, err := range errs {
		if err == nil {
			continue
		}
		key := err.Error()
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, err)
	}
	return out
}
