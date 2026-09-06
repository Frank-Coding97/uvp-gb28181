package auth

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

const (
	mediaTestDevice  = "34020000002000000010"
	mediaTestChannel = "34020000001320000010"
)

type mediaDispatcherStub struct {
	ready        atomic.Bool
	prepareErr   error
	applyErr     error
	ticket       MediaTicket
	authorize    MediaAuthorization
	prepareCall  atomic.Int32
	applyCall    atomic.Int32
	applyStarted chan struct{}
	applyBlock   chan struct{}
	mu           sync.Mutex
	prepared     MediaTarget
	applied      MediaAdmittedRequest
	db           *gorm.DB
	committed    bool
}

func (s *mediaDispatcherStub) Ready() bool { return s.ready.Load() }

func (s *mediaDispatcherStub) Prepare(_ context.Context, target MediaTarget) (MediaTicket, error) {
	s.prepareCall.Add(1)
	s.mu.Lock()
	s.prepared = target
	s.mu.Unlock()
	if s.prepareErr != nil {
		return "", s.prepareErr
	}
	return s.ticket, nil
}

func (s *mediaDispatcherStub) Apply(_ context.Context, request MediaAdmittedRequest) (MediaAuthorization, error) {
	s.applyCall.Add(1)
	s.mu.Lock()
	s.applied = request
	authorization := s.authorize
	if authorization.AuthorizationID == "" {
		authorization.AuthorizationID = request.GrantID
	}
	if s.db != nil {
		var grant models.PlayGrant
		s.committed = s.db.First(&grant, "grant_id = ?", request.GrantID).Error == nil
	}
	started, block := s.applyStarted, s.applyBlock
	s.applyStarted, s.applyBlock = nil, nil
	s.mu.Unlock()
	if started != nil {
		close(started)
	}
	if block != nil {
		<-block
	}
	if s.applyErr != nil {
		return MediaAuthorization{}, s.applyErr
	}
	return authorization, nil
}

func mediaGatewayFixture(t *testing.T, dispatcher MediaDispatcher) (*Gateway, *gorm.DB, string) {
	t.Helper()
	gate, db, secret := gatewayFixture(t)
	require.NoError(t, db.Create(&models.ClientScope{ClientID: 1, Scope: mediaScope, Enabled: true, ScopeEpoch: 1, UpdatedAt: time.Now().UTC()}).Error)
	require.NoError(t, db.AutoMigrate(&models.PlayGrant{}, &models.Viewer{}))
	require.NoError(t, db.Exec(`CREATE TABLE gb_device (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        device_id TEXT NOT NULL UNIQUE,
        name TEXT NOT NULL DEFAULT '', alias TEXT NOT NULL DEFAULT '',
        manufacturer TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '',
        status INTEGER NOT NULL DEFAULT 1, owner_dept_id INTEGER NOT NULL,
        access_epoch INTEGER NOT NULL DEFAULT 1, deleted_at DATETIME NULL
    )`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE gb_channel (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        device_id TEXT NOT NULL, channel_id TEXT NOT NULL,
        name TEXT NOT NULL DEFAULT '', alias TEXT NOT NULL DEFAULT '',
        manufacturer TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '',
        status INTEGER NOT NULL DEFAULT 1, ptz_type INTEGER NOT NULL DEFAULT 0,
        owner_dept_id INTEGER NOT NULL, deleted_at DATETIME NULL
    )`).Error)
	require.NoError(t, db.Exec("INSERT INTO gb_device(device_id,owner_dept_id,access_epoch) VALUES(?,?,?)", mediaTestDevice, 10, 3).Error)
	require.NoError(t, db.Exec("INSERT INTO gb_channel(device_id,channel_id,owner_dept_id) VALUES(?,?,?)", mediaTestDevice, mediaTestChannel, 10).Error)
	gate.media = dispatcher
	return gate, db, secret
}

func mediaRouter(gate *Gateway) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST(mediaPattern, gate.Handler(mediaScope))
	return router
}

func signedMediaRequest(t *testing.T, serverURL, secret string, body []byte, nonce string) *http.Request {
	t.Helper()
	path := "/openapi/v1/devices/" + mediaTestDevice + "/channels/" + mediaTestChannel + "/live-authorizations"
	return signedMediaRequestPath(t, serverURL, secret, path, body, nonce)
}

func signedMediaRequestPath(t *testing.T, serverURL, secret, path string, body []byte, nonce string) *http.Request {
	t.Helper()
	now := fmt.Sprint(time.Now().Unix())
	input := SignatureInput{Method: http.MethodPost, Path: path, ContentType: "application/json", Body: body, AccessKey: fmt.Sprintf("uvp_%032x", 1), Timestamp: now, Nonce: nonce, Audience: "test-audience"}
	signature, err := Sign(input, secret)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, serverURL+path, bytes.NewReader(body))
	request.RequestURI = request.URL.RequestURI()
	request.ContentLength = int64(len(body))
	request.TLS = &tls.ConnectionState{HandshakeComplete: true}
	request.Header.Set("Content-Type", input.ContentType)
	request.Header.Set("X-UVP-Sign-Version", "1")
	request.Header.Set("X-UVP-Access-Key", input.AccessKey)
	request.Header.Set("X-UVP-Timestamp", input.Timestamp)
	request.Header.Set("X-UVP-Nonce", nonce)
	request.Header.Set("X-UVP-Signature", signature)
	return request
}

func sendMediaRequest(t *testing.T, gate *Gateway, secret string, body []byte, nonce string) *httptest.ResponseRecorder {
	t.Helper()
	server := httptest.NewTLSServer(mediaRouter(gate))
	t.Cleanup(server.Close)
	request := signedMediaRequest(t, server.URL, secret, body, nonce)
	request.RequestURI = ""
	response, err := server.Client().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	bodyBytes, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	recorder.Code = response.StatusCode
	recorder.HeaderMap = response.Header.Clone()
	_, _ = recorder.Body.Write(bodyBytes)
	return recorder
}

func TestOpenAPIMediaPOSTAdmitsOnceAndAppliesAfterCommit(t *testing.T) {
	dispatcher := &mediaDispatcherStub{ticket: "ticket-1", authorize: MediaAuthorization{Protocol: "https-flv", URL: "https://media.example.test/live/stream?token=opaque", ExpiresAt: time.Now().UTC().Add(time.Minute)}}
	dispatcher.ready.Store(true)
	gate, db, secret := mediaGatewayFixture(t, dispatcher)
	dispatcher.db = db

	server := httptest.NewTLSServer(mediaRouter(gate))
	defer server.Close()
	request := signedMediaRequest(t, server.URL, secret, []byte(`{"protocol":"https-flv"}`), strings.Repeat("a", 32))
	request.RequestURI = ""
	response, err := server.Client().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	responseBody := readBody(t, response)
	require.EqualValues(t, 1, dispatcher.prepareCall.Load())
	require.EqualValues(t, 1, dispatcher.applyCall.Load())
	require.True(t, dispatcher.committed, "Apply must observe the committed grant")

	var nonceCount, auditCount, grantCount int64
	require.NoError(t, db.Model(&models.Nonce{}).Count(&nonceCount).Error)
	require.NoError(t, db.Model(&models.Audit{}).Where("result = ?", "success").Count(&auditCount).Error)
	require.NoError(t, db.Model(&models.PlayGrant{}).Count(&grantCount).Error)
	require.EqualValues(t, 1, nonceCount)
	require.EqualValues(t, 1, auditCount)
	require.EqualValues(t, 1, grantCount)

	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	require.Contains(t, responseBody, `"authorizationId":"`+dispatcher.applied.GrantID+`"`)
	require.Equal(t, (MediaTarget{DeviceID: mediaTestDevice, ChannelID: mediaTestChannel, Protocol: "https-flv"}), dispatcher.prepared)
	require.Equal(t, dispatcher.prepared, dispatcher.applied.Target)
	require.Equal(t, MediaTicket("ticket-1"), dispatcher.applied.Ticket)
	require.NotEmpty(t, dispatcher.applied.GrantID)
}

func TestOpenAPIMediaNotReadyDoesNotReadOrAdmit(t *testing.T) {
	dispatcher := &mediaDispatcherStub{ticket: "ticket"}
	gate, db, secret := mediaGatewayFixture(t, dispatcher)
	body := &countingBody{Reader: strings.NewReader(`{"protocol":"https-flv"}`)}
	request := signedMediaRequest(t, "https://example.test", secret, []byte(`{"protocol":"https-flv"}`), strings.Repeat("b", 32))
	request.Body = body
	response := httptest.NewRecorder()
	mediaRouter(gate).ServeHTTP(response, request)
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Zero(t, body.reads.Load())
	require.Zero(t, dispatcher.prepareCall.Load())
	require.Zero(t, dispatcher.applyCall.Load())
	var nonceCount int64
	require.NoError(t, db.Model(&models.Nonce{}).Count(&nonceCount).Error)
	require.Zero(t, nonceCount)
}

func TestOpenAPIMediaReadDeadlineUnsupportedDoesNotRead(t *testing.T) {
	dispatcher := &mediaDispatcherStub{ticket: "ticket"}
	dispatcher.ready.Store(true)
	gate, _, secret := mediaGatewayFixture(t, dispatcher)
	body := []byte(`{"protocol":"https-flv"}`)
	request := signedMediaRequest(t, "https://example.test", secret, body, strings.Repeat("c", 32))
	tracked := &countingBody{Reader: bytes.NewReader(body)}
	request.Body = tracked
	response := httptest.NewRecorder()
	mediaRouter(gate).ServeHTTP(response, request)
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Zero(t, tracked.reads.Load())
	require.Zero(t, dispatcher.prepareCall.Load())
}

func TestOpenAPIMediaBodySignatureAndSchemaAreStrict(t *testing.T) {
	dispatcher := &mediaDispatcherStub{ticket: "ticket", authorize: MediaAuthorization{AuthorizationID: "authorization-1", Protocol: "https-flv", URL: "https://media.example.test/live/stream", ExpiresAt: time.Now().UTC().Add(time.Minute)}}
	dispatcher.ready.Store(true)
	gate, db, secret := mediaGatewayFixture(t, dispatcher)
	server := httptest.NewTLSServer(mediaRouter(gate))
	defer server.Close()

	request := signedMediaRequest(t, server.URL, secret, []byte(`{"protocol":"https-flv"}`), strings.Repeat("d", 32))
	request.RequestURI = ""
	request.Body = io.NopCloser(strings.NewReader(`{"protocol": "https-flv"}`))
	request.ContentLength = int64(len(`{"protocol": "https-flv"}`))
	response, err := server.Client().Do(request)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, response.StatusCode)
	response.Body.Close()

	unknown := []byte(`{"protocol":"https-flv","extra":true}`)
	request = signedMediaRequest(t, server.URL, secret, unknown, strings.Repeat("e", 32))
	request.RequestURI = ""
	response, err = server.Client().Do(request)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, response.StatusCode)
	response.Body.Close()
	exactKey := []byte(`{"Protocol":"https-flv"}`)
	request = signedMediaRequest(t, server.URL, secret, exactKey, strings.Repeat("a", 32))
	request.RequestURI = ""
	response, err = server.Client().Do(request)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, response.StatusCode)
	response.Body.Close()
	require.Zero(t, dispatcher.applyCall.Load())
	var nonceCount int64
	require.NoError(t, db.Model(&models.Nonce{}).Count(&nonceCount).Error)
	require.Zero(t, nonceCount)
}

func TestOpenAPIMediaRejectsInvalidBodyFraming(t *testing.T) {
	dispatcher := &mediaDispatcherStub{ticket: "ticket"}
	dispatcher.ready.Store(true)
	gate, db, secret := mediaGatewayFixture(t, dispatcher)
	server := httptest.NewTLSServer(mediaRouter(gate))
	defer server.Close()

	tests := []struct {
		name          string
		body          []byte
		contentLength int64
	}{
		{name: "empty", body: nil, contentLength: 0},
		{name: "too-large", body: bytes.Repeat([]byte("x"), maxBodyBytes+1), contentLength: maxBodyBytes + 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := framedMediaRequest(server.URL, test.body, test.contentLength)
			response, err := server.Client().Do(request)
			require.NoError(t, err)
			defer response.Body.Close()
			require.Equal(t, http.StatusBadRequest, response.StatusCode)
		})
	}

	unknown := signedMediaRequest(t, "https://example.test", secret, []byte(`{"protocol":"https-flv"}`), strings.Repeat("5", 32))
	unknown.ContentLength = -1
	unknown.Body = &countingBody{Reader: bytes.NewReader([]byte(`{"protocol":"https-flv"}`))}
	unknownResponse := httptest.NewRecorder()
	mediaRouter(gate).ServeHTTP(unknownResponse, unknown)
	require.Equal(t, http.StatusBadRequest, unknownResponse.Code)

	chunked := signedMediaRequest(t, server.URL, secret, []byte(`{"protocol":"https-flv"}`), strings.Repeat("6", 32))
	chunked.RequestURI = ""
	chunked.ContentLength = -1
	response, err := server.Client().Do(chunked)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusBadRequest, response.StatusCode)

	var nonceCount int64
	require.NoError(t, db.Model(&models.Nonce{}).Count(&nonceCount).Error)
	require.Zero(t, nonceCount)
}

func TestOpenAPIMediaSlowBodyReadFailsClosedBeforeAdmission(t *testing.T) {
	dispatcher := &mediaDispatcherStub{ticket: "ticket"}
	dispatcher.ready.Store(true)
	gate, db, secret := mediaGatewayFixture(t, dispatcher)
	gate.config.Timeout = 120 * time.Millisecond
	gate.config.AuditReserve = 40 * time.Millisecond
	server := httptest.NewTLSServer(mediaRouter(gate))
	defer server.Close()

	body := []byte(`{"protocol":"https-flv"}`)
	request := signedMediaRequest(t, server.URL, secret, body, strings.Repeat("7", 32))
	start := time.Now()
	response := sendPartialTLSMediaRequest(t, server, request, body[:len(body)-1])
	defer response.Body.Close()
	require.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
	require.Less(t, time.Since(start), time.Second)
	require.Eventually(t, func() bool { return len(gate.slots) == 0 }, time.Second, time.Millisecond)
	require.Zero(t, dispatcher.prepareCall.Load())
	var nonceCount int64
	require.NoError(t, db.Model(&models.Nonce{}).Count(&nonceCount).Error)
	require.Zero(t, nonceCount)
}

func TestOpenAPIMediaPrepareFailureAndQuotaFailureHaveNoAdmission(t *testing.T) {
	t.Run("prepare", func(t *testing.T) {
		dispatcher := &mediaDispatcherStub{prepareErr: errors.New("internal qualification detail")}
		dispatcher.ready.Store(true)
		gate, db, secret := mediaGatewayFixture(t, dispatcher)
		response := sendMediaRequest(t, gate, secret, []byte(`{"protocol":"https-flv"}`), strings.Repeat("f", 32))
		require.Equal(t, http.StatusServiceUnavailable, response.Code)
		var nonceCount, grantCount int64
		require.NoError(t, db.Model(&models.Nonce{}).Count(&nonceCount).Error)
		require.NoError(t, db.Model(&models.PlayGrant{}).Count(&grantCount).Error)
		require.Zero(t, nonceCount)
		require.Zero(t, grantCount)
	})

	t.Run("apply", func(t *testing.T) {
		dispatcher := &mediaDispatcherStub{
			ticket:    "ticket",
			applyErr:  errors.New("internal media dispatch detail"),
			authorize: MediaAuthorization{AuthorizationID: "authorization-1", Protocol: "https-flv", URL: "https://media.example.test/live/stream", ExpiresAt: time.Now().UTC().Add(time.Minute)},
		}
		dispatcher.ready.Store(true)
		gate, db, secret := mediaGatewayFixture(t, dispatcher)
		response := sendMediaRequest(t, gate, secret, []byte(`{"protocol":"https-flv"}`), strings.Repeat("9", 32))
		require.Equal(t, http.StatusServiceUnavailable, response.Code)
		require.EqualValues(t, 1, dispatcher.applyCall.Load())
		var nonceCount, auditCount, grantCount int64
		require.NoError(t, db.Model(&models.Nonce{}).Count(&nonceCount).Error)
		require.NoError(t, db.Model(&models.Audit{}).Where("result = ? AND reason_class = ?", "failed", "SERVICE_UNAVAILABLE").Count(&auditCount).Error)
		require.NoError(t, db.Model(&models.PlayGrant{}).Count(&grantCount).Error)
		require.EqualValues(t, 1, nonceCount)
		require.EqualValues(t, 1, auditCount)
		require.EqualValues(t, 1, grantCount)
	})

	t.Run("mismatched-authorization", func(t *testing.T) {
		dispatcher := &mediaDispatcherStub{ticket: "ticket", authorize: MediaAuthorization{AuthorizationID: "another-grant", Protocol: "https-flv", URL: "https://media.example.test/live/stream", ExpiresAt: time.Now().UTC().Add(time.Minute)}}
		dispatcher.ready.Store(true)
		gate, db, secret := mediaGatewayFixture(t, dispatcher)
		response := sendMediaRequest(t, gate, secret, []byte(`{"protocol":"https-flv"}`), strings.Repeat("a", 32))
		require.Equal(t, http.StatusServiceUnavailable, response.Code)
		require.EqualValues(t, 1, dispatcher.applyCall.Load())
		var successAuditCount int64
		require.NoError(t, db.Model(&models.Audit{}).Where("result = ?", "success").Count(&successAuditCount).Error)
		require.Zero(t, successAuditCount)
	})

	t.Run("quota", func(t *testing.T) {
		dispatcher := &mediaDispatcherStub{ticket: "ticket", authorize: MediaAuthorization{AuthorizationID: "authorization-1", Protocol: "https-flv", URL: "https://media.example.test/live/stream", ExpiresAt: time.Now().UTC().Add(time.Minute)}}
		dispatcher.ready.Store(true)
		gate, db, secret := mediaGatewayFixture(t, dispatcher)
		require.NoError(t, db.Model(&models.Client{}).Where("id = ?", 1).Update("viewer_quota", 1).Error)
		grant := models.PlayGrant{GrantID: "00000000-0000-4000-8000-000000000001", ClientID: 1, Scope: mediaScope, ClientEpoch: 1, ScopeEpoch: 1, DeviceEpoch: 3, DeviceID: stringPtr(mediaTestDevice), ChannelID: stringPtr(mediaTestChannel), IssuedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Minute), State: models.GrantStatePending, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
		require.NoError(t, db.Create(&grant).Error)
		response := sendMediaRequest(t, gate, secret, []byte(`{"protocol":"https-flv"}`), strings.Repeat("0", 32))
		require.Equal(t, http.StatusTooManyRequests, response.Code, response.Body.String())
		require.EqualValues(t, 0, dispatcher.applyCall.Load())
		var nonceCount, auditCount int64
		require.NoError(t, db.Model(&models.Nonce{}).Count(&nonceCount).Error)
		require.NoError(t, db.Model(&models.Audit{}).Count(&auditCount).Error)
		require.Zero(t, nonceCount)
		require.Zero(t, auditCount)
	})
}

func TestOpenAPIMediaAuthorizationGuardsRejectBeforeAdmission(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		prepareCalls int32
		prepare      func(*Gateway, *gorm.DB)
		mutate       func(*http.Request)
	}{
		{name: "wrong-signature", status: http.StatusUnauthorized, mutate: func(request *http.Request) { request.Header.Set("X-UVP-Signature", strings.Repeat("0", 64)) }},
		{name: "missing-scope", status: http.StatusForbidden, prepare: func(_ *Gateway, db *gorm.DB) {
			require.NoError(t, db.Where("client_id = ? AND scope = ?", 1, mediaScope).Delete(&models.ClientScope{}).Error)
		}},
		{name: "owner-mismatch", status: http.StatusNotFound, prepare: func(_ *Gateway, db *gorm.DB) {
			require.NoError(t, db.Exec("UPDATE gb_device SET owner_dept_id = ? WHERE device_id = ?", 11, mediaTestDevice).Error)
		}},
		{name: "admission-frozen", status: http.StatusServiceUnavailable, prepare: func(gate *Gateway, _ *gorm.DB) { gate.admission.Freeze() }},
		{name: "prepare-unavailable", status: http.StatusServiceUnavailable, prepareCalls: 1, prepare: func(_ *Gateway, _ *gorm.DB) {}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dispatcher := &mediaDispatcherStub{ticket: "ticket"}
			if test.name == "prepare-unavailable" {
				dispatcher.prepareErr = errors.New("media prepare unavailable")
			}
			dispatcher.ready.Store(true)
			gate, db, secret := mediaGatewayFixture(t, dispatcher)
			if test.prepare != nil {
				test.prepare(gate, db)
			}
			server := httptest.NewTLSServer(mediaRouter(gate))
			defer server.Close()
			request := signedMediaRequest(t, server.URL, secret, []byte(`{"protocol":"https-flv"}`), strings.Repeat("8", 32))
			request.RequestURI = ""
			if test.mutate != nil {
				test.mutate(request)
			}
			response, err := server.Client().Do(request)
			require.NoError(t, err)
			defer response.Body.Close()
			require.Equal(t, test.status, response.StatusCode)
			require.EqualValues(t, test.prepareCalls, dispatcher.prepareCall.Load())
			require.Zero(t, dispatcher.applyCall.Load())
			var nonceCount, auditCount, grantCount int64
			require.NoError(t, db.Model(&models.Nonce{}).Count(&nonceCount).Error)
			require.NoError(t, db.Model(&models.Audit{}).Count(&auditCount).Error)
			require.NoError(t, db.Model(&models.PlayGrant{}).Count(&grantCount).Error)
			require.Zero(t, nonceCount)
			require.Zero(t, auditCount)
			require.Zero(t, grantCount)
		})
	}
}

func TestOpenAPIMediaLateApplyTimesOutOnceWithoutSuccessAudit(t *testing.T) {
	applyStarted := make(chan struct{})
	applyRelease := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-applyRelease:
		default:
			close(applyRelease)
		}
	})
	dispatcher := &mediaDispatcherStub{ticket: "ticket", applyStarted: applyStarted, applyBlock: applyRelease}
	dispatcher.ready.Store(true)
	gate, db, secret := mediaGatewayFixture(t, dispatcher)
	gate.config.Timeout = 100 * time.Millisecond
	gate.config.AuditReserve = 20 * time.Millisecond
	server := httptest.NewTLSServer(mediaRouter(gate))
	defer server.Close()

	request := signedMediaRequest(t, server.URL, secret, []byte(`{"protocol":"https-flv"}`), strings.Repeat("c", 32))
	request.RequestURI = ""
	response, err := server.Client().Do(request)
	require.NoError(t, err)
	require.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
	body := readBody(t, response)
	response.Body.Close()
	require.NotContains(t, body, "authorizationId")
	require.Eventually(t, func() bool {
		select {
		case <-applyStarted:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
	var nonceCount, successAuditCount int64
	require.NoError(t, db.Model(&models.Nonce{}).Count(&nonceCount).Error)
	require.NoError(t, db.Model(&models.Audit{}).Where("result = ?", "success").Count(&successAuditCount).Error)
	require.EqualValues(t, 1, nonceCount)
	require.Zero(t, successAuditCount)
	close(applyRelease)
	require.Eventually(t, func() bool { return len(gate.slots) == 0 }, time.Second, time.Millisecond)
}

func TestOpenAPIMediaTargetIDsMustBeGBIDs(t *testing.T) {
	for _, test := range []struct {
		name      string
		deviceID  string
		channelID string
	}{
		{name: "short-device", deviceID: "3402000000200000001", channelID: mediaTestChannel},
		{name: "non-digit-channel", deviceID: mediaTestDevice, channelID: "3402000000132000001x"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dispatcher := &mediaDispatcherStub{ticket: "ticket"}
			dispatcher.ready.Store(true)
			gate, db, secret := mediaGatewayFixture(t, dispatcher)
			server := httptest.NewTLSServer(mediaRouter(gate))
			defer server.Close()
			path := "/openapi/v1/devices/" + test.deviceID + "/channels/" + test.channelID + "/live-authorizations"
			request := signedMediaRequestPath(t, server.URL, secret, path, []byte(`{"protocol":"https-flv"}`), strings.Repeat("b", 32))
			request.RequestURI = ""
			response, err := server.Client().Do(request)
			require.NoError(t, err)
			defer response.Body.Close()
			require.Equal(t, http.StatusBadRequest, response.StatusCode)
			require.Zero(t, dispatcher.prepareCall.Load())
			var nonceCount, grantCount int64
			require.NoError(t, db.Model(&models.Nonce{}).Count(&nonceCount).Error)
			require.NoError(t, db.Model(&models.PlayGrant{}).Count(&grantCount).Error)
			require.Zero(t, nonceCount)
			require.Zero(t, grantCount)
		})
	}
}

func TestOpenAPIMediaSlotsFullRejectsWithoutReading(t *testing.T) {
	dispatcher := &mediaDispatcherStub{ticket: "ticket"}
	dispatcher.ready.Store(true)
	gate, _, secret := mediaGatewayFixture(t, dispatcher)
	for i := 0; i < cap(gate.slots); i++ {
		gate.slots <- struct{}{}
	}
	body := &countingBody{Reader: strings.NewReader(`{"protocol":"https-flv"}`)}
	request := signedMediaRequest(t, "https://example.test", secret, []byte(`{"protocol":"https-flv"}`), strings.Repeat("1", 32))
	request.Body = body
	response := httptest.NewRecorder()
	mediaRouter(gate).ServeHTTP(response, request)
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Zero(t, body.reads.Load())
	for len(gate.slots) > 0 {
		<-gate.slots
	}
}

func TestOpenAPIMediaBodyReadFailuresAreClassified(t *testing.T) {
	for _, testCase := range []struct {
		name string
		err  error
		code int
	}{
		{name: "truncated", err: io.ErrUnexpectedEOF, code: http.StatusBadRequest},
		{name: "io failure", err: errors.New("connection reset"), code: http.StatusServiceUnavailable},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			dispatcher := &mediaDispatcherStub{ticket: "ticket"}
			dispatcher.ready.Store(true)
			gate, db, secret := mediaGatewayFixture(t, dispatcher)
			body := []byte(`{"protocol":"https-flv"}`)
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Request.Body = &mediaReadErrorBody{err: testCase.err}
				c.Next()
			})
			router.POST(mediaPattern, gate.Handler(mediaScope))
			server := httptest.NewTLSServer(router)
			defer server.Close()

			request := signedMediaRequest(t, server.URL, secret, body, strings.Repeat("2", 32))
			request.RequestURI = ""
			response, err := server.Client().Do(request)
			require.NoError(t, err)
			defer response.Body.Close()
			require.Equal(t, testCase.code, response.StatusCode)
			require.Zero(t, dispatcher.prepareCall.Load())
			require.Zero(t, dispatcher.applyCall.Load())
			var nonceCount int64
			require.NoError(t, db.Model(&models.Nonce{}).Count(&nonceCount).Error)
			require.Zero(t, nonceCount)
		})
	}
}

func TestOpenAPIMediaBodyOversizeIsInvalidRequest(t *testing.T) {
	dispatcher := &mediaDispatcherStub{ticket: "ticket"}
	dispatcher.ready.Store(true)
	gate, db, secret := mediaGatewayFixture(t, dispatcher)
	body := []byte(`{"protocol":"https-flv"}`)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Request.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", maxBodyBytes+1)))
		c.Next()
	})
	router.POST(mediaPattern, gate.Handler(mediaScope))
	server := httptest.NewTLSServer(router)
	defer server.Close()

	request := signedMediaRequest(t, server.URL, secret, body, strings.Repeat("3", 32))
	request.RequestURI = ""
	response, err := server.Client().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusBadRequest, response.StatusCode)
	require.Zero(t, dispatcher.prepareCall.Load())
	var nonceCount int64
	require.NoError(t, db.Model(&models.Nonce{}).Count(&nonceCount).Error)
	require.Zero(t, nonceCount)
}

type countingBody struct {
	io.Reader
	reads atomic.Int32
}

type mediaReadErrorBody struct {
	err error
}

func (b *mediaReadErrorBody) Read([]byte) (int, error) { return 0, b.err }

func (b *mediaReadErrorBody) Close() error { return nil }

func (b *countingBody) Read(p []byte) (int, error) {
	b.reads.Add(1)
	return b.Reader.Read(p)
}

func (b *countingBody) Close() error { return nil }

func readBody(t *testing.T, response *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return string(body)
}

func framedMediaRequest(serverURL string, body []byte, contentLength int64) *http.Request {
	path := "/openapi/v1/devices/" + mediaTestDevice + "/channels/" + mediaTestChannel + "/live-authorizations"
	request := httptest.NewRequest(http.MethodPost, serverURL+path, bytes.NewReader(body))
	request.RequestURI = ""
	request.ContentLength = contentLength
	request.TLS = &tls.ConnectionState{HandshakeComplete: true}
	request.Header.Set("Content-Type", "application/json")
	if contentLength == 0 {
		request.Body = nil
	}
	return request
}

func sendPartialTLSMediaRequest(t *testing.T, server *httptest.Server, request *http.Request, body []byte) *http.Response {
	t.Helper()
	address := strings.TrimPrefix(server.URL, "https://")
	connection, err := tls.Dial("tcp", address, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
	require.NoError(t, err)
	t.Cleanup(func() { _ = connection.Close() })
	require.NoError(t, connection.SetDeadline(time.Now().Add(2*time.Second)))
	path := request.URL.RequestURI()
	_, err = fmt.Fprintf(connection, "POST %s HTTP/1.1\r\nHost: %s\r\nContent-Length: %d\r\nContent-Type: application/json\r\nX-UVP-Sign-Version: %s\r\nX-UVP-Access-Key: %s\r\nX-UVP-Timestamp: %s\r\nX-UVP-Nonce: %s\r\nX-UVP-Signature: %s\r\nConnection: close\r\n\r\n", path, address, request.ContentLength, request.Header.Get("X-UVP-Sign-Version"), request.Header.Get("X-UVP-Access-Key"), request.Header.Get("X-UVP-Timestamp"), request.Header.Get("X-UVP-Nonce"), request.Header.Get("X-UVP-Signature"))
	require.NoError(t, err)
	_, err = connection.Write(body)
	require.NoError(t, err)
	return readRawHTTPResponse(t, connection)
}

func readRawHTTPResponse(t *testing.T, connection net.Conn) *http.Response {
	t.Helper()
	response, err := http.ReadResponse(bufio.NewReader(connection), nil)
	require.NoError(t, err)
	return response
}

func stringPtr(value string) *string { return &value }

var _ MediaDispatcher = (*mediaDispatcherStub)(nil)
