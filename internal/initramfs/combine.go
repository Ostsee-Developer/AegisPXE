package initramfs

import "errors"

// Combine appends a second initramfs member after padding the first member to
// the four-byte boundary required by the Linux initramfs buffer format. The
// kernel explicitly supports a sequence of compressed and uncompressed CPIO
// archives in one initramfs buffer, which lets AegisPXE hand EFI one initrd
// instead of relying on multi-initrd virtual filesystem behaviour.
func Combine(first, second []byte) ([]byte, error) {
	if len(first) == 0 || len(second) == 0 {
		return nil, errors.New("initramfs members must not be empty")
	}
	padding := (4 - (len(first) % 4)) % 4
	out := make([]byte, 0, len(first)+padding+len(second))
	out = append(out, first...)
	for range padding {
		out = append(out, 0)
	}
	out = append(out, second...)
	return out, nil
}
