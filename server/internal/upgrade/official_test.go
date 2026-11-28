package upgrade

import (
	"strings"
	"testing"
)

// A shipped appliance must be able to check for updates without anyone editing
// its registration file, so the entry point and trust root are embedded.
func TestRegistrationDefaultsFillBuiltInReleaseSource(t *testing.T) {
	var registration Registration
	filled := registration.WithOfficialDefaults()
	if filled.ReleaseSource != OfficialReleaseSource {
		t.Fatalf("release source = %q, want %q", filled.ReleaseSource, OfficialReleaseSource)
	}
	if len(filled.TrustedKeys) != len(OfficialTrustedKeys) {
		t.Fatalf("trusted keys = %d, want %d", len(filled.TrustedKeys), len(OfficialTrustedKeys))
	}
	keys := ParseTrustedKeys(filled.TrustedKeys)
	if len(keys) != len(OfficialTrustedKeys) {
		t.Fatalf("parsed keys = %d, want %d", len(keys), len(OfficialTrustedKeys))
	}
	for id, key := range keys {
		if len(key) != 32 {
			t.Fatalf("key %q has %d bytes, want 32", id, len(key))
		}
	}
}

// A private deployment that registered its own channel keeps it: defaults only
// fill what is absent.
func TestRegistrationDefaultsPreserveRegisteredChannel(t *testing.T) {
	const source = "https://updates.internal.test/channels/stable/manifest.json"
	registration := Registration{
		ReleaseSource: source,
		TrustedKeys:   map[string]string{"internal": "registered-by-operator"},
	}
	filled := registration.WithOfficialDefaults()
	if filled.ReleaseSource != source {
		t.Fatalf("release source = %q, want %q", filled.ReleaseSource, source)
	}
	if len(filled.TrustedKeys) != 1 {
		t.Fatalf("trusted keys = %d, want 1", len(filled.TrustedKeys))
	}
	if _, ok := filled.TrustedKeys["internal"]; !ok {
		t.Fatal("registered key was replaced by the built-in trust root")
	}
}

func TestOfficialReleaseSourceIsHTTPSManifest(t *testing.T) {
	if !strings.HasPrefix(OfficialReleaseSource, "https://") || !strings.HasSuffix(OfficialReleaseSource, "/manifest.json") {
		t.Fatalf("release source %q is not an HTTPS manifest URL", OfficialReleaseSource)
	}
	if len(OfficialTrustedKeys) == 0 {
		t.Fatal("no built-in trust root")
	}
}
