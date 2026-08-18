package debian13

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/Ostsee-Developer/AegisPXE/internal/artifact"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
)

func TestResolvePinsVersionedVerifiedArtifacts(t *testing.T) {
	kernel := []byte("verified-kernel")
	initrd := []byte("verified-initrd")
	manifest := []byte(fmt.Sprintf("%s  ./netboot/debian-installer/amd64/linux\n%s  ./netboot/debian-installer/amd64/initrd.gz\n",
		strings.TrimPrefix(artifact.SHA256(kernel), "sha256:"),
		strings.TrimPrefix(artifact.SHA256(initrd), "sha256:"),
	))
	manifestDigest := strings.TrimPrefix(artifact.SHA256(manifest), "sha256:")
	release := []byte(fmt.Sprintf("Version: 13.6\nCodename: trixie\nSHA256:\n %s %d main/installer-amd64/current/images/SHA256SUMS\n %s %d main/installer-amd64/20250803+deb13u6/images/SHA256SUMS\n",
		manifestDigest, len(manifest), manifestDigest, len(manifest),
	))
	base := debianBaseURL + "/dists/trixie/main/installer-amd64/20250803+deb13u6/images"
	resolver := &ArtifactResolver{
		baseURL: debianBaseURL,
		fetcher: mapFetcher{content: map[string][]byte{
			debianBaseURL + "/dists/trixie/InRelease":                    []byte("signed"),
			base + "/SHA256SUMS":                                          manifest,
			base + "/netboot/debian-installer/amd64/linux":                kernel,
			base + "/netboot/debian-installer/amd64/initrd.gz":            initrd,
		}},
		verifier: staticVerifier{content: release},
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	resolved, err := resolver.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ReleaseVersion != "13.6" || resolved.InstallerVersion != "20250803+deb13u6" {
		t.Fatalf("unexpected release pin: %+v", resolved)
	}
	if resolved.Kernel.Descriptor.Digest != artifact.SHA256(kernel) || resolved.Initrd.Descriptor.Digest != artifact.SHA256(initrd) {
		t.Fatal("resolved artifact digests do not match verified content")
	}
	if strings.Contains(resolved.Kernel.Descriptor.SourceURL, "/current/") || !strings.Contains(resolved.Kernel.Descriptor.SourceURL, "/20250803+deb13u6/") {
		t.Fatalf("kernel source is not version-pinned: %s", resolved.Kernel.Descriptor.SourceURL)
	}
}

func TestResolveRejectsArtifactHashMismatch(t *testing.T) {
	kernel := []byte("expected-kernel")
	manifest := []byte(fmt.Sprintf("%s  ./netboot/debian-installer/amd64/linux\n%s  ./netboot/debian-installer/amd64/initrd.gz\n",
		strings.TrimPrefix(artifact.SHA256(kernel), "sha256:"),
		strings.TrimPrefix(artifact.SHA256([]byte("initrd")), "sha256:"),
	))
	manifestDigest := strings.TrimPrefix(artifact.SHA256(manifest), "sha256:")
	release := []byte(fmt.Sprintf("Version: 13.6\nCodename: trixie\nSHA256:\n %s %d main/installer-amd64/current/images/SHA256SUMS\n %s %d main/installer-amd64/installer-1/images/SHA256SUMS\n",
		manifestDigest, len(manifest), manifestDigest, len(manifest),
	))
	base := debianBaseURL + "/dists/trixie/main/installer-amd64/installer-1/images"
	resolver := &ArtifactResolver{
		baseURL: debianBaseURL,
		fetcher: mapFetcher{content: map[string][]byte{
			debianBaseURL + "/dists/trixie/InRelease":         []byte("signed"),
			base + "/SHA256SUMS":                               manifest,
			base + "/netboot/debian-installer/amd64/linux":     []byte("tampered-kernel"),
		}},
		verifier: staticVerifier{content: release},
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	_, err := resolver.Resolve(context.Background())
	if fault.Code(err) != fault.ArtifactHashMismatch {
		t.Fatalf("hash mismatch code=%q err=%v", fault.Code(err), err)
	}
}

func TestResolveRejectsUntrustedRelease(t *testing.T) {
	resolver := &ArtifactResolver{
		baseURL:  debianBaseURL,
		fetcher:  mapFetcher{content: map[string][]byte{debianBaseURL + "/dists/trixie/InRelease": []byte("signed")}},
		verifier: staticVerifier{err: errors.New("bad signature")},
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	_, err := resolver.Resolve(context.Background())
	if fault.Code(err) != fault.ArtifactTrustFailed {
		t.Fatalf("trust failure code=%q err=%v", fault.Code(err), err)
	}
}

type mapFetcher struct {
	content map[string][]byte
}

func (f mapFetcher) Fetch(_ context.Context, rawURL string, _ int64) ([]byte, error) {
	content, ok := f.content[rawURL]
	if !ok {
		return nil, fmt.Errorf("unexpected fetch %s", rawURL)
	}
	return append([]byte(nil), content...), nil
}

type staticVerifier struct {
	content []byte
	err     error
}

func (v staticVerifier) Verify(_ context.Context, _ []byte) ([]byte, error) {
	if v.err != nil {
		return nil, v.err
	}
	return append([]byte(nil), v.content...), nil
}
