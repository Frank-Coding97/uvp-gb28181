package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	gbrecording "uvplatform.cn/uvp-gb28181/app/gb28181/recording"
	"uvplatform.cn/uvp-gb28181/app/middleware"
)

type routeCatalogService struct{ contentHasDeadline bool }

func (*routeCatalogService) ListFiles(context.Context, uint, gbrecording.FileQuery) (gbrecording.FileDTOPage, error) {
	return gbrecording.FileDTOPage{}, nil
}
func (*routeCatalogService) FileOptions(context.Context, uint, gbrecording.FileQuery) (gbrecording.CatalogOptionsDTO, error) {
	return gbrecording.CatalogOptionsDTO{}, nil
}
func (*routeCatalogService) FileDetail(context.Context, uint, uint64) (gbrecording.FileDTO, error) {
	return gbrecording.FileDTO{}, nil
}
func (*routeCatalogService) IssueAccess(context.Context, uint, uint64, string) (gbrecording.AccessDTO, error) {
	return gbrecording.AccessDTO{}, nil
}
func (*routeCatalogService) CreateDownload(context.Context, uint, uint64) (gbrecording.DownloadTaskView, string, error) {
	return gbrecording.DownloadTaskView{}, "", nil
}
func (*routeCatalogService) DownloadStatus(context.Context, uint, string) (gbrecording.DownloadTaskView, error) {
	return gbrecording.DownloadTaskView{}, nil
}
func (*routeCatalogService) CancelDownload(context.Context, uint, string) (gbrecording.DownloadTaskView, error) {
	return gbrecording.DownloadTaskView{}, nil
}
func (s *routeCatalogService) ClaimDownload(ctx context.Context, writer http.ResponseWriter, _, _, _ string, onClaimed func()) error {
	_, s.contentHasDeadline = ctx.Deadline()
	if onClaimed != nil {
		onClaimed()
	}
	_, _ = writer.Write([]byte("streamed"))
	return nil
}
func (*routeCatalogService) ActiveSessions(context.Context, uint) ([]gbrecording.ActiveSessionDTO, error) {
	return nil, nil
}
func (*routeCatalogService) Reconciliations(context.Context) ([]gbrecording.ReconciliationDTO, error) {
	return nil, nil
}
func (*routeCatalogService) TriggerReconciliation([]int64, *time.Time, *time.Time) ([]int64, error) {
	return nil, nil
}
func (s *routeCatalogService) StreamContent(ctx context.Context, writer http.ResponseWriter, _, _, _ string) error {
	_, s.contentHasDeadline = ctx.Deadline()
	_, _ = writer.Write([]byte("streamed"))
	return nil
}

func TestCloudRecordingCatalogRoutesAndContentTimeoutBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &routeCatalogService{}
	SetCloudRecordingCatalogService(service)
	defer SetCloudRecordingCatalogService(nil)

	engine := gin.New()
	RegisterContentRoutes(engine)
	engine.Use(middleware.TimeoutMiddleware(time.Nanosecond))
	RegisterRoutes(engine.Group("/api"))

	want := map[string]bool{
		"GET /api/gb28181/cloud-recordings/files":                     false,
		"GET /api/gb28181/cloud-recordings/files/options":             false,
		"GET /api/gb28181/cloud-recordings/files/:id":                 false,
		"POST /api/gb28181/cloud-recordings/files/:id/access":         false,
		"POST /api/gb28181/cloud-recordings/files/:id/downloads":      false,
		"GET /api/gb28181/cloud-recordings/downloads/:taskId":         false,
		"DELETE /api/gb28181/cloud-recordings/downloads/:taskId":      false,
		"GET /api/gb28181/cloud-recordings/downloads/:taskId/content": false,
		"GET /api/gb28181/cloud-recordings/active":                    false,
		"GET /api/gb28181/cloud-recordings/reconciliations":           false,
		"POST /api/gb28181/cloud-recordings/reconciliations":          false,
		"GET /api/gb28181/cloud-recordings/content/:id":               false,
	}
	for _, route := range engine.Routes() {
		key := route.Method + " " + route.Path
		if _, exists := want[key]; exists {
			want[key] = true
		}
	}
	for route, found := range want {
		require.True(t, found, route)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/gb28181/cloud-recordings/content/41?cap=signed", nil)
	engine.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, "streamed", recorder.Body.String())
	require.False(t, service.contentHasDeadline, "content route must bypass the global handler timeout")
}
