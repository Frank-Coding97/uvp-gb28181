package gb28181

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	gbsip "uvplatform.cn/uvp-gb28181/app/gb28181/sip"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/openapi/processauthority"
	"uvplatform.cn/uvp-gb28181/internal/authoritytest"
)

// Only registry loading is a fixture; control trust, probe, SQL, SIP assembly,
// UAC discovery and RTP cleanup are real product implementations.
type startupRTPNodeRepo struct {
	node.Repo
	n node.Node
}

func (r startupRTPNodeRepo) List(context.Context) ([]node.Node, error) {
	return []node.Node{r.n}, nil
}

func TestSIPRootRecoversRTPWithSharedStartupTrust(t *testing.T) {
	mode := os.Getenv("UVP_RTP_TRUST_ROOT_CHILD")
	if mode == "" {
		for _, mode := range []string{"trusted", "invalid", "no-registry", "conflict"} {
			t.Run(mode, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSIPRootRecoversRTPWithSharedStartupTrust$")
				cmd.Env = append(os.Environ(), "UVP_RTP_TRUST_ROOT_CHILD="+mode)
				out, err := cmd.CombinedOutput()
				require.NoError(t, err, "%s", out)
			})
		}
		return
	}
	t.Setenv("UVP_GB28181_CASCADE_KEY", "")
	core, logs := observer.New(zap.WarnLevel)
	app.ZapLog = zap.New(core)
	t.Cleanup(func() {
		if t.Failed() {
			for _, entry := range logs.All() {
				t.Log(entry.Message, entry.ContextMap())
			}
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "root.sqlite")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, raw.Close()) })
	app.GormDbMysql = db
	authority := authoritytest.Register(t, db, "")
	require.NoError(t, db.AutoMigrate(&playauth.DeviceOperationIntent{}, &gbmodels.GbPTZOperation{}, &gbmodels.GbPTZOperationAttempt{}, &gbmodels.GbDeviceFirmwareUpgrade{}))
	for _, sql := range []string{
		"ALTER TABLE gb_device_operation_intent ADD COLUMN sip_steps_json TEXT NULL",
		"ALTER TABLE gb_device_operation_intent ADD COLUMN rtp_steps_json TEXT NULL",
		"CREATE TABLE gb_device(id BIGINT PRIMARY KEY, device_id TEXT, access_epoch BIGINT, cleanup_completed_epoch BIGINT, deleted_at DATETIME, legacy_revoked_before DATETIME)",
		"CREATE TABLE gb_channel(id BIGINT PRIMARY KEY, device_id TEXT, channel_id TEXT, deleted_at DATETIME)",
		"INSERT INTO gb_device VALUES(1,'34020000001320000001',1,1,NULL,NULL)",
		"INSERT INTO gb_channel VALUES(11,'34020000001320000001','34020000001320000002',NULL)",
		"CREATE TABLE meta_node(id BIGINT PRIMARY KEY, revision BIGINT, state TEXT, media_server_uuid TEXT, current_boot_nonce TEXT, retired_boot_history TEXT, runtime_epoch BIGINT, runtime_protocol_version BIGINT, runtime_confirmed_revision BIGINT, runtime_confirmed_at DATETIME, runtime_identity_status TEXT)",
		"INSERT INTO meta_node VALUES(7,1,'active','root-node',NULL, '[]',0,0,0,NULL,'unknown')",
	} {
		require.NoError(t, db.Exec(sql).Error)
	}
	id := playauth.DeviceOperationIntentIdentity{OperationID: strings.Repeat("a", 32), DevicePK: 1, DeviceCode: "34020000001320000001", DeviceEpoch: 1, TargetScope: "channel", TargetPK: 11, TargetCode: "34020000001320000002", Kind: "playback"}
	store, err := playauth.NewAuthorizedDeviceOperationIntentStore(db, authority)
	require.NoError(t, err)
	_, err = store.Reserve(ctx, id)
	require.NoError(t, err)
	_, err = store.Dispatch(ctx, id, 1)
	require.NoError(t, err)
	stepID := strings.Repeat("b", 32)
	resourceID, err := playauth.NewDeviceRTPResourceID(id.OperationID, stepID, time.Now().Add(-time.Minute))
	require.NoError(t, err)
	i := playauth.DeviceRTPResourceIdentity{StepID: stepID, NodePK: 7, NodeUUID: "root-node", NodeRevision: 1, BootNonce: strings.Repeat("c", 32), ResourceID: resourceID, VHost: "__defaultVhost__", App: "rtp", Stream: "original-root", LocalIP: "127.0.0.1", SSRC: 1234}
	out, err := store.AddRTPResourceStep(ctx, id, 2, i)
	require.NoError(t, err)
	_, old, err := store.DispatchRTPResourceWork(ctx, id, out.Intent.RowVersion, stepID)
	require.NoError(t, err)
	require.NoError(t, old.Quiesce(ctx))
	require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	var closes, probes, discoveries atomic.Int32
	require.NoError(t, db.Callback().Row().After("gorm:row").Register("fixture:trust-root-discovery", func(tx *gorm.DB) {
		if tx.Statement.Table == "gb_device" {
			discoveries.Add(1)
		}
	}))
	peer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "root-fixture-secret", r.Header.Get("secret"))
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		var body any
		switch r.URL.Path {
		case "/index/api/getRuntimeIdentity":
			probes.Add(1)
			body = map[string]any{"code": 0, "data": map[string]any{"protocolVersion": 1, "bootNonce": i.BootNonce}}
		case "/index/api/getAllSession", "/index/api/getMediaPlayerList":
			body = map[string]any{"code": 0, "bootNonce": i.BootNonce, "data": []any{}}
		case "/index/api/getServerConfig":
			cfg := map[string]string{"general.mediaServerId": i.NodeUUID, "hook.enable": "1", "general.flowThreshold": "0", "api.apiDebug": "0", "hook.timeoutSec": "1", "hook.retry": "0", "hook.retry_delay": "0"}
			for _, event := range playauth.ManagedHookEvents() {
				cap, err := playauth.HookCapability("root-fixture-secret", i.NodeUUID, event)
				require.NoError(t, err)
				cfg["hook."+string(event)] = "https://hooks.example.test/hook/" + string(event) + "?" + url.Values{"node": {i.NodeUUID}, "cap": {cap}}.Encode()
			}
			body = map[string]any{"code": 0, "data": []any{cfg}}
		case "/index/api/closeRtpServerIfMatch", "/index/api/closeRtpIngressIfMatchV2":
			closes.Add(1)
			require.Equal(t, http.MethodPost, r.Method)
			var target zlm.RtpResourceSelector
			require.NoError(t, json.NewDecoder(r.Body).Decode(&target))
			require.Equal(t, zlm.RtpResourceSelector{BootNonce: i.BootNonce, ResourceID: i.ResourceID, VHost: i.VHost, App: i.App, Stream: i.Stream}, target)
			facts, err := store.LoadRTPResourceSteps(r.Context(), id)
			require.NoError(t, err)
			require.NotNil(t, facts.Steps[0].Recovery.CurrentCall)
			require.Empty(t, facts.Steps[0].Recovery.CurrentCall.Outcome)
			result := "close_pending"
			if strings.HasSuffix(r.URL.Path, "V2") {
				result = "rtp_ingress_drained"
			}
			body = map[string]any{"code": 0, "data": map[string]any{"result": result}}
		default:
			t.Errorf("unexpected root control endpoint %s", r.URL.Path)
			w.WriteHeader(500)
			return
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer peer.Close()
	pin := sha256.Sum256(peer.Certificate().RawSubjectPublicKeyInfo)
	data, err := yaml.Marshal(map[string]any{"version": 1, "nodes": []any{map[string]any{
		"node_id": 7, "node_uuid": i.NodeUUID, "binding_revision": 3, "enabled": true,
		"endpoint": peer.URL + "/index/api", "ca_mode": "private", "ca_pem": string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: peer.Certificate().Raw})),
		"spki_sha256": hex.EncodeToString(pin[:]), "hook_base": "https://hooks.example.test/hook",
	}}})
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "trust.yml")
	require.NoError(t, os.WriteFile(path, data, 0600))
	cfgSource := &startupTrustConfig{path: path}
	app.ConfigYml = cfgSource
	if mode == "invalid" {
		require.NoError(t, os.WriteFile(path, []byte("invalid"), 0600))
	}
	snapshot, loadErr := LoadStartupOpenAPIControlBindingsOnce()
	if mode == "invalid" {
		require.Error(t, loadErr)
		require.NoError(t, os.WriteFile(path, data, 0600))
	} else {
		require.NoError(t, loadErr)
		require.NoError(t, os.WriteFile(path, []byte("changed after main prime"), 0600))
	}
	if mode != "no-registry" {
		zlmRegistry = node.NewRegistry(startupRTPNodeRepo{n: node.Node{ID: 7, Revision: 1, MediaServerUUID: i.NodeUUID, ReceiveHost: i.LocalIP, APISecret: "root-fixture-secret", State: node.StateActive}})
		require.NoError(t, zlmRegistry.LoadAll(ctx))
	}
	cfg, err := gbconfig.LoadValidatedFrom(app.ConfigYml)
	require.NoError(t, err)
	cfg.SIP = gbconfig.SIPConfig{ListenIP: "127.0.0.1", AdvertiseIP: "127.0.0.1", Domain: "3402000000", ServerID: "34020000002000000001"}
	defer func() { require.NoError(t, stopSIPDependencies(context.Background())) }()
	// Exercise the same dependency stop/start path used by ReloadSIP. No full
	// HTTP/bootstrap or SIP-config DB endpoint is claimed by this fixture.
	for generation := 0; generation < 2; generation++ {
		t.Logf("SIP dependency generation %d", generation)
		beforeDiscovery, beforeProbe := discoveries.Load(), probes.Load()
		err = startSIPDependenciesWithFactory(cfg, authority, func(c gbconfig.Config) (sipRuntimeServer, error) {
			server, err := gbsip.NewServer(c)
			if err == nil && mode == "conflict" {
				err = configurePlaybackRTPCleanup(server.UAC(), db, store, playauth.NewDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db)))
				require.NoError(t, err)
			}
			return server, err
		})
		if mode == "conflict" {
			require.ErrorIs(t, err, uac.ErrPlaybackUnavailable)
			require.Nil(t, sipServer, "failed trusted attachment must roll back, not start SIP-only")
			require.Nil(t, securityRuntime)
			require.Zero(t, probes.Load())
			require.Zero(t, closes.Load())
			require.Equal(t, int32(1), cfgSource.reads.Load())
			return
		}
		require.NoError(t, err)
		if mode == "trusted" {
			require.Eventually(t, func() bool {
				facts, err := store.LoadRTPResourceSteps(ctx, id)
				return probes.Load() > beforeProbe && err == nil && facts.Steps[0].Recovery != nil && facts.Steps[0].Recovery.LocalQuiescedAt != nil && facts.Steps[0].Recovery.IngressEvidence != nil
			}, 3*time.Second, 10*time.Millisecond)
		} else {
			// Wait for a real discovery pass, not merely the startup return.
			require.Eventually(t, func() bool {
				return discoveries.Load() > beforeDiscovery
			}, time.Second, 10*time.Millisecond)
		}
		require.NoError(t, stopSIPDependencies(ctx))
		require.NoError(t, db.WithContext(ctx).Transaction(authority.CheckTx), "SIP stop must not seal main's authority")
		var generations int64
		require.NoError(t, db.Table("sys_openapi_process_generation").Count(&generations).Error)
		require.EqualValues(t, 1, generations, "reload must reuse the registered generation")
		got, err := LoadStartupOpenAPIControlBindingsOnce()
		require.True(t, got == snapshot && err == loadErr)
	}
	require.Equal(t, int32(1), cfgSource.reads.Load())
	if mode == "trusted" {
		require.GreaterOrEqual(t, closes.Load(), int32(2))
		require.Positive(t, probes.Load())
	} else {
		require.Zero(t, closes.Load())
		require.Zero(t, probes.Load())
	}
	out, err = store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, playauth.IntentDispatched, out.Intent.State, "RTP drain alone is not device completion")
	second, err := processauthority.Register(ctx, db, nil)
	require.ErrorIs(t, err, processauthority.ErrProcessAuthorityUnavailable)
	require.Nil(t, second)
}
