package uac

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	gbplayback "uvplatform.cn/uvp-gb28181/app/gb28181/playback"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/internal/authoritytest"
)

type parentIntegrationNodes struct {
	selected    *node.Node
	destination string
	control     *zlm.OpenAPIRuntimeControl
}

func (n parentIntegrationNodes) Get(pk int64) (*node.Node, bool) {
	return n.selected, pk == n.selected.ID
}
func (n parentIntegrationNodes) Pick(context.Context, gbplayback.PickRequest) (gbplayback.NodeInfo, error) {
	return gbplayback.NodeInfo{ID: "7", NodeUUID: n.selected.MediaServerUUID, NodeRevision: n.selected.Revision,
		ServerID: "34020000002000000001", Destination: n.destination, Transport: "UDP", RecvIP: "127.0.0.1"}, nil
}
func (n parentIntegrationNodes) ResolveRTP(context.Context, string) (gbplayback.IntentRTPRuntime, error) {
	return gbplayback.IntentRTPRuntime{Control: n.control, BootNonce: strings.Repeat("a", 32), Release: n.control.Close}, nil
}
func (n parentIntegrationNodes) Wait(context.Context, string) (gbplayback.MediaReady, error) {
	return gbplayback.MediaReady{URLs: map[string]string{"httpFlv": "http://fixture.invalid/rtp/test.flv"}}, nil
}

func TestPlaybackParentActualServiceRTPAndSIPShareOneIntent(t *testing.T) {
	for _, tc := range []struct {
		name         string
		mode         gbplayback.Mode
		failFinalSQL bool
	}{
		{"playback", gbplayback.ModePlayback, false},
		{"download", gbplayback.ModeDownload, false},
		{"playback-final-SQL", gbplayback.ModePlayback, true},
		{"download-final-SQL", gbplayback.ModeDownload, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}

			mode := tc.mode
			u, db, store, oldID, _ := playbackIntentStoreFixture(t)
			u.client.TxRequester = nil
			require.NoError(t, db.Where("operation_id = ?", oldID.OperationID).Delete(&playauth.DeviceOperationIntent{}).Error)
			require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN legacy_revoked_before DATETIME NULL").Error)
			require.NoError(t, db.Exec("ALTER TABLE gb_device_operation_intent ADD COLUMN rtp_steps_json TEXT NULL").Error)
			barrier := newAuthorizedBarrierTest(t, db)
			var rtpCalls, invites, byes atomic.Int32
			https := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				rtpCalls.Add(1)
				w.Header().Set("Cache-Control", "no-store")
				result := map[string]any{"result": "shutdown_scheduled"}
				switch r.URL.Path {
				case "/index/api/openRtpServerIfMatch":
					result = map[string]any{"result": "created", "port": 30000}
				case "/index/api/closeRtpServerIfMatch":
				case "/index/api/closeRtpIngressIfMatchV2":
					result = map[string]any{"result": "rtp_ingress_drained"}
				default:
					t.Errorf("unexpected RTP endpoint %s", r.URL.Path)
					w.WriteHeader(404)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": result})
			}))
			defer https.Close()
			peer, err := net.ListenPacket("udp4", "127.0.0.1:0")
			require.NoError(t, err)
			done := make(chan struct{})
			defer func() { _ = peer.Close(); <-done }()
			go func() {
				defer close(done)
				buffer := make([]byte, 8192)
				for {
					n, address, err := peer.ReadFrom(buffer)
					if err != nil {
						return
					}
					message, err := sip.ParseMessage(buffer[:n])
					if err != nil {
						t.Error(err)
						return
					}
					request, ok := message.(*sip.Request)
					if !ok {
						continue
					}
					if request.Method == sip.ACK {
						continue
					}
					if request.Method == sip.INVITE {
						invites.Add(1)
					}
					if request.Method == sip.BYE {
						byes.Add(1)
					}
					response := sip.NewResponseFromRequest(request, 200, "fixture", nil)
					response.To().Params.Add("tag", "parent-device")
					if request.Method == sip.INVITE {
						response.AppendHeader(&sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: "device", Host: "127.0.0.1", Port: peer.LocalAddr().(*net.UDPAddr).Port}})
					}
					_, _ = peer.WriteTo([]byte(response.String()), address)
				}
			}()
			selected := &node.Node{ID: 7, Revision: 3, MediaServerUUID: "parent-fixture", APISecret: "fixture-secret", ReceiveHost: "127.0.0.1", State: node.StateActive}
			roots := x509.NewCertPool()
			roots.AddCert(https.Certificate())
			control, err := zlm.NewOpenAPIRuntimeControl(*selected, zlm.OpenAPIControlTLS{Endpoint: https.URL + "/index/api", Roots: roots, SPKISHA256: sha256.Sum256(https.Certificate().RawSubjectPublicKeyInfo)})
			require.NoError(t, err)
			defer control.Close()
			nodes := parentIntegrationNodes{selected: selected, destination: peer.LocalAddr().String(), control: control}
			locations := stream.NewLocationMap()
			service := gbplayback.NewService(gbplayback.NewRegistry(gbplayback.RegistryConfig{}), nodes,
				gbplayback.NewZLMIntentRTPOpener(nodes, locations, nodes), NewPlaybackAdapter(u), nodes,
				gbplayback.ServiceConfig{DeviceOperations: barrier, Intents: store})
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			now := time.Now()
			created, err := service.Create(ctx, gbplayback.CreateRequest{OwnerID: "fixture-owner", DeviceID: oldID.DeviceCode, ChannelID: "11", SIPChannelID: oldID.TargetCode,
				RecordKey: "fixture-record", SegmentStart: now.Add(-time.Hour), SegmentEnd: now.Add(-time.Minute), PlayFrom: now.Add(-time.Hour), Mode: mode,
				Authorization: gbplayback.AuthorizationSnapshot{DevicePK: 1, DeviceCode: oldID.DeviceCode, DeviceEpoch: 1, CleanupCompletedEpoch: 1, ChannelPK: 11, ChannelCode: oldID.TargetCode}})
			require.NoError(t, err)
			require.Equal(t, gbplayback.StatePlaying, created.Session.State)
			var intent playauth.DeviceOperationIntent
			require.NoError(t, db.First(&intent).Error)
			require.Equal(t, string(mode), intent.Kind)
			rtp, err := store.LoadRTPResourceSteps(ctx, intent.DeviceOperationIntentIdentity)
			require.NoError(t, err)
			sipSteps, err := store.LoadSIPInviteSteps(ctx, intent.DeviceOperationIntentIdentity)
			require.NoError(t, err)
			require.Len(t, rtp.Steps, 1)
			require.Len(t, sipSteps.Steps, 1)
			require.Equal(t, created.Session.CallID, sipSteps.Steps[0].Identity.CallID)
			wait, stop := context.WithTimeout(ctx, 10*time.Millisecond)
			require.ErrorIs(t, barrier.WaitBefore(wait, 1, 2), context.DeadlineExceeded)
			stop()
			if tc.failFinalSQL {
				require.NoError(t, db.Exec(`CREATE TRIGGER deny_parent_local_fact BEFORE UPDATE ON gb_device_operation_intent
					WHEN json_type(NEW.rtp_steps_json, '$.steps[0].localQuiescedAt') IS NOT NULL
					BEGIN SELECT RAISE(ABORT,'fixture final local fact unavailable'); END`).Error)
				require.Error(t, service.Stop(ctx, created.Session.ID, "fixture finish"))
				pendingRTP, err := store.LoadRTPResourceSteps(ctx, intent.DeviceOperationIntentIdentity)
				require.NoError(t, err)
				require.Equal(t, "response_observed", pendingRTP.Steps[0].ResourceCloseCall.Outcome)
				require.Equal(t, "response_observed", pendingRTP.Steps[0].IngressCloseCall.Outcome)
				require.Nil(t, pendingRTP.Steps[0].LocalQuiescedAt, "only the whole owner's final fact is blocked")
				pending, ok := service.GetForOwner(created.Session.ID, "fixture-owner")
				require.True(t, ok)
				require.Equal(t, gbplayback.StateStopping, pending.State)
				_, bound := locations.Lookup(created.Session.StreamID)
				require.True(t, bound, "failed persistence cannot discard the binding")
				wait, stop := context.WithTimeout(ctx, 10*time.Millisecond)
				require.ErrorIs(t, barrier.WaitBefore(wait, 1, 2), context.DeadlineExceeded)
				stop()
				require.Error(t, service.Close(ctx), "root retains the same service on local persistence failure")
				require.NoError(t, db.Exec("DROP TRIGGER deny_parent_local_fact").Error)
			}
			require.NoError(t, service.Stop(ctx, created.Session.ID, "fixture finish"), "durable remote pending must not prevent actual local completion")
			require.NoError(t, barrier.WaitBefore(ctx, 1, 2), "all actual children joined before parent lease release")
			stopped, ok := service.GetForOwner(created.Session.ID, "fixture-owner")
			require.True(t, ok)
			require.Equal(t, gbplayback.StateStopped, stopped.State)
			_, bound := locations.Lookup(created.Session.StreamID)
			require.False(t, bound, "only a locally quiesced and durable owner may unbind")
			persisted, err := store.LoadSIPInviteSteps(ctx, intent.DeviceOperationIntentIdentity)
			require.NoError(t, err)
			require.Equal(t, playauth.IntentDispatched, persisted.Intent.State)
			require.Equal(t, playauth.SIPBranchObserverIncomplete, persisted.Steps[0].BranchInventoryFault)
			var completed int64
			require.NoError(t, db.Table("gb_device").Select("cleanup_completed_epoch").Scan(&completed).Error)
			require.EqualValues(t, 1, completed, "local completion cannot advance device coverage")
			require.EqualValues(t, 1, invites.Load())
			require.EqualValues(t, 1, byes.Load())
			require.EqualValues(t, 3, rtpCalls.Load())
			require.NoError(t, service.Close(ctx))
			require.NoError(t, u.ShutdownPlaybackIntents(ctx))
		})
	}
}
