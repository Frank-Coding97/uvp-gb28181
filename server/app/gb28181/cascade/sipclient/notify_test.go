package sipclient

import (
	"context"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

func TestNotifyPreservesSubscriptionDialogAndSequence(t *testing.T) {
	f := mustFactory(t, Identity{UpstreamServerID: "34020000002000000001", UpstreamDomain: "3402000000", Host: "192.0.2.10", Port: 5060, LocalDeviceID: "34020000002000000002", LocalDomain: "3402000000", LocalIP: "192.0.2.20", LocalPort: 5062, Transport: "UDP", Profile: protocol.Version2022})
	from := sip.FromHeader{Address: sip.Uri{Scheme: "sip", User: "34020000002000000002", Host: "3402000000"}, Params: sip.NewParams()}
	from.Params.Add("tag", "local-tag")
	to := sip.ToHeader{Address: sip.Uri{Scheme: "sip", User: "34020000002000000001", Host: "3402000000"}, Params: sip.NewParams()}
	to.Params.Add("tag", "remote-tag")
	dialog := NotifyDialog{CallID: "original-call", From: from, To: to, RemoteTarget: sip.Uri{Scheme: "sip", User: "34020000002000000001", Host: "192.0.2.10", Port: 5060}, Event: "Catalog;id=8"}
	req, err := f.BuildNotify(dialog, 41, "active;expires=599", []byte("<Notify/>"))
	require.NoError(t, err)
	require.Equal(t, sip.NOTIFY, req.Method)
	require.Equal(t, "original-call", req.CallID().Value())
	require.Equal(t, uint32(41), req.CSeq().SeqNo)
	require.Equal(t, from.Value(), req.From().Value())
	require.Equal(t, to.Value(), req.To().Value())
	require.Equal(t, "Catalog;id=8", req.GetHeader("Event").Value())
	require.Equal(t, "active;expires=599", req.GetHeader("Subscription-State").Value())
	require.Equal(t, "192.0.2.10:5060", req.Destination())
	require.Equal(t, "3.0", req.GetHeader("X-GB-Ver").Value())
	req, err = f.BuildNotify(dialog, 42, "terminated;reason=timeout", nil)
	require.NoError(t, err)
	require.Empty(t, req.Body())
	_, err = f.BuildNotify(dialog, 0, "active", nil)
	require.Error(t, err)
	dialog.Event = "Catalog\r\nVia: injected"
	_, err = f.BuildNotify(dialog, 42, "active", nil)
	require.Error(t, err)
}

type notifyTransport struct{ requests []*sip.Request }

func (t *notifyTransport) Do(_ context.Context, r *sip.Request) (*sip.Response, error) {
	t.requests = append(t.requests, r.Clone())
	return sip.NewResponseFromRequest(r, 200, "OK", nil), nil
}
func (t *notifyTransport) DoDigestAuth(ctx context.Context, r *sip.Request, _ *sip.Response, _ Credentials) (*sip.Response, error) {
	return t.Do(ctx, r)
}

func TestNotifyTransactionRejectsResponseOutsideDialog(t *testing.T) {
	f := mustFactory(t, Identity{UpstreamServerID: "34020000002000000001", UpstreamDomain: "3402000000", Host: "192.0.2.10", Port: 5060, LocalDeviceID: "34020000002000000002", LocalDomain: "3402000000", LocalIP: "192.0.2.20", LocalPort: 5062, Transport: "UDP", Profile: protocol.Version2016})
	transport := &notifyTransport{}
	c, err := NewTransactionClient(f, transport, Credentials{}, time.Second)
	require.NoError(t, err)
	from := sip.FromHeader{Address: sip.Uri{User: "34020000002000000002"}, Params: sip.NewParams()}
	from.Params.Add("tag", "local")
	to := sip.ToHeader{Address: sip.Uri{User: "34020000002000000001"}, Params: sip.NewParams()}
	to.Params.Add("tag", "remote")
	dialog := NotifyDialog{CallID: "call", From: from, To: to, RemoteTarget: sip.Uri{Scheme: "sip", User: "34020000002000000001", Host: "192.0.2.10"}, Event: "Catalog;id=1"}
	result := c.SendNotify(context.Background(), dialog, 1, "active;expires=600", []byte("<Notify/>"))
	require.True(t, result.Success)
	require.Equal(t, uint32(1), result.LastCSeq)
}

type notifyResponseTransport struct {
	mutate    func(*sip.Response)
	challenge bool
}

func (t *notifyResponseTransport) Do(_ context.Context, r *sip.Request) (*sip.Response, error) {
	if t.challenge {
		res := sip.NewResponseFromRequest(r, 401, "Unauthorized", nil)
		res.AppendHeader(sip.NewHeader("WWW-Authenticate", "Digest realm=\"upstream\", nonce=\"test\", algorithm=MD5"))
		return res, nil
	}
	res := sip.NewResponseFromRequest(r, 200, "OK", nil)
	if t.mutate != nil {
		t.mutate(res)
	}
	return res, nil
}
func (t *notifyResponseTransport) DoDigestAuth(_ context.Context, r *sip.Request, _ *sip.Response, _ Credentials) (*sip.Response, error) {
	r.CSeq().SeqNo++
	return sip.NewResponseFromRequest(r, 200, "OK", nil), nil
}

func TestNotifyResponseIdentityAndDigestSequence(t *testing.T) {
	f := mustFactory(t, Identity{UpstreamServerID: "34020000002000000001", UpstreamDomain: "3402000000", Host: "192.0.2.10", Port: 5060, LocalDeviceID: "34020000002000000002", LocalDomain: "3402000000", LocalIP: "192.0.2.20", LocalPort: 5062, Transport: "UDP", Profile: protocol.Version2022})
	from := sip.FromHeader{Address: sip.Uri{User: "34020000002000000002"}, Params: sip.NewParams()}
	from.Params.Add("tag", "local")
	to := sip.ToHeader{Address: sip.Uri{User: "34020000002000000001"}, Params: sip.NewParams()}
	to.Params.Add("tag", "remote")
	dialog := NotifyDialog{CallID: "subscription", From: from, To: to, RemoteTarget: sip.Uri{Scheme: "sip", User: to.Address.User, Host: "192.0.2.10"}, Event: "Catalog;id=1"}
	for _, scenario := range []struct {
		name   string
		mutate func(*sip.Response)
		valid  bool
	}{
		{"valid", nil, true},
		{"wrong CallID", func(r *sip.Response) { r.RemoveHeader("Call-ID"); id := sip.CallIDHeader("other"); r.AppendHeader(&id) }, false},
		{"wrong CSeq", func(r *sip.Response) { r.CSeq().SeqNo++ }, false},
		{"wrong method", func(r *sip.Response) { r.CSeq().MethodName = sip.MESSAGE }, false},
		{"wrong local tag", func(r *sip.Response) { r.From().Params.Add("tag", "other") }, false},
		{"wrong remote tag", func(r *sip.Response) { r.To().Params.Add("tag", "other") }, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			c, err := NewTransactionClient(f, &notifyResponseTransport{mutate: scenario.mutate}, Credentials{}, time.Second)
			require.NoError(t, err)
			result := c.SendNotify(context.Background(), dialog, 10, "active;expires=600", []byte("<Notify/>"))
			require.Equal(t, scenario.valid, result.Success)
		})
	}
	c, err := NewTransactionClient(f, &notifyResponseTransport{challenge: true}, Credentials{}, time.Second)
	require.NoError(t, err)
	result := c.SendNotify(context.Background(), dialog, 10, "active;expires=600", []byte("<Notify/>"))
	require.True(t, result.Success)
	require.True(t, result.Retried)
	require.Equal(t, uint32(11), result.LastCSeq)
}

func TestNotifySupportsStrictAndLooseRouteSets(t *testing.T) {
	f := mustFactory(t, Identity{UpstreamServerID: "34020000002000000001", UpstreamDomain: "3402000000", Host: "192.0.2.10", Port: 5060, LocalDeviceID: "34020000002000000002", LocalDomain: "3402000000", LocalIP: "192.0.2.20", LocalPort: 5062, Transport: "UDP", Profile: protocol.Version2022})
	from := sip.FromHeader{Address: sip.Uri{User: "34020000002000000002"}, Params: sip.NewParams()}
	from.Params.Add("tag", "local")
	to := sip.ToHeader{Address: sip.Uri{User: "34020000002000000001"}, Params: sip.NewParams()}
	to.Params.Add("tag", "remote")
	target := sip.Uri{Scheme: "sip", User: to.Address.User, Host: "192.0.2.50"}
	route := sip.Uri{Scheme: "sip", Host: "192.0.2.10", UriParams: sip.NewParams()}
	dialog := NotifyDialog{CallID: "call", From: from, To: to, RemoteTarget: target, Event: "Catalog;id=1", Routes: []sip.Uri{route}}
	req, err := f.BuildNotify(dialog, 1, "active", nil)
	require.NoError(t, err)
	require.Equal(t, route.String(), req.Recipient.String())
	require.Equal(t, target.String(), req.Route().Address.String())
	dialog.Routes[0].UriParams.Add("lr", "")
	req, err = f.BuildNotify(dialog, 2, "active", nil)
	require.NoError(t, err)
	require.Equal(t, target.String(), req.Recipient.String())
	require.Equal(t, dialog.Routes[0].String(), req.Route().Address.String())
	require.Equal(t, "192.0.2.10:5060", req.Destination())
}
