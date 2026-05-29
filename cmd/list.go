package cmd

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/yarma/tsession/internal/cache"
	"github.com/yarma/tsession/internal/render"
	"github.com/yarma/tsession/internal/sessions"
)

func List(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	maxAge := fs.Duration("max-age", 14*24*time.Hour, "ignore sessions older than this")
	noColor := fs.Bool("no-color", false, "disable ANSI colors")
	fzfMode := fs.Bool("fzf", false, "emit tab-delimited lines for fzf consumption")
	noCache := fs.Bool("no-cache", false, "ignore the watch cache and load live")
	active := fs.Bool("active", false, "only show sessions attached to tmux with a known, non-exited state")
	short := fs.Bool("short", false, "compact output: state, age, repo basename, summary truncated to 30 chars")
	lshort := fs.Int("lshort", 0, "like --short, but also truncate each output line to N characters")
	_ = fs.Parse(args)

	merged, err := loadAll(*maxAge, *noCache)
	if err != nil {
		return err
	}

	if *active {
		merged = filterActive(merged)
	}

	useShort := *short || *lshort > 0
	if useShort {
		enrichOrigins(merged)
	}

	color := !*noColor && !*fzfMode
	if *lshort > 0 {
		color = false
	}
	now := time.Now()

	var shortCtx render.ShortContext
	if useShort {
		shortCtx = render.BuildShortContext(merged)
	}

	if !*fzfMode {
		header := render.Header
		if useShort {
			header = render.HeaderShort
		}
		if *lshort > 0 {
			header = truncateRunes(header, *lshort)
		}
		if color {
			fmt.Fprintln(os.Stdout, "\x1b[1;34m"+header+"\x1b[0m")
		} else {
			fmt.Fprintln(os.Stdout, header)
		}
	}

	for _, s := range merged {
		if useShort {
			line := render.FormatLineShortWithContext(s, now, color, shortCtx, *lshort)
			parts := strings.SplitN(line, "\t", 2)
			display, id := parts[0], ""
			if len(parts) == 2 {
				id = parts[1]
			}

			if *fzfMode {
				ts := s.LastEventAt
				if ts.IsZero() {
					ts = s.UpdatedAt
				}
				age := render.FormatAge(now.Sub(ts))
				summary := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(s.Summary, "\n", " "), "\r", " "), "\t", " ")
				if summary == "" {
					summary = "(no summary)"
				}
				fmt.Fprintf(os.Stdout, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					display,
					id,
					s.Repository,
					s.CWD,
					render.OriginShortName(s.Repository),
					s.State.String(),
					age,
					summary,
					shortCtx.LegendField(),
				)
			} else {
				fmt.Fprintln(os.Stdout, display)
			}
		} else {
			fmt.Fprintln(os.Stdout, render.FormatLine(s, now, color))
		}
	}
	return nil
}

// truncateRunes shortens s to at most n runes, preserving any leading ANSI
// SGR escape sequences. We truncate on visible runes so colored headers
// don't get cut mid-escape.
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// loadAll returns the merged session list. When a watcher cache exists and is
// fresh (within 2*interval of now), it returns the cached snapshot — filtered
// to the caller's maxAge. Otherwise it falls back to a live load.
func loadAll(maxAge time.Duration, noCache bool) ([]sessions.Session, error) {
	if !noCache {
		if f, err := cache.Read(); err == nil {
			tol := 2 * f.Interval
			if tol < 5*time.Second {
				tol = 5 * time.Second
			}
			if f.Fresh(time.Now(), tol) {
				return filterByAge(f.Sessions, maxAge), nil
			}
		} else if !cache.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "warning: cache read failed, falling back to live:", err)
		}
	}
	return loadAllLive(maxAge)
}

func filterByAge(in []sessions.Session, maxAge time.Duration) []sessions.Session {
	cutoff := time.Now().Add(-maxAge)
	out := in[:0:0]
	for _, s := range in {
		if !s.UpdatedAt.IsZero() && s.UpdatedAt.Before(cutoff) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// filterActive keeps only sessions that are attached to a tmux session and
// whose state is something other than exited/unknown — i.e. sessions a user
// can resume right now and that have meaningful liveness signal.
func filterActive(in []sessions.Session) []sessions.Session {
	out := in[:0:0]
	for _, s := range in {
		if s.TmuxName == "" {
			continue
		}
		if s.State == sessions.StateExited || s.State == sessions.StateUnknown || s.State == sessions.StateInactiveIdle {
			continue
		}
		out = append(out, s)
	}
	return out
}
