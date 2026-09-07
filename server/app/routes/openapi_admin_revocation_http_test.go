package routes

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/openapi/auth"
	"uvplatform.cn/uvp-gb28181/app/openapi/client"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

func TestOpenAPIAdminRootMetadataLifecycleTakesEffectImmediately(t *testing.T) {
	f := newOpenAPIAdminHTTPFixture(t)
	created := f.do(t, f.adminToken, http.MethodPost, openAPIAdminHTTPPathPrefix, `{"name":"metadata-lifecycle","ownerDeptId":10}`)
	require.Equal(t, http.StatusOK, created.status)
	var envelope struct {
		Data struct {
			Client    client.ClientView `json:"client"`
			SecretKey string            `json:"secretKey"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(created.body, &envelope))
	path := fmt.Sprintf("%s/%d", openAPIAdminHTTPPathPrefix, envelope.Data.Client.ID)
	sequence := 0
	query := func() int {
		t.Helper()
		sequence++
		input := auth.SignatureInput{Method: http.MethodGet, Path: "/openapi/v1/devices",
			AccessKey: envelope.Data.Client.AK, Audience: "admin-http-audience",
			Timestamp: fmt.Sprint(time.Now().Unix()), Nonce: fmt.Sprintf("%032x", sequence)}
		signature, err := auth.Sign(input, envelope.Data.SecretKey)
		require.NoError(t, err)
		request, err := http.NewRequest(http.MethodGet, f.server.URL+input.Path, nil)
		require.NoError(t, err)
		for key, value := range map[string]string{"X-UVP-Sign-Version": "1", "X-UVP-Access-Key": input.AccessKey,
			"X-UVP-Timestamp": input.Timestamp, "X-UVP-Nonce": input.Nonce, "X-UVP-Signature": signature} {
			request.Header.Set(key, value)
		}
		response, err := f.client.Do(request)
		require.NoError(t, err)
		_, err = io.Copy(io.Discard, response.Body)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		return response.StatusCode
	}
	mutate := func(method, suffix, body string, status int) openAPIAdminHTTPResponse {
		t.Helper()
		response := f.do(t, f.adminToken, method, path+suffix, body)
		require.Equal(t, status, response.status)
		return response
	}
	mutate(http.MethodPut, "/scopes", `{"rowVersion":1,"scopes":["device:list"]}`, http.StatusOK)
	require.Equal(t, http.StatusOK, query())
	var audit models.Audit
	require.NoError(t, f.db.Where("client_id = ? AND scope = ? AND result = ?", envelope.Data.Client.ID, "device:list", "success").First(&audit).Error)
	fingerprint := sha256.Sum256([]byte(envelope.Data.Client.AK))
	require.Equal(t, hex.EncodeToString(fingerprint[:]), audit.AKFingerprint)
	serializedAudit, err := json.Marshal(audit)
	require.NoError(t, err)
	require.NotContains(t, string(serializedAudit), envelope.Data.Client.AK)
	require.NotContains(t, string(serializedAudit), envelope.Data.SecretKey)
	mutate(http.MethodPut, "/scopes", `{"rowVersion":2,"scopes":[]}`, http.StatusOK)
	require.Equal(t, http.StatusForbidden, query())
	mutate(http.MethodPut, "/scopes", `{"rowVersion":3,"scopes":["device:list"]}`, http.StatusOK)
	mutate(http.MethodPost, "/disable", `{"rowVersion":4}`, http.StatusAccepted)
	require.Equal(t, http.StatusUnauthorized, query())
	enabled := mutate(http.MethodPost, "/enable", `{"rowVersion":5}`, http.StatusOK)
	var statusEnvelope struct {
		Data struct {
			Client client.ClientView `json:"client"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(enabled.body, &statusEnvelope))
	require.Equal(t, envelope.Data.Client.ID, statusEnvelope.Data.Client.ID, "enable must use the management UI status response DTO")
	require.Equal(t, models.StatusActive, statusEnvelope.Data.Client.Status)
	require.Equal(t, http.StatusOK, query())
	mutate(http.MethodPost, "/revoke", `{"rowVersion":6}`, http.StatusAccepted)
	require.Equal(t, http.StatusUnauthorized, query())
	mutate(http.MethodPost, "/enable", `{"rowVersion":7}`, http.StatusConflict)
	progress := f.do(t, f.adminToken, http.MethodGet, path+"/revocation-status", "")
	require.Equal(t, http.StatusOK, progress.status)
	require.Contains(t, string(progress.body), `"status":"unknown"`)
}

// This is the actual root JWT/Casbin/TLS/SQL chain, not a media-disconnect test.
func TestOpenAPIAdminRootDurableRevocation(t *testing.T) {
	for _, action := range []string{"disable", "revoke", "scopes"} {
		t.Run(action, func(t *testing.T) {
			f := newOpenAPIAdminHTTPFixture(t)
			a := createRootRevocationClient(t, f, "client-a")
			b := createRootRevocationClient(t, f, "client-b")
			aGrant, aViewer := seedRootRevocationMedia(t, f, a.ID)
			bGrant, bViewer := seedRootRevocationMedia(t, f, b.ID)
			path := fmt.Sprintf("%s/%d", openAPIAdminHTTPPathPrefix, a.ID)
			method, body := http.MethodPost, fmt.Sprintf(`{"rowVersion":%d}`, a.RowVersion)
			wantStatus := http.StatusAccepted
			if action == "scopes" {
				method, body, wantStatus = http.MethodPut, fmt.Sprintf(`{"rowVersion":%d,"scopes":["device:list"]}`, a.RowVersion), http.StatusOK
			}
			result := f.do(t, f.adminToken, method, path+"/"+action, body)
			require.Equal(t, wantStatus, result.status)
			require.Equal(t, "no-store", result.headers.Get("Cache-Control"))
			var storedA, storedB models.Client
			require.NoError(t, f.db.First(&storedA, a.ID).Error)
			require.NoError(t, f.db.First(&storedB, b.ID).Error)
			require.EqualValues(t, a.RowVersion+1, storedA.RowVersion)
			require.EqualValues(t, b.RowVersion, storedB.RowVersion)
			require.Equal(t, models.StatusActive, storedB.Status)
			require.NoError(t, f.db.First(&aGrant, "grant_id = ?", aGrant.GrantID).Error)
			require.NoError(t, f.db.First(&aViewer, aViewer.ID).Error)
			require.NoError(t, f.db.First(&bGrant, "grant_id = ?", bGrant.GrantID).Error)
			require.NoError(t, f.db.First(&bViewer, bViewer.ID).Error)
			require.Equal(t, models.GrantStateRevoked, aGrant.State)
			require.Equal(t, models.ViewerStateRevokePending, aViewer.State)
			require.Equal(t, models.GrantStateBound, bGrant.State)
			require.Equal(t, models.ViewerStateActive, bViewer.State)
			progress := f.do(t, f.adminToken, http.MethodGet, path+"/revocation-status", "")
			require.Equal(t, http.StatusOK, progress.status)
			require.Contains(t, string(progress.body), `"status":"pending"`)
			// Missing association evidence must never be translated to "closed".
			require.NoError(t, f.db.Delete(&aViewer).Error)
			progress = f.do(t, f.adminToken, http.MethodGet, path+"/revocation-status", "")
			require.Equal(t, http.StatusOK, progress.status)
			require.Contains(t, string(progress.body), `"status":"unknown"`)
			if action == "scopes" {
				require.Equal(t, models.StatusActive, storedA.Status)
				require.EqualValues(t, 1, storedA.AuthEpoch)
				var scope models.ClientScope
				require.NoError(t, f.db.Where("client_id = ? AND scope = ?", a.ID, "device:list").Take(&scope).Error)
				require.True(t, scope.Enabled)
				require.EqualValues(t, 1, scope.ScopeEpoch)
			} else {
				require.EqualValues(t, 2, storedA.AuthEpoch)
			}
		})
	}
}

func TestOpenAPIAdminRootRevocationAuditFailureRollsBack(t *testing.T) {
	f := newOpenAPIAdminHTTPFixture(t)
	a := createRootRevocationClient(t, f, "rollback-client")
	grant, viewer := seedRootRevocationMedia(t, f, a.ID)
	require.NoError(t, f.db.Exec(`CREATE TRIGGER reject_revoke_audit BEFORE INSERT ON sys_openapi_audit
		WHEN NEW.reason_class = 'client.disabled' BEGIN SELECT RAISE(ABORT, 'test audit failure'); END`).Error)
	path := fmt.Sprintf("%s/%d/disable", openAPIAdminHTTPPathPrefix, a.ID)
	body := fmt.Sprintf(`{"rowVersion":%d}`, a.RowVersion)
	result := f.do(t, f.adminToken, http.MethodPost, path, body)
	require.Equal(t, http.StatusServiceUnavailable, result.status)
	var stored models.Client
	require.NoError(t, f.db.First(&stored, a.ID).Error)
	require.Equal(t, models.StatusActive, stored.Status)
	require.EqualValues(t, a.RowVersion, stored.RowVersion)
	require.EqualValues(t, 1, stored.AuthEpoch)
	require.NoError(t, f.db.First(&grant, "grant_id = ?", grant.GrantID).Error)
	require.NoError(t, f.db.First(&viewer, viewer.ID).Error)
	require.Equal(t, models.GrantStateBound, grant.State)
	require.Equal(t, models.ViewerStateActive, viewer.State)
	// A successful retry proves the failure was at the audit boundary, not an
	// unwired service that happened to return the same 503.
	require.NoError(t, f.db.Exec("DROP TRIGGER reject_revoke_audit").Error)
	require.Equal(t, http.StatusAccepted, f.do(t, f.adminToken, http.MethodPost, path, body).status)
}

func TestOpenAPIAdminRootRevocationStoreFailureRollsBack(t *testing.T) {
	f := newOpenAPIAdminHTTPFixture(t)
	a := createRootRevocationClient(t, f, "store-failure-client")
	grant, _ := seedRootRevocationMedia(t, f, a.ID)
	require.NoError(t, f.db.Migrator().DropTable(&models.Viewer{}))
	path := fmt.Sprintf("%s/%d", openAPIAdminHTTPPathPrefix, a.ID)
	result := f.do(t, f.adminToken, http.MethodPost, path+"/disable", fmt.Sprintf(`{"rowVersion":%d}`, a.RowVersion))
	require.Equal(t, http.StatusServiceUnavailable, result.status)
	var stored models.Client
	require.NoError(t, f.db.First(&stored, a.ID).Error)
	require.Equal(t, models.StatusActive, stored.Status)
	require.Equal(t, a.RowVersion, stored.RowVersion)
	require.EqualValues(t, 1, stored.AuthEpoch)
	require.NoError(t, f.db.First(&grant, "grant_id = ?", grant.GrantID).Error)
	require.Equal(t, models.GrantStateBound, grant.State)
	var count int64
	require.NoError(t, f.db.Model(&models.Audit{}).Where("client_id = ? AND reason_class = ?", a.ID, "client.disabled").Count(&count).Error)
	require.Zero(t, count)
	progress := f.do(t, f.adminToken, http.MethodGet, path+"/revocation-status", "")
	require.Equal(t, http.StatusServiceUnavailable, progress.status)
}

func createRootRevocationClient(t *testing.T, f *openAPIAdminHTTPFixture, name string) client.ClientView {
	t.Helper()
	result := f.do(t, f.adminToken, http.MethodPost, openAPIAdminHTTPPathPrefix, fmt.Sprintf(`{"name":%q,"ownerDeptId":10}`, name))
	require.Equal(t, http.StatusOK, result.status)
	var created struct {
		Data struct {
			Client client.ClientView `json:"client"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(result.body, &created))
	path := fmt.Sprintf("%s/%d/scopes", openAPIAdminHTTPPathPrefix, created.Data.Client.ID)
	granted := f.do(t, f.adminToken, http.MethodPut, path, `{"rowVersion":1,"scopes":["device:list","play:live:apply"]}`)
	require.Equal(t, http.StatusOK, granted.status)
	var updated struct {
		Data client.ClientView `json:"data"`
	}
	require.NoError(t, json.Unmarshal(granted.body, &updated))
	return updated.Data
}

func seedRootRevocationMedia(t *testing.T, f *openAPIAdminHTTPFixture, clientID int64) (models.PlayGrant, models.Viewer) {
	t.Helper()
	now := time.Now().UTC()
	device, channel := "34020000001320000001", "34020000001310000001"
	node, boot, schema, vhost := "node-root-test", "0123456789abcdef0123456789abcdef", "rtmp", "__defaultVhost__"
	app, stream, protocol, generation := "live", "same-stream", "https-flv", uint64(1)
	grant := models.PlayGrant{
		GrantID: uuid.NewString(), ClientID: clientID, Scope: "play:live:apply",
		ClientEpoch: 1, ScopeEpoch: 1, DeviceEpoch: 1, DeviceID: &device, ChannelID: &channel,
		NodeUUID: &node, BootNonce: &boot, Schema: &schema, VHost: &vhost,
		App: &app, Stream: &stream, MediaGeneration: &generation, Protocol: &protocol,
		State: models.GrantStateBound, IssuedAt: now, ExpiresAt: now.Add(time.Minute), CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, f.db.Create(&grant).Error)
	viewer := models.Viewer{
		GrantID: grant.GrantID, NodeUUID: node, BootNonce: boot, Identifier: uuid.NewString(),
		Schema: schema, VHost: vhost, App: app, Stream: stream, MediaGeneration: generation,
		State: models.ViewerStateActive, LastSeenAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, f.db.Create(&viewer).Error)
	return grant, viewer
}
