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

func playbackKnownBranchResponse(request *sip.Request) *sip.Response {
	response := sip.NewResponseFromRequest(request, 200, "OK", nil)
	response.To().Params.Add("tag", "fixture-remote")
	response.AppendHeader(&sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: "device", Host: "127.0.0.1", Port: 5062}})
	for _, host := range []string{"proxy-first.example", "proxy-second.example"} {
		params := sip.NewParams()
		params.Add("lr", "")
		response.AppendHeader(&sip.RecordRouteHeader{Address: sip.Uri{Scheme: "sip", Host: host, Port: 5060, UriParams: params}})
	}
	return response
}

func TestPlaybackIntentKnownBranchExactResponsePersistence(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	u, db, store, id, observer := playbackIntentStoreFixture(t)
	ctx := context.Background()
	request, prepared, err := u.prepareStoredPlaybackInvite(ctx, store, id, 2, strings.Repeat("b", 32), validPlaybackInvite())
	require.NoError(t, err)
	_, err = store.DispatchSIPInviteStep(ctx, id, 3, prepared.Steps[0].Identity.StepID)
	require.NoError(t, err)
	response := playbackKnownBranchResponse(request)
	// Exercise the actual SIP parser, including multiple Record-Route headers.
	parsed, err := sip.ParseMessage([]byte(response.String()))
	require.NoError(t, err)
	out, err := observeStoredPlaybackBranch(ctx, store, id, 4, prepared.Steps[0].Identity, request, parsed.(*sip.Response))
	require.NoError(t, err)
	i := out.Steps[0].KnownBranch.Identity
	require.Equal(t, request.From().Address.String(), prepared.Steps[0].Identity.FromURI)
	require.Equal(t, []string{"sip:proxy-first.example:5060;lr", "sip:proxy-second.example:5060;lr"}, i.RouteSet)
	require.Equal(t, "sip:device@127.0.0.1:5062", i.RemoteTarget)
	require.Equal(t, "fixture-remote", i.RemoteTag)
	require.Equal(t, prepared.Steps[0].Identity.CallID, i.CallID)
	require.Equal(t, playauth.SIPStepPrepared, out.Steps[0].KnownBranch.ACKState)
	require.Nil(t, observer.request, "observing a response never starts another transaction")
	loaded, err := playauth.NewDeviceOperationIntentStore(db).LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, out, loaded)
}

func TestPlaybackIntentKnownBranchRejectsMismatchedActualMaterials(t *testing.T) {
	for _, fault := range []string{"nil-request", "nil-response", "request-body", "request-destination", "call-id", "cseq", "method", "from-uri", "to-uri", "local-tag", "empty-remote-tag", "duplicate-remote-tag", "duplicate-local-tag", "duplicate-contact", "missing-contact", "secret-contact", "duplicate-call-id", "status", "generic-route"} {
		t.Run(fault, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}

			u, _, store, id, observer := playbackIntentStoreFixture(t)
			ctx := context.Background()
			request, prepared, err := u.prepareStoredPlaybackInvite(ctx, store, id, 2, strings.Repeat("b", 32), validPlaybackInvite())
			require.NoError(t, err)
			_, err = store.DispatchSIPInviteStep(ctx, id, 3, prepared.Steps[0].Identity.StepID)
			require.NoError(t, err)
			response := playbackKnownBranchResponse(request)
			switch fault {
			case "nil-request":
				request = nil
			case "nil-response":
				response = nil
			case "request-body":
				request.SetBody([]byte("different SDP"))
			case "request-destination":
				request.SetDestination("127.0.0.2:5060")
			case "call-id":
				*response.CallID() = "different"
			case "cseq":
				response.CSeq().SeqNo++
			case "method":
				response.CSeq().MethodName = sip.BYE
			case "from-uri":
				response.From().Address.User = "different"
			case "to-uri":
				response.To().Address.User = "different"
			case "local-tag":
				response.From().Params.Add("tag", "different")
			case "empty-remote-tag":
				response.To().Params.Add("tag", "")
			case "duplicate-remote-tag":
				response.To().Params = append(response.To().Params, sip.HeaderKV{K: "tag", V: "another"})
			case "duplicate-local-tag":
				response.From().Params = append(response.From().Params, sip.HeaderKV{K: "tag", V: "another"})
			case "duplicate-contact":
				response.AppendHeader(response.Contact().Clone())
			case "missing-contact":
				response.RemoveHeader("Contact")
			case "secret-contact":
				response.Contact().Address.Password = "must-not-persist"
			case "duplicate-call-id":
				response.AppendHeader(sip.NewHeader("Call-ID", "different"))
			case "status":
				response.StatusCode = 300
			case "generic-route":
				response.AppendHeader(sip.NewHeader("Record-Route", "unparsed or unsupported"))
			}
			out, err := observeStoredPlaybackBranch(ctx, store, id, 4, prepared.Steps[0].Identity, request, response)
			require.Error(t, err)
			require.Empty(t, out.Steps)
			require.Nil(t, observer.request)
			loaded, err := store.LoadSIPInviteSteps(ctx, id)
			require.NoError(t, err)
			require.Nil(t, loaded.Steps[0].KnownBranch)
		})
	}
}
