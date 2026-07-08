package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestParseSHA256AcceptsSha256sumFormat(t *testing.T) {
	sum := strings.Repeat("a", sha256.Size*2)
	got, err := parseSHA256(sum + "  rooter-linux-amd64\n")
	if err != nil {
		t.Fatal(err)
	}
	if got != sum {
		t.Fatalf("parseSHA256() = %q want %q", got, sum)
	}
}

func TestParseSHA256RejectsInvalidDigest(t *testing.T) {
	invalid := []string{"", "not-a-digest", strings.Repeat("a", sha256.Size*2-1), strings.Repeat("z", sha256.Size*2)}
	for _, value := range invalid {
		if _, err := parseSHA256(value); err == nil {
			t.Fatalf("parseSHA256(%q) succeeded", value)
		}
	}
}

func TestVerifyUpdateSignature(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sum := strings.Repeat("a", sha256.Size*2)
	signature := ed25519.Sign(privateKey, []byte(sum))
	t.Setenv("ROOTER_UPDATE_PUBLIC_KEY", base64.StdEncoding.EncodeToString(publicKey))

	if err := verifyUpdateSignature(sum, base64.StdEncoding.EncodeToString(signature)); err != nil {
		t.Fatal(err)
	}
	if err := verifyUpdateSignature(sum, base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, []byte("wrong")))); err == nil {
		t.Fatal("verifyUpdateSignature accepted the wrong signature")
	}
}

func TestVerifyUpdateSignatureRejectsMissingPublicKey(t *testing.T) {
	oldPublicKey := updatePublicKey
	updatePublicKey = ""
	t.Cleanup(func() { updatePublicKey = oldPublicKey })
	t.Setenv("ROOTER_UPDATE_PUBLIC_KEY", "")

	err := verifyUpdateSignature(strings.Repeat("a", sha256.Size*2), base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize)))
	if err == nil {
		t.Fatal("verifyUpdateSignature accepted missing public key")
	}
}

func TestInstallExecutableRejectsChecksumMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("new binary"))
	}))
	defer server.Close()

	dir := t.TempDir()
	exePath := filepath.Join(dir, "rooter")
	if err := os.WriteFile(exePath, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("different binary"))
	expected := hex.EncodeToString(sum[:])

	err := installExecutable(context.Background(), server.Client(), server.URL, exePath, expected)
	if err == nil {
		t.Fatal("installExecutable succeeded with checksum mismatch")
	}
	data, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old binary" {
		t.Fatalf("executable was replaced after checksum mismatch: %q", string(data))
	}
}
