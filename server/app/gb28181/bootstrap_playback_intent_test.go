package gb28181

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playback"
	"uvplatform.cn/uvp-gb28181/app/gb28181/recordquery"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	zlmscheduler "uvplatform.cn/uvp-gb28181/app/gb28181/zlm/scheduler"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/internal/authoritytest"
)

// Each caller owns an actual OS process, one root authority and an empty DB.
// The HTTP peer deliberately fails allocation before any SIP connection opens.
func playbackRootFixture(t *testing.T) (gbconfig.Config, *uac.UAC, *playauth.DeviceOperationBarrier, *playauth.DeviceOperationIntentStore, *atomic.Int32) {
	t.Helper()
	isolateSIPShutdownRoot(t)
	db := app.DB()
	authority := authoritytest.Register(t, db, "")
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
	barrier, err := playauth.NewAuthorizedDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db), authority)
	require.NoError(t, err)
	intents, err := playauth.NewAuthorizedDeviceOperationIntentStore(db, authority)
	require.NoError(t, err)
	ua, err := sipgo.NewUA()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, ua.Close()) })
	// Advertised metadata must be unicast; every actual destination below is
	// an explicit loopback listener, never the documentation address.
	inviter, err := uac.New(ua, "34020000002000000001", "3402000000", "192.0.2.1", 5060, false)
	require.NoError(t, err)
	recordQueryService, err = recordquery.NewService(inviter, recordquery.Options{Timeout: time.Second, MaxActiveQueries: 16, MaxRecordsPerQuery: 100, ResultTTL: time.Minute, Location: time.UTC})
	require.NoError(t, err)
	var calls atomic.Int32
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(peer.Close)
	endpoint, err := url.Parse(peer.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(endpoint.Port())
	require.NoError(t, err)
	zlmRegistry = node.NewRegistry(startupRTPNodeRepo{n: node.Node{ID: 7, Revision: 1, MediaServerUUID: "root-node", Host: endpoint.Hostname(), APIPort: port, ReceiveHost: "127.0.0.1", State: node.StateActive}})
	require.NoError(t, zlmRegistry.LoadAll(context.Background()))
	zlmScheduler = zlmscheduler.NewManager(zlmscheduler.NewFactory(zlmRegistry))
	require.NoError(t, zlmScheduler.Switch("roundrobin"))
	zlmLocationMap = stream.NewLocationMap()
	zlmServerConfigCache = zlm.NewServerConfigCache(nil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		require.NoError(t, stopPlaybackRuntime(ctx))
		recordQueryService.Close()
		recordQueryService = nil
		require.NoError(t, inviter.ShutdownPlaybackIntents(ctx))
	})
	cfg := gbconfig.Config{SIP: gbconfig.SIPConfig{ServerID: "34020000002000000001"}}
	return cfg, inviter, barrier, intents, &calls
}

func playbackRootRequest() playback.CreateRequest {
	now := time.Now().Add(-time.Hour)
	return playback.CreateRequest{
		Authorization: playback.AuthorizationSnapshot{DevicePK: 1, DeviceEpoch: 1, CleanupCompletedEpoch: 1, DeviceCode: "34020000001320000001", ChannelPK: 11, ChannelCode: "34020000001320000002"},
		OwnerID:       "1", DeviceID: "34020000001320000001", ChannelID: "11", SIPChannelID: "34020000001320000002",
		RecordKey: "record", IdempotencyKey: "request", SegmentStart: now, SegmentEnd: now.Add(time.Minute),
		Destination: "127.0.0.1:9", Transport: "UDP", Now: time.Now(),
	}
}

func TestPlaybackRootRejectsLegacyAfterPolicyLock(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	cfg, inviter, barrier, intents, calls := playbackRootFixture(t)
	setupPlaybackRuntime(cfg, inviter, barrier, intents)
	require.NotNil(t, playbackService)
	gbconfig.RequirePlayAuth()
	_, err := playbackService.Create(context.Background(), playbackRootRequest())
	require.ErrorIs(t, err, playback.ErrRTPUnavailable)
	require.Zero(t, calls.Load(), "locked root must not reach legacy allocation")
}

func TestPlaybackRootLockedWithoutTrustIsUnavailable(t *testing.T) {
	for _, missing := range []string{"trust-absent", "trust-invalid", "intents", "barrier", "uac"} {
		t.Run(missing, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			cfg, inviter, barrier, intents, calls := playbackRootFixture(t)
			if missing != "trust-absent" {
				peer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
				t.Cleanup(peer.Close)
				source := playbackRootTrust(t, peer)
				if missing == "trust-invalid" {
					require.NoError(t, os.WriteFile(source.path, []byte("invalid"), 0600))
				}
			}
			switch missing {
			case "intents":
				intents = nil
			case "barrier":
				barrier = nil
			case "uac":
				inviter = nil
			}
			gbconfig.RequirePlayAuth()
			setupPlaybackRuntime(cfg, inviter, barrier, intents)
			require.Nil(t, playbackService, "partial locked root must not assemble legacy service")
			require.Zero(t, calls.Load())
		})
	}
}

func playbackRootTrust(t *testing.T, control *httptest.Server) *startupTrustConfig {
	t.Helper()
	pin := sha256.Sum256(control.Certificate().RawSubjectPublicKeyInfo)
	data, err := yaml.Marshal(map[string]any{"version": 1, "nodes": []any{map[string]any{
		"node_id": 7, "node_uuid": "root-node", "binding_revision": 1, "enabled": true,
		"endpoint": control.URL + "/index/api", "ca_mode": "private", "ca_pem": string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: control.Certificate().Raw})),
		"spki_sha256": hex.EncodeToString(pin[:]), "hook_base": "https://hooks.example.test/hook",
	}}})
	require.NoError(t, err)
	source := &startupTrustConfig{path: filepath.Join(t.TempDir(), "trust.yml")}
	require.NoError(t, os.WriteFile(source.path, data, 0600))
	app.ConfigYml = source
	return source
}

func TestPlaybackRootUnlockedKeepsLegacyMode(t *testing.T) {
	for _, mode := range []playback.Mode{playback.ModePlayback, playback.ModeDownload} {
		t.Run(string(mode), func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			cfg, inviter, barrier, intents, calls := playbackRootFixture(t)
			setupPlaybackRuntime(cfg, inviter, barrier, intents)
			require.NotNil(t, playbackService)
			req := playbackRootRequest()
			req.Mode = mode
			_, err := playbackService.Create(context.Background(), req)
			require.ErrorIs(t, err, playback.ErrRTPUnavailable, "fixture allocation fails before SIP")
			require.EqualValues(t, 1, calls.Load())
			var count int64
			require.NoError(t, app.DB().Model(&playauth.DeviceOperationIntent{}).Count(&count).Error)
			require.Zero(t, count, "unlocked backend must not accidentally switch modes")
		})
	}
}

func TestPlaybackRootPersistentPlaybackAndDownload(t *testing.T) {
	for _, mode := range []playback.Mode{playback.ModePlayback, playback.ModeDownload} {
		t.Run(string(mode), func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			cfg, inviter, barrier, intents, legacyCalls := playbackRootFixture(t)
			db := app.DB()
			require.NoError(t, db.Exec("CREATE TABLE meta_node(id BIGINT PRIMARY KEY, revision BIGINT, state TEXT, media_server_uuid TEXT, current_boot_nonce TEXT, retired_boot_history TEXT, runtime_epoch BIGINT, runtime_protocol_version BIGINT, runtime_confirmed_revision BIGINT, runtime_confirmed_at DATETIME, runtime_identity_status TEXT)").Error)
			require.NoError(t, db.Exec("INSERT INTO meta_node VALUES(7,1,'active','root-node',NULL,'[]',0,0,0,NULL,'unknown')").Error)
			var opens, closes, invites, byes atomic.Int32
			boot := strings.Repeat("c", 32)
			control := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Cache-Control", "no-store")
				var body any
				switch r.URL.Path {
				case "/index/api/getRuntimeIdentity":
					body = map[string]any{"code": 0, "data": map[string]any{"protocolVersion": 1, "bootNonce": boot}}
				case "/index/api/getAllSession", "/index/api/getMediaPlayerList":
					body = map[string]any{"code": 0, "bootNonce": boot, "data": []any{}}
				case "/index/api/getServerConfig":
					config := map[string]string{"general.mediaServerId": "root-node", "hook.enable": "1", "general.flowThreshold": "0", "api.apiDebug": "0", "hook.timeoutSec": "1", "hook.retry": "0", "hook.retry_delay": "0"}
					for _, event := range playauth.ManagedHookEvents() {
						cap, err := playauth.HookCapability("root-fixture-secret", "root-node", event)
						require.NoError(t, err)
						config["hook."+string(event)] = "https://hooks.example.test/hook/" + string(event) + "?" + url.Values{"node": {"root-node"}, "cap": {cap}}.Encode()
					}
					body = map[string]any{"code": 0, "data": []any{config}}
				case "/index/api/openRtpServerIfMatch", "/index/api/closeRtpServerIfMatch", "/index/api/closeRtpIngressIfMatchV2":
					var intent playauth.DeviceOperationIntent
					require.NoError(t, db.Order("created_at DESC").First(&intent).Error)
					require.Equal(t, playauth.IntentDispatched, intent.State)
					facts, err := intents.LoadRTPResourceSteps(r.Context(), intent.DeviceOperationIntentIdentity)
					require.NoError(t, err)
					require.Len(t, facts.Steps, 1)
					require.Equal(t, playauth.RTPStepMayHaveDispatched, facts.Steps[0].State)
					require.NotNil(t, facts.Steps[0].DispatchStartedAt, "actual network follows durable dispatch")
					result := map[string]any{"result": "shutdown_scheduled"}
					if strings.Contains(r.URL.Path, "openRtp") {
						opens.Add(1)
						result = map[string]any{"result": "created", "port": 30000}
					} else {
						closes.Add(1)
						if strings.HasSuffix(r.URL.Path, "V2") {
							result = map[string]any{"result": "rtp_ingress_drained"}
						}
					}
					body = map[string]any{"code": 0, "data": result}
				default:
					t.Errorf("unexpected control endpoint %s", r.URL.Path)
					w.WriteHeader(500)
					return
				}
				_ = json.NewEncoder(w).Encode(body)
			}))
			t.Cleanup(control.Close)
			source := playbackRootTrust(t, control)
			_, err := LoadStartupOpenAPIControlBindingsOnce()
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(source.path, []byte("changed after prime"), 0600))
			// Only media readiness is a peer response; root picker, resolver,
			// runtime probe, RTP/SIP factories and durable owner are real.
			media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/index/api/isMediaOnline":
					_, _ = w.Write([]byte(`{"code":0,"online":true}`))
				case "/index/api/getMediaInfo":
					_, _ = w.Write([]byte(`{"code":0,"tracks":[]}`))
				default:
					legacyCalls.Add(1)
					w.WriteHeader(500)
				}
			}))
			t.Cleanup(media.Close)
			endpoint, err := url.Parse(media.URL)
			require.NoError(t, err)
			port, err := strconv.Atoi(endpoint.Port())
			require.NoError(t, err)
			zlmRegistry = node.NewRegistry(startupRTPNodeRepo{n: node.Node{ID: 7, Revision: 1, MediaServerUUID: "root-node", Host: endpoint.Hostname(), APIPort: port, ReceiveHost: "127.0.0.1", APISecret: "root-fixture-secret", State: node.StateActive}})
			require.NoError(t, zlmRegistry.LoadAll(context.Background()))
			zlmScheduler = zlmscheduler.NewManager(zlmscheduler.NewFactory(zlmRegistry))
			require.NoError(t, zlmScheduler.Switch("roundrobin"))
			zlmServerConfigCache = zlm.NewServerConfigCache(func(context.Context, int64) (node.ServerConfig, error) { return node.ServerConfig{HTTPPort: port}, nil })
			peer, err := net.ListenPacket("udp4", "127.0.0.1:0")
			require.NoError(t, err)
			done := make(chan struct{})
			t.Cleanup(func() { _ = peer.Close(); <-done })
			go func() {
				defer close(done)
				buffer := make([]byte, 8192)
				for {
					n, addr, err := peer.ReadFrom(buffer)
					if err != nil {
						return
					}
					message, err := sip.ParseMessage(buffer[:n])
					if err != nil {
						t.Error(err)
						return
					}
					request, ok := message.(*sip.Request)
					if !ok || request.Method == sip.ACK {
						continue
					}
					if request.Method == sip.INVITE {
						invites.Add(1)
						var intent playauth.DeviceOperationIntent
						if err := db.Order("created_at DESC").First(&intent).Error; err != nil {
							t.Error(err)
							return
						}
						steps, err := intents.LoadSIPInviteSteps(context.Background(), intent.DeviceOperationIntentIdentity)
						if err != nil {
							t.Error(err)
							return
						}
						if len(steps.Steps) != 1 || steps.Steps[0].State != playauth.SIPStepMayHaveDispatched || steps.Steps[0].DispatchStartedAt == nil {
							t.Error("SIP preceded confirmed persistent dispatch")
							return
						}
					}
					if request.Method == sip.BYE {
						byes.Add(1)
					}
					response := sip.NewResponseFromRequest(request, 200, "fixture", nil)
					response.To().Params.Add("tag", "root-device")
					if request.Method == sip.INVITE {
						response.AppendHeader(&sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: "device", Host: "127.0.0.1", Port: peer.LocalAddr().(*net.UDPAddr).Port}})
					}
					_, _ = peer.WriteTo([]byte(response.String()), addr)
				}
			}()
			gbconfig.RequirePlayAuth()
			// Close owners before any peer/UA/authority cleanup, even on failure.
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				require.NoError(t, stopPlaybackRuntime(ctx))
			})
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			// Exercise the playback stop/reassembly boundary used by SIP reload.
			// The process authority and primed trust remain the same generation.
			for generation := 0; generation < 2; generation++ {
				setupPlaybackRuntime(cfg, inviter, barrier, intents)
				require.NotNil(t, playbackService)
				req := playbackRootRequest()
				req.Mode, req.Destination = mode, peer.LocalAddr().String()
				created, err := playbackService.Create(ctx, req)
				require.NoError(t, err)
				require.Equal(t, playback.StatePlaying, created.Session.State)
				var intent playauth.DeviceOperationIntent
				require.NoError(t, db.Order("created_at DESC").First(&intent).Error)
				require.Equal(t, string(mode), intent.Kind)
				rtp, err := intents.LoadRTPResourceSteps(ctx, intent.DeviceOperationIntentIdentity)
				require.NoError(t, err)
				sips, err := intents.LoadSIPInviteSteps(ctx, intent.DeviceOperationIntentIdentity)
				require.NoError(t, err)
				require.Len(t, rtp.Steps, 1)
				require.Len(t, sips.Steps, 1)
				require.Equal(t, created.Session.CallID, sips.Steps[0].Identity.CallID)
				if generation == 0 {
					original := playbackService
					require.NoError(t, db.Exec(`CREATE TRIGGER deny_root_local_fact BEFORE UPDATE ON gb_device_operation_intent
						WHEN json_type(NEW.rtp_steps_json, '$.steps[0].localQuiescedAt') IS NOT NULL
						BEGIN SELECT RAISE(ABORT,'fixture final local fact unavailable'); END`).Error)
					require.Error(t, stopPlaybackRuntime(ctx))
					require.Same(t, original, playbackService, "failed local closure retains the exact root owner")
					factories := 0
					err := startSIPDependenciesWithFactory(cfg, nil, func(gbconfig.Config) (sipRuntimeServer, error) { factories++; return nil, nil })
					require.ErrorContains(t, err, "旧运行时尚未释放")
					require.Zero(t, factories, "reload cannot replace the retained old root")
					require.NoError(t, db.Exec("DROP TRIGGER deny_root_local_fact").Error)
				}
				require.NoError(t, playbackService.Stop(ctx, created.Session.ID, "root fixture stop"))
				require.NoError(t, barrier.WaitBefore(ctx, 1, 2))
				sips, err = intents.LoadSIPInviteSteps(ctx, intent.DeviceOperationIntentIdentity)
				require.NoError(t, err)
				require.Equal(t, playauth.IntentDispatched, sips.Intent.State)
				require.Equal(t, playauth.SIPBranchObserverIncomplete, sips.Steps[0].BranchInventoryFault)
				var completed int64
				require.NoError(t, db.Table("gb_device").Select("cleanup_completed_epoch").Scan(&completed).Error)
				require.EqualValues(t, 1, completed)
				require.EqualValues(t, generation+1, opens.Load())
				require.EqualValues(t, 2*(generation+1), closes.Load())
				require.EqualValues(t, generation+1, invites.Load())
				require.EqualValues(t, generation+1, byes.Load())
				require.Zero(t, legacyCalls.Load())
				require.NoError(t, stopPlaybackRuntime(ctx))
				require.Nil(t, playbackService)
			}
			require.EqualValues(t, 1, source.reads.Load(), "reassembly must not reread changed trust")
		})
	}
}
