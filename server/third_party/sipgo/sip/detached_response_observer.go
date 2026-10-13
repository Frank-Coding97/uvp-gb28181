package sip

import (
	"net/netip"
	"strings"
)

// Immutable response selection only. This never reconstructs an INVITE,
// creates a transport/transaction, or authorizes any outgoing packet.
type DetachedInviteResponseSelector struct {
	Branch, CallID, FromURI, LocalTag, ToURI string
	CSeq                                     uint32
	ViaHost                                  string
	ViaPort                                  int
	Transport, Destination                   string
}

func (txl *TransactionLayer) ObserveDetachedClientResponses(selector DetachedInviteResponseSelector, sink ClientResponseSink) (*ClientResponseObservation, error) {
	if sink == nil {
		return nil, ErrClientResponseObservation
	}
	if !selector.valid() {
		sink.ObservationLost()
		return nil, ErrClientResponseObservation
	}
	key := selector.Branch + TxSeperator + string(INVITE)
	o, err := txl.registerResponseObservation(key, nil, &selector, sink)
	if err != nil {
		// Call sinks outside the registry mutex; an existing owner is retained.
		txl.responseObserverMu.Lock()
		old := txl.responseObservers[key]
		txl.responseObserverMu.Unlock()
		if old != nil {
			old.lost()
		}
		sink.ObservationLost()
	}
	return o, err
}

func detachedPart(value string, max int) bool {
	return len(value) > 0 && len(value) <= max && !strings.ContainsAny(value, "\r\n\x00\t ")
}

func detachedURI(value string) bool {
	var uri Uri
	return detachedPart(value, 1024) && ParseUri(value, &uri) == nil && uri.Scheme == "sip" && uri.Host != "" &&
		!uri.Wildcard && uri.Password == "" && len(uri.Headers) == 0 && uri.String() == value
}

func (s DetachedInviteResponseSelector) valid() bool {
	if !detachedPart(s.Branch, 128) || !strings.HasPrefix(s.Branch, RFC3261BranchMagicCookie) || len(s.Branch) <= len(RFC3261BranchMagicCookie) ||
		!detachedPart(s.CallID, 256) || !detachedPart(s.LocalTag, 128) || s.CSeq == 0 || !detachedURI(s.FromURI) || !detachedURI(s.ToURI) ||
		!detachedPart(s.ViaHost, 253) || s.ViaPort < 1 || s.ViaPort > 65535 || (s.Transport != "TCP" && s.Transport != "UDP") {
		return false
	}
	// No DNS lookup or guessed NAT equivalence on the receive path. A durable
	// hostname cannot prove equality to a numeric socket peer after restart.
	address, err := netip.ParseAddrPort(s.Destination)
	return err == nil && address.Port() != 0
}

func (s DetachedInviteResponseSelector) matches(response *Response) bool {
	if response == nil || response.MessageData.Transport() != s.Transport {
		return false
	}
	expected, err := netip.ParseAddrPort(s.Destination)
	if err != nil {
		return false
	}
	actual, err := netip.ParseAddrPort(response.MessageData.Source())
	if err != nil || actual.Port() != expected.Port() || actual.Addr().Unmap() != expected.Addr().Unmap() {
		return false
	}
	for _, name := range []string{"Via", "Call-ID", "CSeq", "From", "To"} {
		if len(response.GetHeaders(name)) != 1 {
			return false
		}
	}
	via, callID, cseq, from, to := response.Via(), response.CallID(), response.CSeq(), response.From(), response.To()
	if via == nil || callID == nil || cseq == nil || from == nil || to == nil ||
		via.ProtocolName != "SIP" || via.ProtocolVersion != "2.0" || via.Transport != s.Transport || via.Host != s.ViaHost || via.Port != s.ViaPort ||
		string(*callID) != s.CallID || cseq.MethodName != INVITE || cseq.SeqNo != s.CSeq || from.Address.String() != s.FromURI || to.Address.String() != s.ToURI {
		return false
	}
	branches, tags := 0, 0
	for _, p := range via.Params {
		if p.K == "branch" {
			branches++
			if p.V != s.Branch {
				return false
			}
		}
	}
	for _, p := range from.Params {
		if p.K == "tag" {
			tags++
			if p.V != s.LocalTag {
				return false
			}
		}
	}
	return branches == 1 && tags == 1
}
