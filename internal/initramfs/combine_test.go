package initramfs

import "testing"

func TestCombinePadsFirstMemberBeforeOverlay(t *testing.T) {
	first := []byte{0x1f, 0x8b, 0x08, 0x00, 0xaa}
	second := []byte("070701overlay")
	combined, err := Combine(first, second)
	if err != nil {
		t.Fatal(err)
	}
	if len(combined) != 8+len(second) {
		t.Fatalf("combined length=%d want=%d", len(combined), 8+len(second))
	}
	for index := len(first); index < 8; index++ {
		if combined[index] != 0 {
			t.Fatalf("padding byte %d=%x want=0", index, combined[index])
		}
	}
	if got := string(combined[8:]); got != string(second) {
		t.Fatalf("second member=%q want=%q", got, second)
	}
}

func TestCombineRejectsEmptyMembers(t *testing.T) {
	if _, err := Combine(nil, []byte("x")); err == nil {
		t.Fatal("empty first member was accepted")
	}
	if _, err := Combine([]byte("x"), nil); err == nil {
		t.Fatal("empty second member was accepted")
	}
}
