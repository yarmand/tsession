package remote

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/yarma/tsession/internal/config"
)

func TestEnsureRemoteBinary_UsesCachedWhenTTLValid(t *testing.T) {
	oldExists := remoteBinaryExistsFn
	t.Cleanup(func() { remoteBinaryExistsFn = oldExists })
	remoteBinaryExistsFn = func(context.Context, config.Remote, string) bool { return true }

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

func TestEnsureRemoteBinary_RefreshesWhenCachedBinaryIsMissing(t *testing.T) {
	oldExists := remoteBinaryExistsFn
	t.Cleanup(func() { remoteBinaryExistsFn = oldExists })
	remoteBinaryExistsFn = func(context.Context, config.Remote, string) bool { return false }

	state := UpdateState{
		LastCheckedAt: time.Now().UTC().Add(-2 * time.Hour),
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
		return ResolvedAsset{
			Version:     "v1.2.3",
			AssetName:   "tsession_v1.2.3_linux_amd64.tar.gz",
			DownloadURL: "https://example/asset",
		}, nil
	}

	_, err := ensureRemoteBinaryWithDeps(
		context.Background(),
		config.Remote{Name: "devbox", Type: "devcontainer", Container: "tsession-test-nonexistent-container"},
		"v1.2.3",
		UpdateOptions{CheckInterval: 24 * time.Hour},
		state,
		detectRuntimeFn,
		resolveReleaseFn,
	)
	if err == nil {
		t.Fatal("expected install error against nonexistent container")
	}
	if !detectCalled || !resolveCalled {
		t.Fatalf("missing cached binary did not refresh: detect=%v resolve=%v", detectCalled, resolveCalled)
	}
}

func TestEnsureRemoteBinary_ForceBypassesCacheAndRefreshes(t *testing.T) {
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
		return ResolvedAsset{Version: "v1.3.0", AssetName: "tsession_linux-amd64.tar.gz", DownloadURL: "https://example/asset"}, nil
	}

	_, err := ensureRemoteBinaryWithDeps(context.Background(), config.Remote{Name: "devbox", Type: "devcontainer", Container: "tsession-test-nonexistent-container"}, "v1.2.3", UpdateOptions{
		Force:         true,
		CheckInterval: 24 * time.Hour,
	}, state, detectRuntimeFn, resolveReleaseFn)
	// A real install would fail in this unit test environment since the
	// container does not exist; we only assert the refresh path was taken.
	if err == nil {
		t.Fatal("expected install error against nonexistent container")
	}
	if !detectCalled || !resolveCalled {
		t.Fatalf("expected detect/resolve calls when forced, got detect=%v resolve=%v", detectCalled, resolveCalled)
	}
}

func TestUpdateOptions_DefaultCheckInterval(t *testing.T) {
	opts := UpdateOptions{}
	if opts.CheckInterval != 0 {
		t.Fatalf("zero-value UpdateOptions.CheckInterval = %v, want 0 (caller supplies default)", opts.CheckInterval)
	}
}
