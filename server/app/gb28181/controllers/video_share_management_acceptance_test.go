package controllers

import (
	"context"
	"errors"
	"fmt"
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

type acceptanceManagement struct {
	videoShareManagerStub
	calls int
}

func (s *acceptanceManagement) Status(ctx context.Context, channel, actor uint) (videoshare.Link, error) {
	s.calls++
	return s.videoShareManagerStub.Status(ctx, channel, actor)
}
func (s *acceptanceManagement) SetEnabled(ctx context.Context, channel, actor uint, enabled bool, revision uint64) (videoshare.Link, error) {
	s.calls++
	return s.videoShareManagerStub.SetEnabled(ctx, channel, actor, enabled, revision)
}
func (s *acceptanceManagement) AcquireLink(ctx context.Context, channel, actor uint, revision uint64) (videoshare.Link, error) {
	s.calls++
	return s.videoShareManagerStub.AcquireLink(ctx, channel, actor, revision)
}
func (s *acceptanceManagement) Regenerate(ctx context.Context, channel, actor uint, revision uint64) (videoshare.Link, error) {
	s.calls++
	return s.videoShareManagerStub.Regenerate(ctx, channel, actor, revision)
}

func managementRequest(router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestVideoShareManagementRejectsChannelTTLAndMalformedBodiesBeforeAnyBusinessCall(t *testing.T) {
	for _, input := range []struct{ method, path, body string }{
		{"PUT", "/channels/42/share", `{"enabled":true,"revision":0,"ttlSeconds":600}`},
		{"PUT", "/channels/42/share", `{"enabled":true,"revision":0,"ttlMinutes":10}`},
		{"POST", "/channels/42/share/link", `{"revision":2,"ttlSeconds":600}`},
		{"POST", "/channels/42/share/regenerate", `{"revision":2,"ttlSeconds":600}`},
		{"PUT", "/channels/42/share", `{"enabled":true,"revision":0}{"enabled":false}`},
		{"POST", "/channels/42/share/link", `{"revision":2}garbage`},
		{"PUT", "/channels/42/share", `{"enabled":null,"revision":0}`},
		{"PUT", "/channels/42/share", `{"enabled":"true","revision":0}`},
		{"POST", "/channels/42/share/link", `{"revision":0}`},
		{"POST", "/channels/42/share/regenerate", `{}`},
	} {
		t.Run(input.method+input.path+input.body, func(t *testing.T) {
			manager := &acceptanceManagement{}
			recorder := managementRequest(videoShareRouter(manager), input.method, input.path, input.body)
			require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
			require.Zero(t, manager.calls, "invalid requests must not reach lifecycle, SIP, RTP or recording work")
		})
	}
}

func TestVideoShareManagementRejectsMissingIdentityAndInvalidTargetsBeforeBusinessCall(t *testing.T) {
	for _, id := range []string{"0", "-1", "not-a-channel", "4294967296"} {
		manager := &acceptanceManagement{}
		recorder := managementRequest(videoShareRouter(manager), "PUT", "/channels/"+id+"/share", `{"enabled":true,"revision":0}`)
		require.Equal(t, http.StatusBadRequest, recorder.Code)
		require.Zero(t, manager.calls)
	}
	gin.SetMode(gin.TestMode)
	app.Response = response.NewResponseHandler()
	manager := &acceptanceManagement{}
	router := gin.New()
	router.PUT("/channels/:id/share", NewVideoShareController(manager).SetEnabled)
	recorder := managementRequest(router, "PUT", "/channels/42/share", `{"enabled":true,"revision":0}`)
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.Zero(t, manager.calls)
}

func TestVideoShareManagementErrorsNeverReturnSuccessfulStateOrPrivateMaterial(t *testing.T) {
	for _, input := range []struct {
		err    error
		status int
	}{
		{videoshare.ErrConflict, 409}, {videoshare.ErrExpired, 410}, {videoshare.ErrNotFound, 404},
		{videoshare.ErrInvalidCredential, 410}, {errors.New("private-db-material"), 500},
	} {
		for _, route := range []struct{ method, path, body string }{
			{"GET", "/channels/42/share", ""}, {"PUT", "/channels/42/share", `{"enabled":true,"revision":4}`},
			{"POST", "/channels/42/share/link", `{"revision":4}`}, {"POST", "/channels/42/share/regenerate", `{"revision":4}`},
		} {
			t.Run(fmt.Sprint(input.status)+route.method+route.path, func(t *testing.T) {
				manager := &acceptanceManagement{videoShareManagerStub: videoShareManagerStub{
					err:  fmt.Errorf("private-db-material: %w", input.err),
					link: videoshare.Link{Enabled: true, Signature: "private-signature", PublicID: "private-public-id"},
				}}
				recorder := managementRequest(videoShareRouter(manager), route.method, route.path, route.body)
				require.Equal(t, input.status, recorder.Code)
				for _, value := range []string{"private-db-material", "private-signature", "private-public-id", `"enabled":true`} {
					require.NotContains(t, recorder.Body.String(), value)
				}
				require.Equal(t, 1, manager.calls)
			})
		}
	}
	recorder := managementRequest(videoShareRouter(nil), "PUT", "/channels/42/share", `{"enabled":true,"revision":0}`)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
}

func TestVideoShareManagementRegeneratePreservesLifecycleResponseAndActor(t *testing.T) {
	expiry := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Second)
	manager := &acceptanceManagement{videoShareManagerStub: videoShareManagerStub{link: videoshare.Link{
		Enabled: true, PublicID: "current-public-id", Signature: "current-signature", Revision: 9, ExpiresAt: &expiry, ServerTime: expiry.Add(-5 * time.Minute),
	}}}
	recorder := managementRequest(videoShareRouter(manager), "POST", "/channels/42/share/regenerate", `{"revision":8}`)
	require.Equal(t, http.StatusOK, recorder.Code)
	for _, field := range []string{`"signature":"current-signature"`, `"revision":9`, `"expiresAt":`, `"serverTime":`} {
		require.Contains(t, recorder.Body.String(), field)
	}
	require.EqualValues(t, 42, manager.last.channel)
	require.EqualValues(t, 19, manager.last.actor)
	require.EqualValues(t, 8, manager.last.revision)
}
