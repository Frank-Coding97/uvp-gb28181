package controllers

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func admissionTestRouter(middleware gin.HandlerFunc, handler gin.HandlerFunc) *gin.Engine {
	router := gin.New()
	router.POST("/resolve", middleware, handler)
	router.POST("/viewers", middleware, handler)
	return router
}

func admissionRequest(router http.Handler, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestPublicVideoShareAdmissionSharesBurstAcrossBothEndpoints(t *testing.T) {
	router := admissionTestRouter(newPublicVideoShareAdmission(rate.Every(time.Hour), 2, 8), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	require.Equal(t, http.StatusNoContent, admissionRequest(router, "/resolve", "{}").Code)
	require.Equal(t, http.StatusNoContent, admissionRequest(router, "/viewers", "{}").Code)
	response := admissionRequest(router, "/resolve", `{"signature":"secret-credential"}`)
	require.Equal(t, http.StatusTooManyRequests, response.Code)
	require.Equal(t, "1", response.Header().Get("Retry-After"))
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	require.NotContains(t, response.Body.String(), "secret-credential")
}

func TestPublicVideoShareAdmissionLimitsConcurrencyAndReturnsSlot(t *testing.T) {
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var calls atomic.Int32
	router := admissionTestRouter(newPublicVideoShareAdmission(rate.Inf, 10, 1), func(c *gin.Context) {
		if calls.Add(1) == 1 {
			entered <- struct{}{}
			<-release
		}
		c.Status(http.StatusNoContent)
	})
	finished := make(chan *httptest.ResponseRecorder, 1)
	go func() { finished <- admissionRequest(router, "/resolve", "{}") }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("request did not enter")
	}
	response := admissionRequest(router, "/viewers", `{"signature":"private"}`)
	close(release)
	require.Equal(t, http.StatusTooManyRequests, response.Code)
	require.Equal(t, "1", response.Header().Get("Retry-After"))
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	require.Equal(t, http.StatusNoContent, (<-finished).Code)
	require.Equal(t, http.StatusNoContent, admissionRequest(router, "/viewers", "{}").Code)
	require.EqualValues(t, 2, calls.Load())
}

func TestPublicVideoShareAdmissionBoundsBodiesWithoutReadingEarly(t *testing.T) {
	for _, chunked := range []bool{false, true} {
		name := "ContentLength"
		if chunked {
			name = "chunked"
		}
		t.Run(name, func(t *testing.T) {
			var entered atomic.Int32
			router := admissionTestRouter(newPublicVideoShareAdmission(rate.Inf, 10, 1), func(c *gin.Context) {
				entered.Add(1)
				var body map[string]string
				err := c.ShouldBindJSON(&body)
				var tooLarge *http.MaxBytesError
				if errors.As(err, &tooLarge) {
					c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"message": "请求内容过大"})
					return
				}
				if err != nil {
					c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"message": "请求无效"})
					return
				}
				c.Status(http.StatusNoContent)
			})
			payload := `{"signature":"` + strings.Repeat("s", 4096) + `"}`
			request := httptest.NewRequest(http.MethodPost, "/resolve", strings.NewReader(payload))
			if chunked {
				request.ContentLength = -1
				request.TransferEncoding = []string{"chunked"}
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, http.StatusRequestEntityTooLarge, response.Code)
			require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
			require.NotContains(t, response.Body.String(), strings.Repeat("s", 30))
			if chunked {
				require.EqualValues(t, 1, entered.Load())
			} else {
				require.Zero(t, entered.Load())
			}
			boundary := `{"signature":"` + strings.Repeat("s", 4096-len(`{"signature":""}`)) + `"}`
			require.Len(t, boundary, 4096)
			require.Equal(t, http.StatusNoContent, admissionRequest(router, "/resolve", boundary).Code)
		})
	}
}

type unreadAdmissionBody struct{ reads int }

func (b *unreadAdmissionBody) Read([]byte) (int, error) { b.reads++; return 0, io.EOF }
func (b *unreadAdmissionBody) Close() error             { return nil }

func TestPublicVideoShareAdmissionDoesNotReadOrCacheCredentials(t *testing.T) {
	body := &unreadAdmissionBody{}
	router := admissionTestRouter(newPublicVideoShareAdmission(rate.Inf, 10, 1), func(c *gin.Context) {
		require.Zero(t, body.reads)
		require.Empty(t, c.Keys, "middleware must not retain tokens or addresses in context")
		c.Status(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodPost, "/resolve", nil)
	request.Body = body
	request.ContentLength = -1
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusNoContent, response.Code)
	require.Zero(t, body.reads)
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
}
