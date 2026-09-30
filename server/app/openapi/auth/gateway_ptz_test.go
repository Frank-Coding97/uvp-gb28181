package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

func signedPTZRequest(t *testing.T, secret, nonce string) *http.Request {
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
	request := httptest.NewRequest(http.MethodGet, "http://example.test"+ptzPresetPath, nil)
	request.RequestURI = request.URL.RequestURI()
	request.Header.Set("X-UVP-Sign-Version", "1")
	request.Header.Set("X-UVP-Access-Key", input.AccessKey)
	request.Header.Set("X-UVP-Timestamp", input.Timestamp)
	request.Header.Set("X-UVP-Nonce", input.Nonce)
	request.Header.Set("X-UVP-Signature", signature)
	return request
}

func servePTZRequest(t *testing.T, gate *Gateway, request *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	ptzRouter(gate).ServeHTTP(response, request)
	return response
}

// capabilityReleaseItemSnapshot mirrors catalog.store's releaseItemSnapshot
// JSON shape. The fixture has to produce a byte-valid release because
// catalog.store.validateReleaseSnapshot re-derives the hash from the stored
// snapshots on every read: a placeholder "{}" snapshot makes LoadActive report
// the release as corrupted, the gateway answers 503, and the test that meant to
// assert CAPABILITY_DENIED would assert the wrong reason instead.
type capabilityReleaseItemSnapshot struct {
	CapabilityID           int64  `json:"capabilityId"`
	OperationID            int64  `json:"operationId"`
	GroupCode              string `json:"groupCode"`
	GroupName              string `json:"groupName"`
	CapabilityCode         string `json:"capabilityCode"`
	CapabilityName         string `json:"capabilityName"`
	OperationName          string `json:"operationName"`
	Scope                  string `json:"scope"`
	Method                 string `json:"method"`
	ExternalPath           string `json:"externalPath"`
	AdapterKey             string `json:"adapterKey"`
	AdapterContractVersion string `json:"adapterContractVersion"`
	ResourceType           string `json:"resourceType"`
	RiskLevel              string `json:"riskLevel"`
	IdempotencyMode        string `json:"idempotencyMode"`
	RequestSchema          string `json:"requestSchema"`
	ResponseSchema         string `json:"responseSchema"`
	SysAPIID               int64  `json:"sysApiId,omitempty"`
	SysAPIPath             string `json:"sysApiPath"`
	SysAPIMethod           string `json:"sysApiMethod"`
	Sort                   int    `json:"sort"`
}

func seedActiveCapabilityRelease(t *testing.T, db *gorm.DB, scopes ...string) {
	t.Helper()
	now := time.Now().UTC()
	require.NoError(t, db.AutoMigrate(&openapimodels.Release{}, &openapimodels.ReleaseItem{}, &openapimodels.RuntimeState{}))
	release := openapimodels.Release{
		Version: 1, Name: "test-release", Status: openapimodels.ReleaseStatusPublished,
		ItemCount: len(scopes), PublishedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&release).Error)
	items := make([]openapimodels.ReleaseItem, 0, len(scopes))
	snapshots := make([]string, 0, len(scopes))
	for index, scope := range scopes {
		// snapshotMatchesItem compares the decoded snapshot against the item
		// columns, including the nullable capability/operation foreign keys. Leaving
		// the pointers nil while the snapshot carried index+1 would make the release
		// look corrupted on read (LoadActive fails, gateway answers 503), which is
		// precisely the failure this fixture used to hide the real assertion behind.
		capabilityID := int64(index + 1)
		operationID := int64(index + 1)
		item := openapimodels.ReleaseItem{
			ReleaseID: release.ID, CapabilityID: &capabilityID, OperationID: &operationID,
			GroupCode: "device-management", GroupName: "设备管理",
			CapabilityCode: strings.ReplaceAll(scope, ":", "."), CapabilityName: scope,
			Scope: scope, Method: http.MethodGet, ExternalPath: "/openapi/v1/test/" + scope,
			AdapterKey: "test", AdapterContractVersion: "v1", ResourceType: "device",
			RiskLevel: "read", IdempotencyMode: "none", RequestSchema: "{}", ResponseSchema: "{}",
			CreatedAt: now,
		}
		raw, err := json.Marshal(capabilityReleaseItemSnapshot{
			CapabilityID: capabilityID, OperationID: operationID,
			GroupCode: item.GroupCode, GroupName: item.GroupName,
			CapabilityCode: item.CapabilityCode, CapabilityName: item.CapabilityName,
			OperationName: item.CapabilityName, Scope: item.Scope, Method: item.Method,
			ExternalPath: item.ExternalPath, AdapterKey: item.AdapterKey,
			AdapterContractVersion: item.AdapterContractVersion, ResourceType: item.ResourceType,
			RiskLevel: item.RiskLevel, IdempotencyMode: item.IdempotencyMode,
			RequestSchema: item.RequestSchema, ResponseSchema: item.ResponseSchema,
			SysAPIPath: "", SysAPIMethod: "",
		})
		require.NoError(t, err)
		item.SnapshotJSON = string(raw)
		items = append(items, item)
		snapshots = append(snapshots, item.SnapshotJSON)
	}
	require.NoError(t, db.Create(&items).Error)
	canonical, err := json.Marshal(snapshots)
	require.NoError(t, err)
	digest := sha256.Sum256(canonical)
	release.SnapshotHash = hex.EncodeToString(digest[:])
	require.NoError(t, db.Model(&release).Update("snapshot_hash", release.SnapshotHash).Error)

	activeRelease := release.ID
	require.NoError(t, db.Create(&openapimodels.RuntimeState{
		ID: 1, ActiveRelease: &activeRelease, ActiveVersion: release.Version,
		SnapshotHash: release.SnapshotHash, Status: openapimodels.RuntimeCatalogReady,
		RuntimeEpoch: 1, LoadedAt: &now, UpdatedAt: now,
	}).Error)
}

func TestOpenAPIPTZGatewayRejectsMissingScopeWithCapabilityDenied(t *testing.T) {
	dispatcher := &ptzDispatcherStub{}
	dispatcher.ready.Store(true)
	gate, _, secret := gatewayFixture(t)
	gate.ptz = dispatcher

	response := servePTZRequest(t, gate, signedPTZRequest(t, secret, strings.Repeat("b", 32)))

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

	response := servePTZRequest(t, gate, signedPTZRequest(t, secret, strings.Repeat("d", 32)))

	require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"code":"CAPABILITY_DENIED"`)
	require.Zero(t, dispatcher.calls.Load())
}

func TestOpenAPIPTZGatewayRejectsUnavailableRuntimeWithServiceUnavailable(t *testing.T) {
	gate, _, secret := gatewayFixture(t)

	response := servePTZRequest(t, gate, signedPTZRequest(t, secret, strings.Repeat("c", 32)))

	require.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"code":"SERVICE_UNAVAILABLE"`)
}
