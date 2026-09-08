package uac_test

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playback"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/internal/authoritytest"
)

type recoveryTLSNodes struct{ n *node.Node }

func (n recoveryTLSNodes) Get(id int64) (*node.Node, bool) { return n.n, id == n.n.ID }

type recoveryTLSResolver func(context.Context, string) (playback.IntentRTPRuntime, error)

func (r recoveryTLSResolver) ResolveRTP(ctx context.Context, id string) (playback.IntentRTPRuntime, error) {
	return r(ctx, id)
}

// Public integration: real discovery/scan, persistent owner and pinned TLS
// adapter. The peer is a protocol fixture, not a ZLM binary or physical device.
func TestPlaybackRTPRecoveryActualTLSWorker(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "worker.sqlite")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(&playauth.DeviceOperationIntent{}))
	for _, sql := range []string{
		"ALTER TABLE gb_device_operation_intent ADD COLUMN sip_steps_json TEXT NULL",
		"ALTER TABLE gb_device_operation_intent ADD COLUMN rtp_steps_json TEXT NULL",
		"CREATE TABLE gb_device(id BIGINT PRIMARY KEY, device_id TEXT, access_epoch BIGINT, cleanup_completed_epoch BIGINT, deleted_at DATETIME, legacy_revoked_before DATETIME)",
		"CREATE TABLE gb_channel(id BIGINT PRIMARY KEY, device_id TEXT, channel_id TEXT, deleted_at DATETIME)",
		"INSERT INTO gb_device VALUES(1,'34020000001320000001',1,1,NULL,NULL)",
		"INSERT INTO gb_channel VALUES(11,'34020000001320000001','34020000001320000002',NULL)",
	} {
		require.NoError(t, db.Exec(sql).Error)
	}
	id := playauth.DeviceOperationIntentIdentity{OperationID: strings.Repeat("a", 32), DevicePK: 1, DeviceCode: "34020000001320000001", DeviceEpoch: 1, TargetScope: "channel", TargetPK: 11, TargetCode: "34020000001320000002", Kind: "playback"}
	store, err := playauth.NewAuthorizedDeviceOperationIntentStore(db, authoritytest.Register(t, db, ""))
	require.NoError(t, err)
	_, err = store.Reserve(ctx, id)
	require.NoError(t, err)
	_, err = store.Dispatch(ctx, id, 1)
	require.NoError(t, err)
	stepID := strings.Repeat("b", 32)
	resourceID, err := playauth.NewDeviceRTPResourceID(id.OperationID, stepID, time.Now().Add(-time.Minute))
	require.NoError(t, err)
	i := playauth.DeviceRTPResourceIdentity{StepID: stepID, NodePK: 7, NodeUUID: "tls-worker", NodeRevision: 1, BootNonce: strings.Repeat("c", 32), ResourceID: resourceID, VHost: "__defaultVhost__", App: "rtp", Stream: "original-worker", LocalIP: "127.0.0.1", SSRC: 1234}
	out, err := store.AddRTPResourceStep(ctx, id, 2, i)
	require.NoError(t, err)
	_, old, err := store.DispatchRTPResourceWork(ctx, id, out.Intent.RowVersion, stepID)
	require.NoError(t, err)
	require.NoError(t, old.Quiesce(ctx))
	require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	var calls, freed atomic.Int32
	peer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodPost, r.Method)
		require.Empty(t, r.URL.RawQuery)
		var target zlm.RtpResourceSelector
		require.NoError(t, json.NewDecoder(r.Body).Decode(&target))
		require.Equal(t, zlm.RtpResourceSelector{BootNonce: i.BootNonce, ResourceID: i.ResourceID, VHost: i.VHost, App: i.App, Stream: i.Stream}, target)
		facts, loadErr := store.LoadRTPResourceSteps(r.Context(), id)
		require.NoError(t, loadErr)
		require.NotNil(t, facts.Steps[0].Recovery.CurrentCall)
		require.Empty(t, facts.Steps[0].Recovery.CurrentCall.Outcome, "dispatch CAS must precede actual TLS")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/index/api/closeRtpServerIfMatch":
			_, _ = w.Write([]byte(`{"code":0,"data":{"result":"close_pending"}}`))
		case "/index/api/closeRtpIngressIfMatchV2":
			_, _ = w.Write([]byte(`{"code":0,"data":{"result":"rtp_ingress_drained"}}`))
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	t.Cleanup(peer.Close)
	n := &node.Node{ID: 7, Revision: 1, MediaServerUUID: i.NodeUUID, ReceiveHost: i.LocalIP, APISecret: "fixture-secret", State: node.StateActive}
	roots := x509.NewCertPool()
	roots.AddCert(peer.Certificate())
	control, err := zlm.NewOpenAPIRuntimeControl(*n, zlm.OpenAPIControlTLS{Endpoint: peer.URL + "/index/api", Roots: roots, SPKISHA256: sha256.Sum256(peer.Certificate().RawSubjectPublicKeyInfo)})
	require.NoError(t, err)
	t.Cleanup(control.Close)
	resolver := playback.NewZLMRTPCleanupResolver(recoveryTLSNodes{n}, recoveryTLSResolver(func(context.Context, string) (playback.IntentRTPRuntime, error) {
		return playback.IntentRTPRuntime{Control: control, BootNonce: i.BootNonce, Release: func() { freed.Add(1); control.Close() }}, nil
	}))
	ua, err := sipgo.NewUA()
	require.NoError(t, err)
	t.Cleanup(func() { _ = ua.Close() })
	u, err := uac.New(ua, "34020000002000000001", "3402000000", "127.0.0.1", 5061, false)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, u.ShutdownPlaybackIntents(context.Background())) })
	barrier := playauth.NewDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db))
	require.NoError(t, u.ConfigurePlaybackRTPCleanup(ctx, store, barrier, resolver))
	done := make(chan error, 1)
	stop, err := u.StartPlaybackRecovery(ctx, playauth.NewDeviceCleanupStore(db), store, barrier, func(_ uac.PlaybackRecoveryTick, err error) {
		select {
		case done <- err:
		default:
		}
	})
	require.NoError(t, err)
	defer stop(context.Background())
	select {
	case err = <-done:
		require.ErrorIs(t, err, uac.ErrPlaybackCleanupUnknown)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.NoError(t, stop(ctx))
	require.EqualValues(t, 2, calls.Load())
	require.EqualValues(t, 1, freed.Load())
	out, err = store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, i, out.Steps[0].Identity)
	require.NotNil(t, out.Steps[0].Recovery.LocalQuiescedAt)
	require.Equal(t, "rtp_ingress_drained", out.Steps[0].Recovery.IngressEvidence.Result)
	require.Equal(t, playauth.IntentDispatched, out.Intent.State)
}
