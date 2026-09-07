package recording_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	gbrecording "uvplatform.cn/uvp-gb28181/app/gb28181/recording"
	gbroutes "uvplatform.cn/uvp-gb28181/app/gb28181/routes"
)

type standaloneContentService struct {
	streamCalls atomic.Int32

	streamFn func(context.Context, http.ResponseWriter, string, string, string) error

	streamStarted  chan struct{}
	streamReleased chan struct{}
	streamReturned chan struct{}

	streamStartOnce   sync.Once
	streamReleaseOnce sync.Once
	streamReturnOnce  sync.Once
}

func (s *standaloneContentService) StreamContent(ctx context.Context, writer http.ResponseWriter, fileID, capability, byteRange string) error {
	s.streamCalls.Add(1)
	s.signal(s.streamStarted, &s.streamStartOnce)
	defer s.signal(s.streamReturned, &s.streamReturnOnce)
	if s.streamFn != nil {
		return s.streamFn(ctx, writer, fileID, capability, byteRange)
	}
	_, _ = writer.Write([]byte("content"))
	return nil
}

func (s *standaloneContentService) ClaimDownload(ctx context.Context, writer http.ResponseWriter, taskID, ticket, byteRange string, onClaimed func()) error {
	return nil
}

func (s *standaloneContentService) signal(ch chan struct{}, once *sync.Once) {
	if ch != nil {
		once.Do(func() { close(ch) })
	}
}

func (*standaloneContentService) ListFiles(context.Context, uint, gbrecording.FileQuery) (gbrecording.FileDTOPage, error) {
	return gbrecording.FileDTOPage{}, nil
}
func (*standaloneContentService) FileOptions(context.Context, uint, gbrecording.FileQuery) (gbrecording.CatalogOptionsDTO, error) {
	return gbrecording.CatalogOptionsDTO{}, nil
}
func (*standaloneContentService) FileDetail(context.Context, uint, uint64) (gbrecording.FileDTO, error) {
	return gbrecording.FileDTO{}, nil
}
func (*standaloneContentService) DeleteFile(context.Context, uint, uint64) (gbrecording.DeleteFileResult, error) {
	return gbrecording.DeleteFileResult{}, nil
}
func (*standaloneContentService) DeleteFiles(context.Context, uint, []uint64) gbrecording.DeleteBatchResult {
	return gbrecording.DeleteBatchResult{}
}
func (*standaloneContentService) IssueAccess(context.Context, uint, uint64, string) (gbrecording.AccessDTO, error) {
	return gbrecording.AccessDTO{}, nil
}
func (*standaloneContentService) CreateDownload(context.Context, uint, uint64) (gbrecording.DownloadTaskView, string, error) {
	return gbrecording.DownloadTaskView{}, "", nil
}
func (*standaloneContentService) DownloadStatus(context.Context, uint, string) (gbrecording.DownloadTaskView, error) {
	return gbrecording.DownloadTaskView{}, nil
}
func (*standaloneContentService) CancelDownload(context.Context, uint, string) (gbrecording.DownloadTaskView, error) {
	return gbrecording.DownloadTaskView{}, nil
}
func (*standaloneContentService) ActiveSessions(context.Context, uint) ([]gbrecording.ActiveSessionDTO, error) {
	return nil, nil
}
func (*standaloneContentService) StopActiveSession(context.Context, uint, uint64) (gbrecording.StopActiveSessionResult, error) {
	return gbrecording.StopActiveSessionResult{}, nil
}
func (*standaloneContentService) Reconciliations(context.Context) ([]gbrecording.ReconciliationDTO, error) {
	return nil, nil
}
func (*standaloneContentService) TriggerReconciliation([]int64, *time.Time, *time.Time) ([]int64, error) {
	return nil, nil
}

func newStandaloneContentServer(t *testing.T, service *standaloneContentService) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	gbroutes.SetCloudRecordingCatalogService(service)
	t.Cleanup(func() { gbroutes.SetCloudRecordingCatalogService(nil) })

	engine := gin.New()
	gbroutes.RegisterContentRoutes(engine)
	return httptest.NewServer(engine)
}

func TestStandaloneContentRouteRejectsMissingCapability(t *testing.T) {
	service := &standaloneContentService{}
	server := newStandaloneContentServer(t, service)
	defer server.Close()

	response, err := server.Client().Get(server.URL + "/api/gb28181/cloud-recordings/content/41")
	require.NoError(t, err)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())

	require.Equal(t, http.StatusForbidden, response.StatusCode)
	require.Zero(t, service.streamCalls.Load(), "missing capability must be rejected before the content service")
	require.NotContains(t, string(body), "secret")
}

func TestStandaloneContentRouteDoesNotEchoInvalidCapability(t *testing.T) {
	const capability = "top-secret-capability"
	service := &standaloneContentService{
		streamFn: func(context.Context, http.ResponseWriter, string, string, string) error {
			return gbrecording.ErrCapabilityInvalid
		},
	}
	server := newStandaloneContentServer(t, service)
	defer server.Close()

	response, err := server.Client().Get(server.URL + "/api/gb28181/cloud-recordings/content/41?cap=" + capability)
	require.NoError(t, err)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())

	require.Equal(t, http.StatusForbidden, response.StatusCode)
	require.EqualValues(t, 1, service.streamCalls.Load())
	require.NotContains(t, string(body), capability)
}

func TestStandaloneContentRouteReleasesOnRequestCancellation(t *testing.T) {
	service := &standaloneContentService{
		streamStarted:  make(chan struct{}),
		streamReleased: make(chan struct{}),
		streamReturned: make(chan struct{}),
	}
	// The stream function needs a stable release signal without capturing a
	// response writer or relying on a client-side response.
	service.streamFn = func(ctx context.Context, _ http.ResponseWriter, _, _, _ string) error {
		<-ctx.Done()
		service.signal(service.streamReleased, &service.streamReleaseOnce)
		return ctx.Err()
	}

	server := newStandaloneContentServer(t, service)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/gb28181/cloud-recordings/content/41?cap=signed", nil)
	require.NoError(t, err)

	result := make(chan error, 1)
	go func() {
		response, requestErr := server.Client().Do(request)
		if response != nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
		result <- requestErr
	}()

	select {
	case <-service.streamStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("content route did not reach the stream service")
	}
	cancel()

	select {
	case <-service.streamReleased:
	case <-time.After(2 * time.Second):
		t.Fatal("request cancellation was not propagated to the content service")
	}
	select {
	case <-service.streamReturned:
	case <-time.After(2 * time.Second):
		t.Fatal("content handler did not return after request cancellation")
	}
	select {
	case <-result:
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP client did not finish after request cancellation")
	}
}

var _ gbcontrollers.CloudRecordingCatalogAPI = (*standaloneContentService)(nil)
