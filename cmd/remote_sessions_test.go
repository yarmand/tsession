package cmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yarma/tsession/internal/cache"
	"github.com/yarma/tsession/internal/config"
	"github.com/yarma/tsession/internal/sessions"
)

func testSession(id string) sessions.Session {
	return sessions.Session{
		ID:         id,
		CWD:        "/work/" + id,
		Repository: "git@github.com:example/" + id + ".git",
		Summary:    "summary " + id,
		UpdatedAt:  time.Now().UTC(),
		State:      sessions.StateWorking,
	}
}

func writeConfigFile(t *testing.T, content string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgDir := filepath.Join(home, ".config", "tsession")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadAllWithRemotes_ConfigOrder(t *testing.T) {
	writeConfigFile(t, `remotes:
  - name: devbox
    host: devbox.example.com
  - name: ci
    host: ci.example.com
  - name: missing
    host: missing.example.com
`)
	if err := cache.Write(cache.File{
		UpdatedAt: time.Now().UTC(),
		Interval:  10 * time.Second,
		Sessions:  []sessions.Session{testSession("local")},
	}); err != nil {
		t.Fatal(err)
	}

	oldFetch := fetchRemoteSessions
	defer func() { fetchRemoteSessions = oldFetch }()
	fetchRemoteSessions = func(ctx context.Context, remotes []config.Remote, maxAge, timeout time.Duration) (map[string][]sessions.Session, []string) {
		if timeout != 10*time.Second {
			t.Fatalf("timeout = %v, want 10s", timeout)
		}
		if got, want := []string{remotes[0].Name, remotes[1].Name, remotes[2].Name}, []string{"devbox", "ci", "missing"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("remote order = %v, want %v", got, want)
		}
		return map[string][]sessions.Session{
			"ci":     {testSession("remote-ci")},
			"devbox": {testSession("remote-devbox")},
		}, []string{"remote missing: unavailable"}
	}

	local, remoteMap, remoteNames, warnings, err := loadAllWithRemotes(24*time.Hour, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(local); got != 1 || local[0].ID != "local" {
		t.Fatalf("local = %+v", local)
	}
	if got, want := remoteNames, []string{"devbox", "ci"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("remoteNames = %v, want %v", got, want)
	}
	if _, ok := remoteMap["devbox"]; !ok {
		t.Fatalf("devbox missing from remoteMap: %+v", remoteMap)
	}
	if _, ok := remoteMap["ci"]; !ok {
		t.Fatalf("ci missing from remoteMap: %+v", remoteMap)
	}
	if got, want := warnings, []string{"remote missing: unavailable"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("warnings = %v, want %v", got, want)
	}
}

func TestLoadAllWithRemotes_FiltersCachedRemoteSessionsFromLocal(t *testing.T) {
	writeConfigFile(t, `remotes:
  - name: devbox
    host: devbox.example.com
`)
	remoteCached := testSession("cached-remote")
	remoteCached.Origin = "devbox"
	if err := cache.Write(cache.File{
		UpdatedAt: time.Now().UTC(),
		Interval:  10 * time.Second,
		Sessions:  []sessions.Session{testSession("local"), remoteCached},
	}); err != nil {
		t.Fatal(err)
	}

	oldFetch := fetchRemoteSessions
	defer func() { fetchRemoteSessions = oldFetch }()
	fetchRemoteSessions = func(ctx context.Context, remotes []config.Remote, maxAge, timeout time.Duration) (map[string][]sessions.Session, []string) {
		return map[string][]sessions.Session{"devbox": {testSession("remote")}}, nil
	}

	local, _, _, _, err := loadAllWithRemotes(24*time.Hour, false, false)
	if err != nil {
		t.Fatal(err)
	}
	gotIDs := make([]string, len(local))
	for i := range local {
		gotIDs[i] = local[i].ID
	}
	if want := []string{"local"}; !reflect.DeepEqual(gotIDs, want) {
		t.Fatalf("local IDs = %v, want %v", gotIDs, want)
	}
}

func TestInitialListBytes_IncludesSectionDividers(t *testing.T) {
	writeConfigFile(t, `remotes:
  - name: devbox
    host: devbox.example.com
`)
	if err := cache.Write(cache.File{
		UpdatedAt: time.Now().UTC(),
		Interval:  10 * time.Second,
		Sessions:  []sessions.Session{testSession("local")},
	}); err != nil {
		t.Fatal(err)
	}

	oldFetch := fetchRemoteSessions
	defer func() { fetchRemoteSessions = oldFetch }()
	fetchRemoteSessions = func(ctx context.Context, remotes []config.Remote, maxAge, timeout time.Duration) (map[string][]sessions.Session, []string) {
		return map[string][]sessions.Session{"devbox": {testSession("remote")}}, nil
	}

	got, err := initialListBytes(24*time.Hour, false, false, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "── Local ") || !strings.Contains(got, "── devbox ") {
		t.Fatalf("missing section dividers in output:\n%s", got)
	}
	if !strings.Contains(got, "\tlocal\n") {
		t.Fatalf("missing local session row in output:\n%s", got)
	}
	if !strings.Contains(got, "\tremote\n") {
		t.Fatalf("missing remote session row in output:\n%s", got)
	}
}

func TestRemoteResumeArgs(t *testing.T) {
	// SSH — always just runs copilot resume on remote (no tmux wrapping)
	sshRemote := config.Remote{Name: "devbox", Host: "devbox", Type: "ssh"}
	got := remoteResumeArgs(sessions.Session{ID: "abcdefgh-1234"}, sshRemote)
	want := []string{"ssh", "-t", "devbox", "copilot --resume=abcdefgh-1234"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ssh resume = %v, want %v", got, want)
	}

	// Codespace
	csRemote := config.Remote{Name: "cs", Type: "codespace", Codespace: "my-cs"}
	got = remoteResumeArgs(sessions.Session{ID: "abcdefgh-1234"}, csRemote)
	want = []string{"gh", "codespace", "ssh", "--codespace", "my-cs", "-t", "--", "copilot --resume=abcdefgh-1234"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("codespace resume = %v, want %v", got, want)
	}

	// Devcontainer
	dcRemote := config.Remote{Name: "dc", Type: "devcontainer", Container: "myapp", User: "vscode"}
	got = remoteResumeArgs(sessions.Session{ID: "abcdefgh-1234"}, dcRemote)
	want = []string{"docker", "exec", "-it", "-u", "vscode", "myapp", "copilot --resume=abcdefgh-1234"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("devcontainer resume = %v, want %v", got, want)
	}
}

func TestResume_RemoteSessionUsesSSHFromCache(t *testing.T) {
	writeConfigFile(t, `remotes:
  - name: devbox
    host: devbox.example.com
`)
	// Remote session WITHOUT TmuxTarget — falls through to SSH resume.
	if err := cache.Write(cache.File{
		UpdatedAt: time.Now().UTC(),
		Interval:  10 * time.Second,
		Sessions: []sessions.Session{{
			ID:        "remote-id",
			Origin:    "devbox",
			UpdatedAt: time.Now().UTC(),
		}},
	}); err != nil {
		t.Fatal(err)
	}

	oldLoadAllLive := loadAllLiveFn
	defer func() { loadAllLiveFn = oldLoadAllLive }()
	loadAllLiveFn = func(maxAge time.Duration) ([]sessions.Session, error) {
		return nil, nil
	}

	oldExec := execCommand
	defer func() { execCommand = oldExec }()
	var gotCmd []string
	execCommand = func(name string, args ...string) *exec.Cmd {
		gotCmd = append([]string{name}, args...)
		return exec.Command("sh", "-c", "true")
	}

	if err := Resume([]string{"remote-id"}); err != nil {
		t.Fatal(err)
	}
	wantCmd := []string{"ssh", "-t", "devbox.example.com", "copilot --resume=remote-id"}
	if !reflect.DeepEqual(gotCmd, wantCmd) {
		t.Fatalf("cmd = %v, want %v", gotCmd, wantCmd)
	}
}

func TestRefresh_WritesRemoteSessionsToCache(t *testing.T) {
	writeConfigFile(t, `remotes:
  - name: devbox
    host: devbox.example.com
  - name: ci
    host: ci.example.com
`)

	oldLoadAllLive := loadAllLiveFn
	defer func() { loadAllLiveFn = oldLoadAllLive }()
	loadAllLiveFn = func(maxAge time.Duration) ([]sessions.Session, error) {
		return []sessions.Session{testSession("local")}, nil
	}

	oldFetch := fetchRemoteSessions
	defer func() { fetchRemoteSessions = oldFetch }()
	fetchRemoteSessions = func(ctx context.Context, remotes []config.Remote, maxAge, timeout time.Duration) (map[string][]sessions.Session, []string) {
		return map[string][]sessions.Session{
			"ci":     {testSession("remote-ci")},
			"devbox": {testSession("remote-devbox")},
		}, []string{"remote ci: warning"}
	}

	if err := refresh(10*time.Second, 24*time.Hour, false); err != nil {
		t.Fatal(err)
	}
	got, err := cache.Read()
	if err != nil {
		t.Fatal(err)
	}
	if got.Interval != 10*time.Second {
		t.Fatalf("interval = %v, want 10s", got.Interval)
	}
	if gotIDs := []string{got.Sessions[0].ID, got.Sessions[1].ID, got.Sessions[2].ID}; !reflect.DeepEqual(gotIDs, []string{"local", "remote-devbox", "remote-ci"}) {
		t.Fatalf("session order = %v", gotIDs)
	}
}
