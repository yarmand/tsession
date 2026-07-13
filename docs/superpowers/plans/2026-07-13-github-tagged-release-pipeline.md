# GitHub Tagged Release Pipeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build and publish cross-compiled `tsession` binaries to GitHub Releases whenever a version tag is pushed.

**Architecture:** Add a dedicated release workflow (`.github/workflows/release.yml`) triggered on `v*` tags. It builds binaries for the supported runtime matrix, packages deterministic artifact names, and uploads them to the release for that tag. Keep CI workflow unchanged for PR/push validation.

**Tech Stack:** GitHub Actions, Go 1.25, matrix builds, `gh` CLI or `softprops/action-gh-release`.

## Global Constraints

- Trigger on version tags (`v*`) only.
- Build targets: `linux/amd64`, `linux/arm64`, `darwin/arm64`.
- Asset naming must be deterministic and resolver-friendly.
- Releases must support exact-tag selection and latest-tag fallback consumption.

---

## File Structure

- `.github/workflows/release.yml` (new) — tagged release workflow.
- `README.md` — release process and artifact naming docs.
- `internal/remote/releases_test.go` — update/confirm asset-name matching expectations against workflow output.

### Task 1: Add tagged release workflow skeleton

**Files:**
- Create: `.github/workflows/release.yml`

**Interfaces:**
- Produces:
  - Trigger: `on.push.tags: ['v*']`
  - Matrix: `{linux/amd64, linux/arm64, darwin/arm64}`
  - Assets: `tsession_<tag>_<os>_<arch>.tar.gz`

- [ ] **Step 1: Write workflow with matrix build + package**

```yaml
name: Release

on:
  push:
    tags:
      - 'v*'

jobs:
  build:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        include:
          - goos: linux
            goarch: amd64
          - goos: linux
            goarch: arm64
          - goos: darwin
            goarch: arm64
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - run: mkdir -p dist
      - run: GOOS=${{ matrix.goos }} GOARCH=${{ matrix.goarch }} go build -ldflags "-X github.com/yarma/tsession/internal/version.tag=${GITHUB_REF_NAME}" -o "dist/tsession"
      - run: tar -C dist -czf "dist/tsession_${GITHUB_REF_NAME}_${{ matrix.goos }}_${{ matrix.goarch }}.tar.gz" tsession
      - uses: actions/upload-artifact@v4
        with:
          name: release-${{ matrix.goos }}-${{ matrix.goarch }}
          path: dist/tsession_${{ github.ref_name }}_${{ matrix.goos }}_${{ matrix.goarch }}.tar.gz
```

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "ci(release): add tag-triggered cross-platform build workflow"
```

### Task 2: Publish release assets to GitHub Release

**Files:**
- Modify: `.github/workflows/release.yml`

**Interfaces:**
- Consumes: artifacts from build matrix job.
- Produces: uploaded assets attached to tag release.

- [ ] **Step 1: Add publish job**

```yaml
  publish:
    runs-on: ubuntu-latest
    needs: build
    permissions:
      contents: write
    steps:
      - uses: actions/download-artifact@v4
        with:
          path: dist
      - run: find dist -type f -name '*.tar.gz' -print
      - uses: softprops/action-gh-release@v2
        with:
          files: dist/**/*.tar.gz
          tag_name: ${{ github.ref_name }}
          generate_release_notes: true
```

- [ ] **Step 2: Add safety check for expected assets**

```yaml
      - name: Validate expected asset count
        run: |
          count=$(find dist -type f -name '*.tar.gz' | wc -l | tr -d ' ')
          test "$count" = "3"
```

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "ci(release): publish matrix artifacts to github release"
```

### Task 3: Align resolver tests with release asset naming

**Files:**
- Modify: `internal/remote/releases_test.go`

**Interfaces:**
- Consumes: workflow output naming convention.
- Produces: tests that assert resolver accepts `tsession_<tag>_<os>_<arch>.tar.gz`.

- [ ] **Step 1: Add failing naming test**

```go
func TestFindRuntimeAsset_MatchesReleaseWorkflowNaming(t *testing.T) {
	assets := []releaseAsset{
		{Name: "tsession_v1.4.0_linux_amd64.tar.gz", BrowserDownloadURL: "https://example/linux-amd64"},
		{Name: "tsession_v1.4.0_linux_arm64.tar.gz", BrowserDownloadURL: "https://example/linux-arm64"},
	}
	got, ok := findRuntimeAsset(release{TagName: "v1.4.0", Assets: assets}, "linux-amd64")
	if !ok {
		t.Fatal("expected linux-amd64 asset match")
	}
	if got.DownloadURL != "https://example/linux-amd64" {
		t.Fatalf("download url = %q", got.DownloadURL)
	}
}
```

- [ ] **Step 2: Run resolver test to verify failure (if matcher still expects old naming)**

Run: `go test ./internal/remote -run TestFindRuntimeAsset_MatchesReleaseWorkflowNaming -v`  
Expected: FAIL until matcher supports workflow naming.

- [ ] **Step 3: Update matcher to accept exact workflow asset names**

```go
want := fmt.Sprintf("tsession_%s_%s_%s.tar.gz", tag, goos, goarch)
if a.Name == want {
	return ResolvedAsset{Version: tag, AssetName: a.Name, DownloadURL: a.BrowserDownloadURL}, true
}
```

- [ ] **Step 4: Run remote tests**

Run: `go test ./internal/remote -run 'TestFindRuntimeAsset_MatchesReleaseWorkflowNaming|TestResolveRelease' -v`  
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/remote/releases_test.go internal/remote/releases.go
git commit -m "test(remote): align release resolver with workflow asset names"
```

### Task 4: Document release process

**Files:**
- Modify: `README.md`

**Interfaces:**
- Produces: contributor docs for tagging flow and published artifacts.

- [ ] **Step 1: Add README release section**

```md
## Releases

Push a version tag (`vX.Y.Z`) to trigger `.github/workflows/release.yml`.
The workflow publishes:
- `tsession_<tag>_linux_amd64.tar.gz`
- `tsession_<tag>_linux_arm64.tar.gz`
- `tsession_<tag>_darwin_arm64.tar.gz`
```

- [ ] **Step 2: Validate docs include filenames**

Run:

```bash
grep -q "tsession_<tag>_linux_amd64.tar.gz" README.md && grep -q ".github/workflows/release.yml" README.md
```

Expected: exit code 0.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: add tagged release workflow and asset naming guide"
```

## Final Verification

- [ ] Run: `go test ./internal/remote -v`
- [ ] Verify workflow syntax with: `gh workflow view release.yml --yaml` (after pushing file)

