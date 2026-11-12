package playauth

import (
	"encoding/hex"
	"net"
	"strconv"
	"strings"

	"github.com/emiago/sipgo/sip"
)

// DeviceSIPInviteIdentity is fixed PRE-TRANSACTION evidence, not a request to
// replay. It contains no SDP, authentication headers, remote branch or callback.
// Only the operation owner may supply it from its prepared request snapshot.
type DeviceSIPInviteIdentity struct {
	StepID       string `json:"-"`
	CallID       string `json:"-"`
	CSeq         uint32 `json:"-"`
	RequestURI   string `json:"-"`
	FromURI      string `json:"-"`
	LocalTag     string `json:"-"`
	ToURI        string `json:"-"`
	ContactURI   string `json:"-"`
	Transport    string `json:"-"`
	Destination  string `json:"-"`
	ViaHost      string `json:"-"`
	ViaPort      int    `json:"-"`
	ViaTransport string `json:"-"`
	Branch       string `json:"-"`
	RPortPresent bool   `json:"-"`
	RPortValue   string `json:"-"`
	MaxForwards  uint32 `json:"-"`
	ContentType  string `json:"-"`
	BodyLength   int    `json:"-"`
	BodySHA256   string `json:"-"`
}

// Private persistence DTO. Separate from the all-json:"-" API-ineligible type.
type sipInviteIdentityWire struct {
	StepID       string `json:"stepID"`
	CallID       string `json:"callID"`
	CSeq         uint32 `json:"cseq"`
	RequestURI   string `json:"requestURI"`
	FromURI      string `json:"fromURI"`
	LocalTag     string `json:"localTag"`
	ToURI        string `json:"toURI"`
	ContactURI   string `json:"contactURI"`
	Transport    string `json:"transport"`
	Destination  string `json:"destination"`
	ViaHost      string `json:"viaHost"`
	ViaPort      int    `json:"viaPort"`
	ViaTransport string `json:"viaTransport"`
	Branch       string `json:"branch"`
	RPortPresent bool   `json:"rportPresent"`
	RPortValue   string `json:"rportValue"`
	MaxForwards  uint32 `json:"maxForwards"`
	ContentType  string `json:"contentType"`
	BodyLength   int    `json:"bodyLength"`
	BodySHA256   string `json:"bodySHA256"`
}

func (i DeviceSIPInviteIdentity) wire() sipInviteIdentityWire {
	return sipInviteIdentityWire(i)
}

// This v1 storage contract accepts the current builder's simple SIP URIs.
// Passwords, URI headers, arbitrary params and noncanonical parser leftovers
// are rejected, never scrubbed into a different recoverable identity. Other
// protocol shapes need an explicit future contract, not silent normalization.
func validSIPIntentURI(raw string, contact bool) bool {
	if len(raw) == 0 || len(raw) > 1024 || strings.ContainsAny(raw, "\r\n\x00\t ") {
		return false
	}
	var uri sip.Uri
	if sip.ParseUri(raw, &uri) != nil || uri.Scheme != "sip" || uri.Wildcard || uri.HierarhicalSlashes || uri.Password != "" ||
		len(uri.Headers) != 0 || len(uri.UriParams) != 0 || uri.User == "" || !sipIdentityPart(uri.User, 256) ||
		!sipIdentityHost(uri.Host) || uri.Port < 0 || uri.Port > 65535 || (contact && uri.Port == 0) || uri.String() != raw {
		return false
	}
	return true
}

func sipIdentityPart(value string, limit int) bool {
	return len(value) > 0 && len(value) <= limit && strings.Trim(value, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_.-@+") == ""
}

func sipIdentityHost(host string) bool {
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	if net.ParseIP(strings.Trim(host, "[]")) != nil {
		return true
	}
	return strings.Trim(host, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789.-") == ""
}

func validSIPInviteIdentity(i DeviceSIPInviteIdentity) bool {
	host, portText, err := net.SplitHostPort(i.Destination)
	port, portErr := strconv.Atoi(portText)
	digest, digestErr := hex.DecodeString(i.BodySHA256)
	if !validIntentID(i.StepID) || !sipIdentityPart(i.CallID, 256) || !sipIdentityPart(i.LocalTag, 128) || !sipIdentityPart(i.Branch, 128) ||
		i.CSeq == 0 || i.MaxForwards > 255 || !validSIPIntentURI(i.RequestURI, false) || !validSIPIntentURI(i.FromURI, false) ||
		!validSIPIntentURI(i.ToURI, false) || !validSIPIntentURI(i.ContactURI, true) ||
		(i.Transport != "UDP" && i.Transport != "TCP") || i.ViaTransport != i.Transport || !sipIdentityHost(i.ViaHost) || i.ViaPort < 1 || i.ViaPort > 65535 ||
		err != nil || portErr != nil || !sipIdentityHost(host) || port < 1 || port > 65535 || net.JoinHostPort(host, strconv.Itoa(port)) != i.Destination ||
		i.ContentType != "application/sdp" || i.BodyLength < 1 || i.BodyLength > 65535 || digestErr != nil || len(digest) != 32 || hex.EncodeToString(digest) != i.BodySHA256 {
		return false
	}
	if i.RPortValue == "" {
		return true
	}
	rport, err := strconv.Atoi(i.RPortValue)
	return i.RPortPresent && err == nil && rport > 0 && rport <= 65535 && strconv.Itoa(rport) == i.RPortValue
}
