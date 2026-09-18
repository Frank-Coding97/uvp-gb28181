package uac

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
)

type snapshotTransactionObserver struct {
	request *sip.Request
}

var errSnapshotTransactionObserved = errors.New("isolated transaction observed without network")

func (o *snapshotTransactionObserver) Request(_ context.Context, req *sip.Request) (sip.ClientTransaction, error) {
	o.request = req.Clone()
	return nil, errSnapshotTransactionObserved
}

func TestPlaybackIntentSnapshotMatchesPreTransactionRequest(t *testing.T) {
	ua, err := sipgo.NewUA()
	require.NoError(t, err)
	defer ua.Close()
	u, err := New(ua, "34020000002000000001", "3402000000", "192.0.2.1", 5061, false)
	require.NoError(t, err)
	observer := &snapshotTransactionObserver{}
	u.client.TxRequester = observer
	request, metadata, err := u.buildPlaybackInviteRequest(validPlaybackInvite())
	require.NoError(t, err)
	require.Nil(t, request.To(), "the existing application builder is not the final transaction request")
	original := request.String()
	_, err = snapshotPlaybackIntentRequest(request)
	require.Error(t, err, "extraction alone cannot build missing fields")
	require.Equal(t, original, request.String())
	localTag, _ := request.From().Params.Get("tag")
	branch, _ := request.Via().Params.Get("branch")
	prepared, snapshot, err := u.preparePlaybackIntentSnapshot(request)
	require.NoError(t, err)
	require.Nil(t, observer.request, "preparation must not invoke the transaction layer")
	require.Equal(t, original, request.String(), "preparation must not mutate the owner's request")
	require.NotNil(t, prepared.To())
	require.Equal(t, metadata.CallID, snapshot.callID)
	require.Equal(t, metadata.CSeq, snapshot.cseq)
	require.Equal(t, localTag, snapshot.localTag)
	require.Equal(t, branch, snapshot.branch)
	require.Equal(t, request.Contact().Address.String(), snapshot.contactURI)
	require.Equal(t, request.Recipient.String(), snapshot.requestURI)
	require.Equal(t, len(request.Body()), snapshot.bodyLength)
	before := prepared.String()
	_, err = u.client.TransactionRequest(context.Background(), prepared)
	require.ErrorIs(t, err, errSnapshotTransactionObserved)
	require.NotNil(t, observer.request)
	require.Equal(t, before, observer.request.String(), "the second sipgo build must not rewrite the prepared request")
	observed, err := snapshotPlaybackIntentRequest(observer.request)
	require.NoError(t, err)
	require.Equal(t, snapshot, observed)
	encoded, err := json.Marshal(snapshot)
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(encoded), "internal snapshot must not become an API payload")
	request.Body()[0] ^= 1
	request.From().Params.Add("tag", "later-owner-mutation")
	request.Via().Params.Add("branch", "later-owner-mutation")
	request.Contact().Address.Host = "192.0.2.200"
	unchanged, err := snapshotPlaybackIntentRequest(prepared)
	require.NoError(t, err)
	require.Equal(t, snapshot, unchanged, "sipgo Clone shares Body; preparation must copy it explicitly")
}

func TestPlaybackIntentSnapshotRejectsUnfixedIdentity(t *testing.T) {
	ua, err := sipgo.NewUA()
	require.NoError(t, err)
	defer ua.Close()
	u, err := New(ua, "34020000002000000001", "3402000000", "192.0.2.1", 5061, false)
	require.NoError(t, err)
	observer := &snapshotTransactionObserver{}
	u.client.TxRequester = observer
	for _, field := range []string{"nil", "method", "Call-ID", "From", "Via", "Contact", "CSeq", "from-tag", "branch", "contact-host", "via-host", "contact-port", "via-port", "destination", "transport", "cseq-zero", "cseq-method", "duplicate-call-id"} {
		t.Run(field, func(t *testing.T) {
			request, _, err := u.buildPlaybackInviteRequest(validPlaybackInvite())
			require.NoError(t, err)
			switch field {
			case "nil":
				request = nil
			case "method":
				request.Method = sip.INFO
			case "from-tag":
				request.From().Params.Remove("tag")
			case "branch":
				request.Via().Params.Remove("branch")
			case "contact-host":
				request.Contact().Address.Host = ""
			case "via-host":
				request.Via().Host = ""
			case "contact-port":
				request.Contact().Address.Port = 0
			case "via-port":
				request.Via().Port = 0
			case "destination":
				request.SetDestination("")
			case "transport":
				request.SetTransport("")
			case "cseq-zero":
				request.CSeq().SeqNo = 0
			case "cseq-method":
				request.CSeq().MethodName = sip.INFO
			case "duplicate-call-id":
				request.AppendHeader(sip.HeaderClone(request.CallID()))
			default:
				require.True(t, request.RemoveHeader(field), "fault injection must actually remove the exact header")
			}
			prepared, snapshot, err := u.preparePlaybackIntentSnapshot(request)
			require.Error(t, err)
			require.Nil(t, prepared)
			require.Equal(t, playbackIntentSnapshot{}, snapshot)
			require.Nil(t, observer.request)
		})
	}
}
