# Repository Aliases in Short Displays Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace origin-letter prefixes in `--short` output with `[repository-or-alias]worktree` labels and add `ctrl-a` repository alias editing while preserving `ctrl-n` session rename.

**Architecture:** Persist aliases in `~/.tsession/repo-names.json`, keyed by normalized repository identity. Resolve aliases in the existing short-render context and use that context in list, browse, fzf reloads, and previews. Add a repository-specific command and shortcut without changing per-session names.

**Tech Stack:** Go 1.25.6, standard library JSON/file APIs, tmux, fzf, existing internal packages.

## Global Constraints

- Preserve source glyphs, state glyphs, summaries, age suffixes, fzf IDs, and `--lshort` age preservation.
- Aliases apply to local and remote sessions and normalize equivalent SSH and HTTPS repository forms.
- Repository and alias labels use the first 10 runes.
- Missing repository data falls back to the worktree basename.
- Empty aliases clear the repository entry; persistence errors are surfaced.
- Keep `ctrl-n` for session rename and use `ctrl-a` for repository rename.
- Do not add dependencies or a branch field.

---

### Task 1: Create normalized repository alias storage

**Files:** Create `internal/repository/repository.go`, `internal/repository/repository_test.go`, `internal/reponames/reponames.go`, `internal/reponames/reponames_test.go`; modify `internal/render/shortctx.go`.

**Interfaces:** Provide `internal/repository.Normalize(string) string`; provide `reponames.Load() (map[string]string, error)`, `reponames.Get(string) (string, error)`, and `reponames.Set(string, string) error`. Set clears on an empty alias and must preserve unexpected read errors.

- [ ] Write tests for SSH/HTTPS normalization, missing files, persistence, clear, malformed JSON, unexpected read errors, and concurrent writers.
- [ ] Run `go test ./internal/repository ./internal/reponames ./internal/render` and verify the new tests fail before implementation.
- [ ] Implement normalization and the locked, atomic alias store following `internal/names` conventions.
- [ ] Run the focused tests and commit `feat: add persistent repository aliases`.

### Task 2: Render repository aliases in `--short`

**Files:** Modify `internal/render/shortctx.go`, `internal/render/render.go`, and `internal/render/render_test.go`.

**Interfaces:** Provide `BuildShortContextWithAliases([]sessions.Session, map[string]string) ShortContext`; retain `BuildShortContext([]sessions.Session) ShortContext`. Render `[label]worktree` when different and `[label]` when identical, preserving existing summary, age, ID, and `lshort` behavior.

- [ ] Add failing tests for 10-rune labels, custom aliases, identical names, missing repository fallback, normalized identity, and age preservation.
- [ ] Run `go test ./internal/render` and verify failure before implementation.
- [ ] Implement alias-aware labels and retain fzf field compatibility.
- [ ] Run `go test ./internal/render` and commit `feat: render repository aliases in short output`.

### Task 3: Add repository rename command and picker shortcut

**Files:** Create `cmd/rename_repo.go`, `cmd/rename_repo_test.go`; modify `main.go`, `cmd/browse.go`, `cmd/browse_test.go`, `README.md`, and `AGENTS.md`.

**Interfaces:** Add `RenameRepository(args []string) error`, invoked as `tsession rename-repo <session-id> [alias]`. Reject sessions without repository identity, accept joined CLI arguments or one trimmed interactive line, clear on empty input, and surface persistence errors. Bind `ctrl-n` to session rename and `ctrl-a` to repository rename; both reload.

- [ ] Add failing command and binding tests.
- [ ] Run `go test ./cmd` and verify failure before implementation.
- [ ] Implement command dispatch, fzf binding, popup behavior, help/footer text, and documentation.
- [ ] Run targeted and command tests and commit `feat: add repository rename shortcut`.

### Task 4: Wire alias loading through list and browse paths

**Files:** Modify `cmd/list.go`, `cmd/browse.go`, `cmd/browse_test.go`, `internal/render/render_test.go`; create `cmd/list_test.go`.

**Interfaces:** Load aliases once for each short render, pass the complete local-plus-remote list and alias map to `BuildShortContextWithAliases`, and return alias-load failures from list and browse initial rendering. Keep fzf field 2 as session ID, field 3 as repository, field 9 as legacy empty legend, and field 10 as remote origin.

- [ ] Add local/remote rendering tests and direct browse alias-load error regression coverage.
- [ ] Run focused tests and verify failure before implementation.
- [ ] Wire list and browse, preserve preview fields, and keep `ctrl-a` documentation/binding consistent.
- [ ] Run `go test ./cmd` and `go test ./...`; commit integration and any necessary baseline test expectation correction.

### Task 5: Final verification and documentation consistency

**Files:** Modify `README.md` and `AGENTS.md` only for stale wording.

- [ ] Search for stale `ctrl-N`, origin-letter, and old short-format references.
- [ ] Run `go test ./...`, `go vet ./...`, `git diff --check`, and inspect status.
- [ ] Commit any final documentation correction.
