package initramfs

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
)

const maxExpandedInitramfsBytes = 512 << 20

// RepackGzip expands a verified gzip-compressed initramfs, appends the AegisPXE
// newc overlay to the native uncompressed CPIO stream, then emits one ordinary
// gzip initramfs again. This intentionally keeps the firmware/iPXE handoff to
// the same single compressed initrd shape used by the known-good Debian path.
func RepackGzip(baseGzip, overlay []byte) ([]byte, error) {
	if len(baseGzip) == 0 || len(overlay) == 0 {
		return nil, errors.New("initramfs members must not be empty")
	}

	reader, err := gzip.NewReader(bytes.NewReader(baseGzip))
	if err != nil {
		return nil, errors.New("base initramfs is not a valid gzip stream")
	}
	expanded, readErr := io.ReadAll(io.LimitReader(reader, maxExpandedInitramfsBytes+1))
	closeErr := reader.Close()
	if readErr != nil {
		return nil, errors.New("could not expand base initramfs")
	}
	if closeErr != nil {
		return nil, errors.New("could not finish reading base initramfs")
	}
	if len(expanded) == 0 || len(expanded) > maxExpandedInitramfsBytes {
		return nil, errors.New("expanded base initramfs exceeds safe bounds")
	}

	merged, err := Combine(expanded, overlay)
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	writer := gzip.NewWriter(&out)
	if _, err := writer.Write(merged); err != nil {
		_ = writer.Close()
		return nil, errors.New("could not compress combined initramfs")
	}
	if err := writer.Close(); err != nil {
		return nil, errors.New("could not finalize combined initramfs")
	}
	return append([]byte(nil), out.Bytes()...), nil
}
