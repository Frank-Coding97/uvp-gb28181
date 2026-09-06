package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/handler"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	openapiclient "uvplatform.cn/uvp-gb28181/app/openapi/client"
	"uvplatform.cn/uvp-gb28181/app/openapi/limit"
	"uvplatform.cn/uvp-gb28181/app/openapi/media"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

const (
	mediaLifecycleBoot    = "200102030405060708090a0b0c0d0e0f"
	mediaLifecycleNode    = nativeGrantViewerNodeUUID
	mediaLifecycleDevice  = nativeGrantViewerDeviceID
	mediaLifecycleChannel = nativeGrantViewerChannelID
	mediaLifecycleStream  = nativeGrantViewerDeviceID + "_" + nativeGrantViewerChannelID
)

// TestOpenAPIMediaLifecycle is an internal SQLite composition fixture. It
// proves the grant/application/Hook/revocation-worker boundaries together; it
// is not T17/T18 evidence and never starts a root service or contacts ZLM.
func TestOpenAPIMediaLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openMediaLifecycleDB(t)
	fixture, err := prepareNativeGrantViewerFixture(t, db.WithContext(ctx))
	require.NoError(t, err)
	var extraClientIDs []int64
	defer func() {
		cleanupNativeRevocationClients(t, db, extraClientIDs)
		fixture.cleanup(t, db)
	}()

	clock := time.Date(2026, 9, 6, 22, 0, 0, 123456789, time.UTC)
	now := func() time.Time { return clock }
	signer, err := playauth.NewSigner([]byte(strings.Repeat("m", 32)), playauth.WithNow(now))
	require.NoError(t, err)
	// NodeAuthority is the real SQL-only runtime identity check. It is not the
	// future T18 topology/qualification provider; that proof remains the fake
	// QualificationProvider below.
	grantService, err := playauth.NewOpenAPIGrantService(db, signer, media.NewNodeAuthority(), now)
	require.NoError(t, err)
	quota := limit.NewQuota(db, now)
	store := playauth.NewOpenAPIRevocationStore(db, now)

	bClient, err := insertNativeRevocationClient(db, "uvp_lifecycle_b", "lifecycle B", clock)
	require.NoError(t, err)
	extraClientIDs = append(extraClientIDs, bClient.ID)
	cClient, err := insertNativeRevocationClient(db, "uvp_lifecycle_c", "lifecycle C", clock)
	require.NoError(t, err)
	extraClientIDs = append(extraClientIDs, cClient.ID)

	provider := &mediaLifecycleQualificationProvider{ticket: media.QualificationTicket{
		QualificationID: "lifecycle-ticket-10s", NodeID: 1, NodeUUID: mediaLifecycleNode,
		NodeRevision: 1, BootNonce: mediaLifecycleBoot, Protocol: play.QualifiedProtocolHTTPSFLV,
		MediaOrigin: "https://media.example:8443", ExpiresAt: clock.Add(10 * time.Second),
	}}
	player := &mediaLifecyclePlayer{result: &play.Result{
		StreamID: mediaLifecycleStream, App: "rtp", Generation: 9,
		Node: &play.ResultNode{ID: 1, MediaServerUUID: mediaLifecycleNode, Revision: 1, Host: "fixture-node"},
		URLs: play.PlaybackURLs{HTTPSFLV: stringPtr("https://media.example:8443/rtp/" + mediaLifecycleStream + ".live.flv")},
	}}
	live := media.NewLiveApplication(provider, player, grantService, true, media.WithLiveApplicationClock(now))

	// The qualification window is intentionally short; the fixed v3 grant TTL
	// remains independently enforced by the real Signer/OpenAPIGrantService.
	ticket, err := live.Preflight(ctx, mediaLifecycleDevice, mediaLifecycleChannel, play.QualifiedProtocolHTTPSFLV)
	require.NoError(t, err)
	require.Equal(t, 10*time.Second, ticket.ExpiresAt.Sub(clock))

	aNormal := applyLifecycleGrant(t, ctx, live, quota, fixture.client.ID, ticket)
	bGrant := applyLifecycleGrant(t, ctx, live, quota, bClient.ID, ticket)
	cGrant := applyLifecycleGrant(t, ctx, live, quota, cClient.ID, ticket)
	require.NotEqual(t, aNormal.token, bGrant.token)
	require.NotEqual(t, bGrant.token, cGrant.token)
	require.Equal(t, playauth.OpenAPIPlayTTL, aNormal.expiresAt.Sub(clock.Truncate(time.Second)))

	previousConfig := app.ConfigYml
	app.ConfigYml = &mediaLifecycleConfig{values: map[string]interface{}{
		gbconfig.PlayAuthEnabledConfigKey: true,
	}}
	t.Cleanup(func() { app.ConfigYml = previousConfig })

	hookNode := &node.Node{ID: 1, MediaServerUUID: mediaLifecycleNode, APISecret: "lifecycle-hook-secret"}
	hook := handler.NewHookController(stream.NewNotifier())
	hook.SetOpenAPIPlayAuthorization(signer, grantService)
	hook.SetPlaybackMediaContextResolver(mediaLifecyclePlaybackResolver{binding: playauth.Binding{
		DeviceID: mediaLifecycleDevice, ChannelID: mediaLifecycleChannel, App: "rtp",
		Stream: mediaLifecycleStream, MediaServerID: mediaLifecycleNode, MediaGeneration: 9,
	}})
	authenticator := handler.NewHookAuthenticator()
	authenticator.SetResolver(mediaLifecycleHookNodeResolver{node: hookNode})
	playPath := mediaLifecycleHookPath(t, hookNode, playauth.HookOnPlay)
	playEngine := gin.New()
	playEngine.POST("/hook", authenticator.Middleware(playauth.HookOnPlay, handler.HookRejectAdmission), hook.OnPlay)

	bindLifecycleViewer(t, playEngine, playPath, "1-1", aNormal.token)
	bindLifecycleViewer(t, playEngine, playPath, "1-2", bGrant.token)
	bindLifecycleViewer(t, playEngine, playPath, "1-3", cGrant.token)
	require.Equal(t, int64(1), occupiedLifecycle(t, quota, fixture.client.ID))
	require.Equal(t, int64(1), occupiedLifecycle(t, quota, bClient.ID))
	require.Equal(t, int64(1), occupiedLifecycle(t, quota, cClient.ID))

	// A's first session exits normally through the authenticated flow Hook.
	hook.SetOpenAPIFlowObserver(grantService)
	flowPath := mediaLifecycleHookPath(t, hookNode, playauth.HookOnFlowReport)
	flowEngine := gin.New()
	flowEngine.POST("/hook", authenticator.Middleware(playauth.HookOnFlowReport, handler.HookRejectNotification), hook.OnFlowReport)
	postMediaLifecycleJSON(t, flowEngine, flowPath, mediaLifecycleFlowBody("1-1"))
	require.Equal(t, models.ViewerStateClosed, lifecycleViewerState(t, db, aNormal.grantID))
	require.Zero(t, occupiedLifecycle(t, quota, fixture.client.ID), "normal flow releases A's bound quota")
	require.Equal(t, models.ViewerStateActive, lifecycleViewerState(t, db, bGrant.grantID))
	require.Equal(t, models.ViewerStateActive, lifecycleViewerState(t, db, cGrant.grantID))

	// A obtains a second independent grant for the revoke/worker half. This
	// keeps the normal-flow and revoke-flow assertions independently durable.
	aRevoke := applyLifecycleGrant(t, ctx, live, quota, fixture.client.ID, ticket)
	bindLifecycleViewer(t, playEngine, playPath, "1-4", aRevoke.token)
	require.Equal(t, int64(1), occupiedLifecycle(t, quota, fixture.client.ID))

	var aBefore models.Client
	require.NoError(t, db.First(&aBefore, "id = ?", fixture.client.ID).Error)
	revokeIntent := openapiclient.RevocationIntent{
		ClientID: fixture.client.ID, ClientEpoch: aBefore.AuthEpoch + 1,
		Reason: "client.disabled", CreatedAt: clock,
	}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		updated := tx.Model(&models.Client{}).
			Where("id = ? AND row_version = ?", aBefore.ID, aBefore.RowVersion).
			Updates(map[string]interface{}{"status": models.StatusDisabled, "auth_epoch": revokeIntent.ClientEpoch, "row_version": aBefore.RowVersion + 1, "updated_at": clock})
		if updated.Error != nil || updated.RowsAffected != 1 {
			return errors.New("lifecycle revoke client update failed")
		}
		return store.RecordRevocationIntent(ctx, tx, revokeIntent)
	}))
	require.Equal(t, models.GrantStateRevoked, lifecycleGrantState(t, db, aRevoke.grantID))
	require.Equal(t, models.ViewerStateRevokePending, lifecycleViewerState(t, db, aRevoke.grantID))

	// A late flow is accepted by the Hook endpoint but only refreshes liveness;
	// the revoked grant remains the worker's responsibility.
	postMediaLifecycleJSON(t, flowEngine, flowPath, mediaLifecycleFlowBody("1-4"))
	require.Equal(t, models.ViewerStateRevokePending, lifecycleViewerState(t, db, aRevoke.grantID))

	control := &mediaLifecycleRevocationControl{
		boot:     mediaLifecycleBoot,
		players:  []zlm.MediaPlayer{{Identifier: "1-4"}, {Identifier: "1-2"}, {Identifier: "1-3"}},
		sessions: []zlm.Session{{ID: "1-4", Identifier: "1-4", Type: "tcp"}, {ID: "1-2", Identifier: "1-2", Type: "tcp"}, {ID: "1-3", Identifier: "1-3", Type: "tcp"}},
	}
	factory := &mediaLifecycleRevocationFactory{runtime: media.RevocationRuntime{
		Control: control, CurrentBootNonce: mediaLifecycleBoot, HookBudget: 5 * time.Second, Trusted: true,
	}}
	worker := media.NewRevocationWorker(db, factory, now, 10)
	firstTick, err := worker.Tick(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, firstTick.Claimed)
	require.Equal(t, models.ViewerStateRevokePending, lifecycleViewerState(t, db, aRevoke.grantID))
	require.Equal(t, media.RevocationErrorShutdownScheduled, lifecycleViewerError(t, db, aRevoke.grantID))
	require.Equal(t, []string{"1-4"}, control.kickCalls)

	// The worker obtains a fresh player and session snapshot after the lease;
	// only exact-A absence closes A. B/C remain in the runtime snapshots and
	// are never kicked.
	control.setTargetAbsent("1-4")
	players, sessions := control.snapshotIdentifiers()
	require.Equal(t, []string{"1-2", "1-3"}, players)
	require.Equal(t, []string{"1-2", "1-3"}, sessions)
	clock = clock.Add(7 * time.Second)
	secondTick, err := worker.Tick(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, secondTick.Closed)
	require.Equal(t, models.ViewerStateClosed, lifecycleViewerState(t, db, aRevoke.grantID))
	require.Equal(t, media.RevocationErrorKicked, lifecycleViewerError(t, db, aRevoke.grantID))
	require.Equal(t, []string{"1-4"}, control.kickCalls, "B/C and unrelated stream identifiers are untouched")
	require.Equal(t, 2, control.playerCalls, "worker used a fresh player snapshot for both phases")
	require.Equal(t, 2, control.sessionCalls, "worker used a fresh session snapshot for both phases")
	require.Zero(t, occupiedLifecycle(t, quota, fixture.client.ID))
	require.Equal(t, models.ViewerStateActive, lifecycleViewerState(t, db, bGrant.grantID))
	require.Equal(t, models.ViewerStateActive, lifecycleViewerState(t, db, cGrant.grantID))

	t.Log("internal composition fixture: 10s qualification window remained independent from the real 120s grant; A normal flow released quota, late revoked flow stayed pending, exact-A fresh kick/absence closed A, and B/C remained in snapshots")
}

type mediaLifecycleQualificationProvider struct{ ticket media.QualificationTicket }

func (p *mediaLifecycleQualificationProvider) Prepare(context.Context, media.QualificationRequest) (media.QualificationTicket, error) {
	return p.ticket, nil
}

func (p *mediaLifecycleQualificationProvider) Validate(context.Context, media.QualificationRequest, media.QualificationTicket) error {
	return nil
}

type mediaLifecyclePlayer struct {
	// LivePlayer deliberately exposes only EnsureLive. This composition test
	// therefore has no Stop capability to exercise or accidentally wire.
	result  *play.Result
	ensureN int
}

func (p *mediaLifecyclePlayer) EnsureLive(context.Context, play.Request) (*play.Result, error) {
	p.ensureN++
	return p.result, nil
}

type mediaLifecyclePlaybackResolver struct{ binding playauth.Binding }

func (r mediaLifecyclePlaybackResolver) ResolvePlaybackMediaContext(appName, streamID, mediaServerID string) (playauth.Binding, error) {
	if r.binding.App != appName || r.binding.Stream != streamID || r.binding.MediaServerID != mediaServerID {
		return playauth.Binding{}, errors.New("lifecycle media context mismatch")
	}
	return r.binding, nil
}

type mediaLifecycleHookNodeResolver struct{ node *node.Node }

func (r mediaLifecycleHookNodeResolver) GetByUUID(uuid string) (*node.Node, bool) {
	if r.node == nil || r.node.MediaServerUUID != uuid {
		return nil, false
	}
	return r.node, true
}

type mediaLifecycleConfig struct{ values map[string]interface{} }

func (c *mediaLifecycleConfig) ConfigFileChangeListen(...func()) {}
func (c *mediaLifecycleConfig) Get(key string) interface{}       { return c.values[key] }
func (c *mediaLifecycleConfig) GetString(key string) string {
	value, _ := c.values[key].(string)
	return value
}
func (c *mediaLifecycleConfig) GetBool(key string) bool {
	value, _ := c.values[key].(bool)
	return value
}
func (c *mediaLifecycleConfig) GetInt(key string) int { value, _ := c.values[key].(int); return value }
func (c *mediaLifecycleConfig) GetInt32(key string) int32 {
	value, _ := c.values[key].(int32)
	return value
}
func (c *mediaLifecycleConfig) GetInt64(key string) int64 {
	value, _ := c.values[key].(int64)
	return value
}
func (c *mediaLifecycleConfig) GetFloat64(key string) float64 {
	value, _ := c.values[key].(float64)
	return value
}
func (c *mediaLifecycleConfig) GetDuration(key string) time.Duration {
	value, _ := c.values[key].(time.Duration)
	return value
}
func (c *mediaLifecycleConfig) GetStringSlice(key string) []string {
	value, _ := c.values[key].([]string)
	return value
}
func (c *mediaLifecycleConfig) GetUintSlice(key string) []uint {
	value, _ := c.values[key].([]uint)
	return value
}
func (c *mediaLifecycleConfig) Set(key string, value interface{}) { c.values[key] = value }
func (c *mediaLifecycleConfig) SaveConfig() error                 { return nil }

type mediaLifecycleRevocationFactory struct{ runtime media.RevocationRuntime }

func (f *mediaLifecycleRevocationFactory) Resolve(context.Context, string) (media.RevocationRuntime, error) {
	return f.runtime, nil
}

type mediaLifecycleRevocationControl struct {
	mu           sync.Mutex
	boot         string
	players      []zlm.MediaPlayer
	sessions     []zlm.Session
	playerCalls  int
	sessionCalls int
	kickCalls    []string
}

func (c *mediaLifecycleRevocationControl) GetRuntimeMediaPlayers(context.Context, zlm.StreamTarget) (zlm.RuntimePlayers, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.playerCalls++
	players := append([]zlm.MediaPlayer(nil), c.players...)
	if players == nil {
		players = []zlm.MediaPlayer{}
	}
	return zlm.RuntimePlayers{BootNonce: c.boot, Players: players}, nil
}

func (c *mediaLifecycleRevocationControl) GetRuntimeSessions(context.Context) (zlm.RuntimeSessions, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessionCalls++
	sessions := append([]zlm.Session(nil), c.sessions...)
	if sessions == nil {
		sessions = []zlm.Session{}
	}
	return zlm.RuntimeSessions{BootNonce: c.boot, Sessions: sessions}, nil
}

func (c *mediaLifecycleRevocationControl) KickSessionIfMatch(_ context.Context, bootNonce, identifier string) (zlm.ConditionalKickResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if bootNonce != c.boot {
		return zlm.KickRuntimeMismatch, nil
	}
	c.kickCalls = append(c.kickCalls, identifier)
	return zlm.KickShutdownScheduled, nil
}

func (c *mediaLifecycleRevocationControl) setTargetAbsent(identifier string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	originalPlayers := append([]zlm.MediaPlayer(nil), c.players...)
	players := make([]zlm.MediaPlayer, 0, len(originalPlayers))
	for _, player := range originalPlayers {
		if player.Identifier != identifier {
			players = append(players, player)
		}
	}
	c.players = players
	originalSessions := append([]zlm.Session(nil), c.sessions...)
	sessions := make([]zlm.Session, 0, len(originalSessions))
	for _, session := range originalSessions {
		if session.Identifier != identifier && session.ID != identifier {
			sessions = append(sessions, session)
		}
	}
	c.sessions = sessions
}

func (c *mediaLifecycleRevocationControl) snapshotIdentifiers() ([]string, []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	players := make([]string, 0, len(c.players))
	for _, player := range c.players {
		players = append(players, player.Identifier)
	}
	sessions := make([]string, 0, len(c.sessions))
	for _, session := range c.sessions {
		identifier := session.Identifier
		if identifier == "" {
			identifier = session.ID
		}
		sessions = append(sessions, identifier)
	}
	return players, sessions
}

type mediaLifecycleGrant struct {
	grantID   string
	token     string
	expiresAt time.Time
}

func applyLifecycleGrant(t *testing.T, ctx context.Context, live *media.LiveApplication, quota *limit.Quota, clientID int64, ticket media.QualificationTicket) mediaLifecycleGrant {
	t.Helper()
	reservation, err := quota.ReservePending(ctx, limit.ReservationRequest{
		ClientID: clientID, Scope: limit.PlayLiveApplyScope,
		DeviceID: mediaLifecycleDevice, ChannelID: mediaLifecycleChannel,
	})
	require.NoError(t, err)
	data, err := live.Apply(ctx, media.ApplyRequest{
		ClientID: clientID, GrantID: reservation.GrantID, DeviceID: mediaLifecycleDevice,
		ChannelID: mediaLifecycleChannel, Ticket: ticket,
	})
	require.NoError(t, err)
	parsed, err := url.Parse(data.URL)
	require.NoError(t, err)
	token := parsed.Query().Get(playauth.QueryParameter)
	require.NotEmpty(t, token)
	require.Equal(t, reservation.GrantID, data.AuthorizationID)
	return mediaLifecycleGrant{grantID: reservation.GrantID, token: token, expiresAt: data.ExpiresAt}
}

func bindLifecycleViewer(t *testing.T, engine *gin.Engine, path, identifier, token string) {
	t.Helper()
	response := postMediaLifecycleJSON(t, engine, path, gin.H{
		"id": identifier, "bootNonce": mediaLifecycleBoot, "protocol": "https",
		"app": "rtp", "stream": mediaLifecycleStream, "schema": "rtmp", "vhost": "__defaultVhost__",
		"mediaServerId": mediaLifecycleNode,
		"params":        url.Values{playauth.QueryParameter: []string{token}}.Encode(),
	})
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), `"code":0`)
}

func mediaLifecycleFlowBody(identifier string) gin.H {
	return gin.H{
		"id": identifier, "mediaServerId": mediaLifecycleNode, "bootNonce": mediaLifecycleBoot,
		"protocol": "https", "schema": "rtmp", "vhost": "__defaultVhost__", "app": "rtp",
		"stream": mediaLifecycleStream, "player": true,
	}
}

func mediaLifecycleHookPath(t *testing.T, hookNode *node.Node, event playauth.HookEvent) string {
	t.Helper()
	capability, err := playauth.HookCapability(hookNode.APISecret, hookNode.MediaServerUUID, event)
	require.NoError(t, err)
	return "/hook?node=" + url.QueryEscape(hookNode.MediaServerUUID) + "&cap=" + url.QueryEscape(capability)
}

func postMediaLifecycleJSON(t *testing.T, engine *gin.Engine, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, req)
	return response
}

func openMediaLifecycleDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "media-lifecycle.sqlite")+"?_busy_timeout=5000"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	raw.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.Exec("PRAGMA foreign_keys = ON").Error)
	require.NoError(t, db.AutoMigrate(&models.Client{}, &models.ClientScope{}, &models.PlayGrant{}, &models.Viewer{}, &models.Audit{}))
	require.NoError(t, db.Exec(`CREATE TABLE gb_device (
		id INTEGER PRIMARY KEY, fixture_label TEXT NOT NULL, device_id TEXT NOT NULL UNIQUE,
		name TEXT NOT NULL DEFAULT '', alias TEXT NOT NULL DEFAULT '', manufacturer TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '',
		status INTEGER, owner_dept_id INTEGER NOT NULL DEFAULT 0, access_epoch INTEGER NOT NULL DEFAULT 1, cleanup_completed_epoch INTEGER NOT NULL DEFAULT 1, deleted_at DATETIME NULL)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE meta_node (
		id INTEGER PRIMARY KEY, fixture_label TEXT NOT NULL, revision INTEGER NOT NULL DEFAULT 1,
		state TEXT NOT NULL DEFAULT 'active', media_server_uuid TEXT NOT NULL DEFAULT '', current_boot_nonce TEXT, retired_boot_history TEXT,
		runtime_epoch INTEGER NOT NULL DEFAULT 0, runtime_protocol_version INTEGER NOT NULL DEFAULT 0,
		runtime_confirmed_revision INTEGER NOT NULL DEFAULT 0, runtime_confirmed_at DATETIME NULL,
		runtime_identity_status TEXT NOT NULL DEFAULT 'unknown')`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE sys_department (id INTEGER PRIMARY KEY, status INTEGER NOT NULL, deleted_at DATETIME NULL)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE gb_channel (
		id INTEGER PRIMARY KEY, channel_id TEXT NOT NULL, device_id TEXT NOT NULL, name TEXT NOT NULL DEFAULT '', alias TEXT NOT NULL DEFAULT '',
		manufacturer TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '', status INTEGER, ptz_type INTEGER NOT NULL DEFAULT 0,
		owner_dept_id INTEGER NOT NULL, deleted_at DATETIME NULL)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE openapi_test_node_qualification (
		node_uuid TEXT NOT NULL, boot_nonce TEXT NOT NULL, protocol TEXT NOT NULL, is_qualified INTEGER NOT NULL,
		PRIMARY KEY (node_uuid, boot_nonce, protocol))`).Error)
	require.NoError(t, db.Exec(`INSERT INTO gb_device (id, fixture_label, device_id, status, owner_dept_id, access_epoch) VALUES (1, 'keep-device', '34020000000000000001', 1, 0, 1)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO meta_node (id, fixture_label) VALUES (1, 'keep-node')`).Error)
	return db
}

func occupiedLifecycle(t *testing.T, quota *limit.Quota, clientID int64) int64 {
	t.Helper()
	occupied, err := quota.Occupied(context.Background(), clientID)
	require.NoError(t, err)
	return occupied
}

func lifecycleGrantState(t *testing.T, db *gorm.DB, grantID string) models.GrantState {
	t.Helper()
	var row models.PlayGrant
	require.NoError(t, db.First(&row, "grant_id = ?", grantID).Error)
	return row.State
}

func lifecycleViewerState(t *testing.T, db *gorm.DB, grantID string) models.ViewerState {
	t.Helper()
	var row models.Viewer
	require.NoError(t, db.Where("grant_id = ?", grantID).First(&row).Error)
	return row.State
}

func lifecycleViewerError(t *testing.T, db *gorm.DB, grantID string) string {
	t.Helper()
	var row models.Viewer
	require.NoError(t, db.Where("grant_id = ?", grantID).First(&row).Error)
	return row.LastErrorClass
}

func stringPtr(value string) *string { return &value }
