package playback

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type intentRTPCleanupResolverFunc func(context.Context, string) (IntentRTPRuntime, error)

// Preserve GORM's underlying DB identity while injecting commit replies.
type rtpCleanupCommitFaultPool struct{ *parentCommitFaultPool }

func (p rtpCleanupCommitFaultPool) GetDBConn() (*sql.DB, error) {
	return p.ConnPool.(*sql.DB), nil
}

func (f intentRTPCleanupResolverFunc) ResolveRTP(ctx context.Context, uuid string) (IntentRTPRuntime, error) {
	return f(ctx, uuid)
}

func rtpRecoveryTLSFixture(t *testing.T) (*gorm.DB, *playauth.DeviceOperationBarrier, *playauth.DeviceOperationIntentStore, playauth.DeviceOperationIntentIdentity, playauth.DeviceRTPResourceIdentity, *node.Node) {
	t.Helper()
	ctx := context.Background()
	db, b, req := playbackEpochFixture(t)
	require.NoError(t, db.Exec("CREATE TABLE gb_channel (id INTEGER PRIMARY KEY, device_id TEXT, channel_id TEXT, deleted_at DATETIME)").Error)
	require.NoError(t, db.Exec("INSERT INTO gb_channel VALUES(2,?,?,NULL)", req.DeviceID, req.SIPChannelID).Error)
	require.NoError(t, db.Exec("ALTER TABLE gb_device_operation_intent ADD COLUMN rtp_steps_json TEXT NULL").Error)
	store := playauth.NewDeviceOperationIntentStore(db)
	o, err := newPlaybackIntentOwner(ctx, store, b, req)
	require.NoError(t, err)
	require.NoError(t, o.begin(ctx))
	stepID, err := playauth.NewDeviceOperationIntentID()
	require.NoError(t, err)
	resourceID, err := playauth.NewDeviceRTPResourceID(o.id.OperationID, stepID, time.Now().Add(-time.Minute))
	require.NoError(t, err)
	identity := playauth.DeviceRTPResourceIdentity{StepID: stepID, NodePK: 7, NodeUUID: "fixture-node", NodeRevision: 3, BootNonce: strings.Repeat("a", 32), ResourceID: resourceID,
		VHost: "__defaultVhost__", App: "rtp", Stream: "original-fixture", LocalIP: "127.0.0.1", SSRC: 12345}
	out, err := store.AddRTPResourceStep(ctx, o.id, o.version, identity)
	require.NoError(t, err)
	_, original, err := store.DispatchRTPResourceWork(ctx, o.id, out.Intent.RowVersion, stepID)
	require.NoError(t, err)
	require.NoError(t, original.Quiesce(ctx))
	o.cancel()
	o.lease.Release()
	require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	n := &node.Node{ID: 7, Revision: 3, MediaServerUUID: "fixture-node", APISecret: "fixture-secret", ReceiveHost: "127.0.0.1", State: node.StateActive}
	return db, b, store, o.id, identity, n
}

func newRTPCleanupTLSControl(t *testing.T, n *node.Node, handler http.Handler) (*httptest.Server, *zlm.OpenAPIRuntimeControl) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	control, err := zlm.NewOpenAPIRuntimeControl(*n, zlm.OpenAPIControlTLS{Endpoint: server.URL + "/index/api", Roots: roots, SPKISHA256: sha256.Sum256(server.Certificate().RawSubjectPublicKeyInfo)})
	require.NoError(t, err)
	t.Cleanup(control.Close)
	return server, control
}

func TestPlaybackRTPRecoveryTLSResolverUsesOnlyOriginalSelector(t *testing.T) {
	_, _, _, _, identity, n := rtpRecoveryTLSFixture(t)
	var calls, releases atomic.Int32
	_, control := newRTPCleanupTLSControl(t, n, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodPost, r.Method)
		require.Empty(t, r.URL.RawQuery)
		var body struct {
			zlm.RtpResourceSelector
			Secret string `json:"secret"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, rtpSelector(identity), body.RtpResourceSelector)
		require.Empty(t, body.Secret)
		require.Equal(t, "fixture-secret", r.Header.Get("secret"))
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		switch r.URL.Path {
		case "/index/api/closeRtpServerIfMatch":
			_, _ = w.Write([]byte(`{"code":0,"data":{"result":"close_pending"}}`))
		case "/index/api/closeRtpIngressIfMatchV2":
			_, _ = w.Write([]byte(`{"code":0,"data":{"result":"rtp_ingress_drained"}}`))
		default:
			t.Errorf("unexpected recovery call: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	resolver := NewZLMRTPCleanupResolver(fakePlaybackNodes{n}, intentRTPCleanupResolverFunc(func(_ context.Context, uuid string) (IntentRTPRuntime, error) {
		require.Equal(t, identity.NodeUUID, uuid)
		return IntentRTPRuntime{Control: control, BootNonce: identity.BootNonce, Release: func() { releases.Add(1); control.Close() }}, nil
	}))
	runtime, err := resolver.ResolveRTPCleanup(context.Background(), identity)
	require.NoError(t, err)
	require.Zero(t, calls.Load(), "resolve is not permission to open or close RTP")
	result, err := runtime.CloseResource(context.Background())
	require.NoError(t, err)
	require.Equal(t, "close_pending", result)
	result, err = runtime.CloseIngress(context.Background())
	require.NoError(t, err)
	require.Equal(t, "rtp_ingress_drained", result)
	runtime.Release()
	runtime.Release()
	_, err = runtime.CloseResource(context.Background())
	require.Error(t, err)
	_, err = runtime.CloseIngress(context.Background())
	require.Error(t, err)
	require.EqualValues(t, 1, releases.Load())
	require.EqualValues(t, 2, calls.Load())
}

func TestPlaybackRTPRecoveryTLSActualOwnerPersistsBeforeCallsAndRetriesOnlyFacts(t *testing.T) {
	for _, failOutcome := range []bool{false, true} {
		t.Run(fmt.Sprint(failOutcome), func(t *testing.T) {
			db, b, store, id, identity, n := rtpRecoveryTLSFixture(t)
			ctx := context.Background()
			var resource, ingress, resolved, freed atomic.Int32
			_, control := newRTPCleanupTLSControl(t, n, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var target zlm.RtpResourceSelector
				require.NoError(t, json.NewDecoder(r.Body).Decode(&target))
				require.Equal(t, rtpSelector(identity), target)
				out, err := store.LoadRTPResourceSteps(r.Context(), id)
				require.NoError(t, err)
				call := out.Steps[0].Recovery.CurrentCall
				require.Empty(t, call.Outcome)
				require.Nil(t, call.LocalQuiescedAt)
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Cache-Control", "no-store")
				switch r.URL.Path {
				case "/index/api/closeRtpServerIfMatch":
					resource.Add(1)
					require.Equal(t, "close_resource", call.Action)
					require.EqualValues(t, 1, call.Sequence)
					_, _ = w.Write([]byte(`{"code":0,"data":{"result":"shutdown_scheduled"}}`))
				case "/index/api/closeRtpIngressIfMatchV2":
					ingress.Add(1)
					require.Equal(t, "close_ingress", call.Action)
					require.EqualValues(t, 2, call.Sequence)
					_, _ = w.Write([]byte(`{"code":0,"data":{"result":"rtp_ingress_drained"}}`))
				default:
					t.Errorf("unexpected recovery endpoint: %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			resolver := NewZLMRTPCleanupResolver(fakePlaybackNodes{n}, intentRTPCleanupResolverFunc(func(context.Context, string) (IntentRTPRuntime, error) {
				resolved.Add(1)
				return IntentRTPRuntime{Control: control, BootNonce: identity.BootNonce, Release: func() { freed.Add(1); control.Close() }}, nil
			}))
			work, err := b.ReserveRTPCleanup(ctx, store, id, identity.StepID)
			require.NoError(t, err)
			require.NoError(t, work.Prepare(ctx))
			if failOutcome {
				require.NoError(t, db.Exec(`CREATE TRIGGER fail_rtp_tls_outcome BEFORE UPDATE ON gb_device_operation_intent WHEN json_extract(NEW.rtp_steps_json,'$.steps[0].cleanup.currentCall.outcome') IS NOT NULL BEGIN SELECT RAISE(FAIL,'fixture response persistence unavailable'); END`).Error)
				require.Error(t, work.Run(ctx, resolver))
				require.EqualValues(t, 1, resource.Load())
				require.Zero(t, ingress.Load())
				require.Zero(t, freed.Load())
				require.NoError(t, db.Exec("DROP TRIGGER fail_rtp_tls_outcome").Error)
			}
			require.NoError(t, work.Run(ctx, resolver))
			require.NoError(t, work.Run(ctx, resolver))
			require.EqualValues(t, 1, resource.Load())
			require.EqualValues(t, 1, ingress.Load())
			require.EqualValues(t, 1, resolved.Load())
			require.NoError(t, work.Quiesce(ctx))
			require.EqualValues(t, 1, freed.Load())
			out, err := store.LoadRTPResourceSteps(ctx, id)
			require.NoError(t, err)
			require.Equal(t, identity, out.Steps[0].Identity)
			require.Nil(t, out.Steps[0].OpenResult)
			require.Empty(t, out.Steps[0].ResourceCloseResult, "original facts must not be replaced by recovery results")
			require.Equal(t, "rtp_ingress_drained", out.Steps[0].Recovery.IngressEvidence.Result)
			require.NotNil(t, out.Steps[0].Recovery.LocalQuiescedAt)
			require.Equal(t, playauth.IntentDispatched, out.Intent.State)
			state, err := playauth.NewDeviceCleanupStore(db).Load(ctx, id.DeviceCode)
			require.NoError(t, err)
			require.EqualValues(t, 1, state.CleanupCompletedEpoch)
		})
	}
}

func TestPlaybackRTPRecoveryTLSRejectsChangedNodeAndBootWithoutClose(t *testing.T) {
	for _, kind := range []string{"node-pk", "node-uuid", "node-revision", "node-state", "node-recovery", "node-address", "boot", "late-revision", "late-secret", "cancelled", "partial-error", "missing-release", "changed-after-resolve"} {
		t.Run(kind, func(t *testing.T) {
			_, _, _, _, identity, n := rtpRecoveryTLSFixture(t)
			var calls, freed, resolved atomic.Int32
			_, control := newRTPCleanupTLSControl(t, n, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch kind {
			case "node-pk":
				identity.NodePK++
			case "node-uuid":
				identity.NodeUUID = "other-node"
			case "node-revision":
				n.Revision++
			case "node-state":
				n.State = node.StateOffline
			case "node-recovery":
				n.RecoveryRequired = true
			case "node-address":
				n.ReceiveHost = "127.0.0.2"
			}
			resolver := NewZLMRTPCleanupResolver(fakePlaybackNodes{n}, intentRTPCleanupResolverFunc(func(context.Context, string) (IntentRTPRuntime, error) {
				resolved.Add(1)
				runtime := IntentRTPRuntime{Control: control, BootNonce: identity.BootNonce, Release: func() { freed.Add(1); control.Close() }}
				switch kind {
				case "boot":
					runtime.BootNonce = strings.Repeat("b", 32)
				case "late-revision":
					n.Revision++
				case "late-secret":
					n.APISecret = "changed-secret"
				case "cancelled":
					cancel()
				case "partial-error":
					return runtime, errors.New("partial resolver failure")
				case "missing-release":
					runtime.Release = nil
				}
				return runtime, nil
			}))
			runtime, err := resolver.ResolveRTPCleanup(ctx, identity)
			if kind == "changed-after-resolve" {
				require.NoError(t, err)
				n.Revision++
				_, err = runtime.CloseResource(ctx)
				require.Error(t, err)
				_, err = runtime.CloseIngress(ctx)
				require.Error(t, err)
				runtime.Release()
			} else {
				require.Error(t, err)
				require.Nil(t, runtime)
			}
			require.Zero(t, calls.Load())
			if resolved.Load() != 0 && kind != "missing-release" {
				require.EqualValues(t, 1, freed.Load())
			}
		})
	}
}

func TestPlaybackRTPRecoveryTLSUnknownCommitDoesNotAuthorizeHTTP(t *testing.T) {
	for _, phase := range []string{"prepare", "dispatch"} {
		for _, committed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/committed=%v", phase, committed), func(t *testing.T) {
				db, b, store, id, identity, n := rtpRecoveryTLSFixture(t)
				ctx := context.Background()
				var resource, ingress, resolved, freed atomic.Int32
				_, control := newRTPCleanupTLSControl(t, n, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var target zlm.RtpResourceSelector
					require.NoError(t, json.NewDecoder(r.Body).Decode(&target))
					require.Equal(t, rtpSelector(identity), target)
					w.Header().Set("Content-Type", "application/json")
					w.Header().Set("Cache-Control", "no-store")
					if r.URL.Path == "/index/api/closeRtpIngressIfMatchV2" {
						ingress.Add(1)
						_, _ = w.Write([]byte(`{"code":0,"data":{"result":"rtp_ingress_drained"}}`))
					} else {
						resource.Add(1)
						t.Error("unconfirmed resource dispatch reached HTTP")
						w.WriteHeader(500)
					}
				}))
				resolver := NewZLMRTPCleanupResolver(fakePlaybackNodes{n}, intentRTPCleanupResolverFunc(func(context.Context, string) (IntentRTPRuntime, error) {
					resolved.Add(1)
					return IntentRTPRuntime{Control: control, BootNonce: identity.BootNonce, Release: func() { freed.Add(1); control.Close() }}, nil
				}))
				// Preserve the same underlying DB identity through the wrapper.
				failAt := int32(1)
				if phase == "dispatch" {
					failAt = 0
				}
				pool := &parentCommitFaultPool{ConnPool: db.Statement.ConnPool, failAt: failAt, committed: committed}
				faultDB := db.Session(&gorm.Session{NewDB: true, Context: ctx})
				faultDB.Statement.ConnPool = rtpCleanupCommitFaultPool{pool}
				work, err := b.ReserveRTPCleanup(ctx, playauth.NewDeviceOperationIntentStore(faultDB), id, identity.StepID)
				require.NoError(t, err)
				if phase == "prepare" {
					require.Error(t, work.Prepare(ctx))
				} else {
					require.NoError(t, work.Prepare(ctx))
					// Run reconciles facts, then commits its first dispatch.
					failAt = pool.commits.Load() + 2
					pool.failAt = failAt
				}
				require.Error(t, work.Run(ctx, resolver))
				require.Equal(t, failAt, pool.commits.Load())
				require.Zero(t, resource.Load())
				require.Zero(t, ingress.Load())
				if phase == "prepare" {
					require.Zero(t, resolved.Load())
				} else {
					// Retry reconciles the non-invocation fact, then authorizes
					// only the still-unattempted ingress action with a new CAS.
					require.NoError(t, work.Run(ctx, resolver))
					require.NoError(t, work.Run(ctx, resolver))
					require.EqualValues(t, 1, ingress.Load())
					require.EqualValues(t, 1, resolved.Load())
				}
				require.NoError(t, work.Quiesce(ctx))
				require.Equal(t, resolved.Load(), freed.Load())
				require.Zero(t, resource.Load())
				out, err := store.LoadRTPResourceSteps(ctx, id)
				require.NoError(t, err)
				require.Equal(t, playauth.IntentDispatched, out.Intent.State)
				if out.Steps[0].Recovery != nil {
					require.Nil(t, out.Steps[0].Recovery.ResourceEvidence)
					require.NotNil(t, out.Steps[0].Recovery.LocalQuiescedAt)
				}
				require.NoError(t, b.WaitBefore(ctx, uint(id.DevicePK), 2))
			})
		}
	}
}
