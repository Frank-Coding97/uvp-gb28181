//go:build openapi_live

package integration

import (
	"context"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	openapiconfig "uvplatform.cn/uvp-gb28181/app/openapi/config"
	"uvplatform.cn/uvp-gb28181/app/openapi/media"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

// This checks actual ZLM configuration readback and durable identity, NOT
// successful Hook delivery or playback qualification. All listeners are owned
// loopback fixtures; no ordinary registered media node is loaded or contacted.
func TestOpenAPINodePreflightActualReadback(t *testing.T) {
	f := newMediaProbeFixture(t)
	f.stop()
	hooks := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, `{"code":0}`)
	}))
	defer hooks.Close()
	base := hooks.URL + "/hook"
	path := filepath.Join(f.dir, "probe.ini")
	original, err := os.ReadFile(path)
	require.NoError(t, err)
	prefix, _, ok := strings.Cut(string(original), "[hook]")
	require.True(t, ok)
	var hookConfig strings.Builder
	hookConfig.WriteString("[hook]\nenable=1\ntimeoutSec=1\nretry=1\nretry_delay=0.5\n")
	for _, event := range playauth.ManagedHookEvents() {
		capability, err := playauth.HookCapability(f.secret, "openapi-isolated-probe", event)
		require.NoError(t, err)
		query := url.Values{"node": {"openapi-isolated-probe"}, "cap": {capability}}
		fmt.Fprintf(&hookConfig, "%s=%s/%s?%s\n", event, base, event, query.Encode())
	}
	require.NoError(t, os.WriteFile(path, []byte(prefix+hookConfig.String()), 0600))
	f.start()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "runtime.sqlite")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.Exec(`CREATE TABLE meta_node (
		id INTEGER PRIMARY KEY, media_server_uuid TEXT NOT NULL, revision INTEGER NOT NULL,
		current_boot_nonce TEXT, retired_boot_history TEXT, runtime_epoch INTEGER NOT NULL DEFAULT 0,
		runtime_protocol_version INTEGER NOT NULL DEFAULT 0, runtime_confirmed_revision INTEGER NOT NULL DEFAULT 0,
		runtime_confirmed_at DATETIME, runtime_identity_status TEXT NOT NULL DEFAULT 'unknown')`).Error)
	require.NoError(t, db.Exec("INSERT INTO meta_node (id, media_server_uuid, revision) VALUES (1, 'openapi-isolated-probe', 1)").Error)
	n := node.Node{ID: 1, Revision: 1, MediaServerUUID: "openapi-isolated-probe", State: node.StateActive, APISecret: f.secret}
	settings := zlm.OpenAPIControlTLS{Endpoint: fmt.Sprintf("https://127.0.0.1:%d/index/api", f.tlsPort), Roots: f.tlsConfig.RootCAs, SPKISHA256: f.controlPin}
	probe := media.NewNodePreflight(openapiconfig.NewNodeRuntimeStore(db, nil))
	got, err := probe.Probe(context.Background(), n, settings, base)
	require.NoError(t, err)
	require.Equal(t, openapiconfig.NodeRuntimeStatusActive, got.IdentityStatus)
	require.EqualValues(t, 1, got.RuntimeEpoch)
	replayed, err := probe.Probe(context.Background(), n, settings, base)
	require.NoError(t, err)
	require.Equal(t, got.CurrentBootNonce, replayed.CurrentBootNonce)
	require.Equal(t, got.RuntimeEpoch, replayed.RuntimeEpoch)

	// Exercise the production resolver against this real isolated ZLM, not a
	// pre-trusted runtime stub. It still does not qualify playback or Hook delivery.
	factory := media.NewTrustedRevocationFactory(liveRevocationRegistry{n}, liveRevocationBinding{media.NodeControlBinding{
		NodeID: n.ID, NodeUUID: n.MediaServerUUID, BindingRevision: 1, Enabled: true, TLS: settings, HookBase: base,
	}}, openapiconfig.NewNodeRuntimeStore(db, nil))
	runtime, err := factory.Resolve(context.Background(), n.MediaServerUUID)
	require.NoError(t, err)
	require.True(t, runtime.Trusted)
	require.Equal(t, got.CurrentBootNonce, runtime.CurrentBootNonce)
	require.Equal(t, 2500*time.Millisecond, runtime.HookBudget)
	require.NotNil(t, runtime.Release)
	defer runtime.Release()
	sessions, err := runtime.Control.GetRuntimeSessions(context.Background())
	require.NoError(t, err)
	require.Equal(t, runtime.CurrentBootNonce, sessions.BootNonce)
	verifyConfiguredRevocationWithRealNode(t, f, db, n, settings, base, runtime.CurrentBootNonce)

	// A bad pin cannot keep the prior mapping marked usable, nor erase history.
	settings.SPKISHA256[0] ^= 1
	_, err = probe.Probe(context.Background(), n, settings, base)
	require.Error(t, err)
	stored, err := openapiconfig.NewNodeRuntimeStore(db, nil).Load(context.Background(), got.NodeRuntimeRef)
	require.NoError(t, err)
	require.Equal(t, openapiconfig.NodeRuntimeStatusUnknown, stored.IdentityStatus)
	require.Equal(t, got.CurrentBootNonce, stored.CurrentBootNonce)
}

func verifyConfiguredRevocationWithRealNode(t *testing.T, f *mediaProbeFixture, db *gorm.DB, n node.Node, settings zlm.OpenAPIControlTLS, hookBase, boot string) {
	t.Helper()
	stream := "revocation-startup-fixture"
	f.publish(t, stream)
	require.NoError(t, db.AutoMigrate(&models.PlayGrant{}, &models.Viewer{}))
	device, channel, schema, vhost, appName := "34020000001320000001", "34020000001310000001", "rtmp", "__defaultVhost__", "live"
	protocol, generation := "https-flv", uint64(1)
	now := time.Now().UTC().Truncate(time.Microsecond)
	grant := models.PlayGrant{GrantID: "00000000-0000-4000-8000-000000000099", ClientID: 1, Scope: "play:live:apply",
		DeviceID: &device, ChannelID: &channel, ClientEpoch: 1, ScopeEpoch: 1, DeviceEpoch: 1, NodeUUID: &n.MediaServerUUID,
		BootNonce: &boot, Schema: &schema, VHost: &vhost, App: &appName, Stream: &stream, MediaGeneration: &generation,
		Protocol: &protocol, State: models.GrantStateRevoked, Reason: "client.disabled", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute), CreatedAt: now.Add(-10 * time.Second), UpdatedAt: now.Add(-10 * time.Second)}
	require.NoError(t, db.Create(&grant).Error)
	// This deliberately absent identifier checks durable recovery, not an
	// active viewer kick or A/B/U playback isolation.
	viewer := models.Viewer{GrantID: grant.GrantID, NodeUUID: n.MediaServerUUID, BootNonce: boot, Identifier: "absent-startup-fixture",
		Schema: schema, VHost: vhost, App: appName, Stream: stream, MediaGeneration: generation,
		State: models.ViewerStateRevokePending, RetryAt: &now, LastErrorClass: media.RevocationErrorPending, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, db.Create(&viewer).Error)
	pemBytes, err := os.ReadFile(filepath.Join(f.dir, "server.pem"))
	require.NoError(t, err)
	cert, _ := pem.Decode(pemBytes)
	require.NotNil(t, cert)
	require.Equal(t, "CERTIFICATE", cert.Type)
	document := map[string]any{"version": 1, "nodes": []any{map[string]any{
		"node_id": n.ID, "node_uuid": n.MediaServerUUID, "binding_revision": 1, "enabled": true, "endpoint": settings.Endpoint,
		"ca_mode": "private", "ca_pem": string(pem.EncodeToMemory(cert)), "spki_sha256": hex.EncodeToString(settings.SPKISHA256[:]), "hook_base": hookBase,
	}}}
	data, err := yaml.Marshal(document)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "revocation.yml")
	require.NoError(t, os.WriteFile(path, data, 0600))
	reports := make(chan media.RevocationTickResult, 8)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	stop, err := media.StartConfiguredRevocation(ctx, db, liveRevocationRegistry{n}, path, func(result media.RevocationTickResult, err error) {
		require.NoError(t, err)
		reports <- result
	})
	require.NoError(t, err)
	defer stop()
	var first media.RevocationTickResult
	select {
	case first = <-reports:
	case <-ctx.Done():
		t.Fatal("real-node startup runner did not report")
	}
	require.Equal(t, 1, first.Pending)
	require.Zero(t, first.Closed)
	for {
		select {
		case result := <-reports:
			if result.Closed == 1 {
				stop()
				require.NoError(t, db.First(&viewer, viewer.ID).Error)
				require.Equal(t, models.ViewerStateClosed, viewer.State)
				require.Equal(t, media.RevocationErrorAlreadyGone, viewer.LastErrorClass)
				return
			}
		case <-ctx.Done():
			t.Fatal("two fresh real-node absence checks never closed the pending fixture")
		}
	}
}

type liveRevocationRegistry struct{ n node.Node }

func (r liveRevocationRegistry) GetByUUID(uuid string) (*node.Node, bool) {
	n := r.n
	return &n, n.MediaServerUUID == uuid
}

type liveRevocationBinding struct{ binding media.NodeControlBinding }

func (b liveRevocationBinding) Lookup(context.Context, string) (media.NodeControlBinding, error) {
	return b.binding, nil
}
