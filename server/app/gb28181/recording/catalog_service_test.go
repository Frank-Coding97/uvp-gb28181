package recording

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
