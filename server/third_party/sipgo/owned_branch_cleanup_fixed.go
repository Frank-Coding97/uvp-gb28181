package sipgo

import (
	"strings"

	"github.com/emiago/sipgo/sip"
)

// NewFixedBranchCleanup owns already fixed compensation requests. It neither
// reconstructs an observed response nor grants recovery/dispatch permission.
// The caller must bind both snapshots to its durable branch, acquire a fresh
// attempt ticket and lease, and separately authorize ACK/BYE before sending.
// No builders, defaults, connection lookup or network operations run here.
func (ua *DialogUA) NewFixedBranchCleanup(ack, bye *sip.Request) (*OwnedBranchCleanup, error) {
	if ua == nil || ua.Client == nil || ua.Client.UserAgent == nil || ua.Client.TransactionLayer() == nil || ua.Client.TxRequester != nil || ua.RewriteContact ||
		!validFixedCleanupRequest(ack, sip.ACK) || !validFixedCleanupRequest(bye, sip.BYE) {
		return nil, ErrOwnedCleanupState
	}
	if ack.CSeq().SeqNo >= bye.CSeq().SeqNo || ack.Recipient.String() != bye.Recipient.String() ||
		ack.MessageData.Transport() != bye.MessageData.Transport() || ack.MessageData.Destination() != bye.MessageData.Destination() {
		return nil, ErrOwnedCleanupState
	}
	for _, name := range []string{"From", "To", "Call-ID", "Contact", "Max-Forwards", "Content-Length", "Route"} {
		a, b := ack.GetHeaders(name), bye.GetHeaders(name)
		if len(a) != len(b) {
			return nil, ErrOwnedCleanupState
		}
		for index := range a {
			if a[index].Value() != b[index].Value() {
				return nil, ErrOwnedCleanupState
			}
		}
	}
	a, b := ack.Via().Clone(), bye.Via().Clone()
	branch := a.Params.GetOr("branch", "")
	if branch == b.Params.GetOr("branch", "") {
		return nil, ErrOwnedCleanupState
	}
	b.Params.Add("branch", branch)
	if a.Value() != b.Value() {
		return nil, ErrOwnedCleanupState
	}
	return &OwnedBranchCleanup{client: ua.Client, ack: cloneOwnedCleanupRequest(ack), bye: cloneOwnedCleanupRequest(bye), quiesced: make(chan struct{})}, nil
}

func validFixedCleanupRequest(r *sip.Request, method sip.RequestMethod) bool {
	if r == nil || r.Method != method || r.SipVersion != "SIP/2.0" || len(r.Body()) != 0 || len(r.String()) > 65536 ||
		!validCleanupTarget(r.Recipient) || r.MessageData.Destination() == "" || len(r.Laddr.IP) != 0 || r.Laddr.Port != 0 {
		return false
	}
	for _, name := range []string{"Via", "From", "To", "Call-ID", "CSeq", "Contact", "Max-Forwards", "Content-Length"} {
		if len(r.GetHeaders(name)) != 1 {
			return false
		}
	}
	for _, h := range r.Headers() {
		switch strings.ToLower(h.Name()) {
		case "via", "from", "to", "call-id", "cseq", "contact", "max-forwards", "content-length", "route":
		default:
			return false
		}
	}
	v, f, to, c, seq := r.Via(), r.From(), r.To(), r.Contact(), r.CSeq()
	if v == nil || f == nil || to == nil || c == nil || seq == nil || r.CallID() == nil || *r.CallID() == "" ||
		r.MaxForwards() == nil || *r.MaxForwards() != 70 || r.ContentLength() == nil || *r.ContentLength() != 0 || seq.SeqNo == 0 || seq.MethodName != method ||
		len(f.Params) != 1 || ownedSingleTag(f.Params) == "" || len(to.Params) != 1 || ownedSingleTag(to.Params) == "" || len(c.Params) != 0 ||
		!validCleanupTarget(f.Address) || !validCleanupTarget(to.Address) || !validCleanupTarget(c.Address) || c.Address.Port == 0 ||
		(r.MessageData.Transport() != "UDP" && r.MessageData.Transport() != "TCP") || v.Transport != r.MessageData.Transport() ||
		v.ProtocolName != "SIP" || v.ProtocolVersion != "2.0" || v.Host == "" || v.Port <= 0 || v.Port > 65535 ||
		!strings.HasPrefix(v.Params.GetOr("branch", ""), "z9hG4bK") {
		return false
	}
	seen := map[string]bool{}
	for _, p := range v.Params {
		if (p.K != "branch" && p.K != "rport") || seen[p.K] {
			return false
		}
		seen[p.K] = true
	}
	if len(r.GetHeaders("Route")) > 8 {
		return false
	}
	for _, route := range r.GetHeaders("Route") {
		var uri sip.Uri
		params := sip.NewParams()
		name, err := sip.ParseAddressValue(route.Value(), &uri, &params)
		if err != nil || name != "" || len(params) != 0 || !validCleanupTarget(uri) {
			return false
		}
	}
	copy := r.Clone()
	setFixedDialogDestination(copy)
	return copy.MessageData.Destination() == r.MessageData.Destination()
}
