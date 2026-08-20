package idgen

import (
	"regexp"
	"testing"
)

func TestNewUUIDReturnsCanonicalV4UUID(t *testing.T) {
	pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	first, err := NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	if !pattern.MatchString(first) || !pattern.MatchString(second) {
		t.Fatalf("unexpected UUIDs: %q %q", first, second)
	}
	if first == second {
		t.Fatal("two generated UUIDs unexpectedly matched")
	}
}
