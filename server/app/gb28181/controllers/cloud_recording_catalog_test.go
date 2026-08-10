package controllers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	gbrecording "uvplatform.cn/uvp-gb28181/app/gb28181/recording"
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
	triggered  []int64
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

func (s *fakeCloudRecordingCatalogService) IssueAccess(context.Context, uint, uint64, string) (gbrecording.AccessDTO, error) {
	return gbrecording.AccessDTO{Mode: gbrecording.CapabilityModePlay, Capability: "signed", ExpiresAt: time.Now()}, s.accessErr
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
