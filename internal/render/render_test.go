package render

import (
	"strings"
	"testing"
	"time"

	"github.com/yarma/tsession/internal/sessions"
)

func TestFormatLine_TabDelimitedWithIDLast(t *testing.T) {
	s := sessions.Session{
		ID:        "uuid-123",
		CWD:       "/Users/x/proj",
		Summary:   "do the thing",
		UpdatedAt: time.Now().Add(-5 * time.Minute),
		State:     sessions.StateWorking,
		TmuxName:  "proj",
	}
	got := FormatLine(s, time.Now(), false)
	parts := strings.Split(got, "\t")
	if len(parts) != 2 {
		t.Fatalf("want 2 tab-delimited fields, got %d: %q", len(parts), got)
	}
	display, id := parts[0], parts[1]
	if id != "uuid-123" {
		t.Errorf("id field: want uuid-123, got %q", id)
	}
	if !strings.Contains(display, "5m") {
		t.Errorf("display should contain age '5m': %q", display)
	}
	if !strings.Contains(display, "proj") {
		t.Errorf("display should contain tmux name 'proj': %q", display)
	}
	if !strings.Contains(display, "working") {
		t.Errorf("display should contain state 'working': %q", display)
	}
	if !strings.Contains(display, "do the thing") {
		t.Errorf("display should contain summary: %q", display)
	}
}

func TestFormatAge(t *testing.T) {
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{30 * time.Second, "now"},
		{5 * time.Minute, "5m"},
		{3 * time.Hour, "3h"},
		{2 * 24 * time.Hour, "2d"},
		{10 * 24 * time.Hour, "1w"},
	}
	for _, c := range cases {
		if got := FormatAge(c.ago); got != c.want {
			t.Errorf("FormatAge(%s) = %q, want %q", c.ago, got, c.want)
		}
	}
}
