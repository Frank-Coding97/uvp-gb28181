package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/management"
	"uvplatform.cn/uvp-gb28181/app/middleware"
)

type t14StreamService struct {
	closeCalls      int
	forceCloseCalls int
	viewerCalls     int
	viewerMedia     management.MediaIdentity
	viewerPage      management.PageRequest
	closeErr        error
	previewErr      error
}

func (s *t14StreamService) ListStreams(context.Context, management.StreamListRequest) (management.StreamListResult, error) {
	return management.StreamListResult{}, nil
}

func (s *t14StreamService) GetStreamDetail(context.Context, int64, management.MediaIdentity) (*management.StreamDetail, error) {
	return nil, nil
}

func (s *t14StreamService) ListStreamViewers(_ context.Context, _ int64, media management.MediaIdentity, page management.PageRequest) (management.StreamViewerPage, error) {
	s.viewerCalls++
	s.viewerMedia = media
	s.viewerPage = page
	return management.StreamViewerPage{}, nil
}

func (s *t14StreamService) IssuePreviewGrant(context.Context, uint64, management.PreviewGrantRequest) (*management.PreviewGrantResponse, error) {
	return nil, s.previewErr
}

func (s *t14StreamService) PreflightCloseStream(context.Context, management.OwnershipTarget) (management.StreamClosePreview, error) {
	return management.StreamClosePreview{}, nil
}

func (s *t14StreamService) CloseStream(context.Context, management.CloseStreamRequest) (management.StreamCloseResult, error) {
	s.closeCalls++
	if s.closeErr != nil {
		return management.StreamCloseResult{}, s.closeErr
	}
	return management.StreamCloseResult{Closed: true}, nil
}

func (s *t14StreamService) ForceCloseStream(context.Context, management.ForceCloseStreamRequest) (management.StreamCloseResult, error) {
	s.forceCloseCalls++
	return management.StreamCloseResult{Closed: true, Force: true}, nil
}

func (s *t14StreamService) PreflightCloseStreams(context.Context, []management.OwnershipTarget) (management.OwnershipBatchPreflight, error) {
	return management.OwnershipBatchPreflight{}, nil
}

func (s *t14StreamService) CloseStreams(context.Context, management.OwnershipBatchPreflight) (management.StreamCloseBatchResult, error) {
	return management.StreamCloseBatchResult{}, nil
}

type t14SnapshotService struct {
	called bool
}

func (s *t14SnapshotService) FetchSnapshot(context.Context, int64, management.MediaIdentity) (management.SnapshotResult, error) {
	s.called = true
	return management.SnapshotResult{
		Body:        []byte{0xff, 0xd8, 0xff, 0xd9},
		ContentType: "image/jpeg",
		NodeUUID:    "node-1",
		Media:       management.MediaIdentity{Schema: "rtsp", Vhost: "__defaultVhost__", App: "live", Stream: "cam-1"},
	}, nil
}

func TestZLMManagementControllerMalformedRequestDoesNotCallService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &t14StreamService{}
	router := gin.New()
	router.POST("/close", NewZLMManagementController(&ZLMManagementBundle{Streams: service}).CloseStream)

	req := httptest.NewRequest(http.MethodPost, "/close", strings.NewReader(`{"target":`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, 0, service.closeCalls)
	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, string(management.CodeInvalidRequest), body["code"])
}

func TestZLMManagementControllerForceBodyNeverSelectsForceService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &t14StreamService{closeErr: management.NewOwnershipConflictError("1", "ownership confirmation required")}
	router := gin.New()
	router.POST("/nodes/:id/streams/close", NewZLMManagementController(&ZLMManagementBundle{Streams: service}).CloseStream)
	router.POST("/nodes/:id/streams/force-close", NewZLMManagementController(&ZLMManagementBundle{Streams: service}).ForceCloseStream)

	body := `{"target":{"media":{"schema":"rtsp","vhost":"__defaultVhost__","app":"live","stream":"cam-1"}},"fingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","force":true}`
	req := httptest.NewRequest(http.MethodPost, "/nodes/1/streams/close?force=true", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Equal(t, 1, service.closeCalls)
	require.Equal(t, 0, service.forceCloseCalls)
}

func TestT22ZLMManagementControllerRejectsCrossNodeTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &t14StreamService{}
	router := gin.New()
	router.POST("/nodes/:id/streams/close", NewZLMManagementController(&ZLMManagementBundle{Streams: service}).CloseStream)

	body := `{"target":{"nodeId":2,"media":{"schema":"rtsp","vhost":"__defaultVhost__","app":"live","stream":"cam-1"}},"fingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`
	req := httptest.NewRequest(http.MethodPost, "/nodes/1/streams/close", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "target.nodeId")
	require.Zero(t, service.closeCalls)
	require.Zero(t, service.forceCloseCalls)
}

func TestZLMManagementControllerMediaAndPageQueriesAcceptTogether(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &t14StreamService{}
	router := gin.New()
	router.GET("/nodes/:id/streams/viewers", NewZLMManagementController(&ZLMManagementBundle{Streams: service}).StreamViewers)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/nodes/7/streams/viewers?schema=rtsp&vhost=__defaultVhost__&app=live&stream=cam-1&page=2&pageSize=5", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, service.viewerCalls)
	require.Equal(t, management.MediaIdentity{Schema: "rtsp", Vhost: "__defaultVhost__", App: "live", Stream: "cam-1"}, service.viewerMedia)
	require.Equal(t, management.PageRequest{Page: 2, PageSize: 5}, service.viewerPage)
}

func TestZLMManagementControllerOversizedJSONDoesNotCallService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &t14StreamService{}
	router := gin.New()
	router.POST("/nodes/:id/streams/close", NewZLMManagementController(&ZLMManagementBundle{Streams: service}).CloseStream)

	body := `{"target":{"media":{"schema":"rtsp","vhost":"__defaultVhost__","app":"live","stream":"cam-1"}}}` + strings.Repeat(" ", (1<<20)+1)
	request := httptest.NewRequest(http.MethodPost, "/nodes/1/streams/close", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, 0, service.closeCalls)
}

func TestZLMManagementControllerNilBundleReturnsTyped503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/overview", NewZLMManagementController(nil).Overview)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/overview", nil))

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, string(management.CodeServiceUnavailable), body["code"])
	require.NotContains(t, recorder.Body.String(), "panic")
}

func TestZLMManagementControllerSnapshotMarksSensitiveBeforeWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	snapshot := &t14SnapshotService{}
	router := gin.New()
	markedBeforeHeader := false
	markedBeforeWrite := false
	handler := NewZLMManagementController(&ZLMManagementBundle{Snapshot: snapshot}).Snapshot
	router.GET("/nodes/:id/streams/snapshot", func(c *gin.Context) {
		c.Writer = &t14SnapshotWriter{
			ResponseWriter: c.Writer,
			Context:        c,
			BeforeHeader:   &markedBeforeHeader,
			BeforeWrite:    &markedBeforeWrite,
		}
		handler(c)
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/nodes/1/streams/snapshot?schema=rtsp&vhost=__defaultVhost__&app=live&stream=cam-1", nil))

	require.True(t, snapshot.called)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, []byte{0xff, 0xd8, 0xff, 0xd9}, recorder.Body.Bytes())
	require.True(t, markedBeforeHeader)
	require.True(t, markedBeforeWrite)
}

type t14SnapshotWriter struct {
	gin.ResponseWriter
	Context      *gin.Context
	BeforeHeader *bool
	BeforeWrite  *bool
}

func (writer *t14SnapshotWriter) WriteHeader(statusCode int) {
	_, marked := middleware.SensitiveOperationMetadata(writer.Context)
	*writer.BeforeHeader = marked
	writer.ResponseWriter.WriteHeader(statusCode)
}

func (writer *t14SnapshotWriter) Write(body []byte) (int, error) {
	_, marked := middleware.SensitiveOperationMetadata(writer.Context)
	*writer.BeforeWrite = marked
	return writer.ResponseWriter.Write(body)
}

func TestZLMManagementControllerAuditTargetRedactsURLLikeMedia(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	markManagementAudit(context, "stream.close", 7, &management.OwnershipTarget{
		NodeID: 7,
		Media: management.MediaIdentity{
			Schema: "rtsp",
			Vhost:  "vhost",
			App:    "rtsp://user:pass@example.invalid/live?token=abc",
			Stream: "stream?access_token=super-secret",
		},
	}, "sha256:fingerprint", "operator reason", "requested")

	metadata, ok := middleware.SensitiveOperationMetadata(context)
	require.True(t, ok)
	encoded, err := json.Marshal(metadata)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "user:pass")
	require.NotContains(t, string(encoded), "token=abc")
	require.NotContains(t, string(encoded), "super-secret")
	require.Contains(t, string(encoded), `"nodeId":7`)
}

func TestZLMManagementControllerErrorDoesNotEchoSensitiveCause(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &t14StreamService{closeErr: errors.New(`upstream secret=super-secret url=https://user:pass@example.invalid/live?token=abc`)}
	router := gin.New()
	router.POST("/nodes/:id/streams/close", NewZLMManagementController(&ZLMManagementBundle{Streams: service}).CloseStream)
	body := `{"target":{"media":{"schema":"rtsp","vhost":"__defaultVhost__","app":"live","stream":"cam-1"}},"fingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`
	req := httptest.NewRequest(http.MethodPost, "/nodes/1/streams/close", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.NotEqual(t, http.StatusOK, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "super-secret")
	require.NotContains(t, recorder.Body.String(), "https://user:pass")
}

func TestZLMManagementControllerKeepsGBPreviewOnPlayAuthorizationBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &t14StreamService{previewErr: errors.Join(management.ErrPreviewUnsupported, management.ErrGBPreviewRequiresPlayAuth)}
	router := gin.New()
	router.POST("/nodes/:id/streams/playback-grant", NewZLMManagementController(&ZLMManagementBundle{Streams: service}).PreviewGrant)
	body := `{"media":{"schema":"rtp","vhost":"__defaultVhost__","app":"rtp","stream":"34020000001320000001_34020000001320000002"},"protocol":"https-flv"}`
	req := httptest.NewRequest(http.MethodPost, "/nodes/1/streams/playback-grant", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
	require.Contains(t, recorder.Body.String(), "gb28181 play authorization")
}
