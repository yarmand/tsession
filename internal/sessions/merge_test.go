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

func TestMerge_SortOrder_Buckets(t *testing.T) {
	// Three buckets, in order:
	//   1) has tmux
	//   2) active (Waiting/Working/ActiveIdle) without tmux
	//   3) the rest
	// Within each bucket, higher state priority wins, then recency.
	now := time.Now()
	store := []Session{
		{ID: "tmux-idle-old",   CWD: "/x/t1",  UpdatedAt: now.Add(-1 * time.Hour)},
		{ID: "no-tmux-working", CWD: "/x/nw",  UpdatedAt: now.Add(-2 * time.Minute)},
		{ID: "no-tmux-waiting", CWD: "/x/na",  UpdatedAt: now.Add(-5 * time.Minute)},
		{ID: "no-tmux-active",  CWD: "/x/ai",  UpdatedAt: now.Add(-3 * time.Minute)},
		{ID: "no-tmux-recent",  CWD: "/x/nr",  UpdatedAt: now.Add(-1 * time.Minute)},
		{ID: "no-tmux-exited",  CWD: "/x/ne",  UpdatedAt: now.Add(-10 * time.Minute)},
	}
	stateDirs := []StateDirInfo{
		{ID: "tmux-idle-old",   State: StateInactiveIdle},
		{ID: "no-tmux-working", State: StateWorking},
		{ID: "no-tmux-waiting", State: StateWaiting},
		{ID: "no-tmux-active",  State: StateActiveIdle},
		{ID: "no-tmux-recent",  State: StateInactiveIdle},
		{ID: "no-tmux-exited",  State: StateExited},
	}
	tmuxs := []tmux.Session{
		{Name: "t1", Path: "/x/t1"},
	}

	got := Merge(store, stateDirs, tmuxs)

	want := []string{
		"tmux-idle-old",   // bucket 0: only tmux session
		"no-tmux-waiting", // bucket 1: Waiting (priority 4)
		"no-tmux-working", // bucket 1: Working (priority 3)
		"no-tmux-active",  // bucket 1: ActiveIdle (priority 2)
		"no-tmux-recent",  // bucket 2: idle, newer
		"no-tmux-exited",  // bucket 2: exited, older
	}
	for i, w := range want {
		if got[i].ID != w {
			t.Errorf("pos %d: want %s, got %s (full=%v)", i, w, got[i].ID, idsOf(got))
		}
	}
}

func idsOf(ss []Session) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.ID
	}
	return out
}
