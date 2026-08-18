package driver

import "testing"

func TestSeedBundleCloneOwnsContent(t *testing.T) {
	seed := SeedBundle{Filename: "preseed.cfg", MediaType: "text/plain", Content: []byte("d-i test/value string ok\n")}
	if err := seed.Validate(); err != nil {
		t.Fatal(err)
	}
	clone := seed.Clone()
	clone.Content[0] = 'X'
	if seed.Content[0] == clone.Content[0] {
		t.Fatal("seed clone shares mutable content")
	}
}
