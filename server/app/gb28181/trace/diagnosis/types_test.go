package diagnosis

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCategoryCodeMatrix(t *testing.T) {
	valid := []struct {
		category Category
		code     Code
	}{
		{CategoryRegisterFailure, CodeDigestFailure},
		{CategoryRegisterFailure, CodeNonceInvalid},
		{CategoryRegisterFailure, CodeNonceExpired},
		{CategoryRegisterFailure, CodeNonceReplay},
		{CategoryRegisterFailure, CodeNonceStale},
		{CategoryRegisterFailure, CodeServerIDMismatch},
		{CategoryRegisterFailure, CodeDeviceNotPreallocated},
		{CategoryRegisterFailure, CodeInvalidRequest},
		{CategoryRegisterFailure, CodeInternalError},
		{CategoryRegisterFailure, CodeRegisterTimeout},
		{CategoryPlayStuck, CodeSignalingTimeout},
		{CategoryPlayStuck, CodeMediaTimeout},
	}
	for _, tt := range valid {
		if err := ValidateCategoryCode(tt.category, tt.code); err != nil {
			t.Fatalf("ValidateCategoryCode(%q, %q): %v", tt.category, tt.code, err)
		}
	}

	invalid := []struct {
		category Category
		code     Code
	}{
		{"", CodeDigestFailure},
		{CategoryRegisterFailure, ""},
		{CategoryPlayStuck, CodeDigestFailure},
		{CategoryRegisterFailure, CodeMediaTimeout},
		{"unknown", "unknown"},
	}
	for _, tt := range invalid {
		if err := ValidateCategoryCode(tt.category, tt.code); err == nil {
			t.Fatalf("ValidateCategoryCode(%q, %q) unexpectedly succeeded", tt.category, tt.code)
		}
	}
}

func TestRegisterCorrelationKeyStableAndNonceSafe(t *testing.T) {
	const nonce = "plain-secret-nonce"
	first := RegisterCorrelationKey("device-1", "call-1", 2, nonce)
	second := RegisterCorrelationKey("device-1", "call-1", 2, nonce)
	if first == "" || first != second {
		t.Fatalf("correlation key is not stable: %q != %q", first, second)
	}
	if strings.Contains(first, nonce) {
		t.Fatalf("correlation key leaked raw nonce: %q", first)
	}
	if first == RegisterCorrelationKey("device-1", "call-1", 3, "other-nonce") {
		t.Fatal("different nonce fingerprints must produce different keys")
	}
}

func TestRegisterCorrelationKeyFallsBackToCSeq(t *testing.T) {
	first := RegisterCorrelationKey("device-1", "call-1", 2, "")
	if first == "" || first != RegisterCorrelationKey("device-1", "call-1", 2, "") {
		t.Fatal("fallback key must be stable")
	}
	if first == RegisterCorrelationKey("device-1", "call-1", 3, "") {
		t.Fatal("fallback key must distinguish CSeq")
	}
}

func TestNewPlayCorrelationKeyIsUnique(t *testing.T) {
	first := NewPlayCorrelationKey()
	second := NewPlayCorrelationKey()
	if first == "" || second == "" || first == second {
		t.Fatalf("play correlation keys must be non-empty and unique: %q %q", first, second)
	}
}

func TestNoopSink(t *testing.T) {
	event := validEvent()
	if err := (NoopSink{}).Emit(context.Background(), event); err != nil {
		t.Fatalf("NoopSink.Emit: %v", err)
	}
}

func TestEventValidateRequiredFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Event)
	}{
		{"category", func(event *Event) { event.Category = "" }},
		{"code", func(event *Event) { event.Code = "" }},
		{"observedAt", func(event *Event) { event.ObservedAt = time.Time{} }},
		{"correlationKey", func(event *Event) { event.CorrelationKey = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := validEvent()
			tt.mutate(&event)
			if err := event.Validate(); err == nil || !strings.Contains(err.Error(), tt.name) {
				t.Fatalf("Validate() error = %v, want field %q", err, tt.name)
			}
		})
	}
}

func validEvent() Event {
	return Event{
		ObservedAt:     time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC),
		CorrelationKey: "register-key",
		State:          StateActive,
		Category:       CategoryRegisterFailure,
		Code:           CodeDigestFailure,
		Stage:          StageRegister,
		Source:         SourceRuntime,
		DeviceID:       "device-1",
		CallID:         "call-1",
		CSeq:           2,
		Method:         "REGISTER",
	}
}
