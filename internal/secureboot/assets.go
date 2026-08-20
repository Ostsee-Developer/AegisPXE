package secureboot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	ExpectedIPXERelease = "v2.0.0"
	ExpectedIPXECommit  = "12798ec29aa8a64d8675c4378b99f5fe28447afb"
	manifestName        = "manifest.json"
	maxManifestBytes    = 64 << 10
	maxEFIBytes         = 32 << 20
)

var requiredFiles = []string{"ipxe-shim.efi", "ipxe.efi"}

type ManifestFile struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type Manifest struct {
	UpstreamRelease    string                  `json:"upstream_release"`
	UpstreamCommit     string                  `json:"upstream_commit"`
	ReleaseAssetSHA256 string                  `json:"release_asset_sha256"`
	Files              map[string]ManifestFile `json:"files"`
}

type AssetReport struct {
	Directory          string
	UpstreamRelease    string
	UpstreamCommit     string
	ReleaseAssetSHA256 string
	Files              map[string]ManifestFile
}

func ValidateAssets(directory string) (AssetReport, error) {
	directory = filepath.Clean(strings.TrimSpace(directory))
	if !filepath.IsAbs(directory) {
		return AssetReport{}, errors.New("Secure Boot asset directory must be absolute")
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return AssetReport{}, fmt.Errorf("inspect Secure Boot asset directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return AssetReport{}, errors.New("Secure Boot asset directory must be a real directory")
	}

	manifestPath := filepath.Join(directory, manifestName)
	manifestBytes, err := readRegularBounded(manifestPath, maxManifestBytes)
	if err != nil {
		return AssetReport{}, fmt.Errorf("read Secure Boot manifest: %w", err)
	}
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(manifestBytes)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return AssetReport{}, fmt.Errorf("decode Secure Boot manifest: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return AssetReport{}, errors.New("Secure Boot manifest must contain one JSON object")
	}
	if manifest.UpstreamRelease != ExpectedIPXERelease || manifest.UpstreamCommit != ExpectedIPXECommit {
		return AssetReport{}, errors.New("Secure Boot manifest references an unpinned iPXE release")
	}
	if !validSHA256(manifest.ReleaseAssetSHA256) {
		return AssetReport{}, errors.New("Secure Boot release asset digest is invalid")
	}
	if len(manifest.Files) != len(requiredFiles) {
		return AssetReport{}, errors.New("Secure Boot manifest contains an unexpected file set")
	}

	verified := make(map[string]ManifestFile, len(requiredFiles))
	for _, name := range requiredFiles {
		expected, ok := manifest.Files[name]
		if !ok || expected.Size <= 0 || expected.Size > maxEFIBytes || !validSHA256(expected.SHA256) {
			return AssetReport{}, fmt.Errorf("Secure Boot manifest entry %q is invalid", name)
		}
		path := filepath.Join(directory, name)
		content, err := readRegularBounded(path, maxEFIBytes)
		if err != nil {
			return AssetReport{}, fmt.Errorf("read Secure Boot asset %q: %w", name, err)
		}
		if int64(len(content)) != expected.Size {
			return AssetReport{}, fmt.Errorf("Secure Boot asset %q size mismatch", name)
		}
		digest := "sha256:" + hex.EncodeToString(sum256(content))
		if digest != expected.SHA256 {
			return AssetReport{}, fmt.Errorf("Secure Boot asset %q digest mismatch", name)
		}
		verified[name] = expected
	}

	return AssetReport{
		Directory:          directory,
		UpstreamRelease:    manifest.UpstreamRelease,
		UpstreamCommit:     manifest.UpstreamCommit,
		ReleaseAssetSHA256: manifest.ReleaseAssetSHA256,
		Files:              verified,
	}, nil
}

func readRegularBounded(path string, max int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("path is not a regular non-symlink file")
	}
	if info.Size() <= 0 || info.Size() > max {
		return nil, errors.New("file size is outside accepted bounds")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > max {
		return nil, errors.New("file exceeds accepted size")
	}
	return content, nil
}

func validSHA256(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}

func sum256(content []byte) []byte {
	digest := sha256.Sum256(content)
	return digest[:]
}
