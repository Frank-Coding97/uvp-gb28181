package recording

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type catalogTestNodes struct{ nodes map[int64]*node.Node }

type catalogTestScheduler struct{ accepted []int64 }

type catalogDeleteClient struct {
	deleted []string
	err     error
}

type catalogRecordingStopper struct {
	stoppedChannels []uint
	stoppedSessions []uint64
	err             error
}

func (s *catalogRecordingStopper) StopSession(_ context.Context, channelID uint, sessionID uint64) (*models.GbChannel, error) {
	s.stoppedChannels = append(s.stoppedChannels, channelID)
	s.stoppedSessions = append(s.stoppedSessions, sessionID)
	return &models.GbChannel{ID: channelID, CloudRecordingEnabled: false, CloudRecordingState: models.CloudRecordingStateDisabled}, s.err
}

func (c *catalogDeleteClient) DeleteMP4RecordFile(_ context.Context, vhost, app, stream, period, name string) error {
	c.deleted = append(c.deleted, strings.Join([]string{vhost, app, stream, period, name}, "|"))
	return c.err
}

func (s *catalogTestScheduler) Enqueue(_ string, nodeIDs []int64, _, _ *time.Time) ([]int64, error) {
	if len(nodeIDs) == 0 {
		return append([]int64(nil), s.accepted...), nil
	}
	return append([]int64(nil), nodeIDs...), nil
}

func (n catalogTestNodes) Get(id int64) (*node.Node, bool) {
	item, ok := n.nodes[id]
	if !ok {
		return nil, false
	}
	copy := *item
	return &copy, true
}

func (n catalogTestNodes) List() []*node.Node {
	result := make([]*node.Node, 0, len(n.nodes))
	for _, item := range n.nodes {
		copy := *item
		result = append(result, &copy)
	}
	return result
}

func TestCatalogServiceScopesQueriesAndIssuesAccess(t *testing.T) {
	db := newCatalogServiceDB(t)
	now := time.Now().UTC()
	visible := catalogFile(101, 10, 1, now, "visible.mp4")
	hidden := catalogFile(102, 20, 1, now, "hidden.mp4")
	require.NoError(t, db.Create(&visible).Error)
	require.NoError(t, db.Create(&hidden).Error)

	signer, err := NewCapabilitySigner([]byte(strings.Repeat("c", 32)), "recording-v1")
	require.NoError(t, err)
	service := NewCatalogService(CatalogServiceConfig{
		Repo: NewGormRepo(db),
		Nodes: catalogTestNodes{nodes: map[int64]*node.Node{1: {
			ID: 1, Name: "zlm-a", Host: "127.0.0.1", APIPort: 18080, APISecret: "configured", State: node.StateActive,
		}}},
		Signer: signer,
		ResolveAccess: func(context.Context, uint) (CatalogAccess, error) {
			return CatalogAccess{DeptIDs: []uint{10}}, nil
		},
	})

	page, err := service.ListFiles(context.Background(), 7, FileQuery{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Total)
	require.Equal(t, "101", page.List[0].ID)
	require.Equal(t, AvailabilityAvailable, page.List[0].Availability)
	require.Equal(t, "zlm-a", page.List[0].Node.Name)

	_, err = service.FileDetail(context.Background(), 7, 102)
	require.ErrorIs(t, err, ErrRecordingFileNotFound)
	detail, err := service.FileDetail(context.Background(), 7, 101)
	require.NoError(t, err)
	require.Equal(t, "101", detail.ID)

	grant, err := service.IssueAccess(context.Background(), 7, 101, CapabilityModePlay)
	require.NoError(t, err)
	claims, err := signer.Verify(grant.Capability, "101", CapabilityModePlay)
	require.NoError(t, err)
	require.Equal(t, uint(7), claims.UserID)
}

func TestCatalogServiceDeletesPhysicalFileBeforeCatalogIndex(t *testing.T) {
	db := newCatalogServiceDB(t)
	now := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	file := catalogFile(111, 10, 1, now, "090000-091000.mp4")
	file.RecordDate = ptrTime(now)
	require.NoError(t, db.Create(&file).Error)
	client := &catalogDeleteClient{}
	service := NewCatalogService(CatalogServiceConfig{
		Repo:          NewGormRepo(db),
		Nodes:         catalogTestNodes{nodes: map[int64]*node.Node{1: {ID: 1, State: node.StateActive}}},
		ResolveAccess: func(context.Context, uint) (CatalogAccess, error) { return CatalogAccess{DeptIDs: []uint{10}}, nil },
		NewFileClient: func(*node.Node) CatalogFileClient { return client },
	})

	result, err := service.DeleteFile(context.Background(), 7, 111)
	require.NoError(t, err)
	require.Equal(t, DeleteFileResult{ID: "111", Deleted: true}, result)
	require.Equal(t, []string{"__defaultVhost__|rtp|stream|2026-08-10|090000-091000.mp4"}, client.deleted)
	_, err = NewGormRepo(db).GetCatalogFile(context.Background(), 111, nil, true)
	require.ErrorIs(t, err, ErrRecordingFileNotFound)
}

func TestCatalogServiceKeepsIndexWhenPhysicalDeleteFails(t *testing.T) {
	db := newCatalogServiceDB(t)
	now := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	file := catalogFile(112, 10, 1, now, "record.mp4")
	file.RecordDate = ptrTime(now)
	require.NoError(t, db.Create(&file).Error)
	client := &catalogDeleteClient{err: zlm.ErrRecordingAccessUnavailable}
	service := NewCatalogService(CatalogServiceConfig{
		Repo:          NewGormRepo(db),
		Nodes:         catalogTestNodes{nodes: map[int64]*node.Node{1: {ID: 1, State: node.StateActive}}},
		ResolveAccess: func(context.Context, uint) (CatalogAccess, error) { return CatalogAccess{DeptIDs: []uint{10}}, nil },
		NewFileClient: func(*node.Node) CatalogFileClient { return client },
	})

	_, err := service.DeleteFile(context.Background(), 7, 112)
	require.ErrorIs(t, err, zlm.ErrRecordingAccessUnavailable)
	_, err = NewGormRepo(db).GetCatalogFile(context.Background(), 112, nil, true)
	require.NoError(t, err)
}

func TestCatalogServiceBatchDeleteReturnsPartialResults(t *testing.T) {
	db := newCatalogServiceDB(t)
	now := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	file := catalogFile(113, 10, 1, now, "record.mp4")
	file.RecordDate = ptrTime(now)
	require.NoError(t, db.Create(&file).Error)
	service := NewCatalogService(CatalogServiceConfig{
		Repo:          NewGormRepo(db),
		Nodes:         catalogTestNodes{nodes: map[int64]*node.Node{1: {ID: 1, State: node.StateActive}}},
		ResolveAccess: func(context.Context, uint) (CatalogAccess, error) { return CatalogAccess{DeptIDs: []uint{10}}, nil },
		NewFileClient: func(*node.Node) CatalogFileClient { return &catalogDeleteClient{} },
	})

	result := service.DeleteFiles(context.Background(), 7, []uint64{113, 999})
	require.Equal(t, 1, result.DeletedCount)
	require.Equal(t, 1, result.FailedCount)
	require.Equal(t, "not_found", result.Results[1].ErrorCode)
}

func TestCatalogServiceContentRechecksPermissionAndDataScope(t *testing.T) {
	db := newCatalogServiceDB(t)
	now := time.Now().UTC()
	file := catalogFile(201, 10, 1, now, "camera.mp4")
	require.NoError(t, db.Create(&file).Error)

	signer, err := NewCapabilitySigner([]byte(strings.Repeat("d", 32)), "recording-v1")
	require.NoError(t, err)
	allowed := true
	downloaderCalls := 0
	service := NewCatalogService(CatalogServiceConfig{
		Repo: NewGormRepo(db),
		Nodes: catalogTestNodes{nodes: map[int64]*node.Node{1: {
			ID: 1, Host: "127.0.0.1", APIPort: 18080, APISecret: "configured", State: node.StateActive,
		}}},
		Signer: signer,
		ResolveAccess: func(context.Context, uint) (CatalogAccess, error) {
			if !allowed {
				return CatalogAccess{}, ErrCatalogAccessRevoked
			}
			return CatalogAccess{DeptIDs: []uint{10}}, nil
		},
		CheckPermission: func(context.Context, uint, string, string) (bool, error) { return allowed, nil },
		NewDownloader: func(*node.Node) ContentDownloader {
			return contentDownloaderFunc(func(context.Context, string, string) (*zlm.DownloadResponse, error) {
				downloaderCalls++
				return &zlm.DownloadResponse{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"video/mp4"}}, Body: io.NopCloser(strings.NewReader("content"))}, nil
			})
		},
	})
	grant, err := service.IssueAccess(context.Background(), 7, 201, CapabilityModePlay)
	require.NoError(t, err)

	allowed = false
	recorder := httptest.NewRecorder()
	err = service.StreamContent(context.Background(), recorder, "201", grant.Capability, "")
	require.ErrorIs(t, err, ErrCatalogAccessRevoked)
	require.Zero(t, downloaderCalls)

	allowed = true
	recorder = httptest.NewRecorder()
	require.NoError(t, service.StreamContent(context.Background(), recorder, "201", grant.Capability, ""))
	require.Equal(t, "content", recorder.Body.String())
	require.Equal(t, 1, downloaderCalls)
}

func TestCatalogServiceDownloadRechecksPermissionBeforeUpstream(t *testing.T) {
	db := newCatalogServiceDB(t)
	now := time.Now().UTC()
	file := catalogFile(202, 10, 1, now, "camera.mp4")
	require.NoError(t, db.Create(&file).Error)
	signer, err := NewCapabilitySigner([]byte(strings.Repeat("f", 32)), "recording-v1")
	require.NoError(t, err)
	allowed := true
	upstreamCalls := 0
	service := NewCatalogService(CatalogServiceConfig{
		Repo: NewGormRepo(db), Downloads: NewDownloadRegistry(DownloadRegistryConfig{}), Signer: signer,
		Nodes: catalogTestNodes{nodes: map[int64]*node.Node{1: {ID: 1, Host: "127.0.0.1", APIPort: 18080, APISecret: "configured", State: node.StateActive}}},
		ResolveAccess: func(context.Context, uint) (CatalogAccess, error) {
			if !allowed {
				return CatalogAccess{}, ErrCatalogAccessRevoked
			}
			return CatalogAccess{DeptIDs: []uint{10}}, nil
		},
		CheckPermission: func(context.Context, uint, string, string) (bool, error) { return allowed, nil },
		NewDownloader: func(*node.Node) ContentDownloader {
			return contentDownloaderFunc(func(context.Context, string, string) (*zlm.DownloadResponse, error) {
				upstreamCalls++
				return &zlm.DownloadResponse{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("content"))}, nil
			})
		},
	})
	task, ticket, err := service.CreateDownload(context.Background(), 7, 202)
	require.NoError(t, err)
	allowed = false
	err = service.ClaimDownload(context.Background(), httptest.NewRecorder(), task.TaskID, ticket, "", nil)
	require.ErrorIs(t, err, ErrCatalogAccessRevoked)
	require.Zero(t, upstreamCalls)
}

func TestCatalogServiceDownloadTimeoutFailsTaskAndReleasesSlot(t *testing.T) {
	db := newCatalogServiceDB(t)
	now := time.Now().UTC()
	file := catalogFile(204, 10, 1, now, "camera.mp4")
	require.NoError(t, db.Create(&file).Error)
	registry := NewDownloadRegistry(DownloadRegistryConfig{PerUserStreaming: 1, PerInstanceStreaming: 1})
	body := newBlockingReadCloser()
	signer, err := NewCapabilitySigner([]byte(strings.Repeat("h", 32)), "recording-v1")
	require.NoError(t, err)
	service := NewCatalogService(CatalogServiceConfig{
		Repo: NewGormRepo(db), Downloads: registry, Signer: signer,
		Nodes:           catalogTestNodes{nodes: map[int64]*node.Node{1: {ID: 1, Host: "127.0.0.1", APIPort: 18080, APISecret: "configured", State: node.StateActive}}},
		ResolveAccess:   func(context.Context, uint) (CatalogAccess, error) { return CatalogAccess{DeptIDs: []uint{10}}, nil },
		CheckPermission: func(context.Context, uint, string, string) (bool, error) { return true, nil },
		NewDownloader: func(*node.Node) ContentDownloader {
			return contentDownloaderFunc(func(context.Context, string, string) (*zlm.DownloadResponse, error) {
				return &zlm.DownloadResponse{StatusCode: http.StatusOK, Header: http.Header{}, Body: body}, nil
			})
		},
		Proxy: &ContentProxy{writeDeadline: time.Second, inactivityTimeout: 20 * time.Millisecond},
	})
	task, ticket, err := service.CreateDownload(context.Background(), 7, file.ID)
	require.NoError(t, err)

	err = service.ClaimDownload(context.Background(), deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}, task.TaskID, ticket, "", nil)
	require.ErrorIs(t, err, ErrContentTimeout)
	view, err := service.DownloadStatus(context.Background(), 7, task.TaskID)
	require.NoError(t, err)
	require.Equal(t, DownloadStatusFailed, view.Status)
	require.Equal(t, "timeout", view.ErrorCode)
	require.Equal(t, 0, registry.streamingTotal)
}

func TestCatalogServiceStalledDownloadAfterFirstChunkReturnsShortRead(t *testing.T) {
	db := newCatalogServiceDB(t)
	now := time.Now().UTC()
	file := catalogFile(205, 10, 1, now, "camera.mp4")
	fileSize := uint64(1024)
	file.FileSize = &fileSize
	require.NoError(t, db.Create(&file).Error)
	registry := NewDownloadRegistry(DownloadRegistryConfig{PerUserStreaming: 1, PerInstanceStreaming: 1})
	body := newFirstChunkThenBlockReadCloser([]byte("partial-mp4"))
	downloaderContextCancelled := make(chan struct{})
	signer, err := NewCapabilitySigner([]byte(strings.Repeat("i", 32)), "recording-v1")
	require.NoError(t, err)
	service := NewCatalogService(CatalogServiceConfig{
		Repo: NewGormRepo(db), Downloads: registry, Signer: signer,
		Nodes:           catalogTestNodes{nodes: map[int64]*node.Node{1: {ID: 1, Host: "127.0.0.1", APIPort: 18080, APISecret: "configured", State: node.StateActive}}},
		ResolveAccess:   func(context.Context, uint) (CatalogAccess, error) { return CatalogAccess{DeptIDs: []uint{10}}, nil },
		CheckPermission: func(context.Context, uint, string, string) (bool, error) { return true, nil },
		NewDownloader: func(*node.Node) ContentDownloader {
			return contentDownloaderFunc(func(ctx context.Context, _ string, _ string) (*zlm.DownloadResponse, error) {
				go func() {
					<-ctx.Done()
					close(downloaderContextCancelled)
				}()
				return &zlm.DownloadResponse{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Length": []string{"1024"}},
					Body:       body,
				}, nil
			})
		},
		Proxy: &ContentProxy{writeDeadline: time.Second, inactivityTimeout: 100 * time.Millisecond},
	})
	task, ticket, err := service.CreateDownload(context.Background(), 7, file.ID)
	require.NoError(t, err)

	claimResult := make(chan error, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		claimResult <- service.ClaimDownload(request.Context(), writer, task.TaskID, ticket, "", nil)
	}))
	server.Config.WriteTimeout = time.Second
	server.Start()
	defer server.Close()

	response, err := server.Client().Get(server.URL)
	require.NoError(t, err)
	downloaded, readErr := io.ReadAll(response.Body)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, []byte("partial-mp4"), downloaded)
	require.ErrorIs(t, readErr, io.ErrUnexpectedEOF)

	select {
	case claimErr := <-claimResult:
		require.ErrorIs(t, claimErr, ErrContentTimeout)
	case <-time.After(2 * time.Second):
		t.Fatal("stalled download handler did not return")
	}
	select {
	case <-downloaderContextCancelled:
	case <-time.After(time.Second):
		t.Fatal("stalled download did not cancel the downloader context")
	}
	view, err := service.DownloadStatus(context.Background(), 7, task.TaskID)
	require.NoError(t, err)
	require.Equal(t, DownloadStatusFailed, view.Status)
	require.Equal(t, "timeout", view.ErrorCode)
	require.Equal(t, 0, registry.streamingTotal)
}

func TestCatalogServiceIssueAccessRejectsDownloadAndContentRequiresPlay(t *testing.T) {
	db := newCatalogServiceDB(t)
	now := time.Now().UTC()
	file := catalogFile(203, 10, 1, now, "camera.mp4")
	require.NoError(t, db.Create(&file).Error)
	signer, err := NewCapabilitySigner([]byte(strings.Repeat("g", 32)), "recording-v1")
	require.NoError(t, err)
	service := NewCatalogService(CatalogServiceConfig{
		Repo: NewGormRepo(db), Signer: signer,
		Nodes:           catalogTestNodes{nodes: map[int64]*node.Node{1: {ID: 1, Host: "127.0.0.1", APIPort: 18080, APISecret: "configured", State: node.StateActive}}},
		ResolveAccess:   func(context.Context, uint) (CatalogAccess, error) { return CatalogAccess{DeptIDs: []uint{10}}, nil },
		CheckPermission: func(context.Context, uint, string, string) (bool, error) { return true, nil },
		NewDownloader: func(*node.Node) ContentDownloader {
			return contentDownloaderFunc(func(context.Context, string, string) (*zlm.DownloadResponse, error) {
				return nil, errors.New("must not call")
			})
		},
	})
	_, err = service.IssueAccess(context.Background(), 7, 203, CapabilityModeDownload)
	require.ErrorIs(t, err, ErrCapabilityInvalid)
	legacy, err := signer.Issue("203", 7, CapabilityModeDownload, nil)
	require.NoError(t, err)
	err = service.StreamContent(context.Background(), httptest.NewRecorder(), "203", legacy.Token, "")
	require.ErrorIs(t, err, ErrCapabilityInvalid)
}

func TestCatalogServiceAvailabilityErrorsAreStable(t *testing.T) {
	db := newCatalogServiceDB(t)
	now := time.Now().UTC()
	file := catalogFile(301, 10, 1, now, "camera.mp4")
	require.NoError(t, db.Create(&file).Error)
	signer, err := NewCapabilitySigner([]byte(strings.Repeat("e", 32)), "recording-v1")
	require.NoError(t, err)

	for _, tc := range []struct {
		name  string
		nodes catalogTestNodes
		want  error
	}{
		{name: "missing node", nodes: catalogTestNodes{nodes: map[int64]*node.Node{}}, want: ErrCatalogNodeMissing},
		{name: "offline node", nodes: catalogTestNodes{nodes: map[int64]*node.Node{1: {ID: 1, State: node.StateOffline}}}, want: ErrCatalogNodeOffline},
		{name: "missing access config", nodes: catalogTestNodes{nodes: map[int64]*node.Node{1: {ID: 1, State: node.StateActive}}}, want: ErrCatalogAccessUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := NewCatalogService(CatalogServiceConfig{
				Repo: NewGormRepo(db), Nodes: tc.nodes, Signer: signer,
				ResolveAccess: func(context.Context, uint) (CatalogAccess, error) { return CatalogAccess{FullAccess: true}, nil },
			})
			_, issueErr := service.IssueAccess(context.Background(), 7, 301, CapabilityModePlay)
			require.ErrorIs(t, issueErr, tc.want)
		})
	}
}

func TestCatalogServiceActiveAndReconciliationViews(t *testing.T) {
	db := newCatalogServiceDB(t)
	now := time.Now().UTC()
	channel := models.GbChannel{ID: 1, ChannelID: "C1", DeviceID: "D1", Name: "大厅", OwnerDeptID: 10}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&models.GbRecordingSession{
		ID: 1, ChannelID: channel.ID, DeviceID: channel.DeviceID, NodeID: 11,
		VHost: "v", App: "a", Stream: "private", State: models.RecordingSessionStateRecording, StartedAt: &now,
	}).Error)
	require.NoError(t, db.Create(&models.GbRecordingReconcileState{
		NodeID: 11, Status: models.RecordingReconcilePartial, LastError: "private upstream path", UpdatedAt: now,
	}).Error)
	scheduler := &catalogTestScheduler{accepted: []int64{11}}
	service := NewCatalogService(CatalogServiceConfig{
		Repo: NewGormRepo(db), Scheduler: scheduler,
		Nodes:         catalogTestNodes{nodes: map[int64]*node.Node{11: {ID: 11, Name: "zlm-a", State: node.StateActive}}},
		ResolveAccess: func(context.Context, uint) (CatalogAccess, error) { return CatalogAccess{DeptIDs: []uint{10}}, nil },
	})

	active, err := service.ActiveSessions(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, active, 1)
	require.Equal(t, "1", active[0].ID)
	require.Equal(t, "zlm-a", active[0].Node.Name)

	states, err := service.Reconciliations(context.Background())
	require.NoError(t, err)
	require.Len(t, states, 1)
	payload := requireJSON(t, states)
	require.NotContains(t, payload, "private upstream path")

	accepted, err := service.TriggerReconciliation([]int64{11}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, []int64{11}, accepted)
}

func TestCatalogServiceStopsOnlyVisibleActiveRecording(t *testing.T) {
	db := newCatalogServiceDB(t)
	now := time.Now().UTC()
	visible := models.GbChannel{ID: 1, ChannelID: "C1", DeviceID: "D1", Name: "大厅", OwnerDeptID: 10}
	hidden := models.GbChannel{ID: 2, ChannelID: "C2", DeviceID: "D2", Name: "机房", OwnerDeptID: 20}
	require.NoError(t, db.Create(&visible).Error)
	require.NoError(t, db.Create(&hidden).Error)
	require.NoError(t, db.Create(&models.GbRecordingSession{ID: 11, ChannelID: visible.ID, DeviceID: visible.DeviceID, NodeID: 1, Stream: "visible", State: models.RecordingSessionStateRecording, StartedAt: &now}).Error)
	require.NoError(t, db.Create(&models.GbRecordingSession{ID: 12, ChannelID: hidden.ID, DeviceID: hidden.DeviceID, NodeID: 1, Stream: "hidden", State: models.RecordingSessionStateRecording, StartedAt: &now}).Error)
	stopper := &catalogRecordingStopper{}
	service := NewCatalogService(CatalogServiceConfig{
		Repo: NewGormRepo(db), Nodes: catalogTestNodes{nodes: map[int64]*node.Node{}}, RecordingStopper: stopper,
		ResolveAccess: func(context.Context, uint) (CatalogAccess, error) { return CatalogAccess{DeptIDs: []uint{10}}, nil },
	})

	result, err := service.StopActiveSession(context.Background(), 7, 11)
	require.NoError(t, err)
	require.Equal(t, StopActiveSessionResult{ID: "11", ChannelID: "1", Stopped: true}, result)
	require.Equal(t, []uint{1}, stopper.stoppedChannels)
	require.Equal(t, []uint64{11}, stopper.stoppedSessions)

	_, err = service.StopActiveSession(context.Background(), 7, 12)
	require.ErrorIs(t, err, ErrRecordingSessionNotFound)
	require.Equal(t, []uint{1}, stopper.stoppedChannels)
	require.Equal(t, []uint64{11}, stopper.stoppedSessions)
}

func requireJSON(t *testing.T, value any) string {
	t.Helper()
	payload, err := json.Marshal(value)
	require.NoError(t, err)
	return string(payload)
}

func newCatalogServiceDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.GbRecordingFile{}, &models.GbRecordingSession{}, &models.GbRecordingReconcileState{}, &models.GbChannel{}))
	return db
}

type firstChunkThenBlockReadCloser struct {
	chunk     []byte
	sent      bool
	closed    chan struct{}
	closeOnce sync.Once
}

func newFirstChunkThenBlockReadCloser(chunk []byte) *firstChunkThenBlockReadCloser {
	return &firstChunkThenBlockReadCloser{chunk: append([]byte(nil), chunk...), closed: make(chan struct{})}
}

func (r *firstChunkThenBlockReadCloser) Read(buffer []byte) (int, error) {
	if !r.sent {
		r.sent = true
		return copy(buffer, r.chunk), nil
	}
	<-r.closed
	return 0, errors.New("body closed")
}

func (r *firstChunkThenBlockReadCloser) Close() error {
	r.closeOnce.Do(func() { close(r.closed) })
	return nil
}
