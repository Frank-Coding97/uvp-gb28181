package uac

import (
	"context"
	"strings"
	"testing"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/internal/authoritytest"
)

func TestPlaybackRecoveryMaterialMatchesDurableCleanup(t *testing.T) {
	for _, transport := range []string{"UDP", "TCP"} {
		for _, shape := range []string{"direct", "direct-ipv6", "default-port", "loose", "strict"} {
			for _, extra := range []bool{false, true} {
				name := transport + "/" + shape
				if extra {
					name += "/additional"
				}
				t.Run(name, func(t *testing.T) {
					if !authoritytest.InProcess(t) {
						return
					}

					u, _, store, id, _ := playbackIntentStoreFixture(t)
					u.client.TxRequester = nil
					ctx := context.Background()
					input := validPlaybackInvite()
					input.Transport = transport
					request, prepared, err := u.prepareStoredPlaybackInvite(ctx, store, id, 2, strings.Repeat("b", 32), input)
					require.NoError(t, err)
					i := prepared.Steps[0].Identity
					request.SetSource("198.51.100.222:5999") // Not durable routing/authority.
					_, err = store.DispatchSIPInviteStep(ctx, id, 3, i.StepID)
					require.NoError(t, err)
					response := playbackKnownBranchResponse(request)
					if shape == "direct" || shape == "direct-ipv6" || shape == "default-port" {
						for len(response.GetHeaders("Record-Route")) != 0 {
							response.RemoveHeader("Record-Route")
						}
					}
					if shape == "direct-ipv6" {
						response.Contact().Address.Host = "::1"
					}
					if shape == "default-port" {
						response.Contact().Address.Port = 0
					}
					if shape == "strict" {
						routes := response.GetHeaders("Record-Route")
						routes[len(routes)-1].(*sip.RecordRouteHeader).Address.UriParams = nil
					}
					stored, err := observeStoredPlaybackBranch(ctx, store, id, 4, i, request, response)
					require.NoError(t, err)
					tag := "fixture-remote"
					if extra {
						tag = "extra-remote"
						response.To().Params.Add("tag", tag)
						stored, err = observeStoredPlaybackBranchWith(ctx, store, id, stored.Intent.RowVersion, i, request, response, true)
						require.NoError(t, err)
					}
					owner, loaded, err := u.prepareRecoveredPlaybackCleanup(ctx, store, id, i.StepID, tag)
					require.NoError(t, err)
					defer owner.Terminate()
					require.Equal(t, stored, loaded, "pure preparation does not mutate the ledger")
					require.Nil(t, owner.Transaction(), "no connection or transaction is created")
					require.Error(t, owner.WriteACK())
					require.Error(t, owner.StartBYE())
					ack, err := snapshotPlaybackCleanupRequest(owner.ACKRequest(), sip.ACK, i.StepID)
					require.NoError(t, err)
					bye, err := snapshotPlaybackCleanupRequest(owner.BYERequest(), sip.BYE, i.StepID)
					require.NoError(t, err)
					require.Equal(t, i.CSeq, ack.Request.CSeq)
					require.Equal(t, i.CSeq+1, bye.Request.CSeq)
					require.Equal(t, tag, ack.RemoteTag)
					require.NotEqual(t, request.Source(), owner.BYERequest().Source(), "never recover the old transport's source handle")
					attempted, ticket, err := store.PrepareSIPBranchCleanupWork(ctx, id, loaded.Intent.RowVersion,
						playauth.DeviceSIPCleanupAttemptIdentity{AttemptID: strings.Repeat("c", 32), ACK: ack, BYE: bye})
					require.NoError(t, err, "fresh durable matcher independently binds the constructed material")
					require.NotNil(t, ticket)
					require.Nil(t, owner.Transaction(), "a store ticket does not start networking")
					owner.Terminate()
					<-owner.Quiesced()
					_, err = store.ObserveSIPCleanupQuiesced(ctx, id, attempted.Intent.RowVersion, strings.Repeat("c", 32))
					require.NoError(t, err)
					next, loaded, err := u.prepareRecoveredPlaybackCleanup(ctx, store, id, i.StepID, tag)
					require.NoError(t, err)
					defer next.Terminate()
					nextACK, err := snapshotPlaybackCleanupRequest(next.ACKRequest(), sip.ACK, i.StepID)
					require.NoError(t, err)
					nextBYE, err := snapshotPlaybackCleanupRequest(next.BYERequest(), sip.BYE, i.StepID)
					require.NoError(t, err)
					require.Equal(t, i.CSeq+2, nextBYE.Request.CSeq, "new attempt follows the prepared predecessor, never reuses its CSeq")
					_, ticket, err = store.PrepareSIPBranchCleanupWork(ctx, id, loaded.Intent.RowVersion,
						playauth.DeviceSIPCleanupAttemptIdentity{AttemptID: strings.Repeat("d", 32), ACK: nextACK, BYE: nextBYE})
					require.NoError(t, err)
					require.NotNil(t, ticket)
				})
			}
		}
	}
}

func TestPlaybackRecoveryMaterialMissingEvidenceDoesNotRepair(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	u, db, store, id, _ := playbackIntentStoreFixture(t)
	u.client.TxRequester = nil
	ctx := context.Background()
	_, prepared, err := u.prepareStoredPlaybackInvite(ctx, store, id, 2, strings.Repeat("b", 32), validPlaybackInvite())
	require.NoError(t, err)
	i := prepared.Steps[0].Identity
	for _, tag := range []string{"", "not-observed"} {
		owner, _, err := u.prepareRecoveredPlaybackCleanup(ctx, store, id, i.StepID, tag)
		require.Error(t, err)
		require.Nil(t, owner)
	}
	require.NoError(t, db.Exec("UPDATE gb_device_operation_intent SET sip_steps_json='{}'").Error)
	owner, _, err := u.prepareRecoveredPlaybackCleanup(ctx, store, id, i.StepID, "not-observed")
	require.Error(t, err)
	require.Nil(t, owner)
}

func TestPlaybackRecoveryMaterialEqualURIsRemainIndependent(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	u, _, store, id, _ := playbackIntentStoreFixture(t)
	_, prepared, err := u.prepareStoredPlaybackInvite(context.Background(), store, id, 2, strings.Repeat("b", 32), validPlaybackInvite())
	require.NoError(t, err)
	i := prepared.Steps[0].Identity
	i.FromURI, i.ToURI = i.ContactURI, i.ContactURI
	b := playauth.DeviceSIPKnownBranchIdentity{RemoteTarget: i.ContactURI, RemoteTag: "remote", RouteSet: []string{}}
	r, err := recoveredPlaybackCleanupRequest(i, b, sip.ACK, i.CSeq)
	require.NoError(t, err)
	require.Equal(t, i.ContactURI, r.Recipient.String())
	require.Equal(t, i.ContactURI, r.From().Address.String())
	require.Equal(t, i.ContactURI, r.To().Address.String())
	require.Equal(t, i.ContactURI, r.Contact().Address.String())
}
