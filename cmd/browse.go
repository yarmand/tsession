package cmd

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/yarma/tsession/internal/render"
)

func Browse(args []string) error {
	fs := flag.NewFlagSet("browse", flag.ExitOnError)
	maxAge := fs.Duration("max-age", 14*24*time.Hour, "ignore sessions older than this")
	_ = fs.Parse(args)
	query := strings.Join(fs.Args(), " ")

	if _, err := exec.LookPath("fzf"); err != nil {
		return fmt.Errorf("fzf not found in PATH (brew install fzf)")
	}

	id, err := runFzf(*maxAge, query, false)
	if err != nil {
		return err
	}
	if id == "" {
		return nil
	}
	return Resume([]string{id})
}

func runFzf(maxAge time.Duration, query string, popup bool) (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	reloadCmd := fmt.Sprintf("%s list --fzf --max-age=%s", shellQuote(self), maxAge)

	fzfArgs := []string{
		"--delimiter=\t",
		"--with-nth=1",
		"--accept-nth=2",
		"--ansi",
		"--no-hscroll",
		"--reverse",
		"--no-info",
		"--header=  STATE     AGE   TMUX             REPO/CWD                       SUMMARY",
		"--header-first",
		"--prompt=session> ",
		"--bind=ctrl-r:reload(" + reloadCmd + ")",
		"--bind=ctrl-y:execute-silent(echo -n {2} | pbcopy)+abort",
	}
	if query != "" {
		fzfArgs = append(fzfArgs, "--query="+query)
	}
	if popup {
		fzfArgs = append(fzfArgs,
			"--listen=0",
			"--bind=start:execute-silent(sh -c 'while sleep 2; do "+
				"curl -s -XPOST localhost:$FZF_PORT -d \"reload("+reloadCmd+")\" >/dev/null 2>&1 || exit 0; "+
				"done &')",
		)
	}

	cmd := exec.Command("fzf", fzfArgs...)
	cmd.Stderr = os.Stderr

	// Feed the initial session list on stdin so fzf renders immediately
	// — no fork+exec of `tsession list` between popup open and first paint.
	// Refresh keeps working via ctrl-r and the popup-mode curl loop.
	stdin, err := initialListBytes(maxAge)
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
func initialListBytes(maxAge time.Duration) (string, error) {
	merged, err := loadAll(maxAge, false)
	if err != nil {
		return "", err
	}
	now := time.Now()
	var b strings.Builder
	for _, s := range merged {
		b.WriteString(render.FormatLine(s, now, false))
		b.WriteByte('\n')
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
