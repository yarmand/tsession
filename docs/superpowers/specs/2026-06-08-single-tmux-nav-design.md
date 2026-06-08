# Single-tmux navigation — design

## Goal & scope

Replace the multi-client `--target` model with a single, self-contained tmux
experience. There is one tmux client and one navigator pane that **docks beside
whichever agent session you select**. Agents are never disrupted: the navigator
travels to them.

This spec covers the navigation/display rework only. The workspace model is
*sketched* — just enough to group the nav list and avoid painting future work
into a corner. Worktree management commands are deferred to a follow-up spec.

### In scope

- Remove `--target` and all multi-client plumbing.
- Auto-create a `sessions-nav` tmux session laid out as `[nav | main]`.
- Implement the "nav travels" pane mechanic (`join-pane`/`break-pane`) with
  navigator-width preservation across hops.
- Rework remote sessions to fit the single-tmux model (no client switching).
- Light workspace attribution (repo/worktree) for nav grouping labels.

### Out of scope

- Worktree commands (`list` / `create` / `delete`).
- `new agent session` creation (current folder / new worktree / existing).
- Relocating or normalizing existing agent panes into per-worktree sessions.

## tmux topology

```
session "sessions-nav"        window 0  (resting state)
┌────────────┬───────────────────────┐
│  nav (fzf) │   main (placeholder)  │
└────────────┴───────────────────────┘

session "<worktree>"          window N  (docked state — agent's own window)
┌────────────┬───────────────────────┐
│  nav (fzf) │   agent (undisturbed) │
└────────────┴───────────────────────┘
```

- **`sessions-nav`** is auto-created by `tsession browse` when launched outside
  tmux. Window 0 has two panes: `nav` (runs the picker) and `main` (a plain
  shell placeholder).
- **`main`** never moves. It is a permanent fixture of `sessions-nav:0` so the
  layout always reads `[nav | main]` at rest, and `[nav | agent]` when docked.
- **Agents** stay in their existing windows/sessions. tsession discovers them
  and hops the navigator beside them; it does not relocate them.
- **`nav`** is the single traveler. It hops out to the selected agent's window
  and back home to `sessions-nav:0` beside `main`.

There is exactly one navigator pane (one fzf process). It is cheap to move and
moving it does not disturb agents.

## The nav-hop mechanic

The navigator runs `tsession browse` (the fzf picker). On `enter`, the fzf
binding executes `tsession resume {id}`. The navigator pane id is read from the
`$TMUX_PANE` environment variable that tmux exports to the execute child.

`tsession resume` performs:

1. **Capture width** — `tmux display -p -t $TMUX_PANE '#{pane_width}'`. This
   preserves a manually-resized navigator width across hops.
2. **Hop** — `tmux join-pane -h -b -l <width> -s $TMUX_PANE -t <agentWindow>`
   inserts the navigator as the left split of the agent's window.
3. **Focus** — `tmux switch-client -t <agentWindow>`.

Behavioral details:

- Selecting a **different** agent hops the navigator from its current window to
  the new target. `join-pane` removes it from the old window, which reflows to a
  full-width agent pane.
- `esc` / no selection hops the navigator **home**: `join-pane -h -b -l <width>`
  to the left of `main` in `sessions-nav:0`, then `switch-client`.
- Re-selecting the **already-docked** agent is a no-op (the navigator is already
  beside it).
- `join-pane` / `break-pane` never kill processes, so agents keep their
  scrollback and pane layout intact.

The navigator runs inside the pane being moved. Moving a pane mid-process is
safe in tmux: the fzf process continues and redraws on the resulting resize.

## Resume flow

| Case | Behavior |
|------|----------|
| Local agent with a tmux window | Nav-hop beside the agent's window. |
| Remote agent with a resolved local pane | Resolve the SSH/codespace pane that hosts it (existing machinery), then nav-hop beside that pane — identical to local. |
| Remote agent with no local pane | Open the SSH connection as a new window, then nav-hop beside it. |
| Cold session, no tmux match | Fall back to spawning into `main` / `copilot --resume` / `pi --session` as today. |

No client switching (`switch-client -c <client>`) occurs anywhere. The single
client always follows via `switch-client -t <target>`.

## Command surface & `--target` teardown

- `tsession browse` remains the entry point; the `--target` flag is removed.
  Outside tmux it auto-creates `sessions-nav` laid out as `[nav | main]` and
  re-execs the picker in the `nav` pane.
- **Removals**: `--target` from `browse` and `resume`; `tmux.ResolveTarget`;
  `tmux.pickClient`; the client argument on `SwitchClientTarget` (collapse to a
  single `SwitchClient(target)`); all `-c <client>` plumbing.
- The session name is standardized to **`sessions-nav`** (current code uses
  `session-nav`).
- README.md and AGENTS.md are updated: remove the native-split / multi-client
  instructions and document the single-tmux `[nav | main]` flow.

## Workspace model sketch

- A **Workspace** is a worktree directory. Its tmux session (named after the
  worktree) is realized later by the `new agent session` command.
- A **Repo** is a grouping label, derived from each session's `cwd` via
  `git rev-parse --git-common-dir` (and toplevel). Sessions carry lightweight
  `Repo` / `Worktree` attribution used **only** for nav grouping labels in this
  spec.
- Deferred to a follow-up spec: `list` / `create` / `delete` worktree,
  `new agent session` (current folder / new worktree / existing worktree), and
  per-worktree tmux-session normalization.

## Components to change

| Path | Change |
|------|--------|
| `internal/tmux/tmux.go` | Add `PaneWidth`, `JoinPaneLeft(src, targetWindow, width)`, and a helper to read the current pane id from `$TMUX_PANE`. Simplify `SwitchClient`. Remove `ResolveTarget` / `pickClient`. |
| `cmd/resume.go` | Implement the nav-hop logic; drop `--target`; rework the remote path to hop beside the resolved/created remote pane. |
| `cmd/browse.go` | Drop `--target`; `launchInTmux` builds the `[nav | main]` layout; rename the session to `sessions-nav`. |
| `internal/sessions` | Add light `Repo` / `Worktree` attribution; keep remote resolution but remove target coupling. |
| `README.md`, `AGENTS.md` | Document the single-tmux flow; remove multi-client guidance. |

## Testing

- Go unit tests for the new tmux command-builders (`JoinPaneLeft`, width
  capture) following the existing `tmux_test.go` parse-test style.
- Update `cmd/remote_sessions_test.go` and resume tests for the no-target flow.
- Validate with `go build .` and `go test ./...` from the repo root.
