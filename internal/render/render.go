package render

import (
	"fmt"
	"strings"
	"time"

	"github.com/yarma/tsession/internal/sessions"
)

const Header = "  STATE     AGE   TMUX             REPO/CWD                       SUMMARY"

func FormatLine(s sessions.Session, now time.Time, color bool) string {
	age := FormatAge(now.Sub(s.UpdatedAt))
	state := s.State.String()
	if color {
		state = colorize(s.State, padRight(stateGlyph(s.State)+state, 9))
	} else {
		state = padRight(stateGlyph(s.State)+state, 9)
	}
	tmux := s.TmuxName
	if tmux == "" {
		tmux = "-"
	}
	repo := s.Repository
	if repo == "" {
		repo = s.CWD
	}
	summary := strings.ReplaceAll(strings.ReplaceAll(s.Summary, "\n", " "), "\r", " ")
	if summary == "" {
		summary = "(no summary)"
	}

	display := fmt.Sprintf("  %s %-5s %-16s %-30s %s",
		state, age,
		truncate(tmux, 16),
		truncate(repo, 30),
		truncate(summary, 80),
	)
	return display + "\t" + s.ID
}

func FormatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	}
}

func stateGlyph(s sessions.State) string {
	switch s {
	case sessions.StateWorking:
		return "● "
	case sessions.StateWaiting:
		return "◐ "
	case sessions.StateActiveIdle:
		return "○ "
	default:
		return "· "
	}
}

func colorize(s sessions.State, text string) string {
	var code string
	switch s {
	case sessions.StateWorking:
		code = "32"
	case sessions.StateWaiting:
		code = "33"
	case sessions.StateActiveIdle:
		code = "36"
	case sessions.StateExited, sessions.StateInactiveIdle:
		code = "90"
	default:
		code = "37"
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func padRight(s string, n int) string {
	for len(s) < n {
		s += " "
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
