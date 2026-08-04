package controllers_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	gbplayback "uvplatform.cn/uvp-gb28181/app/gb28181/playback"
	"uvplatform.cn/uvp-gb28181/app/gb28181/recordquery"
)

type fakePlaybackHTTPService struct {
	mu        sync.Mutex
	created   gbplayback.CreateRequest
	session   *gbplayback.Session
	createErr error
	actionErr error
	actions   []gbplayback.ActionRequest
	stops     int
}

func (f *fakePlaybackHTTPService) Create(_ context.Context, request gbplayback.CreateRequest) (gbplayback.CreateResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = request
	if f.createErr != nil {
		return gbplayback.CreateResult{}, f.createErr
	}
	if f.session == nil {
		f.session = &gbplayback.Session{ID: "session-1", OwnerID: request.OwnerID, ChannelID: request.ChannelID, RecordKey: request.RecordKey, State: gbplayback.StatePlaying,
			SegmentStart: request.SegmentStart, SegmentEnd: request.SegmentEnd, Deadline: time.Unix(1700003600, 0), Scale: 1, MediaURLs: map[string]string{"wsFlv": "ws://node/live.flv"}}
	}
	return gbplayback.CreateResult{Session: f.session}, nil
}
func (f *fakePlaybackHTTPService) GetForOwner(_, owner string) (*gbplayback.Session, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.session, f.session != nil && f.session.OwnerID == owner
}
func (f *fakePlaybackHTTPService) Action(_ context.Context, _, owner string, action gbplayback.ActionRequest) (*gbplayback.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.session == nil || f.session.OwnerID != owner {
		return nil, gbplayback.ErrPlaybackNotFound
	}
	if f.actionErr != nil {
		return nil, f.actionErr
	}
	f.actions = append(f.actions, action)
	return f.session, nil
}
func (f *fakePlaybackHTTPService) StopForOwner(_ context.Context, _ string, owner string, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.session == nil || f.session.OwnerID != owner {
		return gbplayback.ErrPlaybackNotFound
	}
	f.stops++
	f.session.State = gbplayback.StateStopped
	return nil
}

type fakePlaybackSnapshots struct {
	snapshot recordquery.Snapshot
	err      error
}

func (f fakePlaybackSnapshots) Resolve(recordquery.ResolveRequest) (recordquery.Snapshot, error) {
	return f.snapshot, f.err
}

func newPlaybackHTTPFixture(t *testing.T) (recordQueryFixture, *fakePlaybackHTTPService, time.Time) {
	t.Helper()
	fixture := newRecordQueryFixture(t, 10)
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	now := time.Date(2026, 8, 4, 8, 0, 0, 0, location)
	service := &fakePlaybackHTTPService{}
	snapshots := fakePlaybackSnapshots{snapshot: recordquery.Snapshot{RecordKey: "opaque-key", OwnerUserID: 100, ChannelID: fixture.channel.ID,
		DeviceCode: fixture.device.DeviceID, ChannelCode: fixture.channel.ChannelID, SegmentStart: now, SegmentEnd: now.Add(30 * time.Minute), ExpiresAt: now.Add(time.Hour)}}
	fixture.controller.SetPlaybackRuntime(service, snapshots)
	fixture.router = gin.New()
	fixture.router.Use(gin.Recovery(), withClaims(100))
	base := "/channel/:id/playback-sessions"
	fixture.router.POST(base, fixture.controller.CreatePlaybackSession)
	fixture.router.GET(base+"/:sessionId", fixture.controller.GetPlaybackSession)
	fixture.router.POST(base+"/:sessionId/actions", fixture.controller.ActionPlaybackSession)
	fixture.router.DELETE(base+"/:sessionId", fixture.controller.DeletePlaybackSession)
	return fixture, service, now
}

func TestDeviceRecordPlaybackCreateAndGetContract(t *testing.T) {
	fixture, service, _ := newPlaybackHTTPFixture(t)
	path := "/channel/" + uintStr(fixture.channel.ID) + "/playback-sessions"
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"recordKey":"opaque-key","playFrom":"2026-08-04T08:10:00"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "idem-1")
	result := httptest.NewRecorder()
	fixture.router.ServeHTTP(result, request)
	require.Equal(t, http.StatusOK, result.Code, result.Body.String())
	require.Contains(t, result.Body.String(), `"sessionId":"session-1"`)
	require.Contains(t, result.Body.String(), `"state":"playing"`)
	require.Equal(t, "100", service.created.OwnerID)
	require.Equal(t, "opaque-key", service.created.RecordKey)
	require.Equal(t, fixture.channel.ChannelID, service.created.SIPChannelID)
	require.Equal(t, fixture.device.IP+":"+uintStr(uint(fixture.device.Port)), service.created.Destination)
	require.Equal(t, fixture.device.Transport, service.created.Transport)
	require.True(t, service.created.PlayFrom.Hour() == 8 && service.created.PlayFrom.Minute() == 10)

	get := httptest.NewRecorder()
	fixture.router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, path+"/session-1", nil))
	require.Equal(t, http.StatusOK, get.Code, get.Body.String())
	require.Contains(t, get.Body.String(), `"media":{"urls":{"wsFlv":"ws://node/live.flv"}}`)
}

func TestDeviceRecordPlaybackRejectsTamperedAndInvalidCreate(t *testing.T) {
	fixture, service, now := newPlaybackHTTPFixture(t)
	fixture.controller.SetPlaybackRuntime(service, fakePlaybackSnapshots{err: &recordquery.QueryError{Code: recordquery.ErrorCodeExpired, Err: recordquery.ErrSnapshotExpired}})
	path := "/channel/" + uintStr(fixture.channel.ID) + "/playback-sessions"
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"recordKey":"opaque-key","playFrom":"2026-08-04T08:10:00"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "idem-1")
	expired := httptest.NewRecorder()
	fixture.router.ServeHTTP(expired, request)
	require.Equal(t, http.StatusGone, expired.Code, expired.Body.String())
	require.Contains(t, expired.Body.String(), `"errorCode":"playback_expired"`)

	fixture.controller.SetPlaybackRuntime(service, fakePlaybackSnapshots{snapshot: recordquery.Snapshot{RecordKey: "opaque-key", OwnerUserID: 100, ChannelID: fixture.channel.ID, SegmentStart: now, SegmentEnd: now.Add(time.Minute)}})
	unknown := httptest.NewRecorder()
	bad := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"recordKey":"opaque-key","playFrom":"2026-08-04T08:10:00","channelId":999}`))
	bad.Header.Set("Content-Type", "application/json")
	bad.Header.Set("Idempotency-Key", "idem-2")
	fixture.router.ServeHTTP(unknown, bad)
	require.Equal(t, http.StatusUnprocessableEntity, unknown.Code, unknown.Body.String())
	require.Zero(t, service.created.RecordKey)
}

func TestDeviceRecordPlaybackMapsCreateFailureStage(t *testing.T) {
	tests := []struct {
		name, stage string
		status      int
	}{
		{name: "node", stage: "node", status: http.StatusBadGateway},
		{name: "rtp", stage: "rtp", status: http.StatusBadGateway},
		{name: "invite", stage: "invite", status: http.StatusBadGateway},
		{name: "media wait", stage: "media_wait", status: http.StatusGatewayTimeout},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture, service, _ := newPlaybackHTTPFixture(t)
			service.createErr = &gbplayback.ServiceError{Stage: tt.stage, Code: "failed", Err: errors.New("device detail")}
			path := "/channel/" + uintStr(fixture.channel.ID) + "/playback-sessions"
			request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"recordKey":"opaque-key","playFrom":"2026-08-04T08:10:00"}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", "idem-stage")
			result := httptest.NewRecorder()
			fixture.router.ServeHTTP(result, request)
			require.Equal(t, tt.status, result.Code, result.Body.String())
			require.Contains(t, result.Body.String(), `"errorStage":"`+tt.stage+`"`)
			require.NotContains(t, result.Body.String(), "device detail")
		})
	}
}

func TestDeviceRecordPlaybackActionsAndDeleteAreValidatedAndOwnerScoped(t *testing.T) {
	fixture, service, _ := newPlaybackHTTPFixture(t)
	path := "/channel/" + uintStr(fixture.channel.ID) + "/playback-sessions/session-1"
	service.session = &gbplayback.Session{ID: "session-1", OwnerID: "100", ChannelID: uintStr(fixture.channel.ID), State: gbplayback.StatePlaying}
	bad := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, path+"/actions", strings.NewReader(`{"action":"scale","scale":3}`))
	request.Header.Set("Content-Type", "application/json")
	fixture.router.ServeHTTP(bad, request)
	require.Equal(t, http.StatusUnprocessableEntity, bad.Code, bad.Body.String())

	ok := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, path+"/actions", strings.NewReader(`{"action":"pause"}`))
	request.Header.Set("Content-Type", "application/json")
	fixture.router.ServeHTTP(ok, request)
	require.Equal(t, http.StatusOK, ok.Code, ok.Body.String())
	require.Len(t, service.actions, 1)
	for _, body := range []string{
		`{"action":"resume"}`,
		`{"action":"seek","positionSeconds":12}`,
		`{"action":"scale","scale":2}`,
	} {
		control := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path+"/actions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		fixture.router.ServeHTTP(control, req)
		require.Equal(t, http.StatusOK, control.Code, control.Body.String())
	}
	require.Len(t, service.actions, 4)

	deleted := httptest.NewRecorder()
	fixture.router.ServeHTTP(deleted, httptest.NewRequest(http.MethodDelete, path, nil))
	require.Equal(t, http.StatusOK, deleted.Code, deleted.Body.String())
	require.Equal(t, 1, service.stops)

	other := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	otherRouter := gin.New()
	otherRouter.Use(withClaims(200))
	otherRouter.GET("/channel/:id/playback-sessions/:sessionId", fixture.controller.GetPlaybackSession)
	otherRouter.ServeHTTP(other, r)
	require.Equal(t, http.StatusNotFound, other.Code, other.Body.String())
}
