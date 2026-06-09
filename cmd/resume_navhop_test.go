package cmd

import (
	"testing"
	"time"

	"github.com/yarma/tsession/internal/cache"
	"github.com/yarma/tsession/internal/sessions"
)

func TestResume_LocalTmuxSessionHopsToTarget(t *testing.T) {
	writeConfigFile(t, "") // sets HOME to a temp dir, no remotes
	if err := cache.Write(cache.File{
		UpdatedAt: time.Now().UTC(),
		Interval:  10 * time.Second,
		Sessions: []sessions.Session{{
			ID:         "local-id",
			TmuxTarget: "work:2.1",
			UpdatedAt:  time.Now().UTC(),
		}},
	}); err != nil {
		t.Fatal(err)
	}

	oldLoadAllLive := loadAllLiveFn
	defer func() { loadAllLiveFn = oldLoadAllLive }()
	loadAllLiveFn = func(maxAge time.Duration) ([]sessions.Session, error) {
		return []sessions.Session{{ID: "local-id", TmuxTarget: "work:2.1", UpdatedAt: time.Now().UTC()}}, nil
	}

	oldHop := switchToPane
	defer func() { switchToPane = oldHop }()
	var gotNav, gotTarget string
	switchToPane = func(navPane, target string) error {
		gotNav, gotTarget = navPane, target
		return nil
	}

	t.Setenv("TMUX_PANE", "%9")
	if err := Resume([]string{"local-id"}); err != nil {
		t.Fatal(err)
	}
	if gotNav != "%9" || gotTarget != "work:2.1" {
		t.Fatalf("switchToPane(%q, %q), want (%q, %q)", gotNav, gotTarget, "%9", "work:2.1")
	}
}
