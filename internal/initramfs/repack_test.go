package initramfs

import (
	"bytes"
	"compress/gzip"
	"io"
	"testing"
)

func TestRepackGzipEmitsSingleGzipContainingOverlay(t *testing.T) {
	basePayload := []byte("070701base-cpio")
	var base bytes.Buffer
	writer := gzip.NewWriter(&base)
	if _, err := writer.Write(basePayload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	overlay := []byte("070701overlay-cpio")

	repacked, err := RepackGzip(base.Bytes(), overlay)
	if err != nil {
		t.Fatal(err)
	}
	if len(repacked) < 3 || repacked[0] != 0x1f || repacked[1] != 0x8b || repacked[2] != 0x08 {
		t.Fatalf("repacked initramfs is not gzip: %x", repacked[:3])
	}
	reader, err := gzip.NewReader(bytes.NewReader(repacked))
	if err != nil {
		t.Fatal(err)
	}
	expanded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(expanded, basePayload) {
		t.Fatalf("expanded repack lost base payload: %q", expanded)
	}
	if !bytes.HasSuffix(expanded, overlay) {
		t.Fatalf("expanded repack lost overlay payload: %q", expanded)
	}
}

func TestRepackGzipRejectsInvalidBase(t *testing.T) {
	if _, err := RepackGzip([]byte("not-gzip"), []byte("070701overlay")); err == nil {
		t.Fatal("invalid gzip base was accepted")
	}
}
