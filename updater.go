package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const githubLatestReleaseURL = "https://api.github.com/repos/%s/releases/latest"

type updateOptions struct {
	Repository string
	AssetName  string
	Version    string
	Client     *http.Client
}

type githubRelease struct {
	TagName string               `json:"tag_name"`
	Assets  []githubReleaseAsset `json:"assets"`
}

type githubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func defaultUpdateAsset() string {
	return fmt.Sprintf("rooter-%s-%s", runtime.GOOS, runtime.GOARCH)
}

func updateFromGitHub(ctx context.Context, opts updateOptions) (bool, error) {
	repo := strings.TrimSpace(opts.Repository)
	if !validGitHubRepository(repo) {
		return false, errors.New("update repository must be set as owner/repo")
	}
	assetName := strings.TrimSpace(opts.AssetName)
	if assetName == "" {
		assetName = defaultUpdateAsset()
	}
	client := opts.Client
	if client == nil {
		client = http.DefaultClient
	}

	release, err := latestGitHubRelease(ctx, client, repo)
	if err != nil {
		return false, err
	}
	if sameVersion(opts.Version, release.TagName) {
		return false, nil
	}
	asset, ok := releaseAsset(release, assetName)
	if !ok {
		return false, fmt.Errorf("release %s does not contain asset %q", release.TagName, assetName)
	}
	if strings.TrimSpace(asset.BrowserDownloadURL) == "" {
		return false, fmt.Errorf("release asset %q has no download URL", assetName)
	}

	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	if err := installExecutable(ctx, client, asset.BrowserDownloadURL, exe); err != nil {
		return false, err
	}
	return true, nil
}

func latestGitHubRelease(ctx context.Context, client *http.Client, repo string) (githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(githubLatestReleaseURL, repo), nil)
	if err != nil {
		return githubRelease{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "rooter")
	if token := updateToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return githubRelease{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return githubRelease{}, fmt.Errorf("GET latest release returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return githubRelease{}, err
	}
	if strings.TrimSpace(release.TagName) == "" {
		return githubRelease{}, errors.New("latest release has no tag name")
	}
	return release, nil
}

func installExecutable(ctx context.Context, client *http.Client, downloadURL, exePath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "rooter")
	if token := updateToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("GET release asset returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	info, err := os.Stat(exePath)
	if err != nil {
		return err
	}
	mode := info.Mode().Perm()
	if mode&0o111 == 0 {
		mode = 0o755
	}

	tmp, err := os.CreateTemp(filepath.Dir(exePath), ".rooter-update-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, exePath); err != nil {
		return err
	}
	removeTmp = false
	return nil
}

func releaseAsset(release githubRelease, name string) (githubReleaseAsset, bool) {
	for _, asset := range release.Assets {
		if asset.Name == name {
			return asset, true
		}
	}
	return githubReleaseAsset{}, false
}

func sameVersion(current, latest string) bool {
	current = strings.TrimPrefix(strings.TrimSpace(current), "v")
	latest = strings.TrimPrefix(strings.TrimSpace(latest), "v")
	return current != "" && latest != "" && current == latest
}

func validGitHubRepository(repo string) bool {
	owner, name, ok := strings.Cut(repo, "/")
	return ok && strings.TrimSpace(owner) != "" && strings.TrimSpace(name) != "" && !strings.Contains(name, "/")
}

func updateToken() string {
	if token := strings.TrimSpace(os.Getenv("ROOTER_UPDATE_TOKEN")); token != "" {
		return token
	}
	return strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
}
