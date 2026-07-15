# Remote Daemon RPC Watch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace remote gather-script collection with a long-running remote `tsession` daemon and SSH stdio RPC snapshot flow.

**Architecture:** Add a new `tsession remote serve` command that runs on remotes (inside tmux) and serves JSON-line RPC responses for `health` and `snapshot`. On the local side, replace `gather.bash` fetches with RPC client calls that ensure daemon health and request full snapshots (active sessions only), then merge/render exactly as today.

**Tech Stack:** Go 1.25, existing session loaders/merge logic, SSH command execution (`ssh`, `gh codespace ssh`, `docker exec`), table-driven unit tests.

## Global Constraints

- Transport: SSH-managed execution + JSON RPC over stdio.
- Daemon lifecycle: long-running remote daemon in tmux session `tsessiond`.
- Snapshot mode: full snapshot payloads (no delta protocol in v1).
- Session filtering: remote daemon returns active sessions only.
- Active definition: `state != exited && state != unknown && state != inactive-idle`.
- Failure isolation: one remote failure must not block local or other remotes.

---

## File Structure

- `main.go` — add `remote` subcommand dispatch.
- `cmd/remote.go` (new) — parse `tsession remote serve` and run server.
- `internal/remote/protocol.go` (new) — request/response structs and JSONL encode/decode helpers.
- `internal/remote/server.go` (new) — daemon request handling (`health`, `snapshot`).
- `internal/remote/server_test.go` (new) — protocol and active-only snapshot behavior tests.
- `internal/remote/client.go` (new) — local RPC call helpers over remote exec transport.
- `internal/remote/client_test.go` (new) — mocked transport tests.
- `internal/remote/remote.go` — replace gather-script path with daemon-aware fetch path.
- `cmd/remote_sessions_test.go` — update tests for new remote fetch behavior.
- `README.md` — document daemon-based remote watch flow.

### Task 1: Add `remote serve` command entrypoint

**Files:**
- Modify: `main.go`
- Create: `cmd/remote.go`
- Test: `cmd/remote_test.go` (new)

**Interfaces:**
- Consumes: existing command routing in `main.go`.
- Produces:
  - `func Remote(args []string) error`
  - `tsession remote serve` command path.

- [ ] **Step 1: Write failing command dispatch test**

```go
func TestRemoteServeCommand_Dispatches(t *testing.T) {
	err := Remote([]string{"serve"})
	if err != nil && !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./cmd -run TestRemoteServeCommand_Dispatches -v`  
Expected: FAIL because `Remote` command does not exist.

- [ ] **Step 3: Implement command wiring**

```go
// main.go
case "remote":
	err = cmd.Remote(args)
```

```go
// cmd/remote.go
func Remote(args []string) error {
	fs := flag.NewFlagSet("remote", flag.ExitOnError)
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: tsession remote <serve>")
	}
	switch fs.Arg(0) {
	case "serve":
		return remote.Serve(os.Stdin, os.Stdout)
	default:
		return fmt.Errorf("unknown remote subcommand: %s", fs.Arg(0))
	}
}
```

- [ ] **Step 4: Run command tests**

Run: `go test ./cmd -run TestRemoteServeCommand_Dispatches -v`  
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add main.go cmd/remote.go cmd/remote_test.go
git commit -m "feat(remote): add remote serve command entrypoint"
```

### Task 2: Implement RPC protocol and server snapshot handler

**Files:**
- Create: `internal/remote/protocol.go`
- Create: `internal/remote/server.go`
- Create: `internal/remote/server_test.go`

**Interfaces:**
- Consumes: existing local session/state loading helpers.
- Produces:
  - `type RPCRequest struct { ID string; Method string }`
  - `type RPCResponse struct { ID string; OK bool; Error string; Payload SnapshotPayload }`
  - `func Serve(in io.Reader, out io.Writer) error`
  - `func BuildActiveSnapshot(now time.Time) (SnapshotPayload, error)`

- [ ] **Step 1: Write failing snapshot filter test**

```go
func TestBuildActiveSnapshot_ReturnsOnlyActiveStates(t *testing.T) {
	payload, err := BuildActiveSnapshot(time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range payload.Sessions {
		if s.State == "exited" || s.State == "unknown" || s.State == "idle" {
			t.Fatalf("unexpected inactive state in payload: %s", s.State)
		}
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/remote -run TestBuildActiveSnapshot_ReturnsOnlyActiveStates -v`  
Expected: FAIL because snapshot builder is missing.

- [ ] **Step 3: Implement protocol structs + server handlers**

```go
type RPCRequest struct {
	ID     string `json:"id"`
	Method string `json:"method"`
}

type RPCResponse struct {
	ID              string          `json:"id"`
	ProtocolVersion int             `json:"protocolVersion"`
	OK              bool            `json:"ok"`
	Error           string          `json:"error,omitempty"`
	Payload         SnapshotPayload `json:"payload,omitempty"`
}
```

```go
func Serve(in io.Reader, out io.Writer) error {
	dec := json.NewDecoder(in)
	enc := json.NewEncoder(out)
	for {
		var req RPCRequest
		if err := dec.Decode(&req); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		resp := handleRequest(req)
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
}
```

- [ ] **Step 4: Implement active-only snapshot assembly**

```go
func BuildActiveSnapshot(now time.Time) (SnapshotPayload, error) {
	all, err := loadAllLive(14 * 24 * time.Hour)
	if err != nil {
		return SnapshotPayload{}, err
	}
	active := make([]sessions.Session, 0, len(all))
	for _, s := range all {
		if s.State == sessions.StateExited || s.State == sessions.StateUnknown || s.State == sessions.StateInactiveIdle {
			continue
		}
		active = append(active, s)
	}
	return toSnapshotPayload(active, now), nil
}
```

- [ ] **Step 5: Run remote package test subset**

Run: `go test ./internal/remote -run 'TestBuildActiveSnapshot_ReturnsOnlyActiveStates|TestServe' -v`  
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/remote/protocol.go internal/remote/server.go internal/remote/server_test.go
git commit -m "feat(remote): add rpc protocol and active-only snapshot server"
```

### Task 3: Implement local RPC client + daemon lifecycle management

**Files:**
- Create: `internal/remote/client.go`
- Create: `internal/remote/client_test.go`
- Modify: `internal/remote/remote.go`

**Interfaces:**
- Consumes: plan-1 update manager (`EnsureRemoteBinary`) and `config.Remote` commands.
- Produces:
  - `func EnsureDaemonAndSnapshot(ctx context.Context, r config.Remote, opts FetchOptions, maxAge time.Duration) ([]sessions.Session, error)`
  - `func RequestSnapshot(ctx context.Context, r config.Remote) (*SnapshotPayload, error)`

- [ ] **Step 1: Write failing lifecycle test**

```go
func TestEnsureDaemonAndSnapshot_StartsTmuxDaemonAndRequestsSnapshot(t *testing.T) {
	var calls []string
	runRemoteCmd = func(ctx context.Context, r config.Remote, cmd string) ([]byte, error) {
		calls = append(calls, cmd)
		switch {
		case strings.Contains(cmd, "tmux has-session -t tsessiond"):
			return nil, errors.New("no such session")
		case strings.Contains(cmd, "tmux new-session -Ad -s tsessiond"):
			return []byte(""), nil
		case strings.Contains(cmd, "remote rpc snapshot"):
			return []byte(`{"protocolVersion":1,"ok":true,"payload":{"sessions":[{"id":"abc","state":"working","summary":"demo"}]}}`), nil
		default:
			return nil, fmt.Errorf("unexpected cmd: %s", cmd)
		}
	}
	out, err := EnsureDaemonAndSnapshot(context.Background(), config.Remote{Name: "devbox", Host: "devbox"}, FetchOptions{ClientTag: "v1.2.3", CheckInterval: 24 * time.Hour}, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ID != "abc" {
		t.Fatalf("sessions = %+v, want snapshot session", out)
	}
	if len(calls) < 3 {
		t.Fatalf("calls = %v, expected daemon ensure + snapshot calls", calls)
	}
}
```

- [ ] **Step 2: Run lifecycle test to verify failure**

Run: `go test ./internal/remote -run TestEnsureDaemonAndSnapshot_StartsTmuxDaemonAndRequestsSnapshot -v`  
Expected: FAIL because client lifecycle path is missing.

- [ ] **Step 3: Implement daemon ensure + snapshot request flow**

```go
func EnsureDaemonAndSnapshot(ctx context.Context, r config.Remote, opts FetchOptions, maxAge time.Duration) ([]sessions.Session, error) {
	if _, err := EnsureRemoteBinary(ctx, r, opts.ClientTag, UpdateOptions{
		Force:         opts.ForceUpdate,
		CheckInterval: opts.CheckInterval,
	}); err != nil {
		return nil, err
	}
	if err := ensureRemoteDaemon(ctx, r); err != nil {
		return nil, err
	}
	payload, err := RequestSnapshot(ctx, r)
	if err != nil {
		return nil, err
	}
	return payload.ToSessions(r.Name, maxAge), nil
}
```

- [ ] **Step 4: Replace gather-script path in `FetchAll`**

```go
func FetchAll(ctx context.Context, remotes []config.Remote, maxAge, timeout time.Duration, opts FetchOptions) (map[string][]sessions.Session, []string) {
	out := make(map[string][]sessions.Session, len(remotes))
	warnings := make([]string, 0, len(remotes))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, r := range remotes {
		remote := r
		wg.Add(1)
		go func() {
			defer wg.Done()
			sess, err := EnsureDaemonAndSnapshot(ctx, remote, opts, maxAge)
			if err != nil {
				mu.Lock()
				warnings = append(warnings, fmt.Sprintf("remote %s: %v", remote.Name, err))
				mu.Unlock()
				return
			}
			mu.Lock()
			out[remote.Name] = sess
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out, warnings
}
```

- [ ] **Step 5: Run remote package tests**

Run: `go test ./internal/remote -v`  
Expected: PASS including existing parsing tests adapted to RPC payload fixtures.

- [ ] **Step 6: Commit**

```bash
git add internal/remote/client.go internal/remote/client_test.go internal/remote/remote.go
git commit -m "feat(remote): fetch snapshots via managed daemon rpc"
```

### Task 4: Wire command-layer behavior and docs

**Files:**
- Modify: `cmd/remote_sessions_test.go`
- Modify: `README.md`

**Interfaces:**
- Consumes: `remote.FetchAll(..., FetchOptions)`.
- Produces: command tests and docs matching daemon architecture.

- [ ] **Step 1: Update failing command tests for new fetch signature**

```go
oldFetch := fetchRemoteSessions
defer func() { fetchRemoteSessions = oldFetch }()
fetchRemoteSessions = func(ctx context.Context, remotes []config.Remote, maxAge, timeout time.Duration, opts remote.FetchOptions) (map[string][]sessions.Session, []string) {
	if opts.CheckInterval != 24*time.Hour {
		t.Fatalf("CheckInterval = %v, want 24h", opts.CheckInterval)
	}
	return map[string][]sessions.Session{"devbox": {testSession("remote-devbox")}}, nil
}
```

- [ ] **Step 2: Run command tests**

Run: `go test ./cmd -run 'TestLoadAllWithRemotes_ConfigOrder|TestRefresh_WritesRemoteSessionsToCache' -v`  
Expected: PASS with updated fetch function signature.

- [ ] **Step 3: Update README remote mechanism section**

```md
`tsession` now manages a remote daemon (`tsession remote serve`) in tmux and requests snapshots over SSH stdio RPC. The daemon returns active sessions only.
```

- [ ] **Step 4: Run focused checks**

Run:

```bash
go test ./cmd -v
grep -q "remote daemon" README.md
```

Expected: all commands pass; grep exit code 0.

- [ ] **Step 5: Commit**

```bash
git add cmd/remote_sessions_test.go README.md
git commit -m "docs/tests: align remote behavior with daemon rpc architecture"
```

## Final Verification

- [ ] Run: `go test ./internal/remote ./cmd -v`
- [ ] Run: `go test ./...`
