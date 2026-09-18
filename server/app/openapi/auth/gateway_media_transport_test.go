package auth

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

func TestOpenAPIMediaTLSSlowBodyUsesHardReadDeadline(t *testing.T) {
	dispatcher := &mediaDispatcherStub{ticket: "ticket"}
	dispatcher.ready.Store(true)
	gate, db, secret := mediaGatewayFixture(t, dispatcher)
	gate.config.Timeout = 80 * time.Millisecond
	gate.config.AuditReserve = 20 * time.Millisecond

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST(mediaPattern, gate.Handler(mediaScope))
	server := httptest.NewTLSServer(router)
	defer server.Close()

	body := []byte(`{"protocol":"https-flv"}`)
	request := signedMediaRequest(t, server.URL, secret, body, strings.Repeat("4", 32))
	request.RequestURI = ""
	request.Body = &slowMediaRequestBody{first: body[:1], second: body[1:], delay: 300 * time.Millisecond}
	started := time.Now()
	response, err := server.Client().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
	require.Less(t, time.Since(started), 250*time.Millisecond)
	require.Zero(t, dispatcher.prepareCall.Load())
	var nonceCount int64
	require.NoError(t, db.Model(&models.Nonce{}).Count(&nonceCount).Error)
	require.Zero(t, nonceCount)
}

func TestOpenAPIMediaRejectsChunkedBeforeReading(t *testing.T) {
	dispatcher := &mediaDispatcherStub{ticket: "ticket"}
	dispatcher.ready.Store(true)
	gate, db, secret := mediaGatewayFixture(t, dispatcher)
	body := []byte(`{"protocol":"https-flv"}`)
	request := signedMediaRequest(t, "https://example.test", secret, body, strings.Repeat("5", 32))
	request.ContentLength = -1
	request.TransferEncoding = []string{"chunked"}
	tracked := &countingBody{Reader: strings.NewReader(string(body))}
	request.Body = tracked
	response := httptest.NewRecorder()
	mediaRouter(gate).ServeHTTP(response, request)
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Zero(t, tracked.reads.Load())
	require.Zero(t, dispatcher.prepareCall.Load())
	var nonceCount int64
	require.NoError(t, db.Model(&models.Nonce{}).Count(&nonceCount).Error)
	require.Zero(t, nonceCount)
}

type slowMediaRequestBody struct {
	first, second []byte
	delay         time.Duration
	readSecond    bool
}

func (b *slowMediaRequestBody) Read(p []byte) (int, error) {
	if !b.readSecond {
		b.readSecond = true
		return copy(p, b.first), nil
	}
	time.Sleep(b.delay)
	if len(b.second) == 0 {
		return 0, io.EOF
	}
	n := copy(p, b.second)
	b.second = b.second[n:]
	return n, nil
}

func (b *slowMediaRequestBody) Close() error { return nil }
