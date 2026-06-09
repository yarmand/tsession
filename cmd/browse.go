package cmd

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/yarma/tsession/internal/render"
	"github.com/yarma/tsession/internal/sessions"
	"github.com/yarma/tsession/internal/tmux"
)

func Browse(args []string) error {
	// If not inside tmux, launch the navigator tmux session (sessions-nav) and
	// re-exec ourselves inside it with the same arguments.
	if !tmux.InTmux() {
		return launchInTmux(args)
	}

	fs := flag.NewFlagSet("browse", flag.ExitOnError)
	maxAge := fs.Duration("max-age", 14*24*time.Hour, "ignore sessions older than this")
	active := fs.Bool("active", false, "only show sessions attached to tmux with a known, non-exited state")
	short := fs.Bool("short", false, "compact output: state, age, repo basename, summary truncated to 30 chars")
	lshort := fs.Int("lshort", 0, "like --short, but also truncate each output line to N characters")
	localOnly := fs.Bool("local-only", false, "only show local sessions")
	watch := fs.Bool("watch", false, "auto-refresh the list every 5s and keep browsing after each selection")
	_ = fs.Parse(args)
	query := strings.Join(fs.Args(), " ")

	if _, err := exec.LookPath("fzf"); err != nil {
		return fmt.Errorf("fzf not found in PATH (brew install fzf)")
	}

	if !*watch {
		id, err := runFzf(*maxAge, query, false, *active, *short, *lshort, *localOnly)
		if err != nil {
			return err
		}
		if id == "" {
			navHome()
			return nil
		}
		return Resume([]string{id})
	}

	// Watch mode: a single PERSISTENT navigator. The fzf picker stays alive and
	// its pane travels to the selected agent (the enter binding hops without
	// accepting), so we do not recreate fzf on every switch. fzf returns only
	// when the user presses esc/ctrl-q.
	_, err := runFzfOpts(*maxAge, query, false, *active, *short, *lshort, *localOnly, true, true)
	navHome()
	return err
}

// launchInTmux starts the navigator tmux session and runs tsession browse with
// the original arguments inside it.
func launchInTmux(browseArgs []string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}

	tmuxCmd := shellQuote(self) + " browse"
	for _, a := range browseArgs {
		tmuxCmd += " " + shellQuote(a)
	}

	home := os.Getenv("HOME")
	if home == "" {
		home = "/"
	}

	// Create the holding session once, laid out as [nav | main]. If it already
	// exists we just connect. new-session must be detached so we can split in
	// the main pane before connecting. We operate on pane/window IDs rather than
	// ":0.0" so this works regardless of the user's base-index / pane-base-index.
	if !tmux.HasSession(tmux.NavSession) {
		create := exec.Command("tmux", "new-session", "-d", "-s", tmux.NavSession, "-n", "nav", "-c", home, tmuxCmd)
		create.Stdin, create.Stdout, create.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := create.Run(); err != nil {
			return err
		}
		// main: a plain shell to the right of nav, split off the nav pane by id.
		// Sizing the new (main) pane to 80% leaves the nav pane at 20% width.
		if navPane := tmux.FirstPaneID(tmux.NavSession); navPane != "" {
			_ = exec.Command("tmux", "split-window", "-h", "-l", "80%", "-t", navPane, "-c", home).Run()
			_ = exec.Command("tmux", "select-pane", "-t", navPane).Run()
		}
	}

	attach := exec.Command("tmux", "attach-session", "-t", tmux.NavSession)
	attach.Stdin, attach.Stdout, attach.Stderr = os.Stdin, os.Stdout, os.Stderr
	return attach.Run()
}

// navHome returns the navigator pane to its home window in sessions-nav (beside
// main) when the picker exits, so a docked navigator is not orphaned in an
// agent window. The home window is the first window of sessions-nav, resolved
// by id so this is independent of base-index.
func navHome() {
	navPane := os.Getenv("TMUX_PANE")
	if navPane == "" {
		return
	}
	home := tmux.FirstWindowID(tmux.NavSession)
	if home == "" {
		return
	}
	_ = tmux.NavHop(navPane, home)
}

func runFzf(maxAge time.Duration, query string, popup, active, short bool, lshort int, localOnly bool) (string, error) {
	return runFzfOpts(maxAge, query, popup, active, short, lshort, localOnly, false, false)
}

func runFzfOpts(maxAge time.Duration, query string, popup, active, short bool, lshort int, localOnly bool, autoReload, persist bool) (string, error) {
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
	if localOnly {
		reloadCmd += " --local-only"
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

	helpText := `States:
  ● working    Agent is executing tools
  ◐ question   Waiting for your input
  ✓ done       Just finished working
  ○ active     Live session, idle
  · idle       No live process

Keybindings:
  enter     Switch to session
  ctrl-e    Open in VS Code
  ctrl-n    Rename session
  ctrl-r    Reload list
  ?         Show this help
  esc/q     Exit`

	renameCmd := shellQuote(self) + " rename {2}"
	renameBinding := "--bind=ctrl-n:execute(" + renameCmd + ")+reload(" + reloadCmd + ")"
	if tmux.InTmux() {
		renameBinding = "--bind=ctrl-n:execute-silent(tmux display-popup -E -w 99% -h 5 " + shellQuote(self) + " rename {2})+reload(" + reloadCmd + ")"
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
		"--border=none",
		"--footer= ●working ◐question ✓done ○active ·idle\n ?: help | enter: switch | ctrl-e: vscode | ctrl-n: rename | ctrl-r: reload | esc: exit",
		"--footer-border=none",
		"--color=footer:blue:bold",
		enterBinding(self, persist),
		"--bind=ctrl-r:reload(" + reloadCmd + ")",
		"--bind=ctrl-e:execute-silent(" + shellQuote(self) + " vscode {2})",
		"--bind=?:preview(echo " + shellQuote(helpText) + ")",
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
	stdin, err := initialListBytes(maxAge, active, short, lshort, localOnly)
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
func initialListBytes(maxAge time.Duration, active, short bool, lshort int, localOnly bool) (string, error) {
	local, remoteMap, remoteNames, warnings, err := loadAllWithRemotes(maxAge, false, localOnly)
	if err != nil {
		return "", err
	}
	for _, warning := range warnings {
		fmt.Fprintln(os.Stderr, "warning:", warning)
	}
	if active {
		local = filterActive(local)
		for _, name := range remoteNames {
			remoteMap[name] = filterActive(remoteMap[name])
		}
	}

	all := append([]sessions.Session(nil), local...)
	for _, name := range remoteNames {
		all = append(all, remoteMap[name]...)
	}

	useShort := short || lshort > 0
	if useShort {
		enrichOrigins(all)
	}
	now := time.Now()

	var shortCtx render.ShortContext
	if useShort {
		shortCtx = render.BuildShortContext(all)
	}

	var b strings.Builder
	hasRemotes := len(remoteNames) > 0
	if hasRemotes {
		printSectionDivider(&b, "Local", false, true, lshort)
	}
	renderSessionList(&b, local, now, false, true, useShort, shortCtx, lshort)
	for _, name := range remoteNames {
		printSectionDivider(&b, name, false, true, lshort)
		renderSessionList(&b, remoteMap[name], now, false, true, useShort, shortCtx, lshort)
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

// enterBinding returns the fzf --bind for the enter key.
// Inside tmux it hops the navigator to the selected session via `tsession
// resume`. When persist is true (the watch dashboard) the binding omits
// `+accept` so the single fzf process stays alive and travels with its pane
// instead of being recreated on every switch. Outside tmux it simply accepts
// the selection and exits (attach cannot work without a tmux client).
func enterBinding(self string, persist bool) string {
	if tmux.InTmux() {
		resumeCmd := shellQuote(self) + " resume"
		// {10} is session origin (remote name), {8} is summary.
		// fzf shell-quotes field replacements automatically.
		resumeCmd += " --origin {10} --summary {8} {2}"
		binding := "--bind=enter:execute-silent(" + resumeCmd + ")"
		if !persist {
			binding += "+accept"
		}
		return binding
	}
	return "--bind=enter:accept"
}

// shellQuote wraps s in single quotes, escaping any embedded single quotes,
// so that it is safe to embed in shell commands executed by fzf bindings.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
