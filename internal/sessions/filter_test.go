package sessions

import "testing"

func TestFilterActive(t *testing.T) {
	in := []Session{
		{ID: "local-no-tmux", Origin: "", TmuxName: "", State: StateActiveIdle},
		{ID: "local-with-tmux", Origin: "", TmuxName: "proj", State: StateActiveIdle},
		{ID: "local-exited", Origin: "", TmuxName: "proj", State: StateExited},
		{ID: "local-unknown", Origin: "", TmuxName: "proj", State: StateUnknown},
		{ID: "local-idle", Origin: "", TmuxName: "proj", State: StateInactiveIdle},
		{ID: "remote-no-tmux", Origin: "host1", TmuxName: "", State: StateWorking},
		{ID: "remote-exited", Origin: "host1", TmuxName: "", State: StateExited},
	}

	got := FilterActive(in)

	want := map[string]bool{"local-with-tmux": true, "remote-no-tmux": true}
	if len(got) != len(want) {
		t.Fatalf("FilterActive() = %d sessions, want %d: %+v", len(got), len(want), got)
	}
	for _, s := range got {
		if !want[s.ID] {
			t.Errorf("unexpected session %q in FilterActive() result", s.ID)
		}
	}
}
