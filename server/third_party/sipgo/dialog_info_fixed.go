package sipgo

import (
	"errors"
	"net"
	"strconv"
	"strings"

	"github.com/emiago/sipgo/sip"
)

var ErrFixedDialogINFO = errors.New("invalid fixed dialog INFO material")

// BuildDialogINFO only builds a request, without connecting, writing or
// changing a live dialog. The caller must reserve cseq durably and retain the
// concrete transaction before its separately authorized Init. Do/TransactionRequest
// would build again and must not be used with this already fixed request.
func (ua *DialogUA) BuildDialogINFO(invite *sip.Request, response *sip.Response, cseq uint32, contentType string, body []byte) (*sip.Request, error) {
	if !validFixedDialogMaterial(ua, invite, response, cseq) || len(body) == 0 || len(body) > 4096 ||
		contentType == "" || len(contentType) > 128 || strings.ContainsAny(contentType, "\r\n\x00") {
		return nil, ErrFixedDialogINFO
	}
	requestCopy, responseCopy := cloneOwnedCleanupRequest(invite), response.Clone()
	requestCopy.SetBody(nil)
	responseCopy.SetBody(nil)
	session := &DialogClientSession{Dialog: Dialog{InviteRequest: requestCopy, InviteResponse: responseCopy}, UA: ua}
	session.Dialog.Init()
	r := sip.NewRequest(sip.INFO, *responseCopy.Contact().Address.Clone())
	r.SipVersion = invite.SipVersion
	r.SetBody(append([]byte(nil), body...))
	r.AppendHeader(sip.NewHeader("Content-Type", contentType))
	via := invite.Via().Clone()
	via.Params.Add("branch", sip.GenerateBranchN(16))
	r.PrependHeader(via)
	r.AppendHeader(sip.HeaderClone(requestCopy.Contact()))
	session.lastCSeqNo.Store(cseq - 1)
	session.buildReq(r) // Exactly once, preserving existing route semantics.
	r.SetSource(invite.Source())
	setFixedDialogDestination(r)
	return cloneOwnedCleanupRequest(r), nil
}

func validFixedDialogMaterial(ua *DialogUA, invite *sip.Request, response *sip.Response, cseq uint32) bool {
	if ua == nil || ua.Client == nil || ua.Client.UserAgent == nil || ua.Client.TransactionLayer() == nil || ua.Client.TxRequester != nil || ua.RewriteContact ||
		!validCleanupInvite(invite) || !validOwnedInviteResponse(invite, response) || cseq <= invite.CSeq().SeqNo || len(response.String()) > 65536 {
		return false
	}
	if !validCleanupTarget(response.Contact().Address) || len(response.GetHeaders("Record-Route")) > 8 {
		return false
	}
	for _, h := range response.GetHeaders("Record-Route") {
		var uri sip.Uri
		params := sip.NewParams()
		name, err := sip.ParseAddressValue(h.Value(), &uri, &params)
		if err != nil || name != "" || len(params) != 0 || !validCleanupTarget(uri) {
			return false
		}
	}
	return true
}

func setFixedDialogDestination(r *sip.Request) {
	target := r.Recipient
	if route := r.Route(); route != nil {
		target = route.Address
	}
	port := target.Port
	if port == 0 {
		port = 5060
	}
	r.SetDestination(net.JoinHostPort(strings.Trim(target.Host, "[]"), strconv.Itoa(port)))
}
