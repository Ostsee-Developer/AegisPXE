package artifact

import (
	"errors"
	"testing"
)

func TestDescriptorRejectsNonHTTPSSource(t *testing.T) {
	d := Descriptor{
		ID:         "debian13-linux",
		Name:       "linux",
		SourceURL:  "http://example.invalid/linux",
		Version:    "installer-1",
		Digest:     SHA256([]byte("kernel")),
		Size:       int64(len("kernel")),
		Provenance: "debian:test",
	}
	if err := d.Validate(); err == nil {
		t.Fatal("expected non-HTTPS artifact source to be rejected")
	}
}

func TestVerifyContentRejectsHashMismatch(t *testing.T) {
	expected := []byte("kernel")
	d := Descriptor{
		ID:         "debian13-linux",
		Name:       "linux",
		SourceURL:  "https://deb.debian.org/debian/linux",
		Version:    "installer-1",
		Digest:     SHA256(expected),
		Size:       int64(len(expected)),
		Provenance: "debian:test",
	}
	if err := VerifyContent(d, []byte("tamper")); !errors.Is(err, ErrIntegrityMismatch) {
		t.Fatalf("integrity error=%v", err)
	}
}
