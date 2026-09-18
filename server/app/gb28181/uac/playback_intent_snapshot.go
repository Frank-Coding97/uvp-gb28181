package uac

import (
	"crypto/sha256"
	"errors"
	"net"
	"strconv"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

var errPlaybackIntentSnapshot = errors.New("playback intent snapshot unavailable")

// This is an internal PRE-TRANSACTION observation, not a persistence schema,
// dispatch permit, actual wire identity, dialog receipt or coverage evidence.
// Keep it private: SDP is represented only by its byte length and digest.
type playbackIntentSnapshot struct {
	callID, requestURI, fromURI, localTag, toURI, contactURI string
	transport, destination, viaHost, viaTransport, branch    string
	rportValue, contentType                                  string
	viaPort, bodyLength                                      int
	cseq, maxForwards                                        uint32
	rportPresent                                             bool
	bodySHA256                                               [32]byte
}

// preparePlaybackIntentSnapshot has no production caller until a durable
// dispatch owner and the actual transport boundary are verified. It runs the
// pinned sipgo builder without invoking a transaction or changing the source.
// A later persistent adapter must not equate this snapshot with a sent request.
func (u *UAC) preparePlaybackIntentSnapshot(request *sip.Request) (*sip.Request, playbackIntentSnapshot, error) {
	if u == nil || u.client == nil || !fixedPlaybackIntentIdentity(request) {
		return nil, playbackIntentSnapshot{}, errPlaybackIntentSnapshot
	}
	prepared := request.Clone()
	// sipgo's Clone deliberately shares Body. Neither later mutation by the
	// operation owner nor hashing this copy may alter the other request.
	prepared.SetBody(append([]byte(nil), request.Body()...))
	if err := sipgo.ClientRequestBuild(u.client, prepared); err != nil {
		return nil, playbackIntentSnapshot{}, errPlaybackIntentSnapshot
	}
	snapshot, err := snapshotPlaybackIntentRequest(prepared)
	if err != nil {
		return nil, playbackIntentSnapshot{}, err
	}
	return prepared, snapshot, nil
}

// Identity-generating defaults are not allowed here. In particular, an absent
// From/tag, Call-ID, CSeq or Via/branch cannot be repaired after reservation.
// Zero Via/Contact ports still depend on connection-time selection and cannot
// be called fixed by this pre-transaction contract.
func fixedPlaybackIntentIdentity(r *sip.Request) bool {
	return fixedPlaybackRequestIdentity(r, sip.INVITE)
}

func fixedPlaybackRequestIdentity(r *sip.Request, method sip.RequestMethod) bool {
	if r == nil || r.Method != method {
		return false
	}
	for _, header := range []string{"Call-ID", "From", "CSeq", "Via", "Contact"} {
		if len(r.GetHeaders(header)) != 1 {
			return false
		}
	}
	callID, from, cseq, via, contact := r.CallID(), r.From(), r.CSeq(), r.Via(), r.Contact()
	if callID == nil || *callID == "" || from == nil || cseq == nil || cseq.SeqNo == 0 || cseq.MethodName != method || via == nil || contact == nil {
		return false
	}
	tag, tagOK := from.Params.Get("tag")
	branch, branchOK := via.Params.Get("branch")
	if len(from.Params) != 1 || from.Params[0].K != "tag" || len(contact.Params) != 0 {
		return false
	}
	seen := map[string]bool{}
	for _, param := range via.Params {
		if (param.K != "branch" && param.K != "rport") || seen[param.K] {
			return false
		}
		seen[param.K] = true
	}
	// Request getters can derive defaults from URI/Via. Require the explicit
	// application routing snapshot instead of silently accepting that fallback.
	host, portText, err := net.SplitHostPort(r.MessageData.Destination())
	port, portErr := strconv.Atoi(portText)
	return tagOK && tag != "" && branchOK && branch != "" && from.Address.Host != "" && r.Recipient.Host != "" &&
		via.Host != "" && via.Port > 0 && via.Port <= 65535 && contact.Address.Host != "" && contact.Address.Port > 0 && contact.Address.Port <= 65535 &&
		r.MessageData.Transport() != "" && via.Transport == r.MessageData.Transport() && err == nil && host != "" && portErr == nil && port > 0 && port <= 65535
}

// Extract only: this function never generates headers, tags, branches or IDs.
func snapshotPlaybackIntentRequest(r *sip.Request) (playbackIntentSnapshot, error) {
	return snapshotPlaybackRequest(r, sip.INVITE)
}

func snapshotPlaybackRequest(r *sip.Request, method sip.RequestMethod) (playbackIntentSnapshot, error) {
	dialogRequest := method == sip.ACK || method == sip.BYE || method == sip.INFO
	if (method != sip.INVITE && method != sip.CANCEL && !dialogRequest) || !fixedPlaybackRequestIdentity(r, method) {
		return playbackIntentSnapshot{}, errPlaybackIntentSnapshot
	}
	for _, header := range []string{"To", "Max-Forwards", "Content-Length"} {
		if len(r.GetHeaders(header)) != 1 {
			return playbackIntentSnapshot{}, errPlaybackIntentSnapshot
		}
	}
	if r.To() == nil || r.To().Address.Host == "" || r.MaxForwards() == nil || r.ContentLength() == nil || int(*r.ContentLength()) != len(r.Body()) {
		return playbackIntentSnapshot{}, errPlaybackIntentSnapshot
	}
	if (dialogRequest && (len(r.To().Params) != 1 || singlePlaybackBranchTag(r.To().Params) == "")) || (!dialogRequest && len(r.To().Params) != 0) {
		return playbackIntentSnapshot{}, errPlaybackIntentSnapshot
	}
	contentType := ""
	if method == sip.INVITE || method == sip.INFO {
		if len(r.GetHeaders("Content-Type")) != 1 || r.ContentType() == nil {
			return playbackIntentSnapshot{}, errPlaybackIntentSnapshot
		}
		contentType = string(*r.ContentType())
		if method == sip.INFO && (contentType != "Application/MANSRTSP" || len(r.Body()) == 0 || len(r.Body()) > 4096) {
			return playbackIntentSnapshot{}, errPlaybackIntentSnapshot
		}
	} else if len(r.GetHeaders("Content-Type")) != 0 || len(r.Body()) != 0 {
		return playbackIntentSnapshot{}, errPlaybackIntentSnapshot
	}
	tag, _ := r.From().Params.Get("tag")
	via := r.Via()
	branch, _ := via.Params.Get("branch")
	rport, hasRport := via.Params.Get("rport")
	return playbackIntentSnapshot{
		callID: string(*r.CallID()), cseq: r.CSeq().SeqNo, requestURI: r.Recipient.String(),
		fromURI: r.From().Address.String(), localTag: tag, toURI: r.To().Address.String(), contactURI: r.Contact().Address.String(),
		transport: r.Transport(), destination: r.Destination(), viaHost: via.Host, viaPort: via.Port, viaTransport: via.Transport,
		branch: branch, rportValue: rport, rportPresent: hasRport, maxForwards: uint32(*r.MaxForwards()),
		contentType: contentType, bodyLength: len(r.Body()), bodySHA256: sha256.Sum256(r.Body()),
	}, nil
}
