package secureboot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateAssets(t *testing.T) {
	dir := t.TempDir()
	files := map[string][]byte{
		"ipxe-shim.efi": []byte("signed-shim-fixture"),
		"ipxe.efi":      []byte("signed-ipxe-fixture"),
	}
	manifest := Manifest{
		UpstreamRelease:    ExpectedIPXERelease,
		UpstreamCommit:     ExpectedIPXECommit,
		ReleaseAssetSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Files:              make(map[string]ManifestFile),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(content)
		manifest.Files[name] = ManifestFile{SHA256: "sha256:" + hex.EncodeToString(digest[:]), Size: int64(len(content))}
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifestName), encoded, 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := ValidateAssets(dir)
	if err != nil {
		t.Fatal(err)
	}
	if report.UpstreamRelease != ExpectedIPXERelease || len(report.Files) != 2 {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestValidateAssetsRejectsTamperAndSymlink(t *testing.T) {
	t.Run("tamper", func(t *testing.T) {
		dir := t.TempDir()
		writeAssetFixture(t, dir)
		if err := os.WriteFile(filepath.Join(dir, "ipxe.efi"), []byte("tampered"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := ValidateAssets(dir); err == nil {
			t.Fatal("tampered asset was accepted")
		}
	})

	t.Run("symlink", func(t *testing.T) {
		dir := t.TempDir()
		writeAssetFixture(t, dir)
		target := filepath.Join(dir, "target")
		if err := os.WriteFile(target, []byte("replacement"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(dir, "ipxe.efi")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(dir, "ipxe.efi")); err != nil {
			t.Fatal(err)
		}
		if _, err := ValidateAssets(dir); err == nil {
			t.Fatal("symlink asset was accepted")
		}
	})
}

func writeAssetFixture(t *testing.T, dir string) {
	t.Helper()
	files := map[string][]byte{
		"ipxe-shim.efi": []byte("signed-shim-fixture"),
		"ipxe.efi":      []byte("signed-ipxe-fixture"),
	}
	manifest := Manifest{
		UpstreamRelease:    ExpectedIPXERelease,
		UpstreamCommit:     ExpectedIPXECommit,
		ReleaseAssetSHA256: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Files:              make(map[string]ManifestFile),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(content)
		manifest.Files[name] = ManifestFile{SHA256: "sha256:" + hex.EncodeToString(digest[:]), Size: int64(len(content))}
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifestName), encoded, 0o644); err != nil {
		t.Fatal(err)
	}
}
