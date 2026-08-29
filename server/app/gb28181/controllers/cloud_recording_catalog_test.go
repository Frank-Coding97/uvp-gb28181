package controllers

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	gbrecording "uvplatform.cn/uvp-gb28181/app/gb28181/recording"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

type fakeCloudRecordingCatalogService struct {
	query      gbrecording.FileQuery
	userID     uint
	detailErr  error
	accessErr  error
	contentErr error
	claimErr   error
	claimCalls int
	triggered  []int64
	deletedIDs []uint64
	stoppedIDs []uint64
}

func (s *fakeCloudRecordingCatalogService) StopActiveSession(_ context.Context, _ uint, id uint64) (gbrecording.StopActiveSessionResult, error) {
	s.stoppedIDs = append(s.stoppedIDs, id)
	return gbrecording.StopActiveSessionResult{ID: strconv.FormatUint(id, 10), ChannelID: "12", Stopped: true}, nil
}

func (s *fakeCloudRecordingCatalogService) ListFiles(_ context.Context, userID uint, query gbrecording.FileQuery) (gbrecording.FileDTOPage, error) {
	s.userID, s.query = userID, query
	return gbrecording.FileDTOPage{List: []gbrecording.FileDTO{{ID: "41", FileName: "camera.mp4"}}, Total: 1, Page: query.Page, PageSize: query.PageSize}, nil
}

func (*fakeCloudRecordingCatalogService) FileOptions(context.Context, uint, gbrecording.FileQuery) (gbrecording.CatalogOptionsDTO, error) {
	return gbrecording.CatalogOptionsDTO{}, nil
}

func (s *fakeCloudRecordingCatalogService) FileDetail(context.Context, uint, uint64) (gbrecording.FileDTO, error) {
	return gbrecording.FileDTO{ID: "41"}, s.detailErr
}

func (s *fakeCloudRecordingCatalogService) DeleteFile(_ context.Context, _ uint, id uint64) (gbrecording.DeleteFileResult, error) {
	s.deletedIDs = append(s.deletedIDs, id)
	return gbrecording.DeleteFileResult{ID: strconv.FormatUint(id, 10), Deleted: true}, nil
}

func (s *fakeCloudRecordingCatalogService) DeleteFiles(_ context.Context, _ uint, ids []uint64) gbrecording.DeleteBatchResult {
	s.deletedIDs = append(s.deletedIDs, ids...)
	return gbrecording.DeleteBatchResult{DeletedCount: len(ids)}
}

func (s *fakeCloudRecordingCatalogService) IssueAccess(context.Context, uint, uint64, string) (gbrecording.AccessDTO, error) {
	return gbrecording.AccessDTO{Mode: gbrecording.CapabilityModePlay, Capability: "signed", ExpiresAt: time.Now()}, s.accessErr
}

func (*fakeCloudRecordingCatalogService) CreateDownload(context.Context, uint, uint64) (gbrecording.DownloadTaskView, string, error) {
	return gbrecording.DownloadTaskView{TaskID: "download-task", Status: gbrecording.DownloadStatusReady}, "ticket", nil
}

func (*fakeCloudRecordingCatalogService) DownloadStatus(context.Context, uint, string) (gbrecording.DownloadTaskView, error) {
	return gbrecording.DownloadTaskView{TaskID: "download-task", Status: gbrecording.DownloadStatusReady}, nil
}

func (*fakeCloudRecordingCatalogService) CancelDownload(context.Context, uint, string) (gbrecording.DownloadTaskView, error) {
	return gbrecording.DownloadTaskView{TaskID: "download-task", Status: gbrecording.DownloadStatusCancelled}, nil
}

func (s *fakeCloudRecordingCatalogService) ClaimDownload(_ context.Context, _ http.ResponseWriter, _ string, _ string, _ string, onClaimed func()) error {
	s.claimCalls++
	if s.claimErr == nil && onClaimed != nil {
		onClaimed()
	}
	return s.claimErr
}

func (*fakeCloudRecordingCatalogService) ActiveSessions(context.Context, uint) ([]gbrecording.ActiveSessionDTO, error) {
	return []gbrecording.ActiveSessionDTO{}, nil
}

func (*fakeCloudRecordingCatalogService) Reconciliations(context.Context) ([]gbrecording.ReconciliationDTO, error) {
	return []gbrecording.ReconciliationDTO{}, nil
}

func (s *fakeCloudRecordingCatalogService) TriggerReconciliation(nodeIDs []int64, _, _ *time.Time) ([]int64, error) {
	s.triggered = append([]int64(nil), nodeIDs...)
	return nodeIDs, nil
}

func (s *fakeCloudRecordingCatalogService) StreamContent(_ context.Context, writer http.ResponseWriter, _, _, _ string) error {
	if s.contentErr != nil {
		return s.contentErr
	}
	_, _ = writer.Write([]byte("content"))
	return nil
}

func TestCloudRecordingCatalogControllerListBindsFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app.Response = response.NewResponseHandler()
	service := &fakeCloudRecordingCatalogService{}
	controller := NewCloudRecordingCatalogController(service)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/gb28181/cloud-recordings/files?page=2&pageSize=30&channelId=9&deviceId=D1&nodeId=7&keyword=cam&availability=available&metadataState=partial&start=2026-08-09T00:00:00Z&end=2026-08-10T00:00:00Z", nil)
	ctx.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 88}})

	controller.ListFiles(ctx)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, uint(88), service.userID)
	require.Equal(t, 2, service.query.Page)
	require.Equal(t, 30, service.query.PageSize)
	require.Equal(t, uint(9), *service.query.ChannelID)
	require.Equal(t, int64(7), *service.query.NodeID)
	require.Equal(t, "D1", service.query.DeviceID)
	require.Contains(t, recorder.Body.String(), `"id":"41"`)
}

func TestCloudRecordingCatalogControllerDeletesSingleAndBatchFiles(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app.Response = response.NewResponseHandler()
	service := &fakeCloudRecordingCatalogService{}
	controller := NewCloudRecordingCatalogController(service)

	singleRecorder := httptest.NewRecorder()
	singleCtx, _ := gin.CreateTestContext(singleRecorder)
	singleCtx.Params = gin.Params{{Key: "id", Value: "41"}}
	singleCtx.Request = httptest.NewRequest(http.MethodDelete, "/api/gb28181/cloud-recordings/files/41", nil)
	singleCtx.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 88}})
	controller.DeleteFile(singleCtx)
	require.Equal(t, http.StatusOK, singleRecorder.Code)

	batchRecorder := httptest.NewRecorder()
	batchCtx, _ := gin.CreateTestContext(batchRecorder)
	batchCtx.Request = httptest.NewRequest(http.MethodPost, "/api/gb28181/cloud-recordings/files/batch-delete", strings.NewReader(`{"ids":["42","43"]}`))
	batchCtx.Request.Header.Set("Content-Type", "application/json")
	batchCtx.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 88}})
	controller.DeleteFiles(batchCtx)
	require.Equal(t, http.StatusOK, batchRecorder.Code, batchRecorder.Body.String())
	require.Equal(t, []uint64{41, 42, 43}, service.deletedIDs)
}

func TestCloudRecordingCatalogControllerStopsActiveSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app.Response = response.NewResponseHandler()
	service := &fakeCloudRecordingCatalogService{}
	controller := NewCloudRecordingCatalogController(service)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: "77"}}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/gb28181/cloud-recordings/active/77/stop", nil)
	ctx.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 88}})

	controller.StopActiveSession(ctx)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, []uint64{77}, service.stoppedIDs)
	require.Contains(t, recorder.Body.String(), `"stopped":true`)
}

func TestCloudRecordingCatalogControllerMapsDetailAndAccessErrors(t *testing.T) {
	app.Response = response.NewResponseHandler()
	for _, tc := range []struct {
		name   string
		err    error
		access bool
		status int
	}{
		{name: "unauthorized detail", err: gbrecording.ErrRecordingFileNotFound, status: http.StatusNotFound},
		{name: "node missing", err: gbrecording.ErrCatalogNodeMissing, access: true, status: http.StatusServiceUnavailable},
		{name: "node offline", err: gbrecording.ErrCatalogNodeOffline, access: true, status: http.StatusServiceUnavailable},
		{name: "file missing", err: gbrecording.ErrCatalogFileMissing, access: true, status: http.StatusNotFound},
		{name: "access unavailable", err: gbrecording.ErrCatalogAccessUnavailable, access: true, status: http.StatusBadGateway},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &fakeCloudRecordingCatalogService{}
			if tc.access {
				service.accessErr = tc.err
			} else {
				service.detailErr = tc.err
			}
			controller := NewCloudRecordingCatalogController(service)
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			method, target := http.MethodGet, "/api/gb28181/cloud-recordings/files/41"
			if tc.access {
				method, target = http.MethodPost, "/api/gb28181/cloud-recordings/files/41/access"
			}
			ctx.Request = httptest.NewRequest(method, target, nil)
			ctx.Params = gin.Params{{Key: "id", Value: "41"}}
			ctx.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 7}})
			if tc.access {
				ctx.Request = httptest.NewRequest(method, target, strings.NewReader(`{"mode":"play"}`))
				ctx.Request.Header.Set("Content-Type", "application/json")
				controller.IssueAccess(ctx)
			} else {
				controller.FileDetail(ctx)
			}
			require.Equal(t, tc.status, recorder.Code, recorder.Body.String())
		})
	}
}

func TestCloudRecordingCatalogControllerReconcileReturns202(t *testing.T) {
	app.Response = response.NewResponseHandler()
	service := &fakeCloudRecordingCatalogService{}
	controller := NewCloudRecordingCatalogController(service)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/gb28181/cloud-recordings/reconciliations", strings.NewReader(`{"nodeIds":[11,12]}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	controller.TriggerReconciliation(ctx)
	require.Equal(t, http.StatusAccepted, recorder.Code, recorder.Body.String())
	require.Equal(t, []int64{11, 12}, service.triggered)
}

func TestCloudRecordingCatalogControllerContentErrorDoesNotEchoCapability(t *testing.T) {
	service := &fakeCloudRecordingCatalogService{contentErr: gbrecording.ErrCapabilityExpired}
	controller := NewCloudRecordingCatalogController(service)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/gb28181/cloud-recordings/content/41?cap=top-secret-capability", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "41"}}
	controller.Content(ctx)
	require.Equal(t, http.StatusGone, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "top-secret-capability")
}

func TestCloudRecordingCatalogControllerMapsDownloadTimeoutBeforeBody(t *testing.T) {
	app.Response = response.NewResponseHandler()
	for _, timeoutErr := range []error{gbrecording.ErrContentTimeout, zlm.ErrRecordingResponseTimeout} {
		service := &fakeCloudRecordingCatalogService{claimErr: timeoutErr}
		controller := NewCloudRecordingCatalogController(service)
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/api/gb28181/cloud-recordings/downloads/download-task/content", nil)
		ctx.Request.AddCookie(&http.Cookie{Name: downloadTicketCookieName("download-task"), Value: "ticket"})
		ctx.Params = gin.Params{{Key: "taskId", Value: "download-task"}}

		controller.DownloadContent(ctx)

		require.Equal(t, http.StatusGatewayTimeout, recorder.Code, recorder.Body.String())
	}
}

func TestCloudRecordingCatalogControllerMapsUnsupportedDownloadWriter(t *testing.T) {
	app.Response = response.NewResponseHandler()
	service := &fakeCloudRecordingCatalogService{claimErr: gbrecording.ErrContentDeadline}
	controller := NewCloudRecordingCatalogController(service)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/gb28181/cloud-recordings/downloads/download-task/content", nil)
	ctx.Request.AddCookie(&http.Cookie{Name: downloadTicketCookieName("download-task"), Value: "ticket"})
	ctx.Params = gin.Params{{Key: "taskId", Value: "download-task"}}

	controller.DownloadContent(ctx)

	require.Equal(t, http.StatusBadGateway, recorder.Code, recorder.Body.String())
}

func TestCloudRecordingCatalogControllerCreatesCookieBoundDownload(t *testing.T) {
	app.Response = response.NewResponseHandler()
	service := &fakeCloudRecordingCatalogService{}
	controller := NewCloudRecordingCatalogController(service)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/gb28181/cloud-recordings/files/41/downloads", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "41"}}
	ctx.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 7}})
	controller.CreateDownload(ctx)
	require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	cookie := recorder.Result().Cookies()[0]
	require.True(t, cookie.HttpOnly)
	require.False(t, cookie.Secure, "controlled HTTP deployments must retain the same-origin cookie handoff")
	require.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
	require.Equal(t, "/api/gb28181/cloud-recordings/downloads/download-task/content", cookie.Path)
	require.Equal(t, 60, cookie.MaxAge)
	require.NotContains(t, recorder.Body.String(), "ticket")
}

func TestDownloadTicketCookieUsesTransportTLSAndIgnoresForwardedProto(t *testing.T) {
	app.Response = response.NewResponseHandler()
	controller := NewCloudRecordingCatalogController(&fakeCloudRecordingCatalogService{})
	for _, tc := range []struct {
		name           string
		tls            bool
		forwardedProto string
		secure         bool
	}{
		{name: "plain HTTP", secure: false},
		{name: "forged forwarded proto", forwardedProto: "https", secure: false},
		{name: "direct TLS", tls: true, secure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/api/gb28181/cloud-recordings/files/41/downloads", nil)
			ctx.Request.TLS = nil
			if tc.tls {
				ctx.Request.TLS = &tls.ConnectionState{}
			}
			ctx.Request.Header.Set("X-Forwarded-Proto", tc.forwardedProto)
			ctx.Params = gin.Params{{Key: "id", Value: "41"}}
			ctx.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 7}})

			controller.CreateDownload(ctx)

			cookies := recorder.Result().Cookies()
			require.Len(t, cookies, 1)
			require.Equal(t, tc.secure, cookies[0].Secure)
		})
	}
}

func TestCloudRecordingCatalogControllerDownloadContentRejectsQueryCredentials(t *testing.T) {
	service := &fakeCloudRecordingCatalogService{}
	controller := NewCloudRecordingCatalogController(service)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/gb28181/cloud-recordings/downloads/download-task/content?cap=secret&token=jwt", nil)
	ctx.Request.AddCookie(&http.Cookie{Name: downloadTicketCookieName("download-task"), Value: "ticket"})
	ctx.Params = gin.Params{{Key: "taskId", Value: "download-task"}}
	controller.DownloadContent(ctx)
	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Zero(t, service.claimCalls)
}

func TestCloudRecordingCatalogControllerKeepsTicketCookieWhenDownloadSlotIsFull(t *testing.T) {
	app.Response = response.NewResponseHandler()
	service := &fakeCloudRecordingCatalogService{claimErr: gbrecording.ErrDownloadLimit}
	controller := NewCloudRecordingCatalogController(service)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/gb28181/cloud-recordings/downloads/download-task/content", nil)
	ctx.Request.AddCookie(&http.Cookie{Name: downloadTicketCookieName("download-task"), Value: "ticket"})
	ctx.Params = gin.Params{{Key: "taskId", Value: "download-task"}}

	controller.DownloadContent(ctx)

	require.Equal(t, http.StatusTooManyRequests, recorder.Code, recorder.Body.String())
	require.Empty(t, recorder.Header().Values("Set-Cookie"), "a ready task must retain its ticket after a concurrency rejection")
}

func TestCloudRecordingCatalogControllerClearsTicketCookieAfterDownloadClaim(t *testing.T) {
	app.Response = response.NewResponseHandler()
	controller := NewCloudRecordingCatalogController(&fakeCloudRecordingCatalogService{})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/gb28181/cloud-recordings/downloads/download-task/content", nil)
	ctx.Request.AddCookie(&http.Cookie{Name: downloadTicketCookieName("download-task"), Value: "ticket"})
	ctx.Params = gin.Params{{Key: "taskId", Value: "download-task"}}

	controller.DownloadContent(ctx)

	cookies := recorder.Result().Cookies()
	require.Len(t, cookies, 1)
	require.Equal(t, downloadTicketCookieName("download-task"), cookies[0].Name)
	require.Equal(t, -1, cookies[0].MaxAge)
	require.Equal(t, downloadContentPath("download-task"), cookies[0].Path)
}
