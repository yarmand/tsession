# Final Review Fix Report

## Finding
- Medium: `internal/webui/localterm.go` reused `registry.Live(key)` as the only warm-terminal check, so an exited local terminal PTY stayed registered and was bridged again on later `/api/localterm` requests.

## Approach
- Added a dedicated `localTermMu` on `webui.Server` to serialize local-terminal stale-entry recovery and startup.
- Introduced `ensureLocalTerminal` to:
  - check the registered local terminal,
  - detect already-exited PTYs via `term.Exited()`,
  - remove stale entries with `registry.Close(key)`, and
  - create a fresh attachment only after cleanup.
- Kept teardown behavior unchanged: cleanup still removes only the grouped web session and never kills the persistent `tsession-local` shell.
- Limited the change to the local terminal route; shared `/api/terminal` behavior was left untouched.

## Tests
1. Added `TestHandleLocalTerminal_ExitedWarmPTYIsDiscardedBeforeRestart`.
   - Registers an already-exited terminal for the local-terminal key.
   - Forces deterministic startup failure with `SetLocalTerminalHome`.
   - Verifies `/api/localterm` returns HTTP 500 instead of bridging the stale PTY.
   - Verifies the stale terminal is removed from the registry and no replacement remains registered after the failed restart.

## Outputs
- RED:
  ```bash
  go test ./internal/webui/ -run 'TestHandleLocalTerminal_ExitedWarmPTYIsDiscardedBeforeRestart' -v
  ```
  Result: FAIL
  - `expected dial to fail after stale local terminal cleanup`

- GREEN:
  ```bash
  go test ./internal/webui/ -run 'TestHandleLocalTerminal_ExitedWarmPTYIsDiscardedBeforeRestart' -v
  ```
  Result: PASS

- Focused verification:
  ```bash
  go test ./internal/webui/ -run 'LocalTerminal|HandleTerminal' -v
  ```
  Result: PASS

## Commit
- Subject: `fix: recover exited local terminal pty`
