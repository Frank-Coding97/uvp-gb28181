package integration

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
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
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"uvplatform.cn/uvp-gb28181/app/openapi/auth"
	"uvplatform.cn/uvp-gb28181/app/openapi/client"
	"uvplatform.cn/uvp-gb28181/app/openapi/limit"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
	"uvplatform.cn/uvp-gb28181/app/openapi/routes"
)

const (
	boundaryMediaDevice  = "34020000002000000010"
	boundaryMediaChannel = "34020000001320000010"
	boundaryMediaPath    = "/openapi/v1/devices/" + boundaryMediaDevice + "/channels/" + boundaryMediaChannel + "/live-authorizations"
)

type boundaryMediaDispatcher struct {
	ready        atomic.Bool
	prepareCalls atomic.Int32
	applyCalls   atomic.Int32
	committed    atomic.Bool

	db           *gorm.DB
	applyStarted chan struct{}
	applyRelease chan struct{}
	applyDone    chan struct{}
	startOnce    sync.Once
	doneOnce     sync.Once

	mu       sync.Mutex
	prepared auth.MediaTarget
	applied  auth.MediaAdmittedRequest
}

func (d *boundaryMediaDispatcher) Ready() bool {
	return d != nil && d.ready.Load()
}

func (d *boundaryMediaDispatcher) Prepare(_ context.Context, target auth.MediaTarget) (auth.MediaTicket, error) {
	d.prepareCalls.Add(1)
	d.mu.Lock()
	d.prepared = target
	d.mu.Unlock()
	return auth.MediaTicket("boundary-ticket"), nil
}

func (d *boundaryMediaDispatcher) Apply(_ context.Context, request auth.MediaAdmittedRequest) (auth.MediaAuthorization, error) {
	d.applyCalls.Add(1)
	d.mu.Lock()
	d.applied = request
	started, release, done, db := d.applyStarted, d.applyRelease, d.applyDone, d.db
	d.mu.Unlock()
	if db != nil {
		var grant models.PlayGrant
		d.committed.Store(db.First(&grant, "grant_id = ?", request.GrantID).Error == nil)
	}
	if started != nil {
		d.startOnce.Do(func() { close(started) })
	}
	if release != nil {
		<-release
	}
	if done != nil {
		d.doneOnce.Do(func() { close(done) })
	}
	scheme := "https"
	if request.Target.Protocol == "wss-flv" {
		scheme = "wss"
	}
	return auth.MediaAuthorization{
		AuthorizationID: request.GrantID,
		Protocol:        request.Target.Protocol,
		URL:             scheme + "://media.example.test/live/stream?opaque=boundary",
		ExpiresAt:       time.Now().UTC().Add(time.Minute),
	}, nil
}

type boundaryMediaFixture struct {
	gate   *auth.Gateway
	db     *gorm.DB
	secret string
}

func newBoundaryMediaFixture(t *testing.T, dispatcher *boundaryMediaDispatcher, timeout, auditReserve time.Duration) boundaryMediaFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&models.Client{}, &models.ClientScope{}, &models.Nonce{}, &models.Audit{}, &models.PlayGrant{}, &models.Viewer{}))
	require.NoError(t, db.Exec("CREATE TABLE sys_department (id INTEGER PRIMARY KEY, status INTEGER, deleted_at DATETIME NULL)").Error)
	require.NoError(t, db.Exec("INSERT INTO sys_department(id,status) VALUES(10,1)").Error)
	require.NoError(t, db.Exec(`CREATE TABLE gb_device (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        device_id TEXT NOT NULL UNIQUE,
        name TEXT NOT NULL DEFAULT '', alias TEXT NOT NULL DEFAULT '',
        manufacturer TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '',
        status INTEGER NOT NULL DEFAULT 1, owner_dept_id INTEGER NOT NULL,
        access_epoch INTEGER NOT NULL DEFAULT 1, cleanup_completed_epoch INTEGER NOT NULL DEFAULT 1, deleted_at DATETIME NULL
    )`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE gb_channel (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        device_id TEXT NOT NULL, channel_id TEXT NOT NULL,
        name TEXT NOT NULL DEFAULT '', alias TEXT NOT NULL DEFAULT '',
        manufacturer TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '',
        status INTEGER NOT NULL DEFAULT 1, ptz_type INTEGER NOT NULL DEFAULT 0,
        owner_dept_id INTEGER NOT NULL, deleted_at DATETIME NULL,
        UNIQUE(device_id, channel_id)
    )`).Error)
	require.NoError(t, db.Exec("INSERT INTO gb_device(device_id,owner_dept_id,access_epoch,cleanup_completed_epoch) VALUES(?,?,?,?)", boundaryMediaDevice, 10, 3, 3).Error)
	require.NoError(t, db.Exec("INSERT INTO gb_channel(device_id,channel_id,owner_dept_id) VALUES(?,?,?)", boundaryMediaDevice, boundaryMediaChannel, 10).Error)

	keys, err := client.NewSecretManager(bytes.Repeat([]byte{1}, 32), "test")
	require.NoError(t, err)
	secret := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	now := time.Now().UTC().Round(0)
	clientRow := models.Client{
		ID: 1, AK: fmt.Sprintf("uvp_%032x", 1), Name: "boundary test", OwnerDeptID: 10,
		Status: models.StatusActive, SecretCiphertext: []byte{1}, SecretIV: []byte{1}, SecretKeyID: "test",
		SecretVersion: 1, AuthEpoch: 1, RateLimit: 100, Burst: 100, ViewerQuota: 2, RowVersion: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(&clientRow).Error)
	ciphertext, iv, err := keys.Encrypt(clientRow.ID, clientRow.AK, clientRow.SecretVersion, secret)
	require.NoError(t, err)
	require.NoError(t, db.Model(&models.Client{}).Where("id = ?", clientRow.ID).Updates(map[string]any{"secret_ciphertext": ciphertext, "secret_iv": iv}).Error)
	require.NoError(t, db.Create(&models.ClientScope{ClientID: clientRow.ID, Scope: limit.PlayLiveApplyScope, Enabled: true, ScopeEpoch: 1, UpdatedAt: now}).Error)
	dispatcher.db = db

	gate, err := auth.NewGateway(context.Background(), db, keys, auth.GatewayConfig{
		Audience: "test-audience", Timeout: timeout, AuditReserve: auditReserve, MaxInFlight: 2,
	}, auth.WithMediaDispatcher(dispatcher))
	require.NoError(t, err)
	return boundaryMediaFixture{gate: gate, db: db, secret: secret}
}

type boundaryGlobalCounters struct {
	calls     atomic.Int32
	bodyReads atomic.Int32
	records   atomic.Int32
}

type boundaryResponseObserver struct {
	writes     atomic.Int32
	lateWrites atomic.Int32
	returned   atomic.Bool
}

type boundaryObservedResponseWriter struct {
	http.ResponseWriter
	observer *boundaryResponseObserver
}

func (w *boundaryObservedResponseWriter) WriteHeader(status int) {
	w.observe()
	w.ResponseWriter.WriteHeader(status)
}

func (w *boundaryObservedResponseWriter) Write(body []byte) (int, error) {
	w.observe()
	return w.ResponseWriter.Write(body)
}

func (w *boundaryObservedResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *boundaryObservedResponseWriter) observe() {
	if w.observer == nil {
		return
	}
	w.observer.writes.Add(1)
	if w.observer.returned.Load() {
		w.observer.lateWrites.Add(1)
	}
}

func newBoundaryTLSServer(t *testing.T, fixture boundaryMediaFixture, counters *boundaryGlobalCounters) (*httptest.Server, *http.Client, *boundaryResponseObserver) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	root := gin.New()
	require.NoError(t, routes.InstallPublicBoundary(root, fixture.gate, nil))
	root.Use(func(c *gin.Context) {
		if counters != nil {
			counters.calls.Add(1)
			counters.records.Add(1)
			if c.Request.Body != nil {
				_, _ = io.ReadAll(c.Request.Body)
				counters.bodyReads.Add(1)
			}
		}
		c.Next()
	})
	observer := &boundaryResponseObserver{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		observer.returned.Store(false)
		root.ServeHTTP(&boundaryObservedResponseWriter{ResponseWriter: writer, observer: observer}, request)
		observer.returned.Store(true)
	}))
	t.Cleanup(server.Close)
	httpClient := server.Client()
	if transport, ok := httpClient.Transport.(*http.Transport); ok {
		transport.ForceAttemptHTTP2 = false
	}
	return server, httpClient, observer
}

func boundarySignedRequest(t *testing.T, serverURL, secret, path string, body []byte, nonce string) *http.Request {
	t.Helper()
	timestamp := fmt.Sprint(time.Now().Unix())
	input := auth.SignatureInput{
		Method: http.MethodPost, Path: path, ContentType: "application/json", Body: body,
		AccessKey: fmt.Sprintf("uvp_%032x", 1), Timestamp: timestamp, Nonce: nonce, Audience: "test-audience",
	}
	signature, err := auth.Sign(input, secret)
	require.NoError(t, err)
	request, err := http.NewRequest(http.MethodPost, serverURL+path, bytes.NewReader(body))
	require.NoError(t, err)
	request.ContentLength = int64(len(body))
	request.Header.Set("Content-Type", input.ContentType)
	request.Header.Set("X-UVP-Sign-Version", "1")
	request.Header.Set("X-UVP-Access-Key", input.AccessKey)
	request.Header.Set("X-UVP-Timestamp", input.Timestamp)
	request.Header.Set("X-UVP-Nonce", input.Nonce)
	request.Header.Set("X-UVP-Signature", signature)
	return request
}

func countBoundaryRows(t *testing.T, db *gorm.DB) (nonces, audits, successAudits, grants int64) {
	t.Helper()
	require.NoError(t, db.Model(&models.Nonce{}).Count(&nonces).Error)
	require.NoError(t, db.Model(&models.Audit{}).Count(&audits).Error)
	require.NoError(t, db.Model(&models.Audit{}).Where("result = ?", "success").Count(&successAudits).Error)
	require.NoError(t, db.Model(&models.PlayGrant{}).Count(&grants).Error)
	return
}

func readBoundaryResponse(t *testing.T, response *http.Response) string {
	t.Helper()
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return string(body)
}

func TestOpenAPIMediaGatewayBoundarySuccessCommitsBeforeApply(t *testing.T) {
	dispatcher := &boundaryMediaDispatcher{}
	dispatcher.ready.Store(true)
	fixture := newBoundaryMediaFixture(t, dispatcher, 500*time.Millisecond, 100*time.Millisecond)
	counters := &boundaryGlobalCounters{}
	server, httpClient, _ := newBoundaryTLSServer(t, fixture, counters)

	request := boundarySignedRequest(t, server.URL, fixture.secret, boundaryMediaPath, []byte(`{"protocol":"https-flv"}`), strings.Repeat("a", 32))
	response, err := httpClient.Do(request)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, "HTTP/1.1", response.Proto)
	var envelope struct {
		Code string                  `json:"code"`
		Data auth.MediaAuthorization `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(readBoundaryResponse(t, response)), &envelope))
	require.Equal(t, "OK", envelope.Code)
	require.Equal(t, "https-flv", envelope.Data.Protocol)
	require.NotEmpty(t, envelope.Data.AuthorizationID)

	dispatcher.mu.Lock()
	prepared, applied := dispatcher.prepared, dispatcher.applied
	dispatcher.mu.Unlock()
	require.Equal(t, auth.MediaTarget{DeviceID: boundaryMediaDevice, ChannelID: boundaryMediaChannel, Protocol: "https-flv"}, prepared)
	require.Equal(t, prepared, applied.Target)
	require.Equal(t, auth.MediaTicket("boundary-ticket"), applied.Ticket)
	require.Equal(t, applied.GrantID, envelope.Data.AuthorizationID)
	require.True(t, dispatcher.committed.Load(), "Apply must observe the committed reservation")
	require.EqualValues(t, 1, dispatcher.prepareCalls.Load())
	require.EqualValues(t, 1, dispatcher.applyCalls.Load())
	nonces, audits, successAudits, grants := countBoundaryRows(t, fixture.db)
	require.EqualValues(t, 1, nonces)
	require.EqualValues(t, 1, audits)
	require.EqualValues(t, 1, successAudits)
	require.EqualValues(t, 1, grants)
	require.Zero(t, counters.calls.Load(), "global business middleware must not run for /openapi")
	require.Zero(t, counters.bodyReads.Load(), "global business middleware must not read the body")
	require.Zero(t, counters.records.Load(), "global business middleware must not record the request")
}

func TestOpenAPIMediaGatewayBoundarySlowBodyFailsBeforeAdmission(t *testing.T) {
	dispatcher := &boundaryMediaDispatcher{}
	dispatcher.ready.Store(true)
	fixture := newBoundaryMediaFixture(t, dispatcher, 120*time.Millisecond, 40*time.Millisecond)
	server, httpClient, _ := newBoundaryTLSServer(t, fixture, nil)
	body := []byte(`{"protocol":"https-flv"}`)
	request := boundarySignedRequest(t, server.URL, fixture.secret, boundaryMediaPath, body, strings.Repeat("b", 32))
	request.Body = &boundarySlowBody{first: body[:1], rest: body[1:], delay: 300 * time.Millisecond}
	started := time.Now()
	response, err := httpClient.Do(request)
	require.NoError(t, err)
	require.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
	require.Equal(t, "HTTP/1.1", response.Proto)
	_ = readBoundaryResponse(t, response)
	require.Less(t, time.Since(started), time.Second)
	require.Zero(t, dispatcher.prepareCalls.Load())
	require.Zero(t, dispatcher.applyCalls.Load())
	nonces, audits, _, grants := countBoundaryRows(t, fixture.db)
	require.Zero(t, nonces)
	require.Zero(t, audits)
	require.Zero(t, grants)
}

func TestOpenAPIMediaGatewayBoundaryRejectsChunkedAndEncodedBeforeRead(t *testing.T) {
	dispatcher := &boundaryMediaDispatcher{}
	dispatcher.ready.Store(true)
	fixture := newBoundaryMediaFixture(t, dispatcher, 500*time.Millisecond, 100*time.Millisecond)
	server, _, _ := newBoundaryTLSServer(t, fixture, nil)
	body := []byte(`{"protocol":"https-flv"}`)

	chunked := boundarySignedRequest(t, server.URL, fixture.secret, boundaryMediaPath, body, strings.Repeat("c", 32))
	response := boundaryRawShapeRequest(t, server, chunked, "chunked")
	require.Equal(t, http.StatusBadRequest, response.StatusCode)
	require.Equal(t, "HTTP/1.1", response.Proto)
	_ = readBoundaryResponse(t, response)

	encoded := boundarySignedRequest(t, server.URL, fixture.secret, boundaryMediaPath, body, strings.Repeat("d", 32))
	response = boundaryRawShapeRequest(t, server, encoded, "content-encoding")
	require.Equal(t, http.StatusBadRequest, response.StatusCode)
	require.Equal(t, "HTTP/1.1", response.Proto)
	_ = readBoundaryResponse(t, response)

	require.Zero(t, dispatcher.prepareCalls.Load())
	require.Zero(t, dispatcher.applyCalls.Load())
	nonces, audits, _, grants := countBoundaryRows(t, fixture.db)
	require.Zero(t, nonces)
	require.Zero(t, audits)
	require.Zero(t, grants)
}

func TestOpenAPIMediaGatewayBoundaryBindsHMACToRawBodyAndExactTarget(t *testing.T) {
	dispatcher := &boundaryMediaDispatcher{}
	dispatcher.ready.Store(true)
	fixture := newBoundaryMediaFixture(t, dispatcher, 500*time.Millisecond, 100*time.Millisecond)
	server, httpClient, _ := newBoundaryTLSServer(t, fixture, nil)
	compact := []byte(`{"protocol":"https-flv"}`)

	whitespace := boundarySignedRequest(t, server.URL, fixture.secret, boundaryMediaPath, compact, strings.Repeat("e", 32))
	whitespace.Body = io.NopCloser(strings.NewReader(`{"protocol": "https-flv"}`))
	whitespace.ContentLength = int64(len(`{"protocol": "https-flv"}`))
	response, err := httpClient.Do(whitespace)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, response.StatusCode)
	_ = readBoundaryResponse(t, response)

	cases := []struct {
		name string
		path string
		body string
	}{
		{name: "uppercase-key", path: boundaryMediaPath, body: `{"Protocol":"https-flv"}`},
		{name: "unsupported-protocol", path: boundaryMediaPath, body: `{"protocol":"rtmp"}`},
		{name: "short-device", path: "/openapi/v1/devices/3402000000200000001/channels/" + boundaryMediaChannel + "/live-authorizations", body: `{"protocol":"https-flv"}`},
		{name: "non-digit-channel", path: "/openapi/v1/devices/" + boundaryMediaDevice + "/channels/3402000000132000001x/live-authorizations", body: `{"protocol":"https-flv"}`},
	}
	for i, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := boundarySignedRequest(t, server.URL, fixture.secret, testCase.path, []byte(testCase.body), fmt.Sprintf("%x", bytes.Repeat([]byte{byte('f' + i)}, 16)))
			response, err := httpClient.Do(request)
			require.NoError(t, err)
			require.Equal(t, http.StatusBadRequest, response.StatusCode)
			_ = readBoundaryResponse(t, response)
		})
	}

	require.Zero(t, dispatcher.prepareCalls.Load())
	require.Zero(t, dispatcher.applyCalls.Load())
	nonces, audits, _, grants := countBoundaryRows(t, fixture.db)
	require.Zero(t, nonces)
	require.Zero(t, audits)
	require.Zero(t, grants)
}

func TestOpenAPIMediaGatewayBoundaryLateApplyReturnsOnceWithoutSuccessAudit(t *testing.T) {
	var releaseOnce sync.Once
	dispatcher := &boundaryMediaDispatcher{
		applyStarted: make(chan struct{}),
		applyRelease: make(chan struct{}),
		applyDone:    make(chan struct{}),
	}
	release := func() { releaseOnce.Do(func() { close(dispatcher.applyRelease) }) }
	defer release()
	dispatcher.ready.Store(true)
	fixture := newBoundaryMediaFixture(t, dispatcher, 250*time.Millisecond, 50*time.Millisecond)
	server, httpClient, observer := newBoundaryTLSServer(t, fixture, nil)
	request := boundarySignedRequest(t, server.URL, fixture.secret, boundaryMediaPath, []byte(`{"protocol":"https-flv"}`), strings.Repeat("0", 32))

	response, err := httpClient.Do(request)
	require.NoError(t, err)
	require.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
	require.Equal(t, "HTTP/1.1", response.Proto)
	body := readBoundaryResponse(t, response)
	require.NotContains(t, body, "authorizationId")
	require.Eventually(t, func() bool {
		select {
		case <-dispatcher.applyStarted:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
	nonces, audits, successAudits, grants := countBoundaryRows(t, fixture.db)
	require.EqualValues(t, 1, nonces)
	require.EqualValues(t, 1, audits)
	require.Zero(t, successAudits)
	require.EqualValues(t, 1, grants)

	writesBeforeRelease := observer.writes.Load()
	release()
	require.Eventually(t, func() bool {
		select {
		case <-dispatcher.applyDone:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
	require.Equal(t, writesBeforeRelease, observer.writes.Load(), "late worker must not write a second response")
	require.Zero(t, observer.lateWrites.Load(), "late worker must not write after the handler returned")
	_, _, successAudits, _ = countBoundaryRows(t, fixture.db)
	require.Zero(t, successAudits, "late Apply must not turn the audit into success")
}

type boundarySlowBody struct {
	first, rest []byte
	delay       time.Duration
	firstRead   bool
}

func (b *boundarySlowBody) Read(p []byte) (int, error) {
	if !b.firstRead {
		b.firstRead = true
		return copy(p, b.first), nil
	}
	time.Sleep(b.delay)
	if len(b.rest) == 0 {
		return 0, io.EOF
	}
	n := copy(p, b.rest)
	b.rest = b.rest[n:]
	return n, nil
}

func (b *boundarySlowBody) Close() error { return nil }

func boundaryRawShapeRequest(t *testing.T, server *httptest.Server, request *http.Request, mode string) *http.Response {
	t.Helper()
	address := strings.TrimPrefix(server.URL, "https://")
	connection, err := tls.Dial("tcp", address, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
	require.NoError(t, err)
	t.Cleanup(func() { _ = connection.Close() })
	require.NoError(t, connection.SetDeadline(time.Now().Add(2*time.Second)))
	_, err = fmt.Fprintf(connection, "POST %s HTTP/1.1\r\nHost: %s\r\nContent-Type: application/json\r\nX-UVP-Sign-Version: %s\r\nX-UVP-Access-Key: %s\r\nX-UVP-Timestamp: %s\r\nX-UVP-Nonce: %s\r\nX-UVP-Signature: %s\r\nConnection: close\r\n", request.URL.RequestURI(), address, request.Header.Get("X-UVP-Sign-Version"), request.Header.Get("X-UVP-Access-Key"), request.Header.Get("X-UVP-Timestamp"), request.Header.Get("X-UVP-Nonce"), request.Header.Get("X-UVP-Signature"))
	require.NoError(t, err)
	switch mode {
	case "chunked":
		_, err = io.WriteString(connection, "Transfer-Encoding: chunked\r\n\r\n0\r\n\r\n")
	case "content-encoding":
		_, err = fmt.Fprintf(connection, "Content-Length: %d\r\nContent-Encoding: gzip\r\n\r\n", request.ContentLength)
	default:
		t.Fatalf("unknown raw shape mode %q", mode)
	}
	require.NoError(t, err)
	return boundaryReadRawResponse(t, connection)
}

func boundaryReadRawResponse(t *testing.T, connection net.Conn) *http.Response {
	t.Helper()
	response, err := http.ReadResponse(bufio.NewReader(connection), nil)
	require.NoError(t, err)
	return response
}

var _ auth.MediaDispatcher = (*boundaryMediaDispatcher)(nil)
