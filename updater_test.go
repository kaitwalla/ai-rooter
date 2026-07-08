package main

import (
	"runtime"
	"testing"
)

func TestDefaultUpdateAssetMatchesPlatform(t *testing.T) {
	want := "rooter-" + runtime.GOOS + "-" + runtime.GOARCH
	if got := defaultUpdateAsset(); got != want {
		t.Fatalf("defaultUpdateAsset() = %q want %q", got, want)
	}
}

func TestReleaseAssetSelectsByName(t *testing.T) {
	release := githubRelease{
		Assets: []githubReleaseAsset{
			{Name: "rooter-linux-arm64", BrowserDownloadURL: "https://example.test/arm64"},
			{Name: "rooter-linux-amd64", BrowserDownloadURL: "https://example.test/amd64"},
		},
	}
	asset, ok := releaseAsset(release, "rooter-linux-amd64")
	if !ok {
		t.Fatal("releaseAsset() did not find asset")
	}
	if asset.BrowserDownloadURL != "https://example.test/amd64" {
		t.Fatalf("download URL = %q", asset.BrowserDownloadURL)
	}
}

func TestSameVersionIgnoresLeadingV(t *testing.T) {
	if !sameVersion("1.2.3", "v1.2.3") {
		t.Fatal("sameVersion() should ignore a leading v")
	}
	if sameVersion("1.2.3", "v1.2.4") {
		t.Fatal("sameVersion() matched different versions")
	}
}

func TestValidGitHubRepository(t *testing.T) {
	valid := []string{"owner/repo", "openai/rooter"}
	for _, repo := range valid {
		if !validGitHubRepository(repo) {
			t.Fatalf("validGitHubRepository(%q) = false", repo)
		}
	}

	invalid := []string{"", "owner", "owner/", "/repo", "owner/repo/extra"}
	for _, repo := range invalid {
		if validGitHubRepository(repo) {
			t.Fatalf("validGitHubRepository(%q) = true", repo)
		}
	}
}
