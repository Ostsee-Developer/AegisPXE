package artifact

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreRoundTripIsContentAddressed(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("verified-kernel")
	d := Descriptor{
		ID:         "debian13-linux",
		Name:       "linux",
		SourceURL:  "https://deb.debian.org/debian/linux",
		Version:    "installer-1",
		Digest:     SHA256(content),
		Size:       int64(len(content)),
		Provenance: "debian:test",
	}
	path, err := store.Put(Verified{Descriptor: d, Content: content})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Read(d)
	if err != nil {
		t.Fatal(err)
	}
	if string(loaded) != string(content) {
		t.Fatalf("content mismatch: %q", loaded)
	}
	if filepath.Base(path) != "sha256-"+d.Digest[len("sha256:"):] {
		t.Fatalf("artifact path is not content-addressed: %s", path)
	}
}

func TestStoreRejectsCorruptedExistingContent(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("verified-kernel")
	d := Descriptor{
		ID:         "debian13-linux",
		Name:       "linux",
		SourceURL:  "https://deb.debian.org/debian/linux",
		Version:    "installer-1",
		Digest:     SHA256(content),
		Size:       int64(len(content)),
		Provenance: "debian:test",
	}
	path, err := store.path(d.Digest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("tampered"), 0640); err != nil {
		t.Fatal(err)
	}
	_, err = store.Put(Verified{Descriptor: d, Content: content})
	if !errors.Is(err, ErrIntegrityMismatch) {
		t.Fatalf("expected integrity mismatch, got %v", err)
	}
}
