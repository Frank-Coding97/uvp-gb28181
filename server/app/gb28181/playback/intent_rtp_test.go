package playback

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type intentRTPTestResolver struct{ control *zlm.OpenAPIRuntimeControl }

func (r intentRTPTestResolver) ResolveRTP(context.Context, string) (IntentRTPRuntime, error) {
	return IntentRTPRuntime{Control: r.control, BootNonce: strings.Repeat("a", 32), Release: r.control.Close}, nil
}

func TestPlaybackIntentRTPUsesActualPinnedCallsAndDurableFixedIdentity(t *testing.T) {
	db, barrier, req := playbackEpochFixture(t)
	require.NoError(t, db.Exec("CREATE TABLE gb_channel (id INTEGER PRIMARY KEY, device_id TEXT, channel_id TEXT, deleted_at DATETIME)").Error)
	require.NoError(t, db.Exec("INSERT INTO gb_channel VALUES(2,?,?,NULL)", req.DeviceID, req.SIPChannelID).Error)
	require.NoError(t, db.Exec("ALTER TABLE gb_device_operation_intent ADD COLUMN rtp_steps_json TEXT NULL").Error)
	store := playauth.NewDeviceOperationIntentStore(db)
	o, err := newPlaybackIntentOwner(context.Background(), store, barrier, req)
	require.NoError(t, err)
	require.NoError(t, o.begin(context.Background()))
	t.Cleanup(func() { o.cancel(); o.lease.Release() })
	var mu sync.Mutex
	var identities []zlm.RtpResourceSelector
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ zlm.RtpResourceSelector }
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		mu.Lock()
		identities = append(identities, body.RtpResourceSelector)
		mu.Unlock()
		loaded, err := store.LoadRTPResourceSteps(r.Context(), o.id)
		require.NoError(t, err)
		require.Equal(t, playauth.RTPStepMayHaveDispatched, loaded.Steps[0].State, "SQL dispatch precedes every actual wire call")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		switch r.URL.Path {
		case "/index/api/openRtpServerIfMatch":
			_, _ = w.Write([]byte(`{"code":0,"data":{"result":"created","port":30000}}`))
		case "/index/api/closeRtpServerIfMatch":
			_, _ = w.Write([]byte(`{"code":0,"data":{"result":"shutdown_scheduled"}}`))
		case "/index/api/closeRtpIngressIfMatchV2":
			_, _ = w.Write([]byte(`{"code":0,"data":{"result":"rtp_ingress_drained"}}`))
		default:
			t.Errorf("legacy or unexpected call: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	selected := &node.Node{ID: 7, Revision: 3, MediaServerUUID: "fixture-node", APISecret: "fixture-secret", ReceiveHost: "127.0.0.1", State: node.StateActive}
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	control, err := zlm.NewOpenAPIRuntimeControl(*selected, zlm.OpenAPIControlTLS{Endpoint: server.URL + "/index/api", Roots: roots, SPKISHA256: sha256.Sum256(server.Certificate().RawSubjectPublicKeyInfo)})
	require.NoError(t, err)
	defer control.Close()
	factory := NewZLMIntentRTPOpener(fakePlaybackNodes{selected}, stream.NewLocationMap(), intentRTPTestResolver{control}).(IntentRTPFactory)
	child, err := factory.PrepareIntent(context.Background(), store, o.id, o.version,
		NodeInfo{ID: "7", NodeUUID: selected.MediaServerUUID, NodeRevision: 3, RecvIP: "127.0.0.1"}, RTPRequest{NodeID: "7", StreamID: "pb-fixture", SSRC: "12345"})
	require.NoError(t, err)
	actual := child.(*intentRTPChild)
	require.NotNil(t, actual.work, "actual parent adapter retains the cleanup witness before dispatch")
	_, err = actual.work.Open(context.Background(), func(context.Context, playauth.DeviceRTPResourceIdentity) (playauth.DeviceRTPOpenResult, error) {
		t.Fatal("prepared witness has no sending permission")
		return playauth.DeviceRTPOpenResult{}, nil
	})
	require.ErrorIs(t, err, playauth.ErrDeviceIntentConflict)
	allocation, err := child.Open(context.Background())
	require.NoError(t, err)
	require.Equal(t, 30000, allocation.Port)
	local, err := child.Close(context.Background())
	require.NoError(t, err)
	require.True(t, local.LocalQuiesced)
	require.True(t, local.RemotePending, "UDP ingress drained is not all media resource completion")
	loaded, err := store.LoadRTPResourceSteps(context.Background(), o.id)
	require.NoError(t, err)
	require.NotNil(t, loaded.Steps[0].LocalQuiescedAt)
	require.Equal(t, "created", loaded.Steps[0].OpenResult.Result)
	require.Equal(t, "rtp_ingress_drained", loaded.Steps[0].IngressCloseResult)
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, identities, 3)
	require.Equal(t, identities[0], identities[1])
	require.Equal(t, identities[0], identities[2])
	require.Equal(t, loaded.Steps[0].Identity.ResourceID, identities[0].ResourceID)
}
