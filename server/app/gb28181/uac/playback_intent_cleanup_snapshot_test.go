package uac

import (
	"context"
	"strings"
	"testing"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

func TestPlaybackIntentCleanupSnapshotMatchesStoredBranch(t *testing.T) {
	u, _, store, id, _ := playbackIntentStoreFixture(t)
	u.client.TxRequester = nil
	ctx := context.Background()
	request, prepared, err := u.prepareStoredPlaybackInvite(ctx, store, id, 2, strings.Repeat("b", 32), validPlaybackInvite())
	require.NoError(t, err)
	i := prepared.Steps[0].Identity
	_, err = store.DispatchSIPInviteStep(ctx, id, 3, i.StepID)
	require.NoError(t, err)
	response := playbackKnownBranchResponse(request)
	_, err = observeStoredPlaybackBranch(ctx, store, id, 4, i, request, response)
	require.NoError(t, err)
	dua := &sipgo.DialogUA{Client: u.client, ContactHDR: *request.Contact()}
	owner, err := dua.NewBranchCleanup(request, response, i.CSeq+1)
	require.NoError(t, err)
	defer owner.Terminate()
	ack, err := snapshotPlaybackCleanupRequest(owner.ACKRequest(), sip.ACK, i.StepID)
	require.NoError(t, err)
	bye, err := snapshotPlaybackCleanupRequest(owner.BYERequest(), sip.BYE, i.StepID)
	require.NoError(t, err)
	require.Equal(t, []string{"sip:proxy-second.example:5060;lr", "sip:proxy-first.example:5060;lr"}, ack.Routes)
	require.Equal(t, ack.Routes, bye.Routes)
	require.Equal(t, "fixture-remote", ack.RemoteTag)
	require.Equal(t, "proxy-second.example:5060", bye.Request.Destination)
	_, err = store.PrepareSIPBranchCleanup(ctx, id, 5, playauth.DeviceSIPCleanupAttemptIdentity{AttemptID: strings.Repeat("c", 32), ACK: ack, BYE: bye})
	require.NoError(t, err, "actual builder identity must satisfy the durable contract without repair")
}

func TestPlaybackIntentCleanupSnapshotRejectsExtraAuthority(t *testing.T) {
	for _, fault := range []string{"tag", "duplicate-tag", "extra-param", "auth", "body", "content-type", "method", "route-param"} {
		t.Run(fault, func(t *testing.T) {
			u, _, store, id, _ := playbackIntentStoreFixture(t)
			u.client.TxRequester = nil
			request, prepared, err := u.prepareStoredPlaybackInvite(context.Background(), store, id, 2, strings.Repeat("b", 32), validPlaybackInvite())
			require.NoError(t, err)
			dua := &sipgo.DialogUA{Client: u.client, ContactHDR: *request.Contact()}
			owner, err := dua.NewBranchCleanup(request, playbackKnownBranchResponse(request), prepared.Steps[0].Identity.CSeq+1)
			require.NoError(t, err)
			defer owner.Terminate()
			r := owner.ACKRequest()
			switch fault {
			case "tag":
				r.To().Params = nil
			case "duplicate-tag":
				r.To().Params = append(r.To().Params, sip.HeaderKV{K: "tag", V: "another"})
			case "extra-param":
				r.To().Params.Add("other", "value")
			case "auth":
				r.AppendHeader(sip.NewHeader("Authorization", "secret"))
			case "body":
				r.SetBody([]byte("unexpected"))
			case "content-type":
				r.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
			case "method":
				r.Method = sip.BYE
			case "route-param":
				r.AppendHeader(sip.NewHeader("Route", "<sip:other.example;lr>;tag=unexpected"))
			}
			_, err = snapshotPlaybackCleanupRequest(r, sip.ACK, prepared.Steps[0].Identity.StepID)
			require.ErrorIs(t, err, errPlaybackIntentSnapshot)
		})
	}
}
