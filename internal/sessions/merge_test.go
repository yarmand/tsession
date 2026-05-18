package sessions

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/yarma/tsession/internal/tmux"
)

func TestMerge_AttachesTmuxByPathThenByBasename(t *testing.T) {
	store := []Session{
		{ID: "a", CWD: "/Users/x/proj-a", UpdatedAt: time.Now().Add(-1 * time.Minute)},
		{ID: "b", CWD: "/Users/x/work/proj-b", UpdatedAt: time.Now().Add(-2 * time.Minute)},
		{ID: "c", CWD: "/Users/x/other/proj-c", UpdatedAt: time.Now().Add(-3 * time.Minute)},
	}
	stateDirs := []StateDirInfo{
		{ID: "a", State: StateWorking},
		{ID: "b", State: StateWaiting},
		{ID: "c", State: StateInactiveIdle},
	}
	tmuxs := []tmux.Session{
		{Name: "proj-a", Path: "/Users/x/proj-a"},
		{Name: "proj-b", Path: "/somewhere/else"},
	}

	got := Merge(store, stateDirs, tmuxs)

	if len(got) != 3 {
		t.Fatalf("want 3, got %d", len(got))
	}
	byID := map[string]Session{}
	for _, s := range got {
		byID[s.ID] = s
	}
	if byID["a"].TmuxName != "proj-a" {
		t.Errorf("a should match by path, got %q", byID["a"].TmuxName)
	}
	if byID["b"].TmuxName != "proj-b" {
		t.Errorf("b should match by basename, got %q", byID["b"].TmuxName)
	}
	if byID["c"].TmuxName != "" {
		t.Errorf("c should not match, got %q", byID["c"].TmuxName)
	}
	if byID["a"].State != StateWorking {
		t.Errorf("state for a should be working, got %s", byID["a"].State)
	}
}

func TestMerge_SortOrder_TmuxFirstThenStatePriority(t *testing.T) {
	now := time.Now()
	store := []Session{
		{ID: "no-tmux-new", CWD: "/x/no1", UpdatedAt: now.Add(-1 * time.Minute)},
		{ID: "tmux-idle",   CWD: "/x/t1",  UpdatedAt: now.Add(-5 * time.Minute)},
		{ID: "tmux-work",   CWD: "/x/t2",  UpdatedAt: now.Add(-10 * time.Minute)},
		{ID: "tmux-wait",   CWD: "/x/t3",  UpdatedAt: now.Add(-20 * time.Minute)},
		{ID: "no-tmux-old", CWD: "/x/no2", UpdatedAt: now.Add(-30 * time.Minute)},
	}
	stateDirs := []StateDirInfo{
		{ID: "tmux-idle", State: StateActiveIdle},
		{ID: "tmux-work", State: StateWorking},
		{ID: "tmux-wait", State: StateWaiting},
	}
	tmuxs := []tmux.Session{
		{Name: "t1", Path: "/x/t1"},
		{Name: "t2", Path: "/x/t2"},
		{Name: "t3", Path: "/x/t3"},
	}

	got := Merge(store, stateDirs, tmuxs)

	wantOrder := []string{"tmux-wait", "tmux-work", "tmux-idle", "no-tmux-new", "no-tmux-old"}
	for i, want := range wantOrder {
		if got[i].ID != want {
			t.Errorf("position %d: want %s, got %s (full: %v)", i, want, got[i].ID, idsOf(got))
			break
		}
	}
	_ = filepath.Base
}

func idsOf(ss []Session) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.ID
	}
	return out
}
