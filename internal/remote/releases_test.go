package remote

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveRelease_PrefersExactTagThenFallsBackLatest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/yarma/tsession/releases/tags/v1.2.3":
			_, _ = w.Write([]byte(`{"tag_name":"v1.2.3","assets":[{"name":"tsession_darwin-arm64.tar.gz","browser_download_url":"https://example/tag-darwin"}]}`))
		case "/repos/yarma/tsession/releases/latest":
			_, _ = w.Write([]byte(`{"tag_name":"v1.2.9","assets":[{"name":"tsession_linux-amd64.tar.gz","browser_download_url":"https://example/latest-linux"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	got, err := resolveReleaseWithBaseURL(context.Background(), ts.URL, "yarma/tsession", "v1.2.3", "linux-amd64", ts.Client())
	if err != nil {
		t.Fatalf("resolveReleaseWithBaseURL error: %v", err)
	}
	if got.Version != "v1.2.9" {
		t.Fatalf("version = %q, want v1.2.9", got.Version)
	}
	if got.DownloadURL != "https://example/latest-linux" {
		t.Fatalf("downloadURL = %q, want latest linux asset", got.DownloadURL)
	}
}

func TestResolveRelease_ExactTagMatchesRuntime(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/yarma/tsession/releases/tags/v1.2.3":
			_, _ = w.Write([]byte(`{"tag_name":"v1.2.3","assets":[{"name":"tsession_darwin-arm64.tar.gz","browser_download_url":"https://example/tag-darwin"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	got, err := resolveReleaseWithBaseURL(context.Background(), ts.URL, "yarma/tsession", "v1.2.3", "darwin-arm64", ts.Client())
	if err != nil {
		t.Fatalf("resolveReleaseWithBaseURL error: %v", err)
	}
	if got.Version != "v1.2.3" {
		t.Fatalf("version = %q, want v1.2.3", got.Version)
	}
	if got.DownloadURL != "https://example/tag-darwin" {
		t.Fatalf("downloadURL = %q, want tag darwin asset", got.DownloadURL)
	}
}

func TestResolveRelease_NoMatchingAssetErrors(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/yarma/tsession/releases/tags/v1.2.3":
			http.NotFound(w, r)
		case "/repos/yarma/tsession/releases/latest":
			_, _ = w.Write([]byte(`{"tag_name":"v1.2.9","assets":[{"name":"tsession_darwin-arm64.tar.gz","browser_download_url":"https://example/latest-darwin"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	if _, err := resolveReleaseWithBaseURL(context.Background(), ts.URL, "yarma/tsession", "v1.2.3", "linux-arm64", ts.Client()); err == nil {
		t.Fatal("expected error when no asset matches runtime")
	}
}

func TestFindRuntimeAsset_MatchesPublishedAssetNaming(t *testing.T) {
	rel := release{
		TagName: "v0.5.0",
		Assets: []releaseAsset{{
			Name:        "tsession_v0.5.0_linux_amd64.tar.gz",
			DownloadURL: "https://example/linux-amd64",
		}},
	}

	got, ok := findRuntimeAsset(rel, "linux-amd64")
	if !ok {
		t.Fatal("expected published linux_amd64 asset to match linux-amd64 runtime")
	}
	if got.DownloadURL != "https://example/linux-amd64" {
		t.Fatalf("download URL = %q, want published asset URL", got.DownloadURL)
	}
}
