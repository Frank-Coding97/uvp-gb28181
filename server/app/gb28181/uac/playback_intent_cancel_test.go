package uac

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/internal/authoritytest"
)

func TestPlaybackIntentCancelMatchesOwnedPreparation(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	u, db, store, id, _ := playbackIntentStoreFixture(t)
	u.client.TxRequester = nil
	ctx := context.Background()
	peer, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer peer.Close()
	in := validPlaybackInvite()
	in.Destination, in.Transport = peer.LocalAddr().String(), "UDP"
	request, stored, err := u.prepareStoredPlaybackInvite(ctx, store, id, 2, strings.Repeat("b", 32), in)
	require.NoError(t, err)
	_, err = store.DispatchSIPInviteStep(ctx, id, 3, stored.Steps[0].Identity.StepID)
	require.NoError(t, err)
	dua := &sipgo.DialogUA{Client: u.client, ContactHDR: *request.Contact()}
	owned, err := dua.PrepareWriteInviteOwned(ctx, request)
	require.NoError(t, err)
	defer owned.Terminate()
	require.NoError(t, owned.Start())
	cancel, err := owned.PrepareCancel(ctx)
	require.NoError(t, err)
	prepared, err := prepareStoredPlaybackCancel(ctx, store, id, 4, stored.Steps[0].Identity, cancel)
	require.NoError(t, err)
	require.NotNil(t, prepared.Steps[0].Cancel)
	require.Equal(t, playauth.SIPStepPrepared, prepared.Steps[0].Cancel.State)
	require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	_, err = store.DispatchSIPCancel(ctx, id, 5, prepared.Steps[0].Cancel.Identity)
	require.NoError(t, err)
	for _, change := range []func(*sip.Request){
		func(r *sip.Request) { r.CSeq().SeqNo++ },
		func(r *sip.Request) { r.Method = sip.INVITE },
		func(r *sip.Request) { r.To().Params.Add("tag", "not-the-original") },
		func(r *sip.Request) { r.Contact().Params.Add("expires", "5") },
		func(r *sip.Request) { r.SetDestination("127.0.0.1:9999") },
		func(r *sip.Request) { r.Via().Params.Add("branch", "z9hG4bK-other") },
		func(r *sip.Request) { r.AppendHeader(sip.NewHeader("Authorization", "fixture")) },
		func(r *sip.Request) { r.AppendHeader(sip.NewHeader("Content-Type", "application/sdp")) },
		func(r *sip.Request) { r.SetBody([]byte("forbidden")) },
		func(r *sip.Request) { r.AppendHeader(sip.HeaderClone(r.From())) },
		func(r *sip.Request) { r.From().Params = append(r.From().Params, sip.HeaderKV{K: "tag", V: "another"}) },
		func(r *sip.Request) {
			r.Via().Params = append(r.Via().Params, sip.HeaderKV{K: "branch", V: "z9hG4bK-another"})
		},
		func(r *sip.Request) { r.AppendHeader(sip.NewHeader("Route", "<sip:other@127.0.0.1>")) },
	} {
		r := cancel.Clone()
		change(r)
		out, err := prepareStoredPlaybackCancel(ctx, store, id, 6, stored.Steps[0].Identity, r)
		require.Error(t, err)
		require.Empty(t, out)
	}
	owned.Terminate()
	select {
	case <-owned.Quiesced():
	case <-time.After(time.Second):
		t.Fatal("owned fixture still active")
	}
}
