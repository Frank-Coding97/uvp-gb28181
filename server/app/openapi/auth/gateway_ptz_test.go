package auth

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	openapimodels "uvplatform.cn/uvp-gb28181/app/openapi/models"
)

const (
	ptzTestDevice  = "34020000002000000010"
	ptzTestChannel = "34020000001320000010"
	ptzPresetPath  = "/openapi/v1/devices/" + ptzTestDevice + "/channels/" + ptzTestChannel + "/ptz/presets"
)

type ptzDispatcherStub struct {
	ready atomic.Bool
	calls atomic.Int32
}

func (s *ptzDispatcherStub) Ready() bool { return s.ready.Load() }

func (s *ptzDispatcherStub) Handle(context.Context, PTZRequest) (any, error) {
	s.calls.Add(1)
	return map[string]any{}, nil
}

func ptzRouter(gate *Gateway) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/openapi/v1/devices/:deviceId/channels/:channelId/ptz/presets", gate.Handler(PTZPresetListScope))
	return router
}

func signedPTZRequest(t *testing.T, secret, nonce string, secure bool) *http.Request {
	t.Helper()
	now := fmt.Sprint(time.Now().Unix())
	input := SignatureInput{
		Method:    http.MethodGet,
		Path:      ptzPresetPath,
		AccessKey: fmt.Sprintf("uvp_%032x", 1),
		Timestamp: now,
		Nonce:     nonce,
		Audience:  "test-audience",
	}
	signature, err := Sign(input, secret)
	require.NoError(t, err)
	scheme := "http"
	if secure {
		scheme = "https"
	}
	request := httptest.NewRequest(http.MethodGet, scheme+"://example.test"+ptzPresetPath, nil)
	request.RequestURI = request.URL.RequestURI()
	request.Header.Set("X-UVP-Sign-Version", "1")
	request.Header.Set("X-UVP-Access-Key", input.AccessKey)
	request.Header.Set("X-UVP-Timestamp", input.Timestamp)
	request.Header.Set("X-UVP-Nonce", input.Nonce)
	request.Header.Set("X-UVP-Signature", signature)
	if secure {
		request.TLS = &tls.ConnectionState{HandshakeComplete: true}
	}
	return request
}

func servePTZRequest(t *testing.T, gate *Gateway, request *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	ptzRouter(gate).ServeHTTP(response, request)
	return response
}

func seedActiveCapabilityRelease(t *testing.T, db *gorm.DB, scopes ...string) {
	t.Helper()
	now := time.Now().UTC()
	require.NoError(t, db.AutoMigrate(&openapimodels.Release{}, &openapimodels.ReleaseItem{}, &openapimodels.RuntimeState{}))
	release := openapimodels.Release{
		Version: 1, Name: "test-release", Status: openapimodels.ReleaseStatusPublished,
		SnapshotHash: "test-release-hash", ItemCount: len(scopes), PublishedAt: &now,
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&release).Error)
	for _, scope := range scopes {
		require.NoError(t, db.Create(&openapimodels.ReleaseItem{
			ReleaseID: release.ID, GroupCode: "device-management", GroupName: "设备管理",
			CapabilityCode: strings.ReplaceAll(scope, ":", "."), CapabilityName: scope,
			Scope: scope, Method: http.MethodGet, ExternalPath: "/openapi/v1/test/" + scope,
			AdapterKey: "test", AdapterContractVersion: "v1", ResourceType: "device",
			RiskLevel: "read", IdempotencyMode: "none", RequestSchema: "{}", ResponseSchema: "{}",
			SnapshotJSON: "{}", CreatedAt: now,
		}).Error)
	}
	activeRelease := release.ID
	require.NoError(t, db.Create(&openapimodels.RuntimeState{
		ID: 1, ActiveRelease: &activeRelease, ActiveVersion: release.Version,
		SnapshotHash: release.SnapshotHash, Status: openapimodels.RuntimeCatalogReady,
		RuntimeEpoch: 1, LoadedAt: &now, UpdatedAt: now,
	}).Error)
}

func TestOpenAPIPTZGatewayRejectsNonHTTPSWithAuthenticationFailed(t *testing.T) {
	dispatcher := &ptzDispatcherStub{}
	dispatcher.ready.Store(true)
	gate, _, secret := gatewayFixture(t)
	gate.ptz = dispatcher

	response := servePTZRequest(t, gate, signedPTZRequest(t, secret, strings.Repeat("a", 32), false))

	require.Equal(t, http.StatusUnauthorized, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"code":"AUTHENTICATION_FAILED"`)
	require.Zero(t, dispatcher.calls.Load())
}

func TestOpenAPIPTZGatewayRejectsMissingScopeWithCapabilityDenied(t *testing.T) {
	dispatcher := &ptzDispatcherStub{}
	dispatcher.ready.Store(true)
	gate, _, secret := gatewayFixture(t)
	gate.ptz = dispatcher

	response := servePTZRequest(t, gate, signedPTZRequest(t, secret, strings.Repeat("b", 32), true))

	require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"code":"CAPABILITY_DENIED"`)
	require.Zero(t, dispatcher.calls.Load())
}

func TestOpenAPIPTZGatewayRejectsEnabledUnpublishedScopeWithCapabilityDenied(t *testing.T) {
	dispatcher := &ptzDispatcherStub{}
	dispatcher.ready.Store(true)
	gate, db, secret := gatewayFixture(t)
	gate.ptz = dispatcher
	require.NoError(t, db.Create(&openapimodels.ClientScope{
		ClientID: 1, Scope: PTZPresetListScope, Enabled: true, ScopeEpoch: 1, UpdatedAt: time.Now().UTC(),
	}).Error)
	seedActiveCapabilityRelease(t, db, "device:list")

	response := servePTZRequest(t, gate, signedPTZRequest(t, secret, strings.Repeat("d", 32), true))

	require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"code":"CAPABILITY_DENIED"`)
	require.Zero(t, dispatcher.calls.Load())
}

func TestOpenAPIPTZGatewayRejectsUnavailableRuntimeWithServiceUnavailable(t *testing.T) {
	gate, _, secret := gatewayFixture(t)

	response := servePTZRequest(t, gate, signedPTZRequest(t, secret, strings.Repeat("c", 32), true))

	require.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"code":"SERVICE_UNAVAILABLE"`)
}
