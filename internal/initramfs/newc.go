package initramfs

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"strings"
)

const (
	ModeDirectory = 0o040000
	ModeRegular   = 0o100000
)

type Entry struct {
	Path string
	Mode uint32
	Data []byte
}

func BuildNewc(entries []Entry) ([]byte, error) {
	var out bytes.Buffer
	inode := uint32(1)
	for _, entry := range entries {
		if err := validateEntry(entry); err != nil {
			return nil, err
		}
		if err := writeEntry(&out, inode, entry.Path, entry.Mode, entry.Data); err != nil {
			return nil, err
		}
		inode++
	}
	if err := writeEntry(&out, inode, "TRAILER!!!", 0, nil); err != nil {
		return nil, err
	}
	pad(&out, 512)
	return append([]byte(nil), out.Bytes()...), nil
}

func validateEntry(entry Entry) error {
	name := strings.TrimSpace(entry.Path)
	if name == "" || strings.HasPrefix(name, "/") || strings.ContainsRune(name, '\x00') || len(name) > 4095 {
		return errors.New("initramfs entry path is invalid")
	}
	if cleaned := path.Clean(name); cleaned != name || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return errors.New("initramfs entry path escapes archive root")
	}
	kind := entry.Mode & 0o170000
	if kind != ModeDirectory && kind != ModeRegular {
		return errors.New("initramfs entry mode must be a directory or regular file")
	}
	if kind == ModeDirectory && len(entry.Data) != 0 {
		return errors.New("initramfs directory entry must not contain data")
	}
	return nil
}

func writeEntry(out *bytes.Buffer, inode uint32, name string, mode uint32, data []byte) error {
	nameSize := len(name) + 1
	nlink := uint32(1)
	if mode&0o170000 == ModeDirectory {
		nlink = 2
	}
	header := fmt.Sprintf(
		"070701%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x",
		inode,
		mode,
		uint32(0),
		uint32(0),
		nlink,
		uint32(0),
		uint32(len(data)),
		uint32(0),
		uint32(0),
		uint32(0),
		uint32(0),
		uint32(nameSize),
		uint32(0),
	)
	if len(header) != 110 {
		return errors.New("initramfs newc header length is invalid")
	}
	out.WriteString(header)
	out.WriteString(name)
	out.WriteByte(0)
	pad(out, 4)
	out.Write(data)
	pad(out, 4)
	return nil
}

func pad(out *bytes.Buffer, alignment int) {
	for out.Len()%alignment != 0 {
		out.WriteByte(0)
	}
}
