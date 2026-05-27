package cmd

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/yarma/tsession/internal/render"
	"github.com/yarma/tsession/internal/tmux"
)

func Browse(args []string) error {
	fs := flag.NewFlagSet("browse", flag.ExitOnError)
	maxAge := fs.Duration("max-age", 14*24*time.Hour, "ignore sessions older than this")
	active := fs.Bool("active", false, "only show sessions attached to tmux with a known, non-exited state")
	short := fs.Bool("short", false, "compact output: state, age, repo basename, summary truncated to 30 chars")
	lshort := fs.Int("lshort", 0, "like --short, but also truncate each output line to N characters")
	watch := fs.Bool("watch", false, "auto-refresh the list every 5s and keep browsing after each selection")
	_ = fs.Parse(args)
	query := strings.Join(fs.Args(), " ")

	if _, err := exec.LookPath("fzf"); err != nil {
		return fmt.Errorf("fzf not found in PATH (brew install fzf)")
	}

	if !*watch {
		id, err := runFzf(*maxAge, query, false, *active, *short, *lshort)
		if err != nil {
			return err
		}
		if id == "" {
			return nil
		}
		return Resume([]string{id})
	}

	for {
		id, err := runFzfOpts(*maxAge, query, false, *active, *short, *lshort, true)
		if err != nil {
			return err
		}
		if id == "" {
			return nil
		}
		if err := Resume([]string{id}); err != nil {
			fmt.Fprintln(os.Stderr, "warning: resume failed:", err)
		}
	}
}

func runFzf(maxAge time.Duration, query string, popup, active, short bool, lshort int) (string, error) {
	return runFzfOpts(maxAge, query, popup, active, short, lshort, false)
}

func runFzfOpts(maxAge time.Duration, query string, popup, active, short bool, lshort int, autoReload bool) (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	reloadCmd := fmt.Sprintf("%s list --fzf --max-age=%s", shellQuote(self), maxAge)
	if active {
		reloadCmd += " --active"
	}
	if short {
		reloadCmd += " --short"
	}
	if lshort > 0 {
		reloadCmd += fmt.Sprintf(" --lshort=%d", lshort)
	}

	useShort := short || lshort > 0
	header := render.Header
	if useShort {
		header = render.HeaderShort
	}
	if lshort > 0 {
		runes := []rune(header)
		if len(runes) > lshort {
			header = string(runes[:lshort])
		}
	}

	renameCmd := shellQuote(self) + " rename {2}"
	renameBinding := "--bind=r:execute(" + renameCmd + ")+reload(" + reloadCmd + ")"
	if tmux.InTmux() {
		renameBinding = "--bind=r:execute-silent(tmux display-popup -E " + shellQuote(self) + " rename {2})+reload(" + reloadCmd + ")"
	}

	fzfArgs := []string{
		"--delimiter=\t",
		"--with-nth=1",
		"--accept-nth=2",
		"--ansi",
		"--no-hscroll",
		"--reverse",
		"--no-info",
		"--header=" + header,
		"--header-first",
		"--prompt=session> ",
		"--bind=ctrl-r:reload(" + reloadCmd + ")",
		"--bind=ctrl-y:execute-silent(echo -n {2} | pbcopy)+abort",
		"--bind=c:execute-silent(" + shellQuote(self) + " vscode {2})+abort",
		renameBinding,
	}
	if query != "" {
		fzfArgs = append(fzfArgs, "--query="+query)
	}
	if useShort {
		fzfArgs = append(fzfArgs,
			"--preview-window=down:12:wrap",
			`--preview=sh -c 'legend=$(printf "%b" "$7"); printf "ID: %s\nState: %s\nAge: %s\nCWD: %s\nRepo: %s\n\n%s\n\nOrigins:\n%s\n" "$1" "$2" "$3" "$4" "$5" "$6" "$legend"' _ {2} {6} {7} {4} {3} {8} {9}`,
		)
	}
	autoIn5s := autoReload && !popup
	autoIn2s := (popup && active) || (popup && autoReload)
	if autoIn5s || autoIn2s {
		interval := 2
		if autoIn5s {
			interval = 5
		}
		fzfArgs = append(fzfArgs,
			"--listen=0",
			fmt.Sprintf("--bind=start:execute-silent(sh -c 'while sleep %d; do "+
				"curl -s -XPOST localhost:$FZF_PORT -d \"reload(%s)\" >/dev/null 2>&1 || exit 0; "+
				"done &')", interval, reloadCmd),
		)
	}

	cmd := exec.Command("fzf", fzfArgs...)
	cmd.Stderr = os.Stderr

	// Feed the initial session list on stdin so fzf renders immediately
	// — no fork+exec of `tsession list` between popup open and first paint.
	// Refresh keeps working via ctrl-r and the popup-mode curl loop.
	stdin, err := initialListBytes(maxAge, active, short, lshort)
	if err != nil {
		// Non-fatal: fall back to empty stdin + reload-on-start so the
		// picker still works even if the cache + live load both failed.
		fmt.Fprintln(os.Stderr, "warning: initial list load failed, using reload-on-start:", err)
		fzfArgs = append(fzfArgs, "--bind=start:reload("+reloadCmd+")")
		cmd = exec.Command("fzf", fzfArgs...)
		cmd.Stderr = os.Stderr
		cmd.Stdin = nil
	} else {
		cmd.Stdin = strings.NewReader(stdin)
	}

	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if asExit(err, &ee) && ee.ExitCode() == 130 {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// initialListBytes renders the fzf-formatted session list in-process,
// reusing the same cache-first path as `tsession list --fzf`.
func initialListBytes(maxAge time.Duration, active, short bool, lshort int) (string, error) {
	merged, err := loadAll(maxAge, false)
	if err != nil {
		return "", err
	}
	if active {
		merged = filterActive(merged)
	}
	useShort := short || lshort > 0
	if useShort {
		enrichOrigins(merged)
	}
	now := time.Now()

	var shortCtx render.ShortContext
	if useShort {
		shortCtx = render.BuildShortContext(merged)
	}

	var b strings.Builder
	for _, s := range merged {
		if useShort {
			line := render.FormatLineShortWithContext(s, now, false, shortCtx, lshort)
			parts := strings.SplitN(line, "\t", 2)
			display, id := parts[0], ""
			if len(parts) == 2 {
				id = parts[1]
			}

			ts := s.LastEventAt
			if ts.IsZero() {
				ts = s.UpdatedAt
			}
			age := render.FormatAge(now.Sub(ts))
			summary := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(s.Summary, "\n", " "), "\r", " "), "\t", " ")
			if summary == "" {
				summary = "(no summary)"
			}

			fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
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
			b.WriteString(render.FormatLine(s, now, false))
			b.WriteByte('\n')
		}
	}
	return b.String(), nil
}

func asExit(err error, target **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*target = ee
		return true
	}
	return false
}

// shellQuote wraps s in single quotes, escaping any embedded single quotes,
// so that it is safe to embed in shell commands executed by fzf bindings.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
