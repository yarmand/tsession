package cmd

import (
	"flag"
	"fmt"
	"os"
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
	_ = fs.Parse(args)

	if !*noCache {
		if err := EnsureWatcherRunning(*fzfMode); err != nil {
			fmt.Fprintln(os.Stderr, "warning: auto-start watcher failed:", err)
		}
	}

	merged, err := loadAll(*maxAge, *noCache)
	if err != nil {
		return err
	}

	color := !*noColor && !*fzfMode
	now := time.Now()

	if !*fzfMode {
		if color {
			fmt.Fprintln(os.Stdout, "\x1b[1;34m"+render.Header+"\x1b[0m")
		} else {
			fmt.Fprintln(os.Stdout, render.Header)
		}
	}
	for _, s := range merged {
		fmt.Fprintln(os.Stdout, render.FormatLine(s, now, color))
	}
	return nil
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
