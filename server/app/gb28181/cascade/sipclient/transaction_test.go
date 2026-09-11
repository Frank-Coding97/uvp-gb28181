package sipclient

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"

	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
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
