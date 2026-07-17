package cmd

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yarma/tsession/internal/cache"
	"github.com/yarma/tsession/internal/config"
	"github.com/yarma/tsession/internal/remote"
	"github.com/yarma/tsession/internal/sessions"
	"github.com/yarma/tsession/internal/tmux"
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
	fetchRemoteSessions = func(ctx context.Context, remotes []config.Remote, maxAge, timeout time.Duration, opts remote.FetchOptions) (map[string][]sessions.Session, []string) {
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
	fetchRemoteSessions = func(ctx context.Context, remotes []config.Remote, maxAge, timeout time.Duration, opts remote.FetchOptions) (map[string][]sessions.Session, []string) {
		remoteSession := testSession("remote")
		remoteSession.Origin = "devbox"
		remoteSession.Summary = "summary\tremote"
		return map[string][]sessions.Session{"devbox": {remoteSession}}, nil
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
	fetchRemoteSessions = func(ctx context.Context, remotes []config.Remote, maxAge, timeout time.Duration, opts remote.FetchOptions) (map[string][]sessions.Session, []string) {
		remoteSession := testSession("remote")
		remoteSession.Origin = "devbox"
		remoteSession.Summary = "summary\tremote"
		return map[string][]sessions.Session{"devbox": {remoteSession}}, nil
	}

	got, err := initialListBytes(24*time.Hour, false, false, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "── Local ") || !strings.Contains(got, "── devbox ") {
		t.Fatalf("missing section dividers in output:\n%s", got)
	}
	rows := make(map[string][]string)
	for _, line := range strings.Split(got, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) == 10 {
			rows[fields[1]] = fields
		}
	}
	if len(rows["local"]) != 10 {
		t.Fatalf("missing ten-field local session row in output:\n%s", got)
	}
	remoteFields := rows["remote"]
	if len(remoteFields) != 10 || remoteFields[7] != "summary remote" || remoteFields[9] != "devbox" {
		t.Fatalf("remote fzf fields = %v, output:\n%s", remoteFields, got)
	}
}

func TestResumeRemoteSessionEnsuresBridgeAndSwitchesSelectedClient(t *testing.T) {
	writeConfigFile(t, `remotes:
  - name: mstudio
    host: mstudio
`)
	if err := cache.Write(cache.File{
		UpdatedAt: time.Now().UTC(),
		Interval:  10 * time.Second,
		Sessions: []sessions.Session{{
			ID:                  "remote-id",
			Origin:              "mstudio",
			RemoteTmuxAvailable: true,
			RemoteTmuxTarget:    "famstack:2.0",
			UpdatedAt:           time.Now().UTC(),
		}},
	}); err != nil {
		t.Fatal(err)
	}

	oldLoadAllLive := loadAllLiveFn
	defer func() { loadAllLiveFn = oldLoadAllLive }()
	loadAllLiveFn = func(maxAge time.Duration) ([]sessions.Session, error) {
		return nil, nil
	}

	oldEnsure := ensureBridgeFn
	oldSwitch := switchClientTargetFn
	t.Cleanup(func() {
		ensureBridgeFn = oldEnsure
		switchClientTargetFn = oldSwitch
	})

	var gotSpec tmux.BridgeSpec
	ensureBridgeFn = func(spec tmux.BridgeSpec) (string, error) {
		gotSpec = spec
		return spec.Name, nil
	}
	var gotBridge, gotClient string
	switchClientTargetFn = func(bridge, client string) error {
		gotBridge, gotClient = bridge, client
		return nil
	}

	if err := Resume([]string{"--target=/dev/ttys001", "--origin=mstudio", "remote-id"}); err != nil {
		t.Fatal(err)
	}
	if gotSpec.Origin != "mstudio" || gotSpec.SessionID != "remote-id" {
		t.Fatalf("bridge spec = %+v", gotSpec)
	}
	if !strings.Contains(gotSpec.Command, "'ssh' '-t' 'mstudio'") ||
		!strings.Contains(gotSpec.Command, "famstack:2.0") {
		t.Fatalf("bridge command = %q", gotSpec.Command)
	}
	if gotBridge != gotSpec.Name || gotClient != "/dev/ttys001" {
		t.Fatalf("switch = (%q,%q)", gotBridge, gotClient)
	}
}

func TestResumeRemoteSessionFetchesLiveMetadataWithoutCache(t *testing.T) {
	writeConfigFile(t, `remotes:
  - name: mstudio
    host: mstudio
`)

	oldLoadAllLive := loadAllLiveFn
	oldFetch := fetchRemoteSessions
	oldEnsure := ensureBridgeFn
	oldSwitch := switchClientTargetFn
	t.Cleanup(func() {
		loadAllLiveFn = oldLoadAllLive
		fetchRemoteSessions = oldFetch
		ensureBridgeFn = oldEnsure
		switchClientTargetFn = oldSwitch
	})
	loadAllLiveFn = func(maxAge time.Duration) ([]sessions.Session, error) {
		return nil, nil
	}
	fetchRemoteSessions = func(ctx context.Context, remotes []config.Remote, maxAge, timeout time.Duration, opts remote.FetchOptions) (map[string][]sessions.Session, []string) {
		if len(remotes) != 1 || remotes[0].Name != "mstudio" {
			t.Fatalf("fetched remotes = %+v, want only mstudio", remotes)
		}
		return map[string][]sessions.Session{
			"mstudio": {{
				ID:                  "remote-id",
				Origin:              "mstudio",
				RemoteTmuxAvailable: true,
				RemoteTmuxTarget:    "famstack:2.0",
				UpdatedAt:           time.Now().UTC(),
			}},
		}, nil
	}
	var gotSpec tmux.BridgeSpec
	ensureBridgeFn = func(spec tmux.BridgeSpec) (string, error) {
		gotSpec = spec
		return spec.Name, nil
	}
	switchClientTargetFn = func(bridge, client string) error { return nil }

	if err := Resume([]string{"--origin=mstudio", "remote-id"}); err != nil {
		t.Fatal(err)
	}
	if gotSpec.Origin != "mstudio" || gotSpec.SessionID != "remote-id" {
		t.Fatalf("bridge spec = %+v", gotSpec)
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
	fetchRemoteSessions = func(ctx context.Context, remotes []config.Remote, maxAge, timeout time.Duration, opts remote.FetchOptions) (map[string][]sessions.Session, []string) {
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
