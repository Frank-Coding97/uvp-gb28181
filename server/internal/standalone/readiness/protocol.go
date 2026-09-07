// Package readiness authenticates the launcher's local readiness exchange.
package readiness

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

const (
	ChallengeHeader = "X-UVP-Readiness-Challenge"
	ProofHeader     = "X-UVP-Readiness-Proof"
)

type Status struct {
	BusinessReady      bool   `json:"business_ready"`
	BusinessReason     string `json:"business_reason,omitempty"`
	InstallationPhase  string `json:"installation_phase,omitempty"`
	CredentialAccepted bool   `json:"credential_accepted,omitempty"`
	BackendReady       bool   `json:"backend_ready"`
	DatabaseReady      bool   `json:"database_ready"`
	RedisReady         bool   `json:"redis_ready"`
	AuthorizationReady bool   `json:"authorization_ready"`
	SIPState           string `json:"sip_state"`
	PID                int    `json:"pid"`
}

func NewChallenge() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}
func proof(secret, direction, challenge string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("uvp-standalone-readiness-v1\n" + direction + "\n" + challenge + "\n"))
	mac.Write(body)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func RequestProof(secret, challenge string) string { return proof(secret, "request", challenge, nil) }
func ResponseProof(secret, challenge string, body []byte) string {
	return proof(secret, "response", challenge, body)
}
func validChallenge(challenge string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(challenge)
	return err == nil && len(raw) == 32 && base64.RawURLEncoding.EncodeToString(raw) == challenge
}
func VerifyRequest(secret, challenge, signature string) bool {
	return secret != "" && validChallenge(challenge) && hmac.Equal([]byte(RequestProof(secret, challenge)), []byte(signature))
}
func VerifyResponse(secret, challenge string, body []byte, signature string) bool {
	return secret != "" && validChallenge(challenge) && hmac.Equal([]byte(ResponseProof(secret, challenge, body)), []byte(signature))
}
