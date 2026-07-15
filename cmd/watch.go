package cmd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/yarma/tsession/internal/cache"
	"github.com/yarma/tsession/internal/notify"
	"github.com/yarma/tsession/internal/pisessions"
	"github.com/yarma/tsession/internal/remote"
	"github.com/yarma/tsession/internal/sessions"
	"github.com/yarma/tsession/internal/tmux"
)

// daemonEnvFlag is set in the re-exec'd child so it knows it's already
// detached and should just run the loop (rather than fork again).
const daemonEnvFlag = "TSESSION_WATCH_DAEMON"

// Watch refreshes the cache file every --interval. With --daemon, it re-execs
// itself fully detached and returns immediately.
func Watch(args []string) error {
	fs := flag.NewFlagSet("watch", flag.ExitOnError)
	interval := fs.Duration("interval", 10*time.Second, "how often to refresh the cache")
	maxAge := fs.Duration("max-age", 14*24*time.Hour, "ignore sessions older than this")
	daemon := fs.Bool("daemon", false, "re-exec detached, log to ~/.tsession/watch.log")
	notifyFlag := fs.Bool("notify", false, "fire desktop notifications when sessions become done or ask a question (macOS only)")
	_ = fs.Parse(args)

	if *daemon && os.Getenv(daemonEnvFlag) == "" {
		return spawnDaemon(*interval, *maxAge, *notifyFlag)
	}

	if err := writePid(os.Getpid()); err != nil {
		return err
	}
	defer removePid()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	tick := time.NewTicker(*interval)
	defer tick.Stop()

	if err := refresh(*interval, *maxAge, *notifyFlag); err != nil {
		fmt.Fprintln(os.Stderr, "warning: initial refresh failed:", err)
	}
	for {
		select {
		case <-stop:
			return nil
		case <-tick.C:
			if err := refresh(*interval, *maxAge, *notifyFlag); err != nil {
				fmt.Fprintln(os.Stderr, "warning: refresh failed:", err)
			}
		}
	}
}

// StopWatch reads the pidfile and sends SIGTERM to the watcher.
func StopWatch(args []string) error {
	pid, err := readPid()
	if err != nil {
		if cache.IsNotExist(err) {
			fmt.Println("tsession watch is not running")
			return nil
		}
		return err
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		// If the process is already gone, treat as success and clean up.
		if errors.Is(err, os.ErrProcessDone) || errIsESRCH(err) {
			removePid()
			fmt.Println("watcher already stopped; pidfile cleaned up")
			return nil
		}
		return err
	}
	// Best-effort wait for the pidfile to disappear (watcher removes it on exit).
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := readPid(); cache.IsNotExist(err) {
			fmt.Printf("stopped watcher pid=%d\n", pid)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Printf("sent SIGTERM to pid=%d (pidfile still present, may exit shortly)\n", pid)
	return nil
}

func refresh(interval, maxAge time.Duration, notifyEnabled bool) error {
	local, err := loadAllLiveFn(maxAge)
	if err != nil {
		return err
	}

	allSessions := append([]sessions.Session(nil), local...)

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if len(cfg.Remotes) > 0 {
		remoteMap, warnings := fetchRemoteSessions(context.Background(), cfg.Remotes, maxAge, 10*time.Second, remote.FetchOptions{})
		for _, warning := range warnings {
			fmt.Fprintln(os.Stderr, "warning:", warning)
		}
		// Resolve remote sessions to local tmux panes so the cache
		// stores TmuxTarget for instant switching on resume.
		remoteMap = resolveRemotePanes(cfg.Remotes, remoteMap)
		for _, r := range cfg.Remotes {
			if s, ok := remoteMap[r.Name]; ok {
				allSessions = append(allSessions, s...)
			}
		}
	}

	if notifyEnabled {
		if err := notify.Process(allSessions); err != nil {
			fmt.Fprintln(os.Stderr, "warning: notify failed:", err)
		}
	}

	return cache.Write(cache.File{
		UpdatedAt: time.Now().UTC(),
		Interval:  interval,
		Sessions:  allSessions,
	})
}

// loadAllLive is the unconditional live load (no cache consultation).
// loadAll (in list.go) calls this when the cache is missing or stale.
func loadAllLive(maxAge time.Duration) ([]sessions.Session, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(home, ".copilot", "session-store.db")
	stateRoot := filepath.Join(home, ".copilot", "session-state")

	store, err := sessions.LoadRecent(dbPath, maxAge)
	if err != nil {
		return nil, fmt.Errorf("load session store: %w", err)
	}

	// Build a set of known IDs from the DB, then discover live sessions
	// that haven't appeared in the DB yet (brand-new, no interaction).
	knownIDs := make(map[string]bool, len(store))
	for _, s := range store {
		knownIDs[s.ID] = true
	}
	live := sessions.DiscoverLiveSessions(stateRoot, knownIDs)
	store = append(store, live...)

	ids := make([]string, len(store))
	for i, s := range store {
		ids[i] = s.ID
	}
	sd, err := sessions.LoadStateDirsForIDs(stateRoot, ids)
	if err != nil {
		return nil, fmt.Errorf("load state dirs: %w", err)
	}
	tx, err := tmux.ListSessions()
	if err != nil {
		return nil, fmt.Errorf("list tmux: %w", err)
	}
	panes, _ := tmux.ListPanes()
	merged := sessions.Merge(store, sd, tx)
	merged = sessions.ResolveTmuxByPID(merged, sd, panes)

	// Mark Copilot sessions
	for i := range merged {
		if merged[i].Source == "" {
			merged[i].Source = "copilot"
		}
	}

	// Load pi sessions
	piSessions, piErr := pisessions.LoadAll()
	if piErr != nil {
		fmt.Fprintln(os.Stderr, "warning: pi session load failed:", piErr)
	} else if len(piSessions) > 0 {
		cutoff := time.Now().Add(-maxAge)
		var piFiltered []sessions.Session
		for _, ps := range piSessions {
			if !ps.UpdatedAt.IsZero() && ps.UpdatedAt.Before(cutoff) {
				continue
			}
			piFiltered = append(piFiltered, ps)
		}
		// PID-based tmux matching for pi sessions
		piSD := pisessions.StateDirInfos(piFiltered)
		piFiltered = sessions.ResolveTmuxByPID(piFiltered, piSD, panes)
		merged = append(merged, piFiltered...)
	}

	return merged, nil
}

// watcherAlive returns true when the pidfile points at a live process.
// A stale pidfile (process no longer exists) is treated as not running.
func watcherAlive() bool {
	pid, err := readPid()
	if err != nil || pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 just checks existence + permissions.
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return false
	}
	return true
}

func writePid(pid int) error {
	p, err := cache.PidPath()
	if err != nil {
		return err
	}
	return os.WriteFile(p, []byte(strconv.Itoa(pid)+"\n"), 0o644)
}

func readPid() (int, error) {
	p, err := cache.PidPath()
	if err != nil {
		return 0, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

func removePid() {
	if p, err := cache.PidPath(); err == nil {
		_ = os.Remove(p)
	}
}

func errIsESRCH(err error) bool {
	var e syscall.Errno
	return errors.As(err, &e) && e == syscall.ESRCH
}

// spawnDaemon re-execs the current binary with TSESSION_WATCH_DAEMON=1,
// detached from the controlling terminal, with stdout/stderr appended to
// ~/.tsession/watch.log.
func spawnDaemon(interval, maxAge time.Duration, notifyEnabled bool) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	dir, err := cache.Dir()
	if err != nil {
		return err
	}
	logPath := filepath.Join(dir, "watch.log")
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()

	devNull, err := os.OpenFile(os.DevNull, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer devNull.Close()

	args := []string{
		self,
		"watch",
		"--interval=" + interval.String(),
		"--max-age=" + maxAge.String(),
	}
	if notifyEnabled {
		args = append(args, "--notify")
	}
	env := append(os.Environ(), daemonEnvFlag+"=1")

	attr := &os.ProcAttr{
		Files: []*os.File{devNull, logFile, logFile},
		Env:   env,
		Sys:   &syscall.SysProcAttr{Setsid: true},
	}
	proc, err := os.StartProcess(self, args, attr)
	if err != nil {
		return err
	}
	pid := proc.Pid
	_ = proc.Release()
	fmt.Printf("tsession watch started (pid=%d, interval=%s, log=%s)\n",
		pid, interval, logPath)
	// Give the child a moment to write its pidfile so a subsequent
	// `tsession stop-watch` finds it.
	time.Sleep(150 * time.Millisecond)
	return nil
}

// silence unused-import warnings if io/fs or exec ever get dropped.
var _ = fs.ErrNotExist
var _ = exec.Command
