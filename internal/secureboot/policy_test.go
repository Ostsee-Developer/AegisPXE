package secureboot

import "testing"

func TestObserveSecureBootState(t *testing.T) {
	tests := []struct {
		name       string
		firmware   string
		secureBoot string
		setupMode  string
		want       State
		wantErr    bool
	}{
		{name: "enabled", firmware: "efi", secureBoot: "01", setupMode: "00", want: StateEnabled},
		{name: "disabled", firmware: "efi", secureBoot: "00", setupMode: "00", want: StateDisabled},
		{name: "setup mode", firmware: "efi", secureBoot: "00", setupMode: "01", want: StateSetupMode},
		{name: "unknown", firmware: "efi", want: StateUnknown},
		{name: "bios", firmware: "pcbios", want: StateUnsupported},
		{name: "short values", firmware: "efi", secureBoot: "1", setupMode: "0", want: StateEnabled},
		{name: "invalid secure boot", firmware: "efi", secureBoot: "ff", setupMode: "00", want: StateUnknown, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Observe(tc.firmware, tc.secureBoot, tc.setupMode)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Observe() error=%v wantErr=%v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("Observe()=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestRequiredPolicyFailsClosed(t *testing.T) {
	for _, state := range []State{StateUnknown, StateDisabled, StateSetupMode, StateUnsupported} {
		if PolicyRequired.AllowsProvision(state) {
			t.Fatalf("required policy unexpectedly allowed %q", state)
		}
	}
	if !PolicyRequired.AllowsProvision(StateEnabled) {
		t.Fatal("required policy rejected enabled Secure Boot")
	}
}
