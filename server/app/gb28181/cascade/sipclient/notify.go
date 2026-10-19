package sipclient

import (
	"context"
	"fmt"
	"strings"

	"github.com/emiago/sipgo/sip"
)

// NotifyDialog is established from an accepted inbound SUBSCRIBE. The factory
// still pins network delivery to its configured upstream, never a Contact host.
type NotifyDialog struct {
	CallID       string
	From         sip.FromHeader
	To           sip.ToHeader
	RemoteTarget sip.Uri
	Routes       []sip.Uri
	Event        string
}

type NotifyResult struct {
	TransactionResult
	LastCSeq uint32
}

func (f *RequestFactory) BuildNotify(d NotifyDialog, seq uint32, state string, body []byte) (*sip.Request, error) {
	if f == nil || seq == 0 || seq >= 1<<31 || d.CallID == "" || d.Event == "" || state == "" ||
		strings.ContainsAny(d.CallID+d.Event+state, "\r\n\x00") || d.RemoteTarget.Scheme != "sip" || d.RemoteTarget.Host == "" {
		return nil, fmt.Errorf("invalid cascade NOTIFY dialog")
	}
	local, localOK := d.From.Params.Get("tag")
	remote, remoteOK := d.To.Params.Get("tag")
	if !localOK || !remoteOK || local == "" || remote == "" || d.To.Address.User != f.identity.UpstreamServerID {
		return nil, fmt.Errorf("invalid cascade NOTIFY tags or upstream")
	}
	req, err := f.BuildMessage([]byte(" "), d.CallID)
	if err != nil {
		return nil, err
	}
	req.Method = sip.NOTIFY
	req.Recipient = *d.RemoteTarget.Clone()
	req.CSeq().SeqNo = seq
	req.CSeq().MethodName = sip.NOTIFY
	req.RemoveHeader("From")
	from := d.From
	from.Address = *d.From.Address.Clone()
	from.Params = d.From.Params.Clone()
	req.AppendHeader(&from)
	req.RemoveHeader("To")
	to := d.To
	to.Address = *d.To.Address.Clone()
	to.Params = d.To.Params.Clone()
	req.AppendHeader(&to)
	routes := d.Routes
	if len(routes) > 0 && !routes[0].UriParams.Has("lr") {
		req.Recipient = *routes[0].Clone()
		routes = append(append([]sip.Uri(nil), routes[1:]...), d.RemoteTarget)
	}
	for _, route := range routes {
		req.AppendHeader(&sip.RouteHeader{Address: *route.Clone()})
	}
	req.AppendHeader(sip.NewHeader("Event", d.Event))
	req.AppendHeader(sip.NewHeader("Subscription-State", state))
	if len(body) == 0 {
		req.RemoveHeader("Content-Type")
	}
	req.SetBody(body)
	return req, nil
}

func (c *TransactionClient) NotifyFitsUDP(d NotifyDialog, seq uint32, state string, body []byte) bool {
	if c == nil || c.factory == nil {
		return false
	}
	req, err := c.factory.BuildNotify(d, seq, state, body)
	return err == nil && len(req.String())+96 <= sip.UDPMTUSize-200
}

func (c *TransactionClient) SendNotify(ctx context.Context, d NotifyDialog, seq uint32, state string, body []byte) NotifyResult {
	if c == nil || c.factory == nil {
		return NotifyResult{TransactionResult: TransactionResult{BuildErr: fmt.Errorf("NOTIFY client unavailable")}, LastCSeq: seq}
	}
	req, err := c.factory.BuildNotify(d, seq, state, body)
	if err != nil {
		return NotifyResult{TransactionResult: TransactionResult{BuildErr: err}, LastCSeq: seq}
	}
	result := c.execute(ctx, req)
	last := req.CSeq().SeqNo
	// The digest transport may increment CSeq internally. Reserve that sequence
	// even for test transports that return a retried result without mutating req.
	if result.Retried && last == seq {
		last++
	}
	if result.Success {
		res := result.Response
		if res == nil || res.CallID() == nil || res.CallID().Value() != d.CallID || res.CSeq() == nil || res.CSeq().MethodName != sip.NOTIFY || res.CSeq().SeqNo != req.CSeq().SeqNo || res.From() == nil || res.To() == nil || res.From().Value() != req.From().Value() || res.To().Value() != req.To().Value() {
			result.Success = false
			result.TransportErr = fmt.Errorf("NOTIFY response does not match subscription dialog")
		}
	}
	return NotifyResult{TransactionResult: result, LastCSeq: last}
}
