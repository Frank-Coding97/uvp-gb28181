package deviceevents

import (
	"crypto/hmac"
	"crypto/sha256"
)

const cursorKeyContext = "uvp-gb28181/openapi/deviceevents/cursor/v1"

// DeriveCursorKey derives a stable cursor-signing key from the configured
// OpenAPI master key. The context keeps cursor signatures separate from the
// other keys derived from that master secret.
func DeriveCursorKey(masterKey []byte) ([]byte, error) {
	if len(masterKey) < 32 {
		return nil, ErrUnavailable
	}
	mac := hmac.New(sha256.New, masterKey)
	_, _ = mac.Write([]byte(cursorKeyContext))
	return mac.Sum(nil), nil
}
