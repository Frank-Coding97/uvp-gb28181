package sipclient

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"

	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

type fakeTransport struct {
	doResponses     []*sip.Response
	doErr           error
	digestResponses []*sip.Response
	digestErr       error
	doRequests      []*sip.Request
	digestRequests  []*sip.Request
	doDeadline      time.Time
}

func (f *fakeTransport) Do(ctx context.Context, request *sip.Request) (*sip.Response, error) {
	f.doRequests = append(f.doRequests, request)
	f.doDeadline, _ = ctx.Deadline()
	if f.doErr != nil {
		return nil, f.doErr
	}
	if len(f.doResponses) == 0 {
		return nil, errors.New("unexpected Do")
	}
	response := f.doResponses[0]
	f.doResponses = f.doResponses[1:]
	return response, nil
}

func (f *fakeTransport) DoDigestAuth(_ context.Context, request *sip.Request, response *sip.Response, _ Credentials) (*sip.Response, error) {
	f.digestRequests = append(f.digestRequests, request)
	if f.digestErr != nil {
		return nil, f.digestErr
	}
	if len(f.digestResponses) == 0 {
		return nil, errors.New("unexpected DoDigestAuth")
	}
	next := f.digestResponses[0]
	f.digestResponses = f.digestResponses[1:]
	return next, nil
}

type keepaliveEncoderFunc func(protocol.Version) ([]byte, error)

func (f keepaliveEncoderFunc) EncodeKeepalive(profile protocol.Version) ([]byte, error) {
	return f(profile)
}

func TestTransactionClientRetriesDigestChallengeExactlyOnce(t *testing.T) {
	for _, testCase := range []struct {
		name            string
		status          int
		challengeHeader string
	}{
		{name: "www authenticate", status: sip.StatusUnauthorized, challengeHeader: "WWW-Authenticate"},
		{name: "proxy authenticate", status: sip.StatusProxyAuthRequired, challengeHeader: "Proxy-Authenticate"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			challenge := sip.NewResponse(testCase.status, "challenge")
			challenge.AppendHeader(sip.NewHeader(testCase.challengeHeader, `Digest realm="upstream", nonce="nonce"`))
			transport := &fakeTransport{
				doResponses:     []*sip.Response{challenge},
				digestResponses: []*sip.Response{sip.NewResponse(testCase.status, "still unauthorized")},
			}
			client := mustTransactionClient(t, protocol.Version2016, transport)

			result := client.Register(context.Background(), 3600, "register-call")

			if !result.Challenge || !result.Retried || result.Success || result.StatusCode != testCase.status {
				t.Fatalf("result=%+v", result)
			}
			if len(transport.doRequests) != 1 || len(transport.digestRequests) != 1 {
				t.Fatalf("Do=%d DoDigestAuth=%d, want 1 each", len(transport.doRequests), len(transport.digestRequests))
			}
		})
	}
}

func TestTransactionClientUsesGrantedRegisterContactExpiry(t *testing.T) {
	response := sip.NewResponse(sip.StatusOK, "OK")
	contact := &sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: "34020000001320000001", Host: "192.0.2.20", Port: 5061}}
	contact.Params.Add("expires", "1200")
	response.AppendHeader(contact)
	client := mustTransactionClient(t, protocol.Version2016, &fakeTransport{doResponses: []*sip.Response{response}})
	result := client.Register(context.Background(), 3600, "register-call")
	if !result.Success || !result.GrantedExpiresFound || result.GrantedExpires != 1200 {
		t.Fatalf("result=%+v", result)
	}
}

func TestTransactionClientUsesRegisterExpiresHeaderWhenContactHasNoExpiry(t *testing.T) {
	response := sip.NewResponse(sip.StatusOK, "OK")
	response.AppendHeader(sip.NewHeader("Expires", "900"))
	client := mustTransactionClient(t, protocol.Version2016, &fakeTransport{doResponses: []*sip.Response{response}})
	result := client.Register(context.Background(), 3600, "register-call")
	if !result.Success || !result.GrantedExpiresFound || result.GrantedExpires != 900 {
		t.Fatalf("result=%+v", result)
	}
}

func TestTransactionClientParsesFinalRegisterVersion(t *testing.T) {
	for _, testCase := range []struct {
		raw  string
		want protocol.Version
	}{
		{raw: "2.0", want: protocol.Version2016},
		{raw: "3.0", want: protocol.Version2022},
	} {
		response := sip.NewResponse(sip.StatusOK, "OK")
		response.AppendHeader(sip.NewHeader("X-GB-Ver", testCase.raw))
		result := mustTransactionClient(t, protocol.Version2016, &fakeTransport{doResponses: []*sip.Response{response}}).Register(context.Background(), 3600, "version")
		if !result.Success || !result.ReportedGBVersionFound || result.ReportedGBVersion != testCase.raw || !result.NegotiatedVersionFound || result.NegotiatedVersion != testCase.want {
			t.Fatalf("raw=%q result=%+v", testCase.raw, result)
		}
	}
}

func TestTransactionClientOnlyUsesFinalDigestResponseVersion(t *testing.T) {
	challenge := sip.NewResponse(sip.StatusUnauthorized, "challenge")
	challenge.AppendHeader(sip.NewHeader("WWW-Authenticate", `Digest realm="upstream", nonce="nonce"`))
	final := sip.NewResponse(sip.StatusOK, "OK")
	final.AppendHeader(sip.NewHeader("X-GB-Ver", "3.0"))
	result := mustTransactionClient(t, protocol.Version2016, &fakeTransport{doResponses: []*sip.Response{challenge}, digestResponses: []*sip.Response{final}}).Register(context.Background(), 3600, "version-digest")
	if !result.Success || !result.Retried || result.ReportedGBVersion != "3.0" || result.NegotiatedVersion != protocol.Version2022 {
		t.Fatalf("result=%+v", result)
	}
}

func TestTransactionClientObservesRegisterDateWithoutChangingSuccess(t *testing.T) {
	t.Run("valid GMT", func(t *testing.T) {
		response := sip.NewResponse(sip.StatusOK, "OK")
		response.AppendHeader(sip.NewHeader("Date", "Fri, 23 Oct 2026 12:34:56 GMT"))
		result := mustTransactionClient(t, protocol.Version2016, &fakeTransport{doResponses: []*sip.Response{response}}).Register(context.Background(), 3600, "register-date")
		want := time.Date(2026, time.October, 23, 12, 34, 56, 0, time.UTC)
		if !result.Success || !result.ServerDateFound || result.ServerDateErr != nil || !result.ServerDate.Equal(want) {
			t.Fatalf("result=%+v", result)
		}
	})

	for _, raw := range []string{"not a date", "Fri, 23 Oct 2026 12:34:56 UTC"} {
		t.Run(raw, func(t *testing.T) {
			response := sip.NewResponse(sip.StatusOK, "OK")
			response.AppendHeader(sip.NewHeader("Date", raw))
			result := mustTransactionClient(t, protocol.Version2016, &fakeTransport{doResponses: []*sip.Response{response}}).Register(context.Background(), 3600, "register-date-invalid")
			if !result.Success || !result.ServerDateFound || result.ServerDateErr == nil {
				t.Fatalf("optional invalid Date must not reject REGISTER: result=%+v", result)
			}
		})
	}
}

func TestRegisterFollowsBoundedTrustedRedirect(t *testing.T) {
	redirect := sip.NewResponse(sip.StatusMovedTemporarily, "Moved Temporarily")
	redirect.AppendHeader(&sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: "34020000002000000001", Host: "3402000000"}})
	accepted := sip.NewResponse(sip.StatusOK, "OK")
	accepted.AppendHeader(sip.NewHeader("Expires", "3600"))
	transport := &fakeTransport{doResponses: []*sip.Response{redirect, accepted}}
	result := mustTransactionClient(t, protocol.Version2016, transport).Register(context.Background(), 3600, "redirected-register")
	if !result.Success || result.StatusCode != sip.StatusOK || len(transport.doRequests) != 2 {
		t.Fatalf("result=%+v requests=%d", result, len(transport.doRequests))
	}
	first, second := transport.doRequests[0], transport.doRequests[1]
	if second.Recipient.Host != "3402000000" || second.Recipient.User != "34020000002000000001" || second.Destination() != "3402000000:5060" {
		t.Fatalf("redirect request target=%+v destination=%q", second.Recipient, second.Destination())
	}
	if first.CSeq().SeqNo == second.CSeq().SeqNo || first.CallID().Value() != second.CallID().Value() {
		t.Fatalf("redirect must keep Call-ID and use a new CSeq: first=%v second=%v", first.CSeq(), second.CSeq())
	}
}

func TestRegisterRejectsUntrustedRedirectAndRedirectLoop(t *testing.T) {
	t.Run("untrusted host", func(t *testing.T) {
		redirect := sip.NewResponse(sip.StatusMovedTemporarily, "Moved Temporarily")
		redirect.AppendHeader(&sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: "34020000002000000001", Host: "attacker.example"}})
		transport := &fakeTransport{doResponses: []*sip.Response{redirect}}
		result := mustTransactionClient(t, protocol.Version2016, transport).Register(context.Background(), 3600, "redirect-untrusted")
		if result.Success || result.StatusCode != sip.StatusMovedTemporarily || result.BuildErr == nil || len(transport.doRequests) != 1 {
			t.Fatalf("result=%+v requests=%d", result, len(transport.doRequests))
		}
	})

	t.Run("loop", func(t *testing.T) {
		redirect := sip.NewResponse(sip.StatusMovedTemporarily, "Moved Temporarily")
		redirect.AppendHeader(&sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: "34020000002000000001", Host: "3402000000"}})
		transport := &fakeTransport{doResponses: []*sip.Response{redirect, redirect}}
		result := mustTransactionClient(t, protocol.Version2016, transport).Register(context.Background(), 3600, "redirect-loop")
		if result.Success || result.BuildErr == nil || len(transport.doRequests) != 2 {
			t.Fatalf("result=%+v requests=%d", result, len(transport.doRequests))
		}
	})
}

func TestTransactionClientDoesNotTreatUnknownVersionAsNegotiated(t *testing.T) {
	response := sip.NewResponse(sip.StatusOK, "OK")
	response.AppendHeader(sip.NewHeader("X-GB-Ver", "9.9"))
	result := mustTransactionClient(t, protocol.Version2016, &fakeTransport{doResponses: []*sip.Response{response}}).Register(context.Background(), 3600, "version-unknown")
	if !result.Success || !result.ReportedGBVersionFound || result.NegotiatedVersionFound || result.NegotiatedVersionErr == nil {
		t.Fatalf("result=%+v", result)
	}
}

func TestTransactionClientRejectsInvalidRegisterExpiry(t *testing.T) {
	response := sip.NewResponse(sip.StatusOK, "OK")
	response.AppendHeader(sip.NewHeader("Expires", "soon"))
	client := mustTransactionClient(t, protocol.Version2016, &fakeTransport{doResponses: []*sip.Response{response}})
	result := client.Register(context.Background(), 3600, "register-call")
	if result.Success || result.GrantedExpiresErr == nil {
		t.Fatalf("result=%+v", result)
	}
}

func TestTransactionClientDoesNotRetryMalformedChallengeOrTransportFailure(t *testing.T) {
	challenge := sip.NewResponse(sip.StatusUnauthorized, "missing challenge")
	transport := &fakeTransport{doResponses: []*sip.Response{challenge}}
	client := mustTransactionClient(t, protocol.Version2016, transport)
	result := client.Register(context.Background(), 3600, "missing-challenge")
	if !result.Challenge || result.Retried || result.Success || result.StatusCode != sip.StatusUnauthorized {
		t.Fatalf("missing challenge result=%+v", result)
	}
	if len(transport.digestRequests) != 0 {
		t.Fatalf("DoDigestAuth=%d, want 0", len(transport.digestRequests))
	}

	transport = &fakeTransport{doErr: context.DeadlineExceeded}
	client = mustTransactionClient(t, protocol.Version2016, transport)
	result = client.Register(context.Background(), 3600, "timeout")
	if !errors.Is(result.TransportErr, context.DeadlineExceeded) || result.StatusCode != 0 || result.Success {
		t.Fatalf("timeout result=%+v", result)
	}
	if transport.doDeadline.IsZero() {
		t.Fatal("transaction transport context must have a deadline")
	}
}

func TestTransactionClientLogoutAndKeepaliveUseFactoryAndExternalEncoder(t *testing.T) {
	transport := &fakeTransport{doResponses: []*sip.Response{
		sip.NewResponse(sip.StatusOK, "OK"),
		sip.NewResponse(sip.StatusOK, "OK"),
	}}
	client := mustTransactionClient(t, protocol.Version2022, transport)
	logout := client.Logout(context.Background(), "logout-call")
	if !logout.Success || len(transport.doRequests) != 1 {
		t.Fatalf("logout=%+v Do=%d", logout, len(transport.doRequests))
	}
	logoutRequest := transport.doRequests[0]
	if header := logoutRequest.GetHeader("Expires"); header == nil || header.Value() != "0" {
		t.Fatalf("logout Expires=%v", header)
	}
	if header := logoutRequest.GetHeader("X-GB-Ver"); header == nil || header.Value() != "3.0" {
		t.Fatalf("logout X-GB-Ver=%v", header)
	}
	if logoutRequest.CSeq().SeqNo != 1 {
		t.Fatalf("logout CSeq=%d, want 1", logoutRequest.CSeq().SeqNo)
	}

	var receivedProfile protocol.Version
	keepalive := client.Keepalive(context.Background(), "keepalive-call", keepaliveEncoderFunc(func(profile protocol.Version) ([]byte, error) {
		receivedProfile = profile
		return []byte("profile-owned-body"), nil
	}))
	if !keepalive.Success || receivedProfile != protocol.Version2022 || len(transport.doRequests) != 2 {
		t.Fatalf("keepalive=%+v profile=%s Do=%d", keepalive, receivedProfile, len(transport.doRequests))
	}
	request := transport.doRequests[1]
	if request.Method != sip.MESSAGE || string(request.Body()) != "profile-owned-body" {
		t.Fatalf("keepalive request=%s body=%q", request.Method, request.Body())
	}
	if header := request.GetHeader("Content-Type"); header == nil || header.Value() != "Application/MANSCDP+xml" {
		t.Fatalf("Content-Type=%v", header)
	}
	if header := request.GetHeader("X-GB-Ver"); header == nil || header.Value() != "3.0" {
		t.Fatalf("keepalive X-GB-Ver=%v", header)
	}
	if request.CSeq().SeqNo != 2 {
		t.Fatalf("keepalive CSeq=%d, want 2", request.CSeq().SeqNo)
	}
}

func TestTransactionRetryAndRefreshDecisionsAreBoundedAndPure(t *testing.T) {
	if !ShouldRetryDigest(sip.StatusUnauthorized, true, 0) || !ShouldRetryDigest(sip.StatusProxyAuthRequired, true, 0) {
		t.Fatal("initial complete challenge must retry")
	}
	for _, input := range []struct {
		status   int
		header   bool
		attempts int
	}{
		{status: sip.StatusUnauthorized, header: false, attempts: 0},
		{status: sip.StatusUnauthorized, header: true, attempts: 1},
		{status: sip.StatusRequestTimeout, header: true, attempts: 0},
	} {
		if ShouldRetryDigest(input.status, input.header, input.attempts) {
			t.Fatalf("ShouldRetryDigest(%d, %t, %d)=true", input.status, input.header, input.attempts)
		}
	}

	for _, testCase := range []struct {
		expires int
		want    time.Duration
	}{
		{expires: 0, want: 0},
		{expires: 1, want: time.Second},
		{expires: 2, want: time.Second},
		{expires: 3600, want: 48 * time.Minute},
	} {
		if got := RefreshDelay(testCase.expires); got != testCase.want {
			t.Fatalf("RefreshDelay(%d)=%s, want %s", testCase.expires, got, testCase.want)
		}
	}
}

func mustTransactionClient(t *testing.T, profile protocol.Version, transport Transport) *TransactionClient {
	t.Helper()
	factory := mustFactory(t, Identity{
		UpstreamServerID: "34020000002000000001", UpstreamDomain: "3402000000", Host: "192.0.2.10", Port: 5060,
		LocalDeviceID: "34020000001320000001", LocalDomain: "3402000000", LocalIP: "192.0.2.20", LocalPort: 5061,
		Transport: "udp", Profile: profile,
	})
	client, err := NewTransactionClient(factory, transport, Credentials{Username: "platform", Password: "secret"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return client
}
