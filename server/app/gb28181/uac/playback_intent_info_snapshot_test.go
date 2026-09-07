package uac

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
)

func TestPlaybackIntentINFOSnapshotPreservesExistingCommands(t *testing.T) {
	for _, input := range []PlaybackInfoRequest{
		{Action: PlaybackInfoPlay}, {Action: PlaybackInfoPause}, {Action: PlaybackInfoResume},
		{Action: PlaybackInfoSeek, Position: 5 * time.Second, SegmentDuration: time.Minute},
		{Action: PlaybackInfoScale, Scale: 2}, {Action: PlaybackInfoTeardown},
	} {
		t.Run(string(input.Action), func(t *testing.T) {
			u, _, store, id, _ := playbackIntentStoreFixture(t)
			u.client.TxRequester = nil
			request, stored, err := u.prepareStoredPlaybackInvite(context.Background(), store, id, 2, strings.Repeat("b", 32), validPlaybackInvite())
			require.NoError(t, err)
			seq := request.CSeq().SeqNo + 3
			body, err := buildPlaybackInfoBody(input, seq)
			require.NoError(t, err)
			dua := &sipgo.DialogUA{Client: u.client, ContactHDR: *request.Contact()}
			info, err := dua.BuildDialogINFO(request, playbackKnownBranchResponse(request), seq, "Application/MANSRTSP", body)
			require.NoError(t, err)
			snapshot, err := snapshotPlaybackINFORequest(info, stored.Steps[0].Identity.StepID)
			require.NoError(t, err)
			require.Equal(t, seq, snapshot.Request.CSeq)
			require.Equal(t, "Application/MANSRTSP", snapshot.Request.ContentType)
			require.Equal(t, fmt.Sprintf("%x", sha256.Sum256(body)), snapshot.Request.BodySHA256)
			require.Equal(t, len(body), snapshot.Request.BodyLength)
			require.Equal(t, "fixture-remote", snapshot.RemoteTag)
			require.Equal(t, []string{"sip:proxy-second.example:5060;lr", "sip:proxy-first.example:5060;lr"}, snapshot.Routes)
		})
	}
}

func TestPlaybackIntentINFOSnapshotRejectsExtraAuthority(t *testing.T) {
	for _, fault := range []string{"type", "body", "large-body", "auth", "duplicate-type", "tag", "method", "route"} {
		t.Run(fault, func(t *testing.T) {
			u, _, store, id, _ := playbackIntentStoreFixture(t)
			u.client.TxRequester = nil
			request, _, err := u.prepareStoredPlaybackInvite(context.Background(), store, id, 2, strings.Repeat("b", 32), validPlaybackInvite())
			require.NoError(t, err)
			body, err := buildPlaybackInfoBody(PlaybackInfoRequest{Action: PlaybackInfoPause}, request.CSeq().SeqNo+1)
			require.NoError(t, err)
			dua := &sipgo.DialogUA{Client: u.client, ContactHDR: *request.Contact()}
			info, err := dua.BuildDialogINFO(request, playbackKnownBranchResponse(request), request.CSeq().SeqNo+1, "Application/MANSRTSP", body)
			require.NoError(t, err)
			switch fault {
			case "type":
				info.ReplaceHeader(sip.NewHeader("Content-Type", "application/sdp"))
			case "body":
				info.SetBody(nil)
			case "large-body":
				info.SetBody(make([]byte, 4097))
			case "auth":
				info.AppendHeader(sip.NewHeader("Authorization", "secret"))
			case "duplicate-type":
				info.AppendHeader(sip.NewHeader("Content-Type", "Application/MANSRTSP"))
			case "tag":
				info.To().Params = nil
			case "method":
				info.Method = sip.BYE
			case "route":
				info.AppendHeader(sip.NewHeader("Route", "<sip:proxy.example;lr>;extra=value"))
			}
			_, err = snapshotPlaybackINFORequest(info, strings.Repeat("b", 32))
			require.ErrorIs(t, err, errPlaybackIntentSnapshot)
		})
	}
}
