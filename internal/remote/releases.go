package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const githubAPIBaseURL = "https://api.github.com"

// ResolvedAsset describes a release asset selected for a given runtime.
type ResolvedAsset struct {
	Version     string
	AssetName   string
	DownloadURL string
}

type releaseAsset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"browser_download_url"`
}

type release struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

// ResolveRelease resolves the release asset to install for a given runtime,
// preferring an exact match on clientTag and falling back to the latest
// release when the client's tag has no matching asset for runtime.
func ResolveRelease(ctx context.Context, repo, clientTag, runtime string, httpClient *http.Client) (ResolvedAsset, error) {
	return resolveReleaseWithBaseURL(ctx, githubAPIBaseURL, repo, clientTag, runtime, httpClient)
}

func resolveReleaseWithBaseURL(ctx context.Context, baseURL, repo, clientTag, runtime string, httpClient *http.Client) (ResolvedAsset, error) {
	if clientTag != "" {
		if tagRelease, err := fetchReleaseByTag(ctx, httpClient, baseURL, repo, clientTag); err == nil {
			if a, ok := findRuntimeAsset(tagRelease, runtime); ok {
				return a, nil
			}
		}
	}

	latest, err := fetchLatestRelease(ctx, httpClient, baseURL, repo)
	if err != nil {
		return ResolvedAsset{}, err
	}
	if a, ok := findRuntimeAsset(latest, runtime); ok {
		return a, nil
	}
	return ResolvedAsset{}, fmt.Errorf("no release asset for runtime %s", runtime)
}

func fetchReleaseByTag(ctx context.Context, httpClient *http.Client, baseURL, repo, tag string) (release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/tags/%s", baseURL, repo, tag)
	return fetchRelease(ctx, httpClient, url)
}

func fetchLatestRelease(ctx context.Context, httpClient *http.Client, baseURL, repo string) (release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", baseURL, repo)
	return fetchRelease(ctx, httpClient, url)
}

func fetchRelease(ctx context.Context, httpClient *http.Client, url string) (release, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return release{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return release{}, fmt.Errorf("fetch release %s: unexpected status %d", url, resp.StatusCode)
	}

	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return release{}, fmt.Errorf("fetch release %s: %w", url, err)
	}
	return rel, nil
}

func findRuntimeAsset(rel release, runtime string) (ResolvedAsset, bool) {
	suffix := "_" + runtime
	for _, a := range rel.Assets {
		if strings.Contains(a.Name, suffix) {
			return ResolvedAsset{
				Version:     rel.TagName,
				AssetName:   a.Name,
				DownloadURL: a.DownloadURL,
			}, true
		}
	}
	return ResolvedAsset{}, false
}
