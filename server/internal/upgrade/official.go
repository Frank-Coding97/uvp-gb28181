package upgrade

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"strings"
)

// OfficialReleaseSource is the built-in online upgrade entry point. It is
// compiled into the binary on purpose: an appliance operator only needs the
// "check for updates" button, and there is no supported reason for the endpoint
// to be edited on a shipped system. Keeping it out of configuration also means
// a tampered config file cannot quietly repoint upgrades at another host.
const OfficialReleaseSource = "https://uvplatform.com/upgrades/channels/stable/manifest.json"

// OfficialTrustedKeys are the release signing keys embedded as the trust root.
// The matching private key never leaves the release workstation. A release that
// is not signed by one of these keys is rejected before any of its metadata is
// read, so a compromised download host cannot forge a version.
var OfficialTrustedKeys = map[string]string{
	"uvp-release-v1": "UNBgaJGXgEULF9oOLvdlwZ4uBMpztkDOgw82sPnwQuU=",
}

// ParseTrustedKeys decodes trusted key material in either base64 or hex form.
// Entries that are not a valid Ed25519 public key are ignored rather than
// failing the whole set, so one broken registration entry cannot lock an
// appliance out of upgrades it is otherwise entitled to.
func ParseTrustedKeys(values map[string]string) map[string]ed25519.PublicKey {
	keys := make(map[string]ed25519.PublicKey, len(values))
	for id, encoded := range values {
		id, encoded = strings.TrimSpace(id), strings.TrimSpace(encoded)
		if id == "" || encoded == "" {
			continue
		}
		var raw []byte
		var err error
		if len(encoded) == ed25519.PublicKeySize*2 {
			raw, err = hex.DecodeString(encoded)
		} else {
			raw, err = base64.StdEncoding.DecodeString(encoded)
		}
		if err != nil || len(raw) != ed25519.PublicKeySize {
			continue
		}
		keys[id] = ed25519.PublicKey(raw)
	}
	return keys
}

// OfficialPublicKeys returns the embedded trust root in parsed form.
func OfficialPublicKeys() map[string]ed25519.PublicKey {
	return ParseTrustedKeys(OfficialTrustedKeys)
}

// WithOfficialDefaults fills the online upgrade entry point and trust root for
// a registration that predates the built-in release source. An explicitly
// registered source or key set is always preserved: private deployments that
// host their own channel keep working.
func (r Registration) WithOfficialDefaults() Registration {
	if strings.TrimSpace(r.ReleaseSource) == "" {
		r.ReleaseSource = OfficialReleaseSource
	}
	if len(r.TrustedKeys) == 0 {
		keys := make(map[string]string, len(OfficialTrustedKeys))
		for id, value := range OfficialTrustedKeys {
			keys[id] = value
		}
		r.TrustedKeys = keys
	}
	return r
}
