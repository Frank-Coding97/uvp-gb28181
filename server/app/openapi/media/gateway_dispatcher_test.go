package media

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/openapi/auth"
)

func gatewayTestTarget(protocol string) auth.MediaTarget {
	return auth.MediaTarget{
		DeviceID:  testApplicationDevice,
		ChannelID: testApplicationChannel,
		Protocol:  protocol,
	}
}

func gatewayTestDispatcher(t *testing.T, enabled bool) (*GatewayDispatcher, *qualificationFake, *playerFake, *grantFake, auth.MediaTarget) {
	return gatewayTestDispatcherWithLifetimes(t, enabled, play.QualifiedProtocolHTTPSFLV, 2*time.Minute, time.Minute)
}

func gatewayTestDispatcherWithLifetimes(t *testing.T, enabled bool, protocol string, ticketTTL, grantTTL time.Duration) (*GatewayDispatcher, *qualificationFake, *playerFake, *grantFake, auth.MediaTarget) {
	t.Helper()
	ticket := applicationTicket(protocol)
	ticket.ExpiresAt = testApplicationNow.Add(ticketTTL)
	provider := &qualificationFake{ticket: ticket}
	player := &playerFake{result: applicationResult(ticket)}
	grant := applicationGrant(testApplicationGrantA, "token-a")
	grant.ExpiresAt = testApplicationNow.Add(grantTTL)
	issuer := &grantFake{grants: []playauth.Grant{grant}}
	application := newTestApplication(provider, player, issuer, enabled)
	return NewGatewayDispatcher(application), provider, player, issuer, gatewayTestTarget(protocol)
}

func TestGatewayDispatcherPrepareAndApplyPreserveExactEnvelope(t *testing.T) {
	dispatcher, provider, player, issuer, target := gatewayTestDispatcher(t, true)

	require.True(t, dispatcher.Ready())
	ticket, err := dispatcher.Prepare(context.Background(), target)
	require.NoError(t, err)
	require.NotEmpty(t, ticket)
	require.LessOrEqual(t, len(ticket), maxGatewayTicketBytes)
	require.Equal(t, 1, provider.prepareN)
	require.Equal(t, 0, player.ensureN)
	require.Equal(t, 0, issuer.issueN)
	require.Equal(t, 0, issuer.cleanupN)

	var envelope gatewayTicketEnvelope
	require.NoError(t, json.Unmarshal([]byte(ticket), &envelope))
	require.Equal(t, uint8(1), envelope.Version)
	original := applicationTicket(target.Protocol)
	require.Equal(t, target, envelope.Target)
	require.Equal(t, original, envelope.Ticket)

	authorization, err := dispatcher.Apply(context.Background(), auth.MediaAdmittedRequest{
		ClientID:    81,
		GrantID:     testApplicationGrantA,
		DeviceEpoch: 3,
		Target:      target,
		Ticket:      ticket,
	})
	require.NoError(t, err)
	require.Equal(t, testApplicationGrantA, authorization.AuthorizationID)
	require.Equal(t, target.Protocol, authorization.Protocol)
	require.Contains(t, authorization.URL, "token-a")
	require.Equal(t, testApplicationNow.Add(time.Minute), authorization.ExpiresAt)
	require.Equal(t, 1, player.ensureN)
	require.Equal(t, int64(3), player.request.DeviceEpoch)
	require.Equal(t, 1, issuer.issueN)
	require.Equal(t, 0, issuer.cleanupN)
	require.Equal(t, target.DeviceID, issuer.issueRequests[0].DeviceID)
	require.Equal(t, target.ChannelID, issuer.issueRequests[0].ChannelID)
	require.Equal(t, target.Protocol, issuer.issueRequests[0].Protocol)
}

func TestGatewayDispatcherShortQualificationDoesNotShortenGrant(t *testing.T) {
	dispatcher, _, _, issuer, target := gatewayTestDispatcherWithLifetimes(t, true, play.QualifiedProtocolHTTPSFLV, 10*time.Second, 120*time.Second)
	ticket, err := dispatcher.Prepare(context.Background(), target)
	require.NoError(t, err)

	authorization, err := dispatcher.Apply(context.Background(), auth.MediaAdmittedRequest{
		ClientID:    81,
		GrantID:     testApplicationGrantA,
		DeviceEpoch: 3,
		Target:      target,
		Ticket:      ticket,
	})
	require.NoError(t, err)
	require.Equal(t, testApplicationNow.Add(120*time.Second), authorization.ExpiresAt)
	require.Equal(t, 1, issuer.issueN)
}

func TestGatewayDispatcherAppliesWSSAuthorization(t *testing.T) {
	dispatcher, _, _, _, target := gatewayTestDispatcherWithLifetimes(t, true, play.QualifiedProtocolWSSFLV, 10*time.Second, 120*time.Second)
	ticket, err := dispatcher.Prepare(context.Background(), target)
	require.NoError(t, err)

	authorization, err := dispatcher.Apply(context.Background(), auth.MediaAdmittedRequest{
		ClientID:    81,
		GrantID:     testApplicationGrantA,
		DeviceEpoch: 3,
		Target:      target,
		Ticket:      ticket,
	})
	require.NoError(t, err)
	require.Equal(t, play.QualifiedProtocolWSSFLV, authorization.Protocol)
	require.Contains(t, authorization.URL, "wss://media.example:8443/")
}

func TestGatewayDispatcherRejectsBadTicketAndTargetWithCompensation(t *testing.T) {
	tests := []struct {
		name   string
		ticket func(auth.MediaTicket) auth.MediaTicket
		target func(auth.MediaTarget) auth.MediaTarget
	}{
		{name: "malformed", ticket: func(auth.MediaTicket) auth.MediaTicket { return "not-json" }},
		{name: "oversize", ticket: func(auth.MediaTicket) auth.MediaTicket {
			return auth.MediaTicket(strings.Repeat("x", maxGatewayTicketBytes+1))
		}},
		{name: "target-rebind", target: func(target auth.MediaTarget) auth.MediaTarget {
			target.DeviceID = "34020000002000000002"
			return target
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dispatcher, _, player, issuer, target := gatewayTestDispatcher(t, true)
			ticket, err := dispatcher.Prepare(context.Background(), target)
			require.NoError(t, err)
			if test.ticket != nil {
				ticket = test.ticket(ticket)
			}
			if test.target != nil {
				target = test.target(target)
			}

			_, err = dispatcher.Apply(context.Background(), auth.MediaAdmittedRequest{
				ClientID:    81,
				GrantID:     testApplicationGrantA,
				DeviceEpoch: 3,
				Target:      target,
				Ticket:      ticket,
			})
			requireFixedApplicationError(t, err)
			require.Equal(t, 0, player.ensureN)
			require.Equal(t, 0, issuer.issueN)
			require.Equal(t, 1, issuer.cleanupN)
			require.Equal(t, int64(81), issuer.cleanupClient)
			require.Equal(t, testApplicationGrantA, issuer.cleanupGrant)
			require.NoError(t, issuer.cleanupCtxErr)
		})
	}
}

func TestGatewayDispatcherRejectsInvalidEnvelopeWithCompensation(t *testing.T) {
	mutateEnvelope := func(t *testing.T, raw auth.MediaTicket, mutate func(*gatewayTicketEnvelope)) auth.MediaTicket {
		t.Helper()
		var envelope gatewayTicketEnvelope
		require.NoError(t, json.Unmarshal([]byte(raw), &envelope))
		mutate(&envelope)
		encoded, err := json.Marshal(envelope)
		require.NoError(t, err)
		return auth.MediaTicket(string(encoded))
	}
	tests := []struct {
		name   string
		mutate func(*testing.T, auth.MediaTicket) auth.MediaTicket
	}{
		{name: "unknown field", mutate: func(_ *testing.T, raw auth.MediaTicket) auth.MediaTicket {
			return auth.MediaTicket(strings.TrimSuffix(string(raw), "}") + `,"unknown":true}`)
		}},
		{name: "trailing json", mutate: func(_ *testing.T, raw auth.MediaTicket) auth.MediaTicket {
			return auth.MediaTicket(string(raw) + `{"trailing":true}`)
		}},
		{name: "version mismatch", mutate: func(t *testing.T, raw auth.MediaTicket) auth.MediaTicket {
			return mutateEnvelope(t, raw, func(envelope *gatewayTicketEnvelope) { envelope.Version++ })
		}},
		{name: "protocol mismatch", mutate: func(t *testing.T, raw auth.MediaTicket) auth.MediaTicket {
			return mutateEnvelope(t, raw, func(envelope *gatewayTicketEnvelope) { envelope.Ticket.Protocol = play.QualifiedProtocolWSSFLV })
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dispatcher, _, player, issuer, target := gatewayTestDispatcher(t, true)
			ticket, err := dispatcher.Prepare(context.Background(), target)
			require.NoError(t, err)
			ticket = test.mutate(t, ticket)

			_, err = dispatcher.Apply(context.Background(), auth.MediaAdmittedRequest{
				ClientID:    81,
				GrantID:     testApplicationGrantA,
				DeviceEpoch: 3,
				Target:      target,
				Ticket:      ticket,
			})
			requireFixedApplicationError(t, err)
			require.Equal(t, 0, player.ensureN)
			require.Equal(t, 0, issuer.issueN)
			require.Equal(t, 1, issuer.cleanupN)
			require.Equal(t, int64(81), issuer.cleanupClient)
			require.Equal(t, testApplicationGrantA, issuer.cleanupGrant)
		})
	}
}

func TestGatewayDispatcherReadinessRequiresAllDependencies(t *testing.T) {
	target := gatewayTestTarget(play.QualifiedProtocolHTTPSFLV)
	tests := []struct {
		name string
		app  *LiveApplication
	}{
		{name: "nil application", app: nil},
		{name: "disabled", app: func() *LiveApplication {
			provider := &qualificationFake{ticket: applicationTicket(target.Protocol)}
			return newTestApplication(provider, &playerFake{}, &grantFake{}, false)
		}()},
		{name: "typed nil provider", app: func() *LiveApplication {
			var provider *qualificationFake
			return newTestApplication(provider, &playerFake{}, &grantFake{}, true)
		}()},
		{name: "typed nil player", app: func() *LiveApplication {
			var player *playerFake
			return newTestApplication(&qualificationFake{ticket: applicationTicket(target.Protocol)}, player, &grantFake{}, true)
		}()},
		{name: "typed nil grants", app: func() *LiveApplication {
			var grants *grantFake
			return newTestApplication(&qualificationFake{ticket: applicationTicket(target.Protocol)}, &playerFake{}, grants, true)
		}()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dispatcher := NewGatewayDispatcher(test.app)
			require.False(t, dispatcher.Ready())
			_, err := dispatcher.Prepare(context.Background(), target)
			requireFixedApplicationError(t, err)
		})
	}
}

func TestGatewayDispatcherCanceledApplyCompensatesAdmission(t *testing.T) {
	dispatcher, _, player, issuer, target := gatewayTestDispatcher(t, true)
	ticket, err := dispatcher.Prepare(context.Background(), target)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = dispatcher.Apply(ctx, auth.MediaAdmittedRequest{
		ClientID:    81,
		GrantID:     testApplicationGrantA,
		DeviceEpoch: 3,
		Target:      target,
		Ticket:      ticket,
	})
	requireFixedApplicationError(t, err)
	require.Equal(t, 0, player.ensureN)
	require.Equal(t, 0, issuer.issueN)
	require.Equal(t, 1, issuer.cleanupN)
	require.NoError(t, issuer.cleanupCtxErr)
}
