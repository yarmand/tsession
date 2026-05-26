# Browse `--short` Left-Nav Rendering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `tsession browse --short` render as a compact “left navigation pane” by showing only the state glyph, moving age to the end, prefixing worktree display with an origin-letter, and showing an origin legend in a right-side preview.

**Architecture:** Compute a per-render origin→letter mapping from the session list, render `--short` lines using that mapping, and emit extra hidden tab-delimited fields in `list --fzf` so `fzf` preview can display full details plus the legend.

**Tech Stack:** Go, `fzf` (tab-delimited fields + preview), existing `internal/render` + `cmd/list.go` + `cmd/browse.go`.

---

## File map (what changes where)

- Modify: `internal/render/render.go` — update short header + short formatting (glyph only, age at end).
- Create: `internal/render/shortctx.go` — origin→letter mapping + origin short-name helpers + short display builder (including lshort-aware truncation that preserves age).
- Modify: `internal/render/render_test.go` — add tests for new short formatting, origin parsing, and lshort behavior.
- Modify: `cmd/list.go` — for `--fzf --short`, emit extra hidden fields for preview; remove legacy whole-line truncation for `--lshort` (delegated to renderer).
- Modify: `cmd/browse.go` — when `--short/--lshort`, add `fzf` preview pane configured as right-side detail view; ensure initial list bytes use the same fzf line formatting as `list --fzf`.
- (Optional) Modify: `README.md` — update `--short` description to match new layout.

---

### Task 1: Add failing tests for new `--short` rendering

**Files:**
- Modify: `internal/render/render_test.go`

- [ ] **Step 1: Write failing tests**

Add these tests to `internal/render/render_test.go`:

```go
func TestOriginShortName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"ps://github.com/yarmand/tsession.git", "tsession"},
		{"https://github.com/yarmand/tsession", "tsession"},
		{"git@github.com:yarmand/tsession.git", "tsession"},
		{"gh/yarmand/tsession", "tsession"},
		{"", "-"},
	}
	for _, c := range cases {
		if got := originShortName(c.in); got != c.want {
			t.Errorf("originShortName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatLineShort_GlyphOnlyAndAgeAtEnd(t *testing.T) {
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	s := sessions.Session{
		ID:          "uuid-123",
		CWD:         "/tmp/worktrees/better-short",
		Repository:  "ps://github.com/yarmand/tsession.git",
		Summary:     "do the thing",
		LastEventAt: now.Add(-5 * time.Minute),
		State:       sessions.StateWorking,
	}
	ctx := buildShortContext([]sessions.Session{s})
	got := FormatLineShortWithContext(s, now, false, ctx, 0)
	parts := strings.Split(got, "\t")
	if len(parts) != 2 {
		t.Fatalf("want 2 fields (display + id), got %d: %q", len(parts), got)
	}
	display := parts[0]
	if strings.Contains(display, "working") {
		t.Fatalf("short display should not contain state text: %q", display)
	}
	if !strings.Contains(display, "●") {
		t.Fatalf("short display should contain glyph: %q", display)
	}
	if !strings.Contains(display, "A-better-short") {
		t.Fatalf("short display should contain origin-letter prefixed worktree: %q", display)
	}
	// age should be at the end (ignore trailing spaces)
	trim := strings.TrimRight(display, " ")
	if !strings.HasSuffix(trim, "5m") {
		t.Fatalf("short display should end with age '5m': %q", display)
	}
}

func TestFormatLineShort_LshortPreservesAge(t *testing.T) {
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	s := sessions.Session{
		ID:          "uuid-123",
		CWD:         "/tmp/worktrees/better-short",
		Repository:  "ps://github.com/yarmand/tsession.git",
		Summary:     strings.Repeat("x", 200),
		LastEventAt: now.Add(-5 * time.Minute),
		State:       sessions.StateWorking,
	}
	ctx := buildShortContext([]sessions.Session{s})
	got := FormatLineShortWithContext(s, now, false, ctx, 32) // very small
	parts := strings.Split(got, "\t")
	display := parts[0]
	if len([]rune(display)) > 32 {
		t.Fatalf("want display <= 32 runes, got %d: %q", len([]rune(display)), display)
	}
	trim := strings.TrimRight(display, " ")
	if !strings.HasSuffix(trim, "5m") {
		t.Fatalf("lshort display should still end with age '5m': %q", display)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/render -run TestOriginShortName -v
```

Expected: FAIL to compile because `originShortName`, `buildShortContext`, and `FormatLineShortWithContext` don’t exist yet.

- [ ] **Step 3: Commit tests**

```bash
git add internal/render/render_test.go
git commit -m "test: specify browse --short left-nav formatting"
```

---

### Task 2: Implement origin-letter mapping + new short renderer

**Files:**
- Create: `internal/render/shortctx.go`
- Modify: `internal/render/render.go`

- [ ] **Step 1: Implement context + helpers**

Create `internal/render/shortctx.go`:

```go
package render

import (
	"path/filepath"
	"strings"

	"github.com/yarma/tsession/internal/sessions"
)

type shortContext struct {
	originToLetter map[string]string
	legendField    string // single-line, 'A:repo|B:repo'
}

func buildShortContext(all []sessions.Session) shortContext {
	originToLetter := make(map[string]string)
	var legend []string
	for _, s := range all {
		origin := strings.TrimSpace(s.Repository)
		if origin == "" {
			continue
		}
		if _, ok := originToLetter[origin]; ok {
			continue
		}
		letter := string(rune('A' + len(originToLetter)))
		originToLetter[origin] = letter
		legend = append(legend, letter+":"+originShortName(origin))
	}
	return shortContext{originToLetter: originToLetter, legendField: strings.Join(legend, "|")}
}

func originShortName(origin string) string {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return "-"
	}
	s := strings.TrimRight(origin, "/")
	// Handle scp-like: git@github.com:org/repo.git
	if i := strings.LastIndex(s, ":"); i >= 0 && strings.Contains(s[:i], "@") {
		s = s[i+1:]
	}
	base := filepath.Base(s)
	base = strings.TrimSuffix(base, ".git")
	if base == "" {
		return "-"
	}
	return base
}

func shortWorktreeName(s sessions.Session) string {
	if strings.TrimSpace(s.CWD) != "" {
		return filepath.Base(s.CWD)
	}
	if strings.TrimSpace(s.Repository) != "" {
		return originShortName(s.Repository)
	}
	return "-"
}
```

- [ ] **Step 2: Update short formatting in `internal/render/render.go`**

Add a new function (below `FormatLineShort`) and update header:

```go
const HeaderShort = "  S  REPO/CWD                 SUMMARY                          AGE"

func FormatLineShortWithContext(s sessions.Session, now time.Time, color bool, ctx shortContext, lshort int) string {
	ts := s.LastEventAt
	if ts.IsZero() {
		ts = s.UpdatedAt
	}
	age := FormatAge(now.Sub(ts))

	glyph := stateGlyph(s.State)
	if color {
		glyph = colorize(s.State, glyph)
	}

	origin := strings.TrimSpace(s.Repository)
	letter := "-"
	if origin != "" {
		if l, ok := ctx.originToLetter[origin]; ok {
			letter = l
		}
	}

	name := shortWorktreeName(s)
	repoCol := letter + "-" + name

	summary := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(s.Summary, "\n", " "), "\r", " "), "\t", " ")
	if summary == "" {
		summary = "(no summary)"
	}

	prefix := "  " + glyph + " " + padRight(truncate(repoCol, 24), 24) + " "
	suffix := " " + age

	display := prefix + truncate(summary, 60) + suffix
	if lshort > 0 {
		// preserve suffix by truncating summary only
		budget := lshort - len([]rune(prefix)) - len([]rune(suffix))
		if budget < 0 {
			budget = 0
		}
		display = prefix + truncate(summary, budget) + suffix
	}
	return display + "\t" + s.ID
}
```

Then update the existing `FormatLineShort` to call the new function with an empty context (only used in non-list code paths; list/browse will call with real context):

```go
func FormatLineShort(s sessions.Session, now time.Time, color bool) string {
	return FormatLineShortWithContext(s, now, color, shortContext{}, 0)
}
```

- [ ] **Step 3: Run render tests**

```bash
go test ./internal/render -v
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/render/render.go internal/render/shortctx.go
git commit -m "feat: render --short as glyph+origin-letter and age suffix"
```

---

### Task 3: Emit richer `--fzf` fields and add browse preview pane

**Files:**
- Modify: `cmd/list.go`
- Modify: `cmd/browse.go`

- [ ] **Step 1: Update `cmd/list.go` fzf short output**

In the `useShort` branch when `*fzfMode` is true:
- Compute `ctx := render.BuildShortContext(merged)` (export if needed) and per-session `render.FormatLineShortWithContext(..., ctx, *lshort)`.
- Emit extra hidden fields *after* the ID, e.g.:

```go
line := render.FormatLineShortWithContext(s, now, false, ctx, *lshort)
// split into display + id
parts := strings.SplitN(line, "\t", 2)
fmt.Fprintf(os.Stdout, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
	parts[0],
	parts[1],
	s.Repository,
	s.CWD,
	render.OriginShortName(s.Repository),
	s.State.String(),
	render.FormatAge(now.Sub(ts)),
	sanitizedSummary,
	ctx.LegendField(),
)
```

Also remove the existing whole-line truncation logic for `lshort` (renderer will keep age visible).

- [ ] **Step 2: Update `cmd/browse.go` initial list bytes to match**

In `initialListBytes`, compute the same `ctx` from `merged` and use the same formatting/output fields as `list --fzf`.

- [ ] **Step 3: Add fzf preview pane when `useShort`**

In `runFzfOpts`, when `useShort` is true, add:

```go
fzfArgs = append(fzfArgs,
	"--preview-window=right:60%:wrap",
	"--preview=sh -c 'legend=$(printf %s "$7" | tr "|" "\\n"); printf "ID: %s\\nState: %s\\nAge: %s\\nCWD: %s\\nRepo: %s\\n\\n%s\\n\\nOrigins:\\n%s\\n" "$1" "$2" "$3" "$4" "$5" "$6" "$legend"' _ {2} {6} {7} {4} {3} {8} {9}",
)
```

- [ ] **Step 4: Run tests + quick manual smoke**

```bash
go test ./...

go run . list --short | head

go run . list --fzf --short | head
```

- [ ] **Step 5: Commit**

```bash
git add cmd/list.go cmd/browse.go
git commit -m "feat: browse --short preview + origin legend via fzf fields"
```

---

### Task 4: Update README `--short` description

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update docs**

Change `--short` description to reflect:
- glyph-only state
- age at end
- origin-letter prefix + origin legend in browse preview

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: clarify --short left-nav layout"
```

---

### Task 5: Full verification

- [ ] **Step 1: Run full test suite**

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 2: Manual browse**

```bash

tsession browse --short
```
Expected: list shows glyph + `A-<worktree>` + summary + age (age at end). A right-side preview shows ID/state/paths + origin legend at the bottom.

- [ ] **Step 3: Final commit status**

```bash
git status --porcelain
```

Expected: clean.
