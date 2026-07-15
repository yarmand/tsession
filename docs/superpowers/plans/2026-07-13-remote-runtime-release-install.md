# Remote Runtime/Release/Install Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add remote binary lifecycle management (runtime detection, GitHub release resolution, install/update policy) with 24h refresh TTL and `--update-remote` force refresh.

**Architecture:** Introduce a dedicated remote update manager in `internal/remote` that resolves runtime + version + asset URL, installs binaries remotely, and caches per-remote update metadata. Wire update policy into `list`, `browse`, and `watch` before remote snapshot collection. Keep current config compatibility while adding an `active` remote toggle.

**Tech Stack:** Go 1.25, existing `net/http`, existing command execution utilities, table-driven Go tests, GitHub Releases REST API.

## Global Constraints

- Runtime support (v1): `linux/amd64`, `linux/arm64`, `darwin/arm64`.
- Version selection policy: prefer client tag match; fallback to latest release when exact tag asset is unavailable for runtime.
- Refresh policy: do not resolve version/runtime every watch tick; re-check at most once per 24h per remote.
- Force-update policy: `--update-remote` applies to all configured remotes.
- Config compatibility: existing config remains valid; `active` defaults to `true` when omitted.

---

## File Structure

- `internal/config/config.go` — extend `Remote` with `Active bool`, parse default behavior.
- `internal/config/config_test.go` — add tests for `active` default/override.
- `internal/remote/runtime.go` (new) — runtime probing + mapping (`uname -s`, `uname -m` -> supported target).
- `internal/remote/runtime_test.go` (new) — runtime mapping/probe parsing tests.
- `internal/remote/releases.go` (new) — GitHub release resolver (exact tag then latest fallback).
- `internal/remote/releases_test.go` (new) — resolver logic tests using `httptest.Server`.
- `internal/version/version.go` (new) — build-time and runtime-accessible client tag source.
- `internal/version/version_test.go` (new) — current tag behavior tests.
- `internal/remote/update_state.go` (new) — read/write last-check metadata (24h TTL).
- `internal/remote/update_state_test.go` (new) — TTL and persistence tests.
- `internal/remote/installer.go` (new) — remote install/update operations and forced update workflow.
- `internal/remote/installer_test.go` (new) — command construction/policy flow tests with fakes.
- `cmd/list.go` — add `--update-remote`, filter inactive remotes, pass force-update option into remote fetch/update path.
- `cmd/browse.go` — add `--update-remote` and propagate to reload/initial list.
- `cmd/watch.go` — add `--update-remote` and pass force-update through refresh loop.
- `cmd/remote_sessions_test.go` — command-layer behavior tests for new flag and inactive remotes.
- `README.md` — document `active`, supported runtimes, update cadence, and `--update-remote`.

### Task 1: Add remote config activation toggle

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Interfaces:**
- Consumes: existing `type Remote struct` and `parse()` YAML parser.
- Produces:
  - `Remote.Active bool` field.
  - Parsing rule: omitted `active` -> `true`; explicit `active: false` -> `false`.

- [ ] **Step 1: Write failing config tests for `active`**

```go
func TestLoadRemotes_ActiveDefaultsTrue(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgPath, []byte(`remotes:
  - name: devbox
    host: devbox.local
  - name: standby
    host: standby.local
    active: false
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Remotes[0].Active {
		t.Fatalf("remote[0].Active = %v, want true", cfg.Remotes[0].Active)
	}
	if cfg.Remotes[1].Active {
		t.Fatalf("remote[1].Active = %v, want false", cfg.Remotes[1].Active)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config -run TestLoadRemotes_ActiveDefaultsTrue -v`  
Expected: FAIL because `Remote.Active` is missing/unset.

- [ ] **Step 3: Implement minimal parser + model change**

```go
type Remote struct {
	Name       string
	Active     bool
	Type       string
	Host       string
	CopilotDir string
	SSHCommand string
	Codespace  string
	Container  string
	User       string
}
```

```go
current = &Remote{
	Name:       extractValue(trimmed[len("- name:"):]),
	Active:     true,
	CopilotDir: defaultCopilotDir,
	Type:       "ssh",
}
```

```go
case strings.HasPrefix(trimmed, "active:"):
	v := strings.ToLower(extractValue(trimmed[len("active:"):]))
	current.Active = v != "false"
```

- [ ] **Step 4: Run config test suite**

Run: `go test ./internal/config -v`  
Expected: PASS; existing config tests remain green.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add remote active toggle with true default"
```

### Task 2: Implement runtime detection + release resolution

**Files:**
- Create: `internal/remote/runtime.go`
- Create: `internal/remote/runtime_test.go`
- Create: `internal/remote/releases.go`
- Create: `internal/remote/releases_test.go`
- Create: `internal/version/version.go`
- Create: `internal/version/version_test.go`

**Interfaces:**
- Consumes: `config.Remote.GatherCommand()` for remote command execution.
- Produces:
  - `func DetectRuntime(ctx context.Context, r config.Remote) (string, error)` returning `linux-amd64|linux-arm64|darwin-arm64`.
  - `func ResolveRelease(ctx context.Context, repo, clientTag, runtime string, httpClient *http.Client) (ResolvedAsset, error)`.
  - `type ResolvedAsset struct { Version string; AssetName string; DownloadURL string }`.
  - `func version.CurrentTag() string` returning build-time tag (e.g. `v1.2.3`) or empty string when unavailable.

- [ ] **Step 1: Write failing runtime mapping tests**

```go
func TestMapRuntime(t *testing.T) {
	cases := []struct {
		unameS, unameM string
		want           string
	}{
		{"Linux", "x86_64", "linux-amd64"},
		{"Linux", "aarch64", "linux-arm64"},
		{"Darwin", "arm64", "darwin-arm64"},
	}
	for _, tc := range cases {
		got, err := mapRuntime(tc.unameS, tc.unameM)
		if err != nil {
			t.Fatalf("mapRuntime(%q,%q) error: %v", tc.unameS, tc.unameM, err)
		}
		if got != tc.want {
			t.Fatalf("mapRuntime(%q,%q) = %q, want %q", tc.unameS, tc.unameM, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run runtime test to verify failure**

Run: `go test ./internal/remote -run TestMapRuntime -v`  
Expected: FAIL because `mapRuntime` is undefined.

- [ ] **Step 3: Implement runtime mapper**

```go
func mapRuntime(unameS, unameM string) (string, error) {
	switch strings.ToLower(unameS) {
	case "linux":
		switch strings.ToLower(unameM) {
		case "x86_64", "amd64":
			return "linux-amd64", nil
		case "aarch64", "arm64":
			return "linux-arm64", nil
		}
	case "darwin":
		if strings.ToLower(unameM) == "arm64" {
			return "darwin-arm64", nil
		}
	}
	return "", fmt.Errorf("unsupported runtime: %s/%s", unameS, unameM)
}
```

- [ ] **Step 4: Write failing resolver tests (exact tag then latest fallback)**

```go
func TestResolveRelease_PrefersExactTagThenFallsBackLatest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/yarma/tsession/releases/tags/v1.2.3":
			_, _ = w.Write([]byte(`{"tag_name":"v1.2.3","assets":[{"name":"tsession_darwin-arm64.tar.gz","browser_download_url":"https://example/tag-darwin"}]}`))
		case "/repos/yarma/tsession/releases/latest":
			_, _ = w.Write([]byte(`{"tag_name":"v1.2.9","assets":[{"name":"tsession_linux-amd64.tar.gz","browser_download_url":"https://example/latest-linux"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	got, err := ResolveRelease(context.Background(), "yarma/tsession", "v1.2.3", "linux-amd64", ts.Client())
	if err != nil {
		t.Fatalf("ResolveRelease error: %v", err)
	}
	if got.Version != "v1.2.9" {
		t.Fatalf("version = %q, want v1.2.9", got.Version)
	}
	if got.DownloadURL != "https://example/latest-linux" {
		t.Fatalf("downloadURL = %q, want latest linux asset", got.DownloadURL)
	}
}
```

- [ ] **Step 5: Implement resolver against GitHub Releases API**

```go
type ResolvedAsset struct {
	Version     string
	AssetName   string
	DownloadURL string
}
```

```go
func ResolveRelease(ctx context.Context, repo, clientTag, runtime string, httpClient *http.Client) (ResolvedAsset, error) {
	tagRelease, err := fetchReleaseByTag(ctx, httpClient, repo, clientTag)
	if err == nil {
		if a, ok := findRuntimeAsset(tagRelease, runtime); ok {
			return a, nil
		}
	}
	latest, err := fetchLatestRelease(ctx, httpClient, repo)
	if err != nil {
		return ResolvedAsset{}, err
	}
	if a, ok := findRuntimeAsset(latest, runtime); ok {
		return a, nil
	}
	return ResolvedAsset{}, fmt.Errorf("no release asset for runtime %s", runtime)
}
```

- [ ] **Step 6: Run targeted remote package tests**

- [ ] **Step 6: Implement client tag helper**

```go
package version

var tag = "" // set by -ldflags "-X github.com/yarma/tsession/internal/version.tag=vX.Y.Z"

func CurrentTag() string {
	return strings.TrimSpace(tag)
}
```

```go
func TestCurrentTag(t *testing.T) {
	orig := tag
	t.Cleanup(func() { tag = orig })

	tag = "v1.2.3"
	if got := CurrentTag(); got != "v1.2.3" {
		t.Fatalf("CurrentTag() = %q, want v1.2.3", got)
	}
}
```

- [ ] **Step 7: Run targeted remote package tests**

Run: `go test ./internal/remote ./internal/version -run 'TestMapRuntime|TestResolveRelease|TestCurrentTag' -v`  
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/remote/runtime.go internal/remote/runtime_test.go internal/remote/releases.go internal/remote/releases_test.go internal/version/version.go internal/version/version_test.go
git commit -m "feat(remote): add runtime detection, release resolver, and client tag source"
```

### Task 3: Add update-state TTL and installer workflow

**Files:**
- Create: `internal/remote/update_state.go`
- Create: `internal/remote/update_state_test.go`
- Create: `internal/remote/installer.go`
- Create: `internal/remote/installer_test.go`

**Interfaces:**
- Consumes: `DetectRuntime`, `ResolveRelease`, `config.Remote.GatherCommand`.
- Produces:
  - `type UpdateOptions struct { Force bool; CheckInterval time.Duration }`
  - `func EnsureRemoteBinary(ctx context.Context, r config.Remote, clientTag string, opts UpdateOptions) (string, error)` returning remote binary path.
  - `func NeedsRefresh(state UpdateState, now time.Time, interval time.Duration, force bool) bool`.

- [ ] **Step 1: Write failing TTL policy tests**

```go
func TestNeedsRefresh(t *testing.T) {
	now := time.Now().UTC()
	state := UpdateState{LastCheckedAt: now.Add(-23 * time.Hour)}
	if NeedsRefresh(state, now, 24*time.Hour, false) {
		t.Fatal("expected no refresh inside ttl")
	}
	if !NeedsRefresh(state, now, 24*time.Hour, true) {
		t.Fatal("expected forced refresh")
	}
}
```

- [ ] **Step 2: Run TTL test to verify failure**

Run: `go test ./internal/remote -run TestNeedsRefresh -v`  
Expected: FAIL because `NeedsRefresh` / `UpdateState` are undefined.

- [ ] **Step 3: Implement update-state persistence**

```go
type UpdateState struct {
	LastCheckedAt time.Time `json:"last_checked_at"`
	Runtime       string    `json:"runtime"`
	Version       string    `json:"version"`
	AssetName     string    `json:"asset_name"`
}
```

```go
func NeedsRefresh(s UpdateState, now time.Time, interval time.Duration, force bool) bool {
	if force || s.LastCheckedAt.IsZero() {
		return true
	}
	return now.Sub(s.LastCheckedAt) >= interval
}
```

- [ ] **Step 4: Write failing installer flow test**

```go
func TestEnsureRemoteBinary_UsesCachedWhenTTLValid(t *testing.T) {
	now := time.Now().UTC()
	state := UpdateState{
		LastCheckedAt: now.Add(-2 * time.Hour),
		Runtime:       "linux-amd64",
		Version:       "v1.2.3",
		AssetName:     "tsession_linux-amd64.tar.gz",
	}

	detectCalled := false
	resolveCalled := false
	detectRuntimeFn := func(context.Context, config.Remote) (string, error) {
		detectCalled = true
		return "linux-amd64", nil
	}
	resolveReleaseFn := func(context.Context, string, string, string, *http.Client) (ResolvedAsset, error) {
		resolveCalled = true
		return ResolvedAsset{}, nil
	}

	path, err := ensureRemoteBinaryWithDeps(context.Background(), config.Remote{Name: "devbox", Host: "devbox"}, "v1.2.3", UpdateOptions{
		Force:         false,
		CheckInterval: 24 * time.Hour,
	}, state, detectRuntimeFn, resolveReleaseFn)
	if err != nil {
		t.Fatalf("ensureRemoteBinaryWithDeps error: %v", err)
	}
	if path == "" {
		t.Fatal("expected cached binary path")
	}
	if detectCalled || resolveCalled {
		t.Fatalf("expected no detect/resolve calls when ttl valid, got detect=%v resolve=%v", detectCalled, resolveCalled)
	}
}
```

- [ ] **Step 5: Implement installer workflow**

```go
type UpdateOptions struct {
	Force         bool
	CheckInterval time.Duration
}
```

```go
func EnsureRemoteBinary(ctx context.Context, r config.Remote, clientTag string, opts UpdateOptions) (string, error) {
	state, _ := LoadUpdateState(r.Name)
	if !NeedsRefresh(state, time.Now().UTC(), opts.CheckInterval, opts.Force) && remoteBinaryExists(r, state.Version) {
		return remoteBinaryPath(r, state.Version), nil
	}
	runtime, err := DetectRuntime(ctx, r)
	if err != nil {
		return "", err
	}
	asset, err := ResolveRelease(ctx, "yarma/tsession", clientTag, runtime, http.DefaultClient)
	if err != nil {
		return "", err
	}
	path, err := InstallRemoteBinary(ctx, r, asset)
	if err != nil {
		return "", err
	}
	state = UpdateState{
		LastCheckedAt: time.Now().UTC(),
		Runtime:       runtime,
		Version:       asset.Version,
		AssetName:     asset.AssetName,
	}
	if err := SaveUpdateState(r.Name, state); err != nil {
		return "", err
	}
	return path, nil
}
```

- [ ] **Step 6: Run installer tests**

Run: `go test ./internal/remote -run 'TestNeedsRefresh|TestEnsureRemoteBinary' -v`  
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/remote/update_state.go internal/remote/update_state_test.go internal/remote/installer.go internal/remote/installer_test.go
git commit -m "feat(remote): add ttl-based binary update workflow"
```

### Task 4: Wire `--update-remote` and active-remote filtering into commands

**Files:**
- Modify: `cmd/list.go`
- Modify: `cmd/browse.go`
- Modify: `cmd/watch.go`
- Modify: `cmd/remote_sessions_test.go`

**Interfaces:**
- Consumes:
  - `config.Remote.Active`
  - `type remote.FetchOptions struct { ForceUpdate bool; CheckInterval time.Duration; ClientTag string; Repo string }`
  - `remote.FetchAll(ctx, remotes, maxAge, timeout, opts)`
- Produces:
  - `--update-remote` flag in list/browse/watch.
  - Remote selection logic that skips `active: false` entries.
  - Force-update propagation to all configured active remotes.

- [ ] **Step 1: Write failing command behavior tests**

```go
func TestLoadAllWithRemotes_SkipsInactiveRemotes(t *testing.T) {
	writeConfigFile(t, `remotes:
  - name: devbox
    host: devbox.example.com
    active: true
  - name: standby
    host: standby.example.com
    active: false
`)
	oldFetch := fetchRemoteSessions
	defer func() { fetchRemoteSessions = oldFetch }()
	fetchRemoteSessions = func(ctx context.Context, remotes []config.Remote, maxAge, timeout time.Duration, _ remote.FetchOptions) (map[string][]sessions.Session, []string) {
		if got := len(remotes); got != 1 || remotes[0].Name != "devbox" {
			t.Fatalf("active remotes = %+v, want only devbox", remotes)
		}
		return map[string][]sessions.Session{"devbox": {testSession("remote-devbox")}}, nil
	}

	_, _, remoteNames, _, err := loadAllWithRemotes(24*time.Hour, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(remoteNames, []string{"devbox"}) {
		t.Fatalf("remoteNames = %v, want [devbox]", remoteNames)
	}
}

func TestListUpdateRemoteFlag_TriggersForceUpdate(t *testing.T) {
	writeConfigFile(t, `remotes:
  - name: devbox
    host: devbox.example.com
`)
	forceSeen := false
	oldFetch := fetchRemoteSessions
	defer func() { fetchRemoteSessions = oldFetch }()
	fetchRemoteSessions = func(ctx context.Context, remotes []config.Remote, maxAge, timeout time.Duration, opts remote.FetchOptions) (map[string][]sessions.Session, []string) {
		forceSeen = opts.ForceUpdate
		return map[string][]sessions.Session{"devbox": {testSession("remote-devbox")}}, nil
	}

	if err := List([]string{"--fzf", "--update-remote"}); err != nil {
		t.Fatal(err)
	}
	if !forceSeen {
		t.Fatal("expected ForceUpdate=true when --update-remote is set")
	}
}
```

- [ ] **Step 2: Run command test subset to verify failure**

Run: `go test ./cmd -run 'TestLoadAllWithRemotes_SkipsInactiveRemotes|TestListUpdateRemoteFlag_TriggersForceUpdate' -v`  
Expected: FAIL because flag/filter/force path is missing.

- [ ] **Step 3: Add new flags and option plumbing**

```go
updateRemote := fs.Bool("update-remote", false, "force runtime/version refresh for all configured remotes")
```

```go
for _, r := range cfg.Remotes {
	if !r.Active {
		continue
	}
	activeRemotes = append(activeRemotes, r)
}
```

```go
opts := remote.FetchOptions{
	ForceUpdate:   *updateRemote,
	CheckInterval: 24 * time.Hour,
	ClientTag:     version.CurrentTag(),
	Repo:          "yarma/tsession",
}
remoteMap, warnings = fetchRemoteSessions(context.Background(), activeRemotes, maxAge, 10*time.Second, opts)
```

- [ ] **Step 4: Update browse reload command propagation**

```go
if updateRemote {
	reloadCmd += " --update-remote"
}
```

- [ ] **Step 5: Run command package tests**

Run: `go test ./cmd -v`  
Expected: PASS, including updated remote session tests.

- [ ] **Step 6: Commit**

```bash
git add cmd/list.go cmd/browse.go cmd/watch.go cmd/remote_sessions_test.go
git commit -m "feat(cmd): add --update-remote and inactive-remote filtering"
```

### Task 5: Document runtime/update behavior

**Files:**
- Modify: `README.md`

**Interfaces:**
- Consumes: implemented flag/config/runtime behavior from Tasks 1-4.
- Produces: user-facing docs for `active`, supported runtimes, 24h cadence, and `--update-remote`.

- [ ] **Step 1: Add failing doc check command**

```bash
grep -q "active:" README.md
grep -q -- "--update-remote" README.md
grep -q "linux/amd64" README.md
```

- [ ] **Step 2: Run doc checks to confirm current failure**

Run:

```bash
grep -q "active:" README.md && grep -q -- "--update-remote" README.md && grep -q "darwin/arm64" README.md
```

Expected: non-zero exit until README is updated.

- [ ] **Step 3: Update README remote section**

```md
- `active` (optional, default `true`) keeps a remote configured but disabled when set to `false`.
- Supported remote runtimes: `linux/amd64`, `linux/arm64`, `darwin/arm64`.
- Runtime/version checks run at most once every 24h per remote.
- Use `--update-remote` to force immediate refresh on all configured remotes.
```

- [ ] **Step 4: Run doc checks again**

Run:

```bash
grep -q "active:" README.md && grep -q -- "--update-remote" README.md && grep -q "darwin/arm64" README.md
```

Expected: exit code 0.

- [ ] **Step 5: Commit**

```bash
git add README.md
git commit -m "docs: document remote runtime update lifecycle"
```

## Final Verification

- [ ] Run: `go test ./internal/config ./internal/remote ./cmd -v`
- [ ] Run: `go test ./...`
- [ ] Run: `go test ./cmd -run 'TestLoadAllWithRemotes_ConfigOrder|TestLoadAllWithRemotes_SkipsInactiveRemotes|TestListUpdateRemoteFlag_TriggersForceUpdate' -v`
