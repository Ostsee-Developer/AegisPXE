package agent

import (
	"reflect"
	"testing"
	"time"
)

func TestNormalizeCapabilityCeilingIsDeterministic(t *testing.T) {
	got, err := NormalizeCapabilityCeiling([]string{"service.status", " diagnostics.read ", "service.status", "logs.read"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"diagnostics.read", "logs.read", "service.status"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("capabilities=%v want=%v", got, want)
	}
}

func TestNormalizeCapabilityCeilingRejectsGenericOrMalformedCapabilities(t *testing.T) {
	for _, value := range []string{"shell", "Shell.exec", "../root", "service status", "service"} {
		if _, err := NormalizeCapabilityCeiling([]string{value}); err == nil {
			t.Fatalf("capability %q was accepted", value)
		}
	}
}

func TestRecordValidationRejectsInvalidState(t *testing.T) {
	now := time.Now().UTC()
	record := Record{
		ID:                "550e8400-e29b-41d4-a716-446655440000",
		InstallationID:    "i_test",
		MachineID:         "m_test",
		State:             StatePendingBuild,
		UpdateMode:        UpdateModeManual,
		UpdateState:       UpdateStateIdle,
		DesiredGeneration: 1,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := record.Validate(); err != nil {
		t.Fatalf("valid record rejected: %v", err)
	}
	record.State = State("flying")
	if err := record.Validate(); err == nil {
		t.Fatal("invalid agent state was accepted")
	}
}

func TestValidateIDRejectsNonV4UUID(t *testing.T) {
	if err := ValidateID("550e8400-e29b-11d4-a716-446655440000"); err == nil {
		t.Fatal("version 1 UUID was accepted")
	}
}
