package media

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

const (
	testApplicationDevice  = "34020000002000000001"
	testApplicationChannel = "34020000001320000001"
	testApplicationGrantA  = "11111111-1111-4111-8111-111111111111"
	testApplicationGrantB  = "22222222-2222-4222-8222-222222222222"
)

var testApplicationNow = time.Date(2030, time.January, 2, 3, 4, 5, 0, time.UTC)

type qualificationFake struct {
	mu         sync.Mutex
	prepareN   int
	validateN  int
	ticket     QualificationTicket
	prepareErr error
	validate   []error
}

func (f *qualificationFake) Prepare(context.Context, QualificationRequest) (QualificationTicket, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prepareN++
	if f.prepareErr != nil {
		return QualificationTicket{}, f.prepareErr
	}
	return f.ticket, nil
}

func (f *qualificationFake) Validate(context.Context, QualificationRequest, QualificationTicket) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.validateN++
	if len(f.validate) == 0 {
		return nil
	}
	err := f.validate[0]
	f.validate = f.validate[1:]
	return err
}

type playerFake struct {
	mu      sync.Mutex
	ensureN int
	request play.Request
	result  *play.Result
	err     error
}

func (f *playerFake) EnsureLive(_ context.Context, request play.Request) (*play.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureN++
	f.request = request
	return f.result, f.err
}

type grantFake struct {
	mu            sync.Mutex
	issueN        int
	issueRequests []playauth.OpenAPIGrantIssueRequest
	grants        []playauth.Grant
	issueErr      error
	onIssue       func()
	cleanupN      int
	cleanupClient int64
	cleanupGrant  string
	cleanupErr    error
	cleanupCtx    context.Context
	cleanupHasDL  bool
	cleanupDL     time.Time
	cleanupCtxErr error
}

func (f *grantFake) Issue(_ context.Context, request playauth.OpenAPIGrantIssueRequest) (playauth.Grant, error) {
	if f.onIssue != nil {
		f.onIssue()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.issueN++
	f.issueRequests = append(f.issueRequests, request)
	if f.issueErr != nil {
		return playauth.Grant{}, f.issueErr
	}
	if len(f.grants) == 0 {
		return playauth.Grant{}, errors.New("fake issue failure")
	}
	grant := f.grants[0]
	f.grants = f.grants[1:]
	return grant, nil
}

func (f *grantFake) FailUnboundGrant(ctx context.Context, clientID int64, grantID string) error {
	f.mu.Lock()
	f.cleanupN++
	f.cleanupClient = clientID
	f.cleanupGrant = grantID
	f.cleanupCtx = ctx
	f.cleanupDL, f.cleanupHasDL = ctx.Deadline()
	f.cleanupCtxErr = ctx.Err()
	err := f.cleanupErr
	f.mu.Unlock()
	return err
}

func applicationTicket(protocol string) QualificationTicket {
	origin := "https://media.example:8443"
	if protocol == play.QualifiedProtocolWSSFLV {
		origin = "wss://media.example:8443"
	}
	return QualificationTicket{
		QualificationID: "qualification-1",
		NodeID:          7,
		NodeUUID:        "node-uuid-7",
		NodeRevision:    9,
		BootNonce:       "0123456789abcdef0123456789abcdef",
		Protocol:        protocol,
		MediaOrigin:     origin,
		ExpiresAt:       testApplicationNow.Add(2 * time.Minute),
	}
}

func applicationResult(ticket QualificationTicket) *play.Result {
	stream := testApplicationDevice + "_" + testApplicationChannel
	httpsURL := "https://media.example:8443/rtp/" + stream + ".live.flv"
	wssURL := "wss://media.example:8443/rtp/" + stream + ".live.flv"
	return &play.Result{
		StreamID:   stream,
		App:        "rtp",
		Node:       &play.ResultNode{ID: ticket.NodeID, MediaServerUUID: ticket.NodeUUID, Revision: ticket.NodeRevision, Host: "internal-node-host"},
		Generation: 42,
		URLs: play.PlaybackURLs{
			HTTPSFLV: &httpsURL,
			WSSFLV:   &wssURL,
		},
	}
}

func applicationGrant(id, token string) playauth.Grant {
	return playauth.Grant{
		Token:                   token,
		ExpiresAt:               testApplicationNow.Add(time.Minute),
		AuthorizationGeneration: id,
	}
}

func newTestApplication(provider *qualificationFake, player *playerFake, issuer *grantFake, enabled bool) *LiveApplication {
	return NewLiveApplication(provider, player, issuer, enabled, WithLiveApplicationClock(func() time.Time {
		return testApplicationNow
	}))
}

func testApplyRequest(ticket QualificationTicket, grantID string) ApplyRequest {
	return ApplyRequest{
		ClientID:  81,
		GrantID:   grantID,
		DeviceID:  testApplicationDevice,
		ChannelID: testApplicationChannel,
		Ticket:    ticket,
	}
}

func requireFixedApplicationError(t *testing.T, err error) {
	t.Helper()
	require.ErrorIs(t, err, ErrLiveApplicationUnavailable)
	require.NotContains(t, err.Error(), "fake")
	require.NotContains(t, err.Error(), "internal")
}

func TestLiveApplicationPreflightRequiresTrustedProviderAndTicket(t *testing.T) {
	provider := &qualificationFake{ticket: applicationTicket(play.QualifiedProtocolHTTPSFLV)}
	app := newTestApplication(provider, nil, nil, true)
	ticket, err := app.Preflight(context.Background(), testApplicationDevice, testApplicationChannel, play.QualifiedProtocolHTTPSFLV)
	require.NoError(t, err)
	require.Equal(t, provider.ticket, ticket)
	require.Equal(t, 1, provider.prepareN)

	for _, test := range []struct {
		name     string
		app      *LiveApplication
		device   string
		channel  string
		protocol string
	}{
		{name: "disabled", app: newTestApplication(provider, nil, nil, false), device: testApplicationDevice, channel: testApplicationChannel, protocol: play.QualifiedProtocolHTTPSFLV},
		{name: "empty provider", app: newTestApplication(nil, nil, nil, true), device: testApplicationDevice, channel: testApplicationChannel, protocol: play.QualifiedProtocolHTTPSFLV},
		{name: "short device", app: newTestApplication(provider, nil, nil, true), device: "1", channel: testApplicationChannel, protocol: play.QualifiedProtocolHTTPSFLV},
		{name: "invalid protocol", app: newTestApplication(provider, nil, nil, true), device: testApplicationDevice, channel: testApplicationChannel, protocol: "http-flv"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.app.Preflight(context.Background(), test.device, test.channel, test.protocol)
			requireFixedApplicationError(t, err)
		})
	}
	require.Equal(t, 1, provider.prepareN)
}

func TestLiveApplicationApplyReturnsOnlyBoundProtocolAndDoesNotMutateResult(t *testing.T) {
	ticket := applicationTicket(play.QualifiedProtocolHTTPSFLV)
	provider := &qualificationFake{ticket: ticket}
	result := applicationResult(ticket)
	beforeHTTPS := *result.URLs.HTTPSFLV
	beforeWSS := *result.URLs.WSSFLV
	player := &playerFake{result: result}
	issuer := &grantFake{grants: []playauth.Grant{
		applicationGrant(testApplicationGrantA, "token-a"),
		applicationGrant(testApplicationGrantB, "token-b"),
	}}
	app := newTestApplication(provider, player, issuer, true)

	first, err := app.Apply(context.Background(), testApplyRequest(ticket, testApplicationGrantA))
	require.NoError(t, err)
	second, err := app.Apply(context.Background(), testApplyRequest(ticket, testApplicationGrantB))
	require.NoError(t, err)
	require.NotEqual(t, first.URL, second.URL)
	require.Contains(t, first.URL, "play_token=token-a")
	require.Contains(t, second.URL, "play_token=token-b")
	require.Equal(t, beforeHTTPS, *result.URLs.HTTPSFLV)
	require.Equal(t, beforeWSS, *result.URLs.WSSFLV)
	require.Equal(t, 2, player.ensureN)
	require.Empty(t, player.request.AuthorizationID)
	require.Equal(t, play.QualifiedProtocolHTTPSFLV, first.Protocol)
	require.Equal(t, testApplicationGrantA, first.AuthorizationID)
	require.Equal(t, applicationGrant(testApplicationGrantA, "token-a").ExpiresAt, first.ExpiresAt)
	require.Equal(t, 2, issuer.issueN)
	require.Equal(t, "rtmp", issuer.issueRequests[0].Schema)
	require.Equal(t, "__defaultVhost__", issuer.issueRequests[0].VHost, "match the case-sensitive ZLM socket Hook tuple")

	encoded, err := json.Marshal(first)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "nodeUUID")
	require.NotContains(t, string(encoded), "internal-node-host")
	require.NotContains(t, string(encoded), "wssFlv")
}

func TestLiveApplicationApplySupportsWSSFLVIndependently(t *testing.T) {
	ticket := applicationTicket(play.QualifiedProtocolWSSFLV)
	provider := &qualificationFake{ticket: ticket}
	player := &playerFake{result: applicationResult(ticket)}
	issuer := &grantFake{grants: []playauth.Grant{applicationGrant(testApplicationGrantA, "wss-token")}}
	app := newTestApplication(provider, player, issuer, true)

	data, err := app.Apply(context.Background(), testApplyRequest(ticket, testApplicationGrantA))
	require.NoError(t, err)
	require.Equal(t, play.QualifiedProtocolWSSFLV, data.Protocol)
	require.True(t, strings.HasPrefix(data.URL, "wss://media.example:8443/rtp/"))
	require.Contains(t, data.URL, "play_token=wss-token")
}

func TestLiveApplicationApplyRejectsWithoutEnsureOrIssueForDisabledMalformedOrCanceled(t *testing.T) {
	ticket := applicationTicket(play.QualifiedProtocolHTTPSFLV)
	for _, test := range []struct {
		name    string
		enabled bool
		request ApplyRequest
		cancel  bool
	}{
		{name: "disabled", enabled: false, request: testApplyRequest(ticket, testApplicationGrantA)},
		{name: "bad device", enabled: true, request: func() ApplyRequest {
			r := testApplyRequest(ticket, testApplicationGrantA)
			r.DeviceID = "bad"
			return r
		}()},
		{name: "bad grant", enabled: true, request: func() ApplyRequest {
			r := testApplyRequest(ticket, "not-a-uuid")
			return r
		}()},
		{name: "bad protocol ticket", enabled: true, request: func() ApplyRequest {
			r := testApplyRequest(ticket, testApplicationGrantA)
			r.Ticket.Protocol = "http-flv"
			return r
		}()},
		{name: "canceled", enabled: true, request: testApplyRequest(ticket, testApplicationGrantA), cancel: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := &qualificationFake{ticket: ticket}
			player := &playerFake{result: applicationResult(ticket)}
			issuer := &grantFake{grants: []playauth.Grant{applicationGrant(testApplicationGrantA, "token")}}
			app := newTestApplication(provider, player, issuer, test.enabled)
			ctx := context.Background()
			if test.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			_, err := app.Apply(ctx, test.request)
			requireFixedApplicationError(t, err)
			require.Equal(t, 0, player.ensureN)
			require.Equal(t, 0, issuer.issueN)
			if test.cancel {
				require.Equal(t, 1, issuer.cleanupN, "an admitted canceled request releases only its own unbound grant")
			}
			if test.name == "bad device" || test.name == "bad grant" {
				require.Equal(t, 0, issuer.cleanupN)
			}
		})
	}
}

func TestLiveApplicationApplyValidatesResultTicketAndURLBeforeIssue(t *testing.T) {
	baseTicket := applicationTicket(play.QualifiedProtocolHTTPSFLV)
	cases := []struct {
		name   string
		mutate func(*QualificationTicket, *play.Result)
	}{
		{name: "node id", mutate: func(_ *QualificationTicket, r *play.Result) { r.Node.ID++ }},
		{name: "node uuid", mutate: func(_ *QualificationTicket, r *play.Result) { r.Node.MediaServerUUID = "other" }},
		{name: "node revision", mutate: func(_ *QualificationTicket, r *play.Result) { r.Node.Revision++ }},
		{name: "app", mutate: func(_ *QualificationTicket, r *play.Result) { r.App = "other" }},
		{name: "generation", mutate: func(_ *QualificationTicket, r *play.Result) { r.Generation = 0 }},
		{name: "stream", mutate: func(_ *QualificationTicket, r *play.Result) { r.StreamID = "" }},
		{name: "origin", mutate: func(_ *QualificationTicket, r *play.Result) {
			*r.URLs.HTTPSFLV = "https://other.example:8443/rtp/" + r.StreamID + ".live.flv"
		}},
		{name: "query", mutate: func(_ *QualificationTicket, r *play.Result) { *r.URLs.HTTPSFLV += "?existing=1" }},
		{name: "userinfo", mutate: func(_ *QualificationTicket, r *play.Result) {
			*r.URLs.HTTPSFLV = strings.Replace(*r.URLs.HTTPSFLV, "https://", "https://user:pass@", 1)
		}},
		{name: "fragment", mutate: func(_ *QualificationTicket, r *play.Result) { *r.URLs.HTTPSFLV += "#fragment" }},
		{name: "path", mutate: func(_ *QualificationTicket, r *play.Result) {
			*r.URLs.HTTPSFLV = strings.Replace(*r.URLs.HTTPSFLV, ".live.flv", ".live.mp4", 1)
		}},
		{name: "scheme", mutate: func(_ *QualificationTicket, r *play.Result) {
			*r.URLs.HTTPSFLV = strings.Replace(*r.URLs.HTTPSFLV, "https://", "http://", 1)
		}},
		{name: "missing selected url", mutate: func(_ *QualificationTicket, r *play.Result) { r.URLs.HTTPSFLV = nil }},
		{name: "ticket expired", mutate: func(ticket *QualificationTicket, _ *play.Result) {
			ticket.ExpiresAt = testApplicationNow.Add(-time.Second)
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ticket := baseTicket
			result := applicationResult(ticket)
			test.mutate(&ticket, result)
			provider := &qualificationFake{ticket: ticket}
			player := &playerFake{result: result}
			issuer := &grantFake{grants: []playauth.Grant{applicationGrant(testApplicationGrantA, "token")}}
			app := newTestApplication(provider, player, issuer, true)
			_, err := app.Apply(context.Background(), testApplyRequest(ticket, testApplicationGrantA))
			requireFixedApplicationError(t, err)
			require.Equal(t, 0, issuer.issueN)
			require.Equal(t, 1, issuer.cleanupN)
		})
	}
}

func TestLiveApplicationApplyLateQualificationAndIssueFailureCompensateOnlyGrant(t *testing.T) {
	ticket := applicationTicket(play.QualifiedProtocolHTTPSFLV)
	provider := &qualificationFake{ticket: ticket, validate: []error{nil, errors.New("late qualification")}}
	player := &playerFake{result: applicationResult(ticket)}
	issuer := &grantFake{grants: []playauth.Grant{applicationGrant(testApplicationGrantA, "token")}}
	app := newTestApplication(provider, player, issuer, true)
	_, err := app.Apply(context.Background(), testApplyRequest(ticket, testApplicationGrantA))
	requireFixedApplicationError(t, err)
	require.Equal(t, 2, provider.validateN)
	require.Equal(t, 1, player.ensureN)
	require.Equal(t, 0, issuer.issueN)
	require.Equal(t, 1, issuer.cleanupN)
	require.Equal(t, int64(81), issuer.cleanupClient)
	require.Equal(t, testApplicationGrantA, issuer.cleanupGrant)

	provider = &qualificationFake{ticket: ticket}
	player = &playerFake{result: applicationResult(ticket)}
	issuer = &grantFake{issueErr: errors.New("token signing secret"), grants: []playauth.Grant{applicationGrant(testApplicationGrantA, "token")}}
	app = newTestApplication(provider, player, issuer, true)
	_, err = app.Apply(context.Background(), testApplyRequest(ticket, testApplicationGrantA))
	requireFixedApplicationError(t, err)
	require.Equal(t, 1, issuer.issueN)
	require.Equal(t, 1, issuer.cleanupN)
	require.NotContains(t, err.Error(), "token signing secret")
}

func TestLiveApplicationCleanupUsesBoundedWithoutCancelContext(t *testing.T) {
	ticket := applicationTicket(play.QualifiedProtocolHTTPSFLV)
	provider := &qualificationFake{ticket: ticket}
	player := &playerFake{result: applicationResult(ticket)}
	issuer := &grantFake{issueErr: errors.New("issue"), cleanupErr: context.DeadlineExceeded}
	app := newTestApplication(provider, player, issuer, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	issuer.onIssue = cancel
	_, err := app.Apply(ctx, testApplyRequest(ticket, testApplicationGrantA))
	requireFixedApplicationError(t, err)
	require.Equal(t, 1, issuer.cleanupN)
	require.NotNil(t, issuer.cleanupCtx)
	deadline, ok := issuer.cleanupDL, issuer.cleanupHasDL
	require.True(t, ok)
	require.WithinDuration(t, time.Now().Add(time.Second), deadline, 2*time.Second)
	require.NoError(t, issuer.cleanupCtxErr)
}

func TestLiveApplicationDoesNotReleaseURLAfterCancellationDuringIssue(t *testing.T) {
	ticket := applicationTicket(play.QualifiedProtocolHTTPSFLV)
	provider := &qualificationFake{ticket: ticket}
	player := &playerFake{result: applicationResult(ticket)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	issuer := &grantFake{onIssue: cancel, grants: []playauth.Grant{applicationGrant(testApplicationGrantA, "unreleased-token")}}
	app := newTestApplication(provider, player, issuer, true)
	data, err := app.Apply(ctx, testApplyRequest(ticket, testApplicationGrantA))
	requireFixedApplicationError(t, err)
	require.Empty(t, data.URL)
	require.Equal(t, 1, issuer.cleanupN)
	require.NoError(t, issuer.cleanupCtxErr)
}
