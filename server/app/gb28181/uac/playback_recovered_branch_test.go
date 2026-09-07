package uac

import (
	"context"
	"strings"
	"testing"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

func TestPlaybackRecoveredBranchUsesDurableIdentityWithoutRequest(t *testing.T) {
	u, _, store, id, observer := playbackIntentStoreFixture(t)
	ctx := context.Background()
	request, prepared, err := u.prepareStoredPlaybackInvite(ctx, store, id, 2, strings.Repeat("b", 32), validPlaybackInvite())
	require.NoError(t, err)
	i := prepared.Steps[0].Identity
	_, err = store.DispatchSIPInviteStep(ctx, id, 3, i.StepID)
	require.NoError(t, err)
	response := playbackKnownBranchResponse(request)
	loaded, err := store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	// Only this actual response and strict durable identity cross the boundary;
	// no reconstructed request/body/transaction is supplied to the extractor.
	branch, err := snapshotRecoveredPlaybackBranch(loaded.Steps[0].Identity, response)
	require.NoError(t, err)
	out, err := store.ObserveSIPKnownBranch(ctx, id, 4, branch)
	require.NoError(t, err)
	require.Equal(t, "fixture-remote", out.Steps[0].KnownBranch.Identity.RemoteTag)
	require.Equal(t, []string{"sip:proxy-first.example:5060;lr", "sip:proxy-second.example:5060;lr"}, branch.RouteSet)
	require.Nil(t, observer.request)
	require.Empty(t, out.Steps[0].KnownBranch.CleanupAttempts)
	require.Equal(t, playauth.SIPStepPrepared, out.Steps[0].KnownBranch.ACKState)
}

func TestPlaybackRecoveredBranchRejectsWrongTransaction(t *testing.T) {
	u, _, store, id, _ := playbackIntentStoreFixture(t)
	request, prepared, err := u.prepareStoredPlaybackInvite(context.Background(), store, id, 2, strings.Repeat("b", 32), validPlaybackInvite())
	require.NoError(t, err)
	for _, fault := range []string{"call-id", "cseq", "method", "from", "to", "local-tag", "remote-tag", "via-branch", "via-host", "via-port", "via-transport", "duplicate-via", "duplicate-branch", "duplicate-contact", "status", "nil"} {
		t.Run(fault, func(t *testing.T) {
			response := playbackKnownBranchResponse(request)
			switch fault {
			case "call-id":
				*response.CallID() = "other"
			case "cseq":
				response.CSeq().SeqNo++
			case "method":
				response.CSeq().MethodName = sip.BYE
			case "from":
				response.From().Address.User = "other"
			case "to":
				response.To().Address.User = "other"
			case "local-tag":
				response.From().Params.Add("tag", "other")
			case "remote-tag":
				response.To().Params.Add("tag", "")
			case "via-branch":
				response.Via().Params.Add("branch", sip.GenerateBranch())
			case "via-host":
				response.Via().Host = "127.0.0.99"
			case "via-port":
				response.Via().Port++
			case "via-transport":
				response.Via().Transport = "UDP"
			case "duplicate-via":
				response.AppendHeader(response.Via().Clone())
			case "duplicate-branch":
				response.Via().Params = append(response.Via().Params, sip.HeaderKV{K: "branch", V: response.Via().Params.GetOr("branch", "")})
			case "duplicate-contact":
				response.AppendHeader(response.Contact().Clone())
			case "status":
				response.StatusCode = 300
			case "nil":
				response = nil
			}
			branch, err := snapshotRecoveredPlaybackBranch(prepared.Steps[0].Identity, response)
			require.Error(t, err)
			require.Empty(t, branch)
		})
	}
}
