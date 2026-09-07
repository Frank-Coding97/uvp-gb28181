//go:build openapi_live

package integration

import (
	"bytes"
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
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	appmodels "uvplatform.cn/uvp-gb28181/app/models"
	openapiclient "uvplatform.cn/uvp-gb28181/app/openapi/client"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

type activeRevocationFixture struct {
	probe        *mediaProbeFixture
	db           *gorm.DB
	node         node.Node
	service      *openapiclient.Service
	bindingsPath string
}

func newActiveRevocationFixture(t *testing.T) *activeRevocationFixture {
	t.Helper()
	p := newMediaProbeFixture(t)
	p.stop()
	n := node.Node{ID: 1, Revision: 1, MediaServerUUID: "openapi-isolated-probe", State: node.StateActive, APISecret: p.secret}
	// Use exact managed HTTPS Hook URLs so the production resolver must pass
	// real configuration readback. Only this fixture maps play/flow to labels.
	caps := make(map[string]string)
	for _, event := range playauth.ManagedHookEvents() {
		capability, err := playauth.HookCapability(p.secret, n.MediaServerUUID, event)
		require.NoError(t, err)
		caps[string(event)] = capability
	}
	hooks := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		event := strings.TrimPrefix(r.URL.Path, "/hook/")
		if caps[event] == "" || r.URL.Query().Get("cap") != caps[event] || r.URL.Query().Get("node") != n.MediaServerUUID {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if event == "on_play" || event == "on_flow_report" {
			r.URL.Path = "/play"
			if event == "on_flow_report" {
				r.URL.Path = "/flow"
			}
			p.hook(w, r)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, `{"code":0}`)
	}))
	t.Cleanup(hooks.Close)
	// Stop media before closing its Hook server, including failure paths.
	t.Cleanup(p.stop)
	base := hooks.URL + "/hook"
	configPath := filepath.Join(p.dir, "probe.ini")
	original, err := os.ReadFile(configPath)
	require.NoError(t, err)
	prefix, _, found := strings.Cut(string(original), "[hook]")
	require.True(t, found)
	var config strings.Builder
	config.WriteString(prefix + "[hook]\nenable=1\ntimeoutSec=1\nretry=1\nretry_delay=0.5\n")
	for _, event := range playauth.ManagedHookEvents() {
		query := url.Values{"node": {n.MediaServerUUID}, "cap": {caps[string(event)]}}
		fmt.Fprintf(&config, "%s=%s/%s?%s\n", event, base, event, query.Encode())
	}
	require.NoError(t, os.WriteFile(configPath, []byte(config.String()), 0600))
	p.start()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "revocation.sqlite")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(&models.Client{}, &models.ClientScope{}, &models.Audit{}, &appmodels.SysOperationLog{}, &models.PlayGrant{}, &models.Viewer{}))
	require.NoError(t, db.Exec(`CREATE TABLE meta_node (
		id INTEGER PRIMARY KEY, media_server_uuid TEXT NOT NULL, revision INTEGER NOT NULL,
		current_boot_nonce TEXT, retired_boot_history TEXT, runtime_epoch INTEGER NOT NULL DEFAULT 0,
		runtime_protocol_version INTEGER NOT NULL DEFAULT 0, runtime_confirmed_revision INTEGER NOT NULL DEFAULT 0,
		runtime_confirmed_at DATETIME, runtime_identity_status TEXT NOT NULL DEFAULT 'unknown')`).Error)
	require.NoError(t, db.Exec("INSERT INTO meta_node (id, media_server_uuid, revision) VALUES (?, ?, ?)", n.ID, n.MediaServerUUID, n.Revision).Error)
	secrets, err := openapiclient.NewSecretManager(bytes.Repeat([]byte{0xA5}, 32), "isolated-revocation")
	require.NoError(t, err)
	service, err := openapiclient.NewService(db, secrets,
		openapiclient.WithManagementBoundary(activeRevocationManagement{}),
		openapiclient.WithRevocationIntentStore(playauth.NewOpenAPIRevocationStore(db, nil)))
	require.NoError(t, err)
	pemBytes, err := os.ReadFile(filepath.Join(p.dir, "server.pem"))
	require.NoError(t, err)
	cert, _ := pem.Decode(pemBytes)
	require.NotNil(t, cert)
	require.Equal(t, "CERTIFICATE", cert.Type)
	document := map[string]any{"version": 1, "nodes": []any{map[string]any{
		"node_id": n.ID, "node_uuid": n.MediaServerUUID, "binding_revision": 1, "enabled": true,
		"endpoint": fmt.Sprintf("https://127.0.0.1:%d/index/api", p.tlsPort), "ca_mode": "private",
		"ca_pem": string(pem.EncodeToMemory(cert)), "spki_sha256": hex.EncodeToString(p.controlPin[:]), "hook_base": base,
	}}}
	data, err := yaml.Marshal(document)
	require.NoError(t, err)
	bindingsPath := filepath.Join(t.TempDir(), "revocation.yml")
	require.NoError(t, os.WriteFile(bindingsPath, data, 0600))
	return &activeRevocationFixture{probe: p, db: db, node: n, service: service, bindingsPath: bindingsPath}
}

// Personnel/RBAC admission is explicitly outside this media runner test.
type activeRevocationManagement struct{}

func (activeRevocationManagement) AuthorizeCreate(context.Context, uint, uint) error { return nil }
func (activeRevocationManagement) AuthorizeClient(context.Context, uint, string, uint) error {
	return nil
}

func (f *activeRevocationFixture) client(t *testing.T, name string) openapiclient.ClientView {
	t.Helper()
	c, _, err := f.service.Create(context.Background(), openapiclient.CreateRequest{Name: name, OwnerDeptID: 10, ResponsibleUserID: 7, CreatedBy: 7})
	require.NoError(t, err)
	c, err = f.service.SetScopes(context.Background(), c.ID, []string{"play:live:apply", "device:list"}, c.RowVersion, 7)
	require.NoError(t, err)
	return c
}

func (f *activeRevocationFixture) bind(t *testing.T, c openapiclient.ClientView, protocol string, e probeEvent) models.Viewer {
	t.Helper()
	device, channel, generation := "34020000001320000001", "34020000001310000001", uint64(1)
	now := time.Now().UTC().Truncate(time.Microsecond)
	// Seed from the exact authenticated fixture Hook, never list order or IP.
	// Synthetic device/generation and seeded grant are not product admission.
	g := models.PlayGrant{GrantID: uuid.NewString(), ClientID: c.ID, Scope: "play:live:apply", DeviceID: &device, ChannelID: &channel,
		ClientEpoch: c.AuthEpoch, ScopeEpoch: 1, DeviceEpoch: 1, NodeUUID: &f.node.MediaServerUUID, BootNonce: &e.Boot,
		Schema: &e.Schema, VHost: &e.VHost, App: &e.App, Stream: &e.Stream, MediaGeneration: &generation, Protocol: &protocol,
		State: models.GrantStateBound, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute), CreatedAt: now, UpdatedAt: now}
	require.NoError(t, f.db.Create(&g).Error)
	v := models.Viewer{GrantID: g.GrantID, NodeUUID: f.node.MediaServerUUID, BootNonce: e.Boot, Identifier: e.ID,
		Schema: e.Schema, VHost: e.VHost, App: e.App, Stream: e.Stream, MediaGeneration: generation,
		State: models.ViewerStateActive, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, f.db.Create(&v).Error)
	require.NoError(t, f.db.First(&v, v.ID).Error)
	return v
}

func (f *activeRevocationFixture) publisher(t *testing.T) string {
	t.Helper()
	sessions, err := f.probe.client.GetRuntimeSessions(context.Background())
	require.NoError(t, err)
	id := ""
	for _, session := range sessions.Sessions {
		if session.LocalPort == f.probe.rtmpPort {
			require.Empty(t, id)
			id = session.ID
		}
	}
	require.NotEmpty(t, id)
	return id
}

func (f *activeRevocationFixture) checkSurvivors(t *testing.T, stream string, events []probeEvent) {
	t.Helper()
	players, err := f.probe.client.GetRuntimeMediaPlayers(context.Background(), probeTarget(stream))
	require.NoError(t, err)
	require.Equal(t, events[0].Boot, players.BootNonce)
	ids := make([]string, 0, len(players.Players))
	for _, p := range players.Players {
		ids = append(ids, p.Identifier)
	}
	require.ElementsMatch(t, []string{events[1].ID, events[2].ID}, ids)
	sessions, err := f.probe.client.GetRuntimeSessions(context.Background())
	require.NoError(t, err)
	require.Equal(t, events[0].Boot, sessions.BootNonce)
	ids = nil
	for _, s := range sessions.Sessions {
		ids = append(ids, s.ID)
	}
	require.NotContains(t, ids, events[0].ID)
	require.Contains(t, ids, events[1].ID)
	require.Contains(t, ids, events[2].ID)
}

func (f *activeRevocationFixture) checkAuthority(t *testing.T, a, b openapiclient.ClientView, action string) {
	t.Helper()
	ctx := context.Background()
	_, err := f.service.LoadVerificationMaterial(ctx, a.AK)
	if action == "scope" {
		require.NoError(t, err)
		scope, err := f.service.GetScope(ctx, a.ID, "play:live:apply")
		require.NoError(t, err)
		require.False(t, scope.Enabled)
		require.EqualValues(t, 2, scope.ScopeEpoch)
	} else {
		require.ErrorIs(t, err, openapiclient.ErrAuthenticationFailed)
	}
	query, err := f.service.GetScope(ctx, a.ID, "device:list")
	require.NoError(t, err)
	require.True(t, query.Enabled)
	require.EqualValues(t, 1, query.ScopeEpoch)
	_, err = f.service.LoadVerificationMaterial(ctx, b.AK)
	require.NoError(t, err)
}
