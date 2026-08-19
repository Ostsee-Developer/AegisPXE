package initramfs

import (
	"encoding/hex"
	"strconv"
	"testing"
)

func TestBuildNewcPreservesDirectoryAndExecutableModes(t *testing.T) {
	archive, err := BuildNewc([]Entry{
		{Path: "aegispxe", Mode: ModeDirectory | 0o755},
		{Path: "aegispxe/reporter", Mode: ModeRegular | 0o755, Data: []byte("reporter")},
		{Path: "aegispxe/reporter.json", Mode: ModeRegular | 0o600, Data: []byte("{}")},
		{Path: "preseed.cfg", Mode: ModeRegular | 0o600, Data: []byte("#_preseed_V1\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(archive)%512 != 0 {
		t.Fatalf("archive size %d is not 512-byte padded", len(archive))
	}
	entries := parseNewcForTest(t, archive)
	if got := entries["aegispxe"]; got.mode != ModeDirectory|0o755 {
		t.Fatalf("aegispxe mode=%#o", got.mode)
	}
	if got := entries["aegispxe/reporter"]; got.mode != ModeRegular|0o755 || string(got.data) != "reporter" {
		t.Fatalf("reporter entry=%+v", got)
	}
	if got := entries["aegispxe/reporter.json"]; got.mode != ModeRegular|0o600 || string(got.data) != "{}" {
		t.Fatalf("reporter config entry=%+v", got)
	}
	if got := entries["preseed.cfg"]; got.mode != ModeRegular|0o600 {
		t.Fatalf("preseed mode=%#o", got.mode)
	}
}

type parsedEntry struct {
	mode uint32
	data []byte
}

func parseNewcForTest(t *testing.T, archive []byte) map[string]parsedEntry {
	t.Helper()
	entries := map[string]parsedEntry{}
	offset := 0
	for {
		if offset+110 > len(archive) {
			t.Fatal("truncated newc header")
		}
		header := archive[offset : offset+110]
		if string(header[:6]) != "070701" {
			t.Fatalf("invalid newc magic at offset %d: %s", offset, hex.EncodeToString(header[:6]))
		}
		mode := parseHex32(t, header[14:22])
		fileSize := int(parseHex32(t, header[54:62]))
		nameSize := int(parseHex32(t, header[94:102]))
		offset += 110
		if nameSize <= 0 || offset+nameSize > len(archive) {
			t.Fatal("invalid newc name size")
		}
		nameBytes := archive[offset : offset+nameSize]
		name := string(nameBytes[:len(nameBytes)-1])
		offset += nameSize
		offset = align4(offset)
		if offset+fileSize > len(archive) {
			t.Fatal("invalid newc file size")
		}
		data := append([]byte(nil), archive[offset:offset+fileSize]...)
		offset += fileSize
		offset = align4(offset)
		if name == "TRAILER!!!" {
			return entries
		}
		entries[name] = parsedEntry{mode: mode, data: data}
	}
}

func parseHex32(t *testing.T, value []byte) uint32 {
	t.Helper()
	parsed, err := strconv.ParseUint(string(value), 16, 32)
	if err != nil {
		t.Fatal(err)
	}
	return uint32(parsed)
}

func align4(value int) int { return (value + 3) &^ 3 }
