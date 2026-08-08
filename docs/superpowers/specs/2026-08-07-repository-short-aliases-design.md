# Repository aliases in short session displays

## Goal

Improve `--short` readability by replacing origin-letter prefixes with a compact repository label and add a separate shortcut for renaming that repository label.

## User-visible behavior

Short session labels use this shape:

```text
[repository-or-alias] worktree-basename
```

The repository or alias is truncated to its first 10 runes. The worktree basename is included unless it is identical to the untruncated repository name or alias; in that case only the bracketed label is shown. Existing source glyphs, state glyphs, summaries, age suffixes, fzf IDs, and `--lshort` behavior remain intact. `--lshort N` continues to cap each visible line at N runes while preserving the age suffix.

If repository data is unavailable, short rendering falls back to the existing worktree basename. Repository aliases apply to local and remote sessions and are keyed by normalized repository identity, so equivalent SSH and HTTPS forms share one alias.

## Architecture

Add a focused repository-alias persistence package that stores aliases in:

```text
~/.tsession/repo-names.json
```

The package exposes load, lookup, set, and clear operations. It should follow the existing names package's file permissions, directory handling, and synchronization conventions while keeping repository aliases separate from per-session display names.

Extend the short-render context to resolve the repository identity, alias, compact repository label, and worktree basename for each session. The context is built once from the complete local-plus-remote list and reused by list output, browse output, and fzf reloads so all views use the same labels.

## Shortcuts and command flow

Keep `ctrl-n` mapped to the existing per-session rename command.

Add `ctrl-N` as a repository-alias action. It opens a small tmux popup when browsing inside tmux, otherwise executes directly, showing the selected session's repository and current alias. The command accepts a new alias or an empty value to clear it, persists the result, and reloads the picker. If the selected session has no repository identity, report an explicit error rather than silently changing unrelated state.

Update help text, footer text, and README keybinding documentation to distinguish:

```text
ctrl-n  Rename session
ctrl-N  Rename repository
```

## Error handling

Persistence path, read, parse, and write failures must be surfaced by the alias command. Empty aliases clear the repository entry. A missing alias file is treated as an empty alias map. Malformed JSON follows the repository's established names-file behavior unless tests expose a need for a stricter migration path.

## Testing

Add focused tests for:

- repository identity normalization across SSH and HTTPS forms;
- alias load, set, lookup, clear, and persistence;
- 10-rune repository/alias truncation;
- suppression of the worktree basename when names are identical;
- fallback behavior when repository data is missing;
- short rendering through list and browse/fzf contexts;
- `--lshort` line limits and age preservation;
- separate `ctrl-n` and `ctrl-N` binding commands and reload behavior;
- explicit failure for repository-alias editing without repository identity.

No branch field is needed for this feature; the existing worktree basename remains the session-specific suffix.
