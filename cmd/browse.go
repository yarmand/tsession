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
	"github.com/yarma/tsession/internal/sessions"
)

func Browse(args []string) error {
	// If not inside tmux, launch a new tmux session named 'session-nav' and
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
	target := fs.String("target", "", "tmux client to switch (/dev/... path, or any value to pick interactively)")
	_ = fs.Parse(args)
	query := strings.Join(fs.Args(), " ")

	if _, err := exec.LookPath("fzf"); err != nil {
		return fmt.Errorf("fzf not found in PATH (brew install fzf)")
	}

	// Resolve target once at startup so the picker doesn't re-prompt on every selection.
	resolvedTarget, err := tmux.ResolveTarget(*target)
	if err != nil {
		return err
	}

	if !*watch {
		id, err := runFzf(*maxAge, query, false, *active, *short, *lshort, *localOnly, resolvedTarget)
		if err != nil {
			return err
		}
		if id == "" {
			return nil
		}
		return Resume([]string{id})
	}

	for {
		id, err := runFzfOpts(*maxAge, query, false, *active, *short, *lshort, *localOnly, true, resolvedTarget)
		if err != nil {
			return err
		}
		// User pressed esc/ctrl-q — fzf exited with 130, no selection made.
		if id == "" {
			return nil
		}
	}
}

// launchInTmux starts a tmux session named "tsession" (in $HOME) and runs
// tsession browse with the original arguments inside it.
func launchInTmux(browseArgs []string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}

	// Build the command to run inside tmux: tsession browse <original args>
	tmuxCmd := shellQuote(self) + " browse"
	for _, a := range browseArgs {
		tmuxCmd += " " + shellQuote(a)
	}

	home := os.Getenv("HOME")
	if home == "" {
		home = "/"
	}

	// Create or attach to a tmux session named "tsession", running our command.
	// Use new-session with -A to attach if it already exists.
	cmd := exec.Command("tmux", "new-session", "-A", "-s", "session-nav", "-c", home, tmuxCmd)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func runFzf(maxAge time.Duration, query string, popup, active, short bool, lshort int, localOnly bool, target string) (string, error) {
	return runFzfOpts(maxAge, query, popup, active, short, lshort, localOnly, false, target)
}

func runFzfOpts(maxAge time.Duration, query string, popup, active, short bool, lshort int, localOnly bool, autoReload bool, target string) (string, error) {
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
		enterBinding(self, target),
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
// Inside tmux it switches to the selected session; outside tmux it simply
// accepts the selection and exits (attach cannot work without a tmux client).
func enterBinding(self, target string) string {
	if tmux.InTmux() {
		resumeCmd := shellQuote(self) + " resume"
		if target != "" {
			resumeCmd += " --target=" + shellQuote(target)
		}
		// {10} is session origin (remote name), {8} is summary.
		// fzf shell-quotes field replacements automatically.
		resumeCmd += " --origin {10} --summary {8} {2}"
		return "--bind=enter:execute-silent(" + resumeCmd + ")+accept"
	}
	return "--bind=enter:accept"
}

// shellQuote wraps s in single quotes, escaping any embedded single quotes,
// so that it is safe to embed in shell commands executed by fzf bindings.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
