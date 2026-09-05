package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"unicode/utf8"

	"github.com/emiago/sipgo/sip"
	"github.com/icholy/digest"
	gbsecurity "uvplatform.cn/uvp-gb28181/app/gb28181/security"
)

type sourceBoundRegisterSecurity interface {
	IssueNonceForSource(sourceIP string) (string, error)
	VerifyNonceSource(nonce, sourceIP string) error
	ValidateNonceForSourceTransaction(nonce, nonceCount, transactionFingerprint, sourceIP string) error
}

func (s *standaloneRegisterSecurity) IssueNonceForSource(sourceIP string) (string, error) {
	return s.nonce.IssueForSource(sourceIP)
}
func (s *standaloneRegisterSecurity) VerifyNonceSource(nonce, sourceIP string) error {
	return s.nonce.VerifySource(nonce, sourceIP)
}
func (s *standaloneRegisterSecurity) ValidateNonceForSourceTransaction(nonce, nc, transaction, sourceIP string) error {
	return s.nonce.ValidateForSourceTransaction(nonce, nc, transaction, sourceIP)
}

// A returned challenge proves reachability only. Digest is still required to
// authenticate the device, and this check never consumes the nonce.
func (h *RegisterHandler) verifiedRegisterSource(req *sip.Request) bool {
	security, ok := h.security.(sourceBoundRegisterSecurity)
	header := req.GetHeader("Authorization")
	if !ok || header == nil {
		return false
	}
	credentials, err := digest.ParseCredentials(header.Value())
	if err != nil {
		return false
	}
	source, _ := splitHostPort(req.Source())
	return security.VerifyNonceSource(credentials.Nonce, source) == nil
}

// Deliberately excludes Authorization and DeviceID: changing them inside one
// SIP transaction must not turn retransmissions into enumeration evidence.
func registerSecurityTransaction(req *sip.Request) string {
	if req.CallID() == nil || req.CSeq() == nil || req.Via() == nil {
		return ""
	}
	branch, _ := req.Via().Params.Get("branch")
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s", registerCallID(req), registerCSeq(req), branch)))
	return hex.EncodeToString(sum[:])
}

func securityDeviceID(id string) string {
	if len(id) <= 64 && utf8.ValidString(id) {
		return id
	}
	// Bound attacker-controlled metadata without collapsing different long IDs.
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

func (h *RegisterHandler) rejectInvalidDeviceID(req *sip.Request, tx sip.ServerTransaction, deviceID string) int {
	h.recordSecurity(req, deviceID, gbsecurity.ReasonRegisterIDInvalid)
	status := sip.StatusBadRequest
	// A UDP scanner may return this challenge to prove the source address.
	// Even with a valid nonce, an invalid ID never enters business registration.
	if req.Transport() == "UDP" && deviceID != "" && !h.verifiedRegisterSource(req) {
		status, _ = h.respondChallenge(req, tx)
	} else {
		_ = tx.Respond(h.newResponse(req, status, "Invalid device id", nil))
	}
	h.recordEnd(req, status, false)
	return status
}
