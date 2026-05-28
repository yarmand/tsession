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

func TestOriginShortName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"ps://github.com/yarmand/tsession.git", "tsession"},
		{"https://github.com/yarmand/tsession", "tsession"},
		{"git@github.com:yarmand/tsession.git", "tsession"},
		{"gh/yarmand/tsession", "tsession"},
		{"", "-"},
	}
	for _, c := range cases {
		if got := originShortName(c.in); got != c.want {
			t.Errorf("originShortName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatLineShort_GlyphOnlyAndAgeAtEnd(t *testing.T) {
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	s := sessions.Session{
		ID:          "uuid-123",
		CWD:         "/tmp/worktrees/better-short",
		Repository:  "ps://github.com/yarmand/tsession.git",
		Summary:     "do the thing",
		LastEventAt: now.Add(-5 * time.Minute),
		State:       sessions.StateWorking,
	}
	ctx := buildShortContext([]sessions.Session{s})
	got := FormatLineShortWithContext(s, now, false, ctx, 0)
	parts := strings.Split(got, "\t")
	if len(parts) != 2 {
		t.Fatalf("want 2 fields (display + id), got %d: %q", len(parts), got)
	}
	display := parts[0]
	if strings.Contains(display, "working") {
		t.Fatalf("short display should not contain state text: %q", display)
	}
	if !strings.Contains(display, "●") {
		t.Fatalf("short display should contain glyph: %q", display)
	}
	if !strings.Contains(display, "A-better-short") {
		t.Fatalf("short display should contain origin-letter prefixed worktree: %q", display)
	}
	trim := strings.TrimRight(display, " ")
	if !strings.HasSuffix(trim, "5m") {
		t.Fatalf("short display should end with age '5m': %q", display)
	}
}

func TestFormatLineShort_LshortPreservesAge(t *testing.T) {
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	s := sessions.Session{
		ID:          "uuid-123",
		CWD:         "/tmp/worktrees/better-short",
		Repository:  "ps://github.com/yarmand/tsession.git",
		Summary:     strings.Repeat("x", 200),
		LastEventAt: now.Add(-5 * time.Minute),
		State:       sessions.StateWorking,
	}
	ctx := buildShortContext([]sessions.Session{s})
	got := FormatLineShortWithContext(s, now, false, ctx, 32)
	parts := strings.Split(got, "\t")
	display := parts[0]
	if len([]rune(display)) > 32 {
		t.Fatalf("want display <= 32 runes, got %d: %q", len([]rune(display)), display)
	}
	trim := strings.TrimRight(display, " ")
	if !strings.HasSuffix(trim, "5m") {
		t.Fatalf("lshort display should still end with age '5m': %q", display)
	}
}

func TestFormatSectionDivider(t *testing.T) {
	got := FormatSectionDivider("devbox", true, 0)
	if !strings.Contains(got, "devbox") {
		t.Errorf("divider missing name: %q", got)
	}
	if !strings.Contains(got, "──") {
		t.Errorf("divider missing box chars: %q", got)
	}
	if !strings.HasSuffix(got, "\t") {
		t.Errorf("divider should end with tab (empty ID): %q", got)
	}
}

func TestFormatSectionDividerNoColor(t *testing.T) {
	got := FormatSectionDivider("Local", false, 0)
	if strings.Contains(got, "\x1b[") {
		t.Error("no-color divider contains ANSI escape")
	}
	if !strings.Contains(got, "Local") {
		t.Errorf("divider missing name: %q", got)
	}
}

func TestFormatSectionDividerLshort(t *testing.T) {
	got := FormatSectionDivider("devbox", false, 20)
	runes := []rune(strings.Split(got, "\t")[0])
	if len(runes) > 20 {
		t.Errorf("lshort divider has %d runes, want <=20", len(runes))
	}
}
