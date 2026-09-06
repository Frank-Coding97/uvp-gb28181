package auth

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
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
	"uvplatform.cn/uvp-gb28181/app/openapi/client"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

func gatewayFixture(t *testing.T) (*Gateway, *gorm.DB, string) {
	t.Helper()
	db := admissionDB(t)
	require.NoError(t, db.Exec("CREATE TABLE sys_department (id INTEGER PRIMARY KEY,status INTEGER,deleted_at DATETIME)").Error)
	require.NoError(t, db.Exec("INSERT INTO sys_department(id,status) VALUES(10,1)").Error)
	keys, err := client.NewSecretManager(bytes.Repeat([]byte{1}, 32), "test")
	require.NoError(t, err)
	secret := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	ciphertext, iv, err := keys.Encrypt(1, fmt.Sprintf("uvp_%032x", 1), 1, secret)
	require.NoError(t, err)
	require.NoError(t, db.Model(&models.Client{}).Where("id = 1").Updates(map[string]any{"secret_ciphertext": ciphertext, "secret_iv": iv}).Error)
	gate, err := NewGateway(context.Background(), db, keys, GatewayConfig{Audience: "test-audience", Timeout: time.Second, AuditReserve: 200 * time.Millisecond, MaxInFlight: 2})
	require.NoError(t, err)
	return gate, db, secret
}

func gatewayCall(t *testing.T, gate *Gateway, secret, nonce string, mutate func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/openapi/v1/devices", gate.Handler("device:list"))
	now := fmt.Sprint(time.Now().Unix())
	input := SignatureInput{Method: "GET", Path: "/openapi/v1/devices", AccessKey: fmt.Sprintf("uvp_%032x", 1), Timestamp: now, Nonce: nonce, Audience: "test-audience"}
	signature, err := Sign(input, secret)
	require.NoError(t, err)
	r := httptest.NewRequest("GET", input.Path, nil)
	r.TLS = &tls.ConnectionState{HandshakeComplete: true}
	for key, value := range map[string]string{"X-UVP-Sign-Version": "1", "X-UVP-Access-Key": input.AccessKey, "X-UVP-Timestamp": now, "X-UVP-Nonce": nonce, "X-UVP-Signature": signature} {
		r.Header.Set(key, value)
	}
	out := httptest.NewRecorder()
	if mutate != nil {
		mutate(r)
	}
	router.ServeHTTP(out, r)
	return out
}

func TestOpenAPIGatewayDeadlineNeverWritesLateResult(t *testing.T) {
	gate, _, secret := gatewayFixture(t)
	gate.config.Timeout = 30 * time.Millisecond
	release := make(chan struct{})
	var ended atomic.Bool
	gate.run = func(ctx context.Context, request gatewayRequest) gatewayResponse {
		<-release
		ended.Store(true)
		return gatewayResponse{status: 200, body: []byte(`{"lateSecret":"must-not-write"}`)}
	}
	start := time.Now()
	out := gatewayCall(t, gate, secret, strings.Repeat("a", 32), nil)
	require.Less(t, time.Since(start), 500*time.Millisecond)
	require.Equal(t, 503, out.Code)
	require.NotContains(t, out.Body.String(), "lateSecret")
	require.False(t, ended.Load())
	// A timed-out worker keeps its slot until it actually exits.
	require.Len(t, gate.slots, 1)
	body := out.Body.String()
	close(release)
	require.Eventually(t, func() bool { return ended.Load() && len(gate.slots) == 0 }, time.Second, time.Millisecond)
	require.Equal(t, body, out.Body.String())
}

func TestOpenAPIGatewayRejectsBeforeBusinessOrNonce(t *testing.T) {
	for _, kind := range []string{"signature", "jwt-only", "duplicate-header", "expired", "no-scope", "inactive-dept", "untrusted-proxy", "audit-start-failure", "client-store-failure"} {
		t.Run(kind, func(t *testing.T) {
			gate, db, secret := gatewayFixture(t)
			var calls atomic.Int32
			gate.read = func(context.Context, *gorm.DB, metadataInput) (any, error) { calls.Add(1); return nil, nil }
			status := 401
			switch kind {
			case "duplicate-header":
				status = 400
			case "client-store-failure":
				require.NoError(t, db.Migrator().DropTable(&models.Client{}))
				status = 503
			case "no-scope":
				require.NoError(t, db.Where("client_id = ?", 1).Delete(&models.ClientScope{}).Error)
				status = 403
			case "inactive-dept":
				require.NoError(t, db.Exec("UPDATE sys_department SET status=0").Error)
				status = 404
			case "audit-start-failure":
				require.NoError(t, db.Migrator().DropTable(&models.Audit{}))
				status = 503
			}
			out := gatewayCall(t, gate, secret, strings.Repeat("c", 32), func(r *http.Request) {
				switch kind {
				case "signature":
					r.Header.Set("X-UVP-Signature", strings.Repeat("0", 64))
				case "jwt-only":
					r.Header.Del("X-UVP-Signature")
					r.Header.Set("Authorization", "Bearer test-jwt-not-machine-authority")
				case "duplicate-header":
					r.Header.Add("X-UVP-Nonce", strings.Repeat("c", 32))
				case "untrusted-proxy":
					r.TLS = nil
					r.RemoteAddr = "198.51.100.4:123"
					r.Header.Set("X-Forwarded-Proto", "https")
				case "expired":
					r.Header.Set("X-UVP-Timestamp", fmt.Sprint(time.Now().Add(-301*time.Second).Unix()))
					sig, err := Sign(SignatureInput{Method: r.Method, Path: r.URL.Path, AccessKey: r.Header.Get("X-UVP-Access-Key"), Timestamp: r.Header.Get("X-UVP-Timestamp"), Nonce: r.Header.Get("X-UVP-Nonce"), Audience: "test-audience"}, secret)
					require.NoError(t, err)
					r.Header.Set("X-UVP-Signature", sig)
				}
			})
			require.Equal(t, status, out.Code, out.Body.String())
			require.Zero(t, calls.Load())
			var count int64
			require.NoError(t, db.Model(&models.Nonce{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestOpenAPIGatewayLateAdmissionDoesNotDispatchBusiness(t *testing.T) {
	gate, _, secret := gatewayFixture(t)
	gate.config.Timeout = 100 * time.Millisecond
	gate.config.AuditReserve = 20 * time.Millisecond
	release := make(chan struct{})
	var calls atomic.Int32
	gate.read = func(context.Context, *gorm.DB, metadataInput) (any, error) { calls.Add(1); return nil, nil }
	transact := gate.admission.transact
	gate.admission.transact = func(ctx context.Context, fn func(*gorm.DB) error) error {
		err := transact(ctx, fn)
		if err == nil {
			<-release
		}
		return err
	}
	out := gatewayCall(t, gate, secret, strings.Repeat("d", 32), nil)
	require.Equal(t, 503, out.Code)
	close(release)
	require.Eventually(t, func() bool { return len(gate.slots) == 0 }, time.Second, time.Millisecond)
	require.Zero(t, calls.Load())
}

func TestOpenAPIGatewayAuditFailureSuppressesSuccess(t *testing.T) {
	gate, db, secret := gatewayFixture(t)
	gate.read = func(context.Context, *gorm.DB, metadataInput) (any, error) {
		return map[string]string{"privateResult": "must-not-write"}, nil
	}
	gate.complete = func(context.Context, string, string, time.Duration) error { return fmt.Errorf("audit unavailable") }
	out := gatewayCall(t, gate, secret, strings.Repeat("b", 32), nil)
	require.Equal(t, 503, out.Code, out.Body.String())
	require.NotContains(t, out.Body.String(), "privateResult")
	var count int64
	require.NoError(t, db.Model(&models.Nonce{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	replay := gatewayCall(t, gate, secret, strings.Repeat("b", 32), nil)
	require.Equal(t, 401, replay.Code, replay.Body.String())
	require.Contains(t, replay.Body.String(), "REQUEST_REPLAYED")
}

func TestOpenAPIMetadataRejectsExplicitEmptyNumbersAndEnums(t *testing.T) {
	for _, query := range []string{"page=", "pageSize=", "status="} {
		_, err := parseMetadata(gatewayRequest{scope: "device:list", rawQuery: query}, 10)
		require.Error(t, err, query)
	}
	m, err := parseMetadata(gatewayRequest{scope: "device:list", rawQuery: "keyword="}, 10)
	require.NoError(t, err)
	require.Empty(t, m.keyword)
	require.Equal(t, 1, m.page)
	require.Equal(t, 20, m.size)
}

func TestOpenAPIGatewayCredentialFailureClasses(t *testing.T) {
	for _, kind := range []string{"unknown-ak", "inactive", "key-id", "iv", "ciphertext", "aad", "master-key"} {
		t.Run(kind, func(t *testing.T) {
			gate, db, secret := gatewayFixture(t)
			expected := 503
			switch kind {
			case "unknown-ak":
				require.NoError(t, db.Where("id = ?", 1).Delete(&models.Client{}).Error)
				expected = 401
			case "inactive":
				require.NoError(t, db.Model(&models.Client{}).Where("id = ?", 1).Update("status", models.StatusDisabled).Error)
				expected = 401
			case "key-id":
				require.NoError(t, db.Model(&models.Client{}).Where("id = ?", 1).Update("secret_key_id", "unavailable-key").Error)
			case "iv":
				require.NoError(t, db.Model(&models.Client{}).Where("id = ?", 1).Update("secret_iv", []byte{1}).Error)
			case "ciphertext":
				require.NoError(t, db.Model(&models.Client{}).Where("id = ?", 1).Update("secret_ciphertext", bytes.Repeat([]byte{0}, 64)).Error)
			case "aad":
				require.NoError(t, db.Model(&models.Client{}).Where("id = ?", 1).Update("secret_version", 2).Error)
			case "master-key":
				keys, err := client.NewSecretManager(bytes.Repeat([]byte{9}, 32), "test")
				require.NoError(t, err)
				gate.clients, err = client.NewService(db, keys)
				require.NoError(t, err)
			}
			out := gatewayCall(t, gate, secret, strings.Repeat("f", 32), nil)
			require.Equal(t, expected, out.Code, kind)
			require.NotContains(t, out.Body.String(), secret)
			require.NotContains(t, out.Body.String(), "unavailable-key")
			// Legacy verifier continues to hide all dependency classifications.
			require.ErrorIs(t, gate.clients.ValidateSecret(context.Background(), fmt.Sprintf("uvp_%032x", 1), secret), client.ErrAuthenticationFailed)
			var count int64
			require.NoError(t, db.Model(&models.Nonce{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestOpenAPIGatewayMissingVersusMalformedHeaders(t *testing.T) {
	gate, db, secret := gatewayFixture(t)
	for _, name := range []string{"X-UVP-Sign-Version", "X-UVP-Access-Key", "X-UVP-Timestamp", "X-UVP-Nonce", "X-UVP-Signature"} {
		for _, value := range []string{"<absent>", "", "bad,header", " whitespace "} {
			out := gatewayCall(t, gate, secret, strings.Repeat("e", 32), func(r *http.Request) {
				if value == "<absent>" {
					r.Header.Del(name)
				} else {
					r.Header.Set(name, value)
				}
			})
			expected := 400
			if value == "<absent>" {
				expected = 401
			}
			require.Equal(t, expected, out.Code, name)
		}
	}
	var count int64
	require.NoError(t, db.Model(&models.Nonce{}).Count(&count).Error)
	require.Zero(t, count)
}
