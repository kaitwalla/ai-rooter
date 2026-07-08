package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const githubLatestReleaseURL = "https://api.github.com/repos/%s/releases/latest"
const maxUpdateAssetBytes = 200 << 20

var updatePublicKey = ""

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
	checksumAsset, ok := releaseAsset(release, assetName+".sha256")
	if !ok {
		return false, fmt.Errorf("release %s does not contain checksum asset %q", release.TagName, assetName+".sha256")
	}
	expectedSHA256, err := downloadSHA256(ctx, client, checksumAsset.BrowserDownloadURL)
	if err != nil {
		return false, err
	}
	signatureAsset, ok := releaseAsset(release, assetName+".sha256.sig")
	if !ok {
		return false, fmt.Errorf("release %s does not contain signature asset %q", release.TagName, assetName+".sha256.sig")
	}
	signature, err := downloadSmallText(ctx, client, signatureAsset.BrowserDownloadURL, 16<<10)
	if err != nil {
		return false, err
	}
	if err := verifyUpdateSignature(expectedSHA256, signature); err != nil {
		return false, err
	}

	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	if err := installExecutable(ctx, client, asset.BrowserDownloadURL, exe, expectedSHA256); err != nil {
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

func downloadSHA256(ctx context.Context, client *http.Client, checksumURL string) (string, error) {
	data, err := downloadSmallText(ctx, client, checksumURL, 16<<10)
	if err != nil {
		return "", err
	}
	return parseSHA256(data)
}

func downloadSmallText(ctx context.Context, client *http.Client, downloadURL string, maxBytes int64) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "rooter")
	if token := updateToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return "", fmt.Errorf("GET checksum asset returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > maxBytes {
		return "", errors.New("checksum asset is too large")
	}
	return string(data), nil
}

func parseSHA256(value string) (string, error) {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return "", errors.New("checksum asset is empty")
	}
	sum := strings.ToLower(fields[0])
	if len(sum) != sha256.Size*2 {
		return "", errors.New("checksum must be a SHA-256 hex digest")
	}
	if _, err := hex.DecodeString(sum); err != nil {
		return "", errors.New("checksum must be a SHA-256 hex digest")
	}
	return sum, nil
}

func verifyUpdateSignature(expectedSHA256, signatureText string) error {
	publicKey, err := parseUpdatePublicKey(effectiveUpdatePublicKey())
	if err != nil {
		return err
	}
	signature, err := parseBase64OrHex(strings.TrimSpace(signatureText), ed25519.SignatureSize, "signature")
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, []byte(expectedSHA256), signature) {
		return errors.New("update signature verification failed")
	}
	return nil
}

func effectiveUpdatePublicKey() string {
	if key := strings.TrimSpace(os.Getenv("ROOTER_UPDATE_PUBLIC_KEY")); key != "" {
		return key
	}
	return strings.TrimSpace(updatePublicKey)
}

func parseUpdatePublicKey(value string) (ed25519.PublicKey, error) {
	key, err := parseBase64OrHex(strings.TrimSpace(value), ed25519.PublicKeySize, "update public key")
	if err != nil {
		if strings.TrimSpace(value) == "" {
			return nil, errors.New("update public key is not configured; refusing unsigned update")
		}
		return nil, err
	}
	return ed25519.PublicKey(key), nil
}

func parseBase64OrHex(value string, wantLen int, label string) ([]byte, error) {
	if value == "" {
		return nil, fmt.Errorf("%s is empty", label)
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(value)
	}
	if err != nil {
		decoded, err = hex.DecodeString(value)
	}
	if err != nil || len(decoded) != wantLen {
		return nil, fmt.Errorf("%s must be base64 or hex encoded %d-byte data", label, wantLen)
	}
	return decoded, nil
}

func installExecutable(ctx context.Context, client *http.Client, downloadURL, exePath, expectedSHA256 string) error {
	expectedSHA256, err := parseSHA256(expectedSHA256)
	if err != nil {
		return err
	}
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

	hasher := sha256.New()
	written, err := copyAndHash(tmp, resp.Body, hasher, maxUpdateAssetBytes)
	if err != nil {
		_ = tmp.Close()
		return err
	}
	if written == 0 {
		_ = tmp.Close()
		return errors.New("downloaded update asset is empty")
	}
	actualSHA256 := hex.EncodeToString(hasher.Sum(nil))
	if actualSHA256 != expectedSHA256 {
		_ = tmp.Close()
		return fmt.Errorf("downloaded update checksum mismatch: got %s want %s", actualSHA256, expectedSHA256)
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

func copyAndHash(dst io.Writer, src io.Reader, hasher hash.Hash, maxBytes int64) (int64, error) {
	written, err := io.Copy(io.MultiWriter(dst, hasher), io.LimitReader(src, maxBytes+1))
	if err != nil {
		return written, err
	}
	if written > maxBytes {
		return written, errors.New("downloaded update asset is too large")
	}
	return written, nil
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
