package diagnosis

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
)

func RegisterCorrelationKey(deviceID, callID string, cseq uint32, nonce string) string {
	material := fmt.Sprintf("register\x00%s\x00%s\x00", deviceID, callID)
	if nonce == "" {
		material += fmt.Sprintf("cseq\x00%d", cseq)
	} else {
		nonceDigest := sha256.Sum256([]byte(nonce))
		material += "nonce-sha256\x00" + hex.EncodeToString(nonceDigest[:])
	}
	digest := sha256.Sum256([]byte(material))
	return "reg-" + hex.EncodeToString(digest[:])
}

func NewPlayCorrelationKey() string {
	return "play-" + uuid.NewString()
}
