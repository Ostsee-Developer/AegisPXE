package debian13

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/artifact"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
)

const (
	debianBaseURL   = "https://deb.debian.org/debian"
	debianHost      = "deb.debian.org"
	debianSuite     = "trixie"
	debianArch      = "amd64"
	debianKeyring   = "/usr/share/keyrings/debian-archive-keyring.gpg"
	gpgvBinary      = "/usr/bin/gpgv"
	maxInRelease    = 2 << 20
	maxManifest     = 2 << 20
	maxInstallerBin = 128 << 20
)

var installerManifestPath = regexp.MustCompile(`^main/installer-amd64/([^/]+)/images/SHA256SUMS$`)

type fetcher interface {
	Fetch(context.Context, string, int64) ([]byte, error)
}

type signatureVerifier interface {
	Verify(context.Context, []byte) ([]byte, error)
}

type ArtifactResolver struct {
	fetcher  fetcher
	verifier signatureVerifier
	baseURL  string
	logger   *slog.Logger
}

type Resolution struct {
	ReleaseVersion   string
	InstallerVersion string
	Kernel           artifact.Verified
	Initrd           artifact.Verified
}

func NewArtifactResolver(logger *slog.Logger) *ArtifactResolver {
	if logger == nil {
		logger = slog.Default()
	}
	client := &http.Client{
		Timeout: 2 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many Debian mirror redirects")
			}
			if req.URL.Scheme != "https" || req.URL.Host != debianHost {
				return errors.New("Debian mirror redirect left trusted HTTPS origin")
			}
			return nil
		},
	}
	return &ArtifactResolver{
		fetcher:  httpFetcher{client: client},
		verifier: gpgvVerifier{},
		baseURL:  debianBaseURL,
		logger:   logger,
	}
}

func (r *ArtifactResolver) Resolve(ctx context.Context) (Resolution, error) {
	inReleaseURL := r.baseURL + "/dists/" + debianSuite + "/InRelease"
	inRelease, err := r.fetcher.Fetch(ctx, inReleaseURL, maxInRelease)
	if err != nil {
		return Resolution{}, fault.New(fault.ArtifactFetchFailed, "could not fetch Debian InRelease metadata", err)
	}
	release, err := r.verifier.Verify(ctx, inRelease)
	if err != nil {
		return Resolution{}, fault.New(fault.ArtifactTrustFailed, "Debian InRelease signature verification failed", err)
	}
	info, err := parseRelease(release)
	if err != nil {
		return Resolution{}, fault.New(fault.ArtifactTrustFailed, "verified Debian release metadata is invalid", err)
	}

	imagesBase := r.baseURL + "/dists/" + debianSuite + "/main/installer-amd64/" + url.PathEscape(info.InstallerVersion) + "/images"
	manifestURL := imagesBase + "/SHA256SUMS"
	manifest, err := r.fetcher.Fetch(ctx, manifestURL, maxManifest)
	if err != nil {
		return Resolution{}, fault.New(fault.ArtifactFetchFailed, "could not fetch Debian installer checksum manifest", err)
	}
	if int64(len(manifest)) != info.ManifestSize || artifact.SHA256(manifest) != info.ManifestDigest {
		return Resolution{}, fault.New(fault.ArtifactHashMismatch, "Debian installer checksum manifest failed integrity verification", nil)
	}
	checksums, err := parseSHA256SUMS(manifest)
	if err != nil {
		return Resolution{}, fault.New(fault.ArtifactTrustFailed, "verified Debian installer checksum manifest is invalid", err)
	}

	provenance := fmt.Sprintf("debian:%s:release=%s:installer=%s", debianSuite, info.ReleaseVersion, info.InstallerVersion)
	kernel, err := r.fetchArtifact(ctx, imagesBase, info.InstallerVersion, provenance, "debian13-amd64-netboot-linux", "linux", "netboot/debian-installer/amd64/linux", checksums)
	if err != nil {
		return Resolution{}, err
	}
	initrd, err := r.fetchArtifact(ctx, imagesBase, info.InstallerVersion, provenance, "debian13-amd64-netboot-initrd", "initrd.gz", "netboot/debian-installer/amd64/initrd.gz", checksums)
	if err != nil {
		return Resolution{}, err
	}

	result := Resolution{
		ReleaseVersion:   info.ReleaseVersion,
		InstallerVersion: info.InstallerVersion,
		Kernel:           kernel,
		Initrd:           initrd,
	}
	r.logger.InfoContext(ctx, "Debian installer artifacts verified",
		"component", "driver.debian13",
		"operation", "resolve_artifacts",
		"release_version", result.ReleaseVersion,
		"installer_version", result.InstallerVersion,
		"kernel_digest", result.Kernel.Descriptor.Digest,
		"initrd_digest", result.Initrd.Descriptor.Digest,
	)
	return result, nil
}

func (r *ArtifactResolver) fetchArtifact(ctx context.Context, imagesBase, version, provenance, id, name, path string, checksums map[string]string) (artifact.Verified, error) {
	digest, ok := checksums[path]
	if !ok {
		return artifact.Verified{}, fault.New(fault.ArtifactTrustFailed, "required Debian installer artifact is absent from verified manifest", nil)
	}
	sourceURL := imagesBase + "/" + path
	content, err := r.fetcher.Fetch(ctx, sourceURL, maxInstallerBin)
	if err != nil {
		return artifact.Verified{}, fault.New(fault.ArtifactFetchFailed, "could not fetch Debian installer artifact", err)
	}
	descriptor := artifact.Descriptor{
		ID:         id,
		Name:       name,
		SourceURL:  sourceURL,
		Version:    version,
		Digest:     digest,
		Size:       int64(len(content)),
		Provenance: provenance,
	}
	if err := artifact.VerifyContent(descriptor, content); err != nil {
		return artifact.Verified{}, fault.New(fault.ArtifactHashMismatch, "Debian installer artifact failed integrity verification", err)
	}
	return artifact.Verified{Descriptor: descriptor, Content: content}, nil
}

type releaseInfo struct {
	ReleaseVersion   string
	InstallerVersion string
	ManifestDigest   string
	ManifestSize     int64
}

func parseRelease(data []byte) (releaseInfo, error) {
	const currentPath = "main/installer-amd64/current/images/SHA256SUMS"
	var version, codename string
	var currentDigest string
	var currentSize int64
	type manifestCandidate struct {
		version string
		digest  string
		size    int64
	}
	var candidates []manifestCandidate

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	inSHA256 := false
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "Version: "):
			version = strings.TrimSpace(strings.TrimPrefix(line, "Version: "))
		case strings.HasPrefix(line, "Codename: "):
			codename = strings.TrimSpace(strings.TrimPrefix(line, "Codename: "))
		case line == "SHA256:":
			inSHA256 = true
		case inSHA256 && strings.HasPrefix(line, " "):
			fields := strings.Fields(line)
			if len(fields) != 3 || !artifact.ValidSHA256("sha256:"+fields[0]) {
				continue
			}
			size, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil || size <= 0 {
				continue
			}
			path := fields[2]
			if path == currentPath {
				currentDigest = "sha256:" + fields[0]
				currentSize = size
			}
			if matches := installerManifestPath.FindStringSubmatch(path); len(matches) == 2 && matches[1] != "current" {
				candidates = append(candidates, manifestCandidate{version: matches[1], digest: "sha256:" + fields[0], size: size})
			}
		case inSHA256 && line != "":
			inSHA256 = false
		}
	}
	if err := scanner.Err(); err != nil {
		return releaseInfo{}, err
	}
	if codename != debianSuite || !strings.HasPrefix(version, "13.") {
		return releaseInfo{}, errors.New("release is not Debian 13 trixie")
	}
	if currentDigest == "" || currentSize <= 0 {
		return releaseInfo{}, errors.New("current amd64 installer manifest is missing")
	}

	installerVersion := ""
	for _, candidate := range candidates {
		if candidate.digest == currentDigest && candidate.size == currentSize {
			if installerVersion != "" && installerVersion != candidate.version {
				return releaseInfo{}, errors.New("current installer resolves to multiple versioned manifests")
			}
			installerVersion = candidate.version
		}
	}
	if installerVersion == "" {
		return releaseInfo{}, errors.New("current installer has no matching versioned manifest")
	}
	return releaseInfo{
		ReleaseVersion:   version,
		InstallerVersion: installerVersion,
		ManifestDigest:   currentDigest,
		ManifestSize:     currentSize,
	}, nil
}

func parseSHA256SUMS(data []byte) (map[string]string, error) {
	checksums := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 || !artifact.ValidSHA256("sha256:"+fields[0]) {
			return nil, errors.New("invalid SHA256SUMS entry")
		}
		path := strings.TrimPrefix(fields[1], "./")
		if path == "" || strings.Contains(path, "..") {
			return nil, errors.New("invalid SHA256SUMS path")
		}
		if _, exists := checksums[path]; exists {
			return nil, errors.New("duplicate SHA256SUMS path")
		}
		checksums[path] = "sha256:" + fields[0]
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return checksums, nil
}

type httpFetcher struct {
	client *http.Client
}

func (f httpFetcher) Fetch(ctx context.Context, rawURL string, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}
	content, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > maxBytes {
		return nil, errors.New("response exceeds size limit")
	}
	return content, nil
}

type gpgvVerifier struct{}

func (gpgvVerifier) Verify(ctx context.Context, signed []byte) ([]byte, error) {
	dir, err := os.MkdirTemp("", "aegispxe-inrelease-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	signedPath := filepath.Join(dir, "InRelease")
	outputPath := filepath.Join(dir, "Release")
	if err := os.WriteFile(signedPath, signed, 0600); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, gpgvBinary, "--keyring", debianKeyring, "--output", outputPath, signedPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if len(message) > 1024 {
			message = message[:1024]
		}
		return nil, fmt.Errorf("gpgv failed: %w: %s", err, message)
	}
	verified, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, err
	}
	if len(verified) == 0 {
		return nil, errors.New("gpgv produced empty verified release metadata")
	}
	return verified, nil
}
