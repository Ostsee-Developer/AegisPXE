package profile

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestSnapshotRejectsInvalidSSHKeyPayload(t *testing.T) {
	snapshot := validSnapshot()
	snapshot.Admin.AuthorizedSSHKeys[0] = "ssh-ed25519 not-base64"
	if err := snapshot.Validate(); err == nil {
		t.Fatal("expected invalid SSH public key payload")
	}
}

func TestSnapshotCloneOwnsSlices(t *testing.T) {
	snapshot := validSnapshot()
	clone := snapshot.Clone()
	clone.Admin.AuthorizedSSHKeys[0] = testSSHKey("b")
	clone.Packages[0] = "curl"
	if clone.Admin.AuthorizedSSHKeys[0] == snapshot.Admin.AuthorizedSSHKeys[0] || clone.Packages[0] == snapshot.Packages[0] {
		t.Fatal("profile snapshot clone shares mutable slices")
	}
}

func validSnapshot() Snapshot {
	return Snapshot{
		SchemaVersion: SchemaVersion,
		Hostname:      "aegis-node",
		Locale:        "de_DE.UTF-8",
		Keyboard:      "de",
		Timezone:      "Europe/Berlin",
		Admin: Admin{
			Username:          "guardian",
			FullName:          "Aegis Administrator",
			AuthorizedSSHKeys: []string{testSSHKey("a")},
		},
		Packages: []string{"curl"},
	}
}

func testSSHKey(value string) string {
	payload := base64.StdEncoding.EncodeToString([]byte(strings.Repeat(value, 64)))
	return "ssh-ed25519 " + payload + " test"
}
