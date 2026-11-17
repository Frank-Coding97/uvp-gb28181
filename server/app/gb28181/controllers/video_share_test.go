package controllers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/videoshare"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/global/consts"
	"uvplatform.com/uvp-gb28181/app/middleware"
	"uvplatform.com/uvp-gb28181/app/utils/response"
)

type videoShareManagerStub struct {
	link videoshare.Link
	err  error
	last struct {
		channel  uint
		actor    uint
		enabled  bool
		revision uint64
	}
}

func (s *videoShareManagerStub) Status(_ context.Context, channel uint, actor uint) (videoshare.Link, error) {
	s.last.channel, s.last.actor = channel, actor
	return s.link, s.err
}
func (s *videoShareManagerStub) SetEnabled(_ context.Context, channel uint, actor uint, enabled bool, revision uint64) (videoshare.Link, error) {
	s.last.channel, s.last.actor, s.last.enabled, s.last.revision = channel, actor, enabled, revision
	return s.link, s.err
}
func (s *videoShareManagerStub) AcquireLink(_ context.Context, channel uint, actor uint, revision uint64) (videoshare.Link, error) {
	s.last.channel, s.last.actor, s.last.revision = channel, actor, revision
	return s.link, s.err
}

func (s *videoShareManagerStub) Regenerate(_ context.Context, channel uint, actor uint, revision uint64) (videoshare.Link, error) {
	s.last.channel, s.last.actor, s.last.revision = channel, actor, revision
	return s.link, s.err
}

func videoShareRouter(manager VideoShareManager) *gin.Engine {
	gin.SetMode(gin.TestMode)
	app.Response = response.NewResponseHandler()
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 19}})
		c.Next()
	})
	controller := NewVideoShareController(manager)
	r.GET("/channels/:id/share", controller.Status)
	r.PUT("/channels/:id/share", controller.SetEnabled)
	r.POST("/channels/:id/share/link", controller.AcquireLink)
	r.POST("/channels/:id/share/regenerate", controller.Regenerate)
	return r
}

func TestVideoShareControllerStatusDoesNotExposeCredential(t *testing.T) {
	manager := &videoShareManagerStub{link: videoshare.Link{PublicID: "public", Signature: "secret", Pending: true, Revision: 4}}
	w := httptest.NewRecorder()
	videoShareRouter(manager).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/channels/42/share", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"publicId":"public"`)
	require.NotContains(t, w.Body.String(), "secret")
	require.EqualValues(t, 42, manager.last.channel)
	require.EqualValues(t, 19, manager.last.actor)
}

func TestVideoShareControllerSetAndAcquireLink(t *testing.T) {
	manager := &videoShareManagerStub{link: videoshare.Link{PublicID: "public", Signature: "signed", ExpiresAt: ptrTime(time.Now().UTC()), Revision: 5}}
	r := videoShareRouter(manager)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/channels/42/share", bytes.NewBufferString(`{"enabled":true,"revision":4}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.True(t, manager.last.enabled)
	require.EqualValues(t, 4, manager.last.revision)

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/channels/42/share/link", bytes.NewBufferString(`{"revision":5}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"signature":"signed"`)
	require.EqualValues(t, 5, manager.last.revision)
}

func TestVideoShareControllerAcquireRequiresObservedRevision(t *testing.T) {
	for _, body := range []string{"", `{}`, `{"revision":0}`} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/channels/42/share/link", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		videoShareRouter(&videoShareManagerStub{}).ServeHTTP(w, req)
		require.Equal(t, http.StatusBadRequest, w.Code)
	}
}

func TestVideoShareControllerMarksCredentialResponsesSensitive(t *testing.T) {
	for _, path := range []string{"", "/link", "/regenerate"} {
		manager := &videoShareManagerStub{link: videoshare.Link{Signature: "never-store-this", Revision: 4}}
		r := videoShareRouter(manager)
		var metadata map[string]any
		r.Use(func(c *gin.Context) { c.Next(); metadata, _ = middleware.SensitiveOperationMetadata(c) })
		// Register a fresh route after the capturing middleware was installed.
		controller := NewVideoShareController(manager)
		method := http.MethodPut
		handler := controller.SetEnabled
		if path == "/link" {
			method, handler = http.MethodPost, controller.AcquireLink
		}
		if path == "/regenerate" {
			method, handler = http.MethodPost, controller.Regenerate
		}
		r.Handle(method, "/audit/:id"+path, handler)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/audit/42"+path, bytes.NewBufferString(`{"enabled":true,"revision":4}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		require.NotNil(t, metadata)
		require.EqualValues(t, 42, metadata["channel_row_id"])
		require.NotContains(t, metadata, "signature")
	}
}

func TestVideoShareControllerRejectsInvalidIDsAndMapsRevisionConflict(t *testing.T) {
	manager := &videoShareManagerStub{err: videoshare.ErrConflict}
	r := videoShareRouter(manager)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/channels/0/share", bytes.NewBufferString(`{"enabled":true,"revision":0}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/channels/42/share", bytes.NewBufferString(`{"enabled":true,"revision":0}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusConflict, w.Code)
}

func ptrTime(value time.Time) *time.Time { return &value }
