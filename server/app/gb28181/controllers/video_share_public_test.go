package controllers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/videoshare"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/utils/response"
)

type publicVideoShareResolverStub struct {
	result      videoshare.PublicShare
	gotID       string
	gotSig      string
	resolveErr  error
	viewerToken string
}

func (s *publicVideoShareResolverStub) ResolvePublic(_ context.Context, id, signature string) (videoshare.PublicShare, error) {
	s.gotID, s.gotSig = id, signature
	return s.result, s.resolveErr
}
func (s *publicVideoShareResolverStub) CreateViewer(_ context.Context, id, signature string) (videoshare.Viewer, error) {
	s.gotID, s.gotSig = id, signature
	return videoshare.Viewer{Token: "viewer-token", HTTPFLVPath: "/api/public/video-share/viewers/viewer-token/live.flv"}, nil
}
func (s *publicVideoShareResolverStub) LookupViewer(_ context.Context, token string) (videoshare.ViewerAccess, error) {
	s.viewerToken = token
	return videoshare.ViewerAccess{ExpiresAt: time.Date(2026, 11, 17, 13, 45, 0, 0, time.UTC)}, nil
}

func TestPublicVideoShareStatusBoundsChunkedCredentialBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app.Response = response.NewResponseHandler()
	resolver := &publicVideoShareResolverStub{}
	router := gin.New()
	router.POST("/public/video-share/viewers/status", NewPublicVideoShareController(resolver).ViewerStatus)
	for _, token := range []string{"valid-viewer", strings.Repeat("x", int(publicVideoShareBodyLimit)+1)} {
		resolver.viewerToken = ""
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/public/video-share/viewers/status", strings.NewReader(`{"viewerToken":"`+token+`"}`))
		request.ContentLength = -1
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, request)
		if len(token) > int(publicVideoShareBodyLimit) {
			require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
			require.Empty(t, resolver.viewerToken)
		} else {
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Equal(t, token, resolver.viewerToken)
		}
	}
}
func (s *publicVideoShareResolverStub) CloseViewer(string) error { return nil }
func (s *publicVideoShareResolverStub) RelayHTTPFLV(_ context.Context, w http.ResponseWriter, _ string) {
	w.WriteHeader(http.StatusOK)
}
func (s *publicVideoShareResolverStub) RelayHLS(_ context.Context, w http.ResponseWriter, _, _ string) {
	w.WriteHeader(http.StatusOK)
}

func TestPublicVideoShareResolveAcceptsCredentialsInBodyAndReturnsMinimalData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app.Response = response.NewResponseHandler()
	expires := time.Date(2026, 11, 17, 13, 45, 0, 0, time.UTC)
	resolver := &publicVideoShareResolverStub{result: videoshare.PublicShare{Name: "Lobby", Online: true, ExpiresAt: expires, ServerTime: expires.Add(-time.Minute)}}
	router := gin.New()
	router.POST("/public/video-share/resolve", NewPublicVideoShareController(resolver).Resolve)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/public/video-share/resolve", strings.NewReader(`{"publicId":"public-id","signature":"secret-signature"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, "public-id", resolver.gotID)
	require.Equal(t, "secret-signature", resolver.gotSig)
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	require.Contains(t, recorder.Body.String(), `"name":"Lobby"`)
	require.NotContains(t, recorder.Body.String(), "secret-signature")
	require.NotContains(t, recorder.Body.String(), "deviceId")
}

func TestPublicVideoShareCreateViewerReturnsOnlyLocalMediaPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app.Response = response.NewResponseHandler()
	resolver := &publicVideoShareResolverStub{}
	router := gin.New()
	router.POST("/public/video-share/viewers", NewPublicVideoShareController(resolver).CreateViewer)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/public/video-share/viewers", strings.NewReader(`{"publicId":"public-id","signature":"signature"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), `"viewerToken":"viewer-token"`)
	require.Contains(t, recorder.Body.String(), `"httpFlvPath":"/api/public/video-share/viewers/viewer-token/live.flv"`)
	require.NotContains(t, recorder.Body.String(), "http://zlm")
}

func TestPublicVideoShareResolveRejectsMissingCredential(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app.Response = response.NewResponseHandler()
	resolver := &publicVideoShareResolverStub{}
	router := gin.New()
	router.POST("/public/video-share/resolve", NewPublicVideoShareController(resolver).Resolve)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/public/video-share/resolve", strings.NewReader(`{"publicId":"public-id"}`)))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Empty(t, resolver.gotID)
}

func TestPublicVideoShareResolveDistinguishesDependencyFailureFromExpired(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app.Response = response.NewResponseHandler()
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{
		{"database unavailable", errors.New("private database address"), http.StatusServiceUnavailable},
		{"expired", videoshare.ErrExpired, http.StatusGone},
		{"unknown", videoshare.ErrNotFound, http.StatusGone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolver := &publicVideoShareResolverStub{resolveErr: tc.err}
			router := gin.New()
			router.POST("/public/video-share/resolve", NewPublicVideoShareController(resolver).Resolve)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/public/video-share/resolve", strings.NewReader(`{"publicId":"id","signature":"key"}`))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, request)
			require.Equal(t, tc.status, recorder.Code)
			require.NotContains(t, recorder.Body.String(), "private database address")
		})
	}
}
