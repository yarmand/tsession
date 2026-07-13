package cmd

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/yarma/tsession/internal/cache"
	"github.com/yarma/tsession/internal/config"
	"github.com/yarma/tsession/internal/notify"
	"github.com/yarma/tsession/internal/remote"
	"github.com/yarma/tsession/internal/render"
	"github.com/yarma/tsession/internal/sessions"
	"github.com/yarma/tsession/internal/tmux"
)

var (
	loadConfig              = config.Load
	fetchRemoteSessions     = remote.FetchAll
	loadAllLiveFn           = loadAllLive
	listPanesWithTitleFn    = tmux.ListPanesWithTitle
)

func List(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	maxAge := fs.Duration("max-age", 14*24*time.Hour, "ignore sessions older than this")
	noColor := fs.Bool("no-color", false, "disable ANSI colors")
	fzfMode := fs.Bool("fzf", false, "emit tab-delimited lines for fzf consumption")
	noCache := fs.Bool("no-cache", false, "ignore the watch cache and load live")
	localOnly := fs.Bool("local-only", false, "only show local sessions")
	active := fs.Bool("active", false, "only show sessions attached to tmux with a known, non-exited state")
	short := fs.Bool("short", false, "compact output: state, age, repo basename, summary truncated to 30 chars")
	lshort := fs.Int("lshort", 0, "like --short, but also truncate each output line to N characters")
	notifyFlag := fs.Bool("notify", false, "fire desktop notifications when sessions become done or ask a question (macOS only)")
	_ = fs.Parse(args)

	local, remoteMap, remoteNames, warnings, err := loadAllWithRemotes(*maxAge, *noCache, *localOnly)
	if err != nil {
		return err
	}
	for _, warning := range warnings {
		fmt.Fprintln(os.Stderr, "warning:", warning)
	}

	if *notifyFlag {
		full := append([]sessions.Session(nil), local...)
		for _, name := range remoteNames {
			full = append(full, remoteMap[name]...)
		}
		if err := notify.Process(full); err != nil {
			fmt.Fprintln(os.Stderr, "warning: notify failed:", err)
		}
	}

	if *active {
		local = filterActive(local)
		for _, name := range remoteNames {
			remoteMap[name] = filterActive(remoteMap[name])
		}
	}

	all := append([]sessions.Session(nil), local...)
	for _, name := range remoteNames {
		all = append(all, remoteMap[name]...)
	}

	useShort := *short || *lshort > 0
	if useShort {
		enrichOrigins(all)
	}

	color := !*noColor && !*fzfMode
	if *lshort > 0 {
		color = false
	}
	now := time.Now()

	var shortCtx render.ShortContext
	if useShort {
		shortCtx = render.BuildShortContext(all)
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

	hasRemotes := len(remoteNames) > 0
	if hasRemotes {
		printSectionDivider(os.Stdout, "Local", color, *fzfMode, *lshort)
	}
	renderSessionList(os.Stdout, local, now, color, *fzfMode, useShort, shortCtx, *lshort)
	for _, name := range remoteNames {
		printSectionDivider(os.Stdout, name, color, *fzfMode, *lshort)
		renderSessionList(os.Stdout, remoteMap[name], now, color, *fzfMode, useShort, shortCtx, *lshort)
	}
	return nil
}

func renderSessionList(w io.Writer, list []sessions.Session, now time.Time, color, fzfMode, useShort bool, shortCtx render.ShortContext, lshort int) {
	for _, s := range list {
		if useShort {
			line := render.FormatLineShortWithContext(s, now, color, shortCtx, lshort)
			parts := strings.SplitN(line, "\t", 2)
			display, id := parts[0], ""
			if len(parts) == 2 {
				id = parts[1]
			}

			if fzfMode {
				ts := s.LastEventAt
				if ts.IsZero() {
					ts = s.UpdatedAt
				}
				age := render.FormatAge(now.Sub(ts))
				summary := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(s.Summary, "\n", " "), "\r", " "), "\t", " ")
				if summary == "" {
					summary = "(no summary)"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					display,
					id,
					s.Repository,
					s.CWD,
					render.OriginShortName(s.Repository),
					s.State.String(),
					age,
					summary,
					shortCtx.LegendField(),
					s.Origin,
				)
			} else {
				fmt.Fprintln(w, display)
			}
		} else {
			fmt.Fprintln(w, render.FormatLine(s, now, color))
		}
	}
}

func printSectionDivider(w io.Writer, name string, color, fzfMode bool, lshort int) {
	divider := render.FormatSectionDivider(name, color, lshort)
	if !fzfMode {
		divider = strings.TrimSuffix(divider, "\t")
	}
	fmt.Fprintln(w, divider)
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

func loadAllWithRemotes(maxAge time.Duration, noCache bool, localOnly bool) (local []sessions.Session, remoteMap map[string][]sessions.Session, remoteNames []string, warnings []string, err error) {
	local, err = loadAll(maxAge, noCache)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	local = filterOrigin(local, "")
	remoteMap = map[string][]sessions.Session{}
	if localOnly {
		return local, remoteMap, nil, nil, nil
	}

	cfg, err := loadConfig()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if len(cfg.Remotes) == 0 {
		return local, remoteMap, nil, nil, nil
	}

	remoteMap, warnings = fetchRemoteSessions(context.Background(), cfg.Remotes, maxAge, 10*time.Second, remote.FetchOptions{})
	for _, r := range cfg.Remotes {
		if _, ok := remoteMap[r.Name]; ok {
			remoteNames = append(remoteNames, r.Name)
		}
	}

	// Resolve remote sessions to local tmux panes.
	remoteMap = resolveRemotePanes(cfg.Remotes, remoteMap)
	return local, remoteMap, remoteNames, warnings, nil
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
	return loadAllLiveFn(maxAge)
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

func filterOrigin(in []sessions.Session, origin string) []sessions.Session {
	out := in[:0:0]
	for _, s := range in {
		if s.Origin != origin {
			continue
		}
		out = append(out, s)
	}
	return out
}

// filterActive keeps only sessions that are attached to a tmux session and
// whose state is something other than exited/unknown — i.e. sessions a user
// can resume right now and that have meaningful liveness signal.
// For remote sessions (Origin != ""), tmux attachment is not required since
// the remote tmux server may not be accessible via SSH.
func filterActive(in []sessions.Session) []sessions.Session {
	out := in[:0:0]
	for _, s := range in {
		if s.Origin == "" && s.TmuxName == "" {
			continue
		}
		if s.State == sessions.StateExited || s.State == sessions.StateUnknown || s.State == sessions.StateInactiveIdle {
			continue
		}
		out = append(out, s)
	}
	return out
}

// resolveRemotePanes matches remote sessions to local tmux panes that are
// connected to the corresponding remote host. This allows resume to switch
// to the local pane rather than opening a new connection.
func resolveRemotePanes(remotes []config.Remote, remoteMap map[string][]sessions.Session) map[string][]sessions.Session {
	if len(remoteMap) == 0 {
		return remoteMap
	}
	panes, err := listPanesWithTitleFn()
	if err != nil || len(panes) == 0 {
		return remoteMap
	}

	// Build match patterns from config.
	infos := make([]struct{ Name, Type, Host, SSHCommand, Codespace, Container string }, 0, len(remotes))
	for _, r := range remotes {
		infos = append(infos, struct{ Name, Type, Host, SSHCommand, Codespace, Container string }{
			Name: r.Name, Type: r.Type, Host: r.Host, SSHCommand: r.SSHCommand,
			Codespace: r.Codespace, Container: r.Container,
		})
	}
	return sessions.ResolveRemotePanes(remoteMap, panes, sessions.MatchPatterns(infos))
}
