# Browse `--short` left-nav rendering

## Context
`tsession browse` uses `fzf` and feeds it output from `tsession list --fzf`.
Today, `--short` renders:
- `STATE` (glyph + state text)
- `AGE`
- `REPO/CWD` (basename)
- truncated `SUMMARY`

## Goals
- Make `browse --short` feel like a continuous “left navigation pane”.
- In `--short`, hide the **state text** and keep only the **state icon**.
- Move **age to the end** of the displayed line for better scanning.
- Prefix `REPO/CWD` with a letter (e.g. `A-`) indicating the **origin repo**.
- Show a legend mapping letters to origin repos (e.g. `A:tsession`) at the bottom of the browse UI.

## Non-goals
- Do not run `git` in every session directory to discover origin; use the existing `Repository` field.
- Do not add a new persistent config format for letter assignment.

## Approaches considered
1. **Format-only change**: adjust `FormatLineShort` columns, put legend in the header.
   - Pros: minimal change.
   - Cons: doesn’t really create a “left-nav pane”; legend isn’t at the bottom.
2. **fzf preview + hidden fields (recommended)**: keep list compact, add a preview pane with details + legend at the bottom.
   - Pros: matches “left navigation pane” mental model; legend naturally lives at bottom of preview.
   - Cons: requires changing `--fzf` output format to carry extra fields.
3. **New command (e.g. `tsession show <id>`)** for preview.
   - Pros: clean UI boundary.
   - Cons: more surface area; not necessary.

## Design (selected)
### Short line display (what fzf shows)
Displayed (field 1) will be:

```
  <glyph> <LETTER>-<worktree> <summary> <age>
```

- `<glyph>`: from the existing state glyph mapping.
- Hide state text entirely in `--short` display.
- `<worktree>`: `basename(CWD)` when available; otherwise `basename(Repository)`.
- `<LETTER>`: `A`, `B`, ... assigned by first appearance of each unique `Repository` in the rendered list.
- `<age>`: right-aligned, always at the end of the visible line.

### `--lshort N` behavior
Guarantee age stays visible at the end:
- Treat the display as `prefix + summary + suffix`.
- Compute the max summary width as `N - len(prefix) - len(suffix)` (in runes).
- Truncate only the summary to fit.

### Origin legend
Compute a legend string like:

```
Origins:
A:tsession
B:another-repo
```

Where the repo short-name is derived from `Repository` by taking the last path segment and stripping `.git`.

### fzf browse/popup behavior (left-nav pane)
When `--short`/`--lshort` is active in `browse`/`popup`:
- Enable a right-side preview pane: `--preview-window=right:60%:wrap`.
- Add a preview command that shows:
  - ID, state text, age
  - full CWD and Repository
  - full summary
  - the origin legend at the bottom

Implementation detail: extend `list --fzf` output to include extra hidden tab-delimited fields:

```
<display>\t<ID>\t<origin>\t<CWD>\t<repoShort>\t<stateText>\t<age>\t<fullSummary>\t<legend>
```

`fzf` continues to show only field 1 and accept/copy field 2.

## Verification
- `go test ./...`
- Manual:
  - `go run . list --short`
  - `go run . browse --short`
  - `go run . popup --active --short`
