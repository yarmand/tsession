package render

import (
	"fmt"
	"strings"
	"time"

	"github.com/yarma/tsession/internal/sessions"
)

const Header = "  STATE     AGE   TMUX             NAME                           SUMMARY                                                                           ID"

const HeaderShort = "  S  NAME                        SUMMARY                          AGE"

func FormatLine(s sessions.Session, now time.Time, color bool) string {
	return formatLineLong(s, now, color)
}

// FormatLineShort renders the compact display form. For best results (origin
// letters + legend), prefer FormatLineShortWithContext with a context built from
// the full session list.
func FormatLineShort(s sessions.Session, now time.Time, color bool) string {
	return FormatLineShortWithContext(s, now, color, ShortContext{}, 0)
}

// FormatLineShortWithContext renders a --short line with the provided origin-letter
// context. If lshort > 0, the display is truncated to at most lshort runes while
// preserving the age suffix.
func FormatLineShortWithContext(s sessions.Session, now time.Time, color bool, ctx ShortContext, lshort int) string {
	ts := s.LastEventAt
	if ts.IsZero() {
		ts = s.UpdatedAt
	}
	age := FormatAge(now.Sub(ts))

	glyph := strings.TrimSpace(stateGlyph(s.State))
	if color {
		glyph = colorize(s.State, glyph)
	}

	src := sourceGlyph(s.Source)
	letter := ctx.letterForOrigin(s.Repository)
	name := shortWorktreeName(s)
	repoCol := name
	if letter != "" {
		repoCol = letter + "-" + name
	}

	summary := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(s.Summary, "\n", " "), "\r", " "), "\t", " ")
	if summary == "" {
		summary = "(no summary)"
	}

	nameWidth := 24
	prefix := "  " + src + glyph + " " + padRight(truncate(repoCol, nameWidth), nameWidth) + " "
	suffix := " " + age

	// Default summary truncation in --short (when not using --lshort)
	if lshort <= 0 {
		display := prefix + truncate(summary, 30) + suffix
		return display + "\t" + s.ID
	}

	// With lshort: shrink name column if prefix+suffix already exceeds budget
	for len([]rune(prefix))+len([]rune(suffix)) > lshort && nameWidth > 1 {
		nameWidth--
		prefix = "  " + src + glyph + " " + padRight(truncate(repoCol, nameWidth), nameWidth) + " "
	}

	budget := lshort - len([]rune(prefix)) - len([]rune(suffix))
	if budget < 0 {
		budget = 0
	}
	display := prefix + truncate(summary, budget) + suffix
	// Final safety: hard-cap at lshort
	if r := []rune(display); len(r) > lshort {
		display = string(r[:lshort])
	}
	return display + "\t" + s.ID
}

func sourceGlyph(source string) string {
	switch source {
	case "pi":
		return "π"
	default:
		return "©"
	}
}

func formatLineLong(s sessions.Session, now time.Time, color bool) string {
	ts := s.LastEventAt
	if ts.IsZero() {
		ts = s.UpdatedAt
	}
	age := FormatAge(now.Sub(ts))
	src := sourceGlyph(s.Source)
	state := s.State.String()
	if color {
		state = colorize(s.State, padRight(stateGlyph(s.State)+state, 9))
	} else {
		state = padRight(stateGlyph(s.State)+state, 9)
	}
	repo := s.Name
	if repo == "" {
		repo = s.Repository
	}
	if repo == "" {
		repo = s.CWD
	}
	summary := strings.ReplaceAll(strings.ReplaceAll(s.Summary, "\n", " "), "\r", " ")
	if summary == "" {
		summary = "(no summary)"
	}

	tmux := s.TmuxName
	if tmux == "" {
		tmux = "-"
	}
	display := fmt.Sprintf("  %s%s %-5s %-16s %-30s %-80s  %s",
		src, state, age,
		truncate(tmux, 16),
		truncate(repo, 30),
		truncate(summary, 80),
		s.ID,
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
	case sessions.StateDone:
		return "✓ "
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
	case sessions.StateDone:
		code = "35"
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
