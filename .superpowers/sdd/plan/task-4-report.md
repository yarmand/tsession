# Task 4 Report

## Changed files
- `AGENTS.md`
  - Added the `### Local terminal (`Alt+T`)` subsection to the Web UI (`serve`) documentation.
  - Added `Alt+T` to the picker-shortcut table.
- `docs/superpowers/plans/2026-09-30-local-terminal.md`
  - Copied the session plan from `/Users/yarma/.copilot/session-state/8298b380-b146-4dbb-84e7-c5e12d921d09/plan.md`.

## Validation
Command run:
```bash
go build ./... && go vet ./... && go test ./...
```
Result: PASS

Summary of output:
- `go build ./...` completed successfully.
- `go vet ./...` completed successfully.
- `go test ./...` completed successfully across all packages.

## Commit
- SHA: `141175c`
- Subject: `docs: describe the pinned local terminal and its lifecycle`

## Concerns
- The prescribed browser manual check was unavailable and is intentionally not claimed here, per the task brief.

## Final review fix
- Finding: local terminal reused an exited PTY because `handleLocalTerminal` only checked `registry.Live(key)`, which excludes closed terminals but still returns exited processes.
- Fix: serialized local-terminal ensure/recovery with a dedicated server mutex, detected `term.Exited()` before bridging, closed stale registry entries, and only then attempted a fresh local attachment.
- Regression test: `TestHandleLocalTerminal_ExitedWarmPTYIsDiscardedBeforeRestart` pre-registers an exited local terminal, forces deterministic startup failure via `SetLocalTerminalHome`, and verifies `/api/localterm` returns HTTP 500 instead of bridging stale state while leaving no terminal registered.
- Validation:
  - RED: `go test ./internal/webui/ -run 'TestHandleLocalTerminal_ExitedWarmPTYIsDiscardedBeforeRestart' -v` → FAIL (`expected dial to fail after stale local terminal cleanup`)
  - GREEN: `go test ./internal/webui/ -run 'TestHandleLocalTerminal_ExitedWarmPTYIsDiscardedBeforeRestart' -v` → PASS
  - Focused suite: `go test ./internal/webui/ -run 'LocalTerminal|HandleTerminal' -v` → PASS
- Commit: `fix: recover exited local terminal pty`
