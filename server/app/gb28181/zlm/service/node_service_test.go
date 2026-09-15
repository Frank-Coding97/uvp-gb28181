package service_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/service"
)

// memoryRepo 内存版 Repo,T1.1 测试已写过,本地再来一份避跨包依赖
type memoryRepo struct {
	rows   map[int64]node.Node
	nextID int64
	failOn string // "create" / "update" / "delete" → 强制 err 用于测试
}

func newMemoryRepo() *memoryRepo { return &memoryRepo{rows: map[int64]node.Node{}} }

func (r *memoryRepo) List(_ context.Context) ([]node.Node, error) {
	out := make([]node.Node, 0, len(r.rows))
	for _, n := range r.rows {
		out = append(out, n)
	}
	return out, nil
}
func (r *memoryRepo) Get(_ context.Context, id int64) (*node.Node, error) {
	if n, ok := r.rows[id]; ok {
		copy := n
		return &copy, nil
	}
	return nil, nil
}
func (r *memoryRepo) Create(_ context.Context, n node.Node) (int64, error) {
	if r.failOn == "create" {
		return 0, errors.New("forced create error")
	}
	r.nextID++
	n.ID = r.nextID
	r.rows[n.ID] = n
	return n.ID, nil
}
func (r *memoryRepo) Update(_ context.Context, n node.Node) error {
	if r.failOn == "update" {
		return errors.New("forced update error")
	}
	r.rows[n.ID] = n
	return nil
}
func (r *memoryRepo) Delete(_ context.Context, id int64) error {
	if r.failOn == "delete" {
		return errors.New("forced delete error")
	}
	delete(r.rows, id)
	return nil
}

// mockProbe 替代真 ZLM client probe 行为
type mockProbe struct {
	mu                 sync.Mutex
	getServerConfigErr error
	getServerConfig    map[string]string
	setServerConfigErr error
	calls              []string
	lastSetParams      map[string]string
}

type blockingApplyProbe struct {
	entered chan struct{}
	release chan struct{}
}

type parallelApplyProbe struct {
	mu         sync.Mutex
	want       int
	entered    int
	allEntered chan struct{}
	release    chan struct{}
}

func (p *parallelApplyProbe) GetServerConfig(context.Context, *node.Node) (map[string]string, error) {
	return map[string]string{}, nil
}
func (p *parallelApplyProbe) ApplyConfigForNode(context.Context, *node.Node, service.MediaTuning) error {
	p.mu.Lock()
	p.entered++
	if p.entered == p.want {
		close(p.allEntered)
	}
	p.mu.Unlock()
	<-p.release
	return nil
}
func (p *parallelApplyProbe) KickSessions(context.Context, *node.Node) (int, error) { return 0, nil }
func (p *parallelApplyProbe) RestartServer(context.Context, *node.Node, int) error  { return nil }

func (p *blockingApplyProbe) GetServerConfig(context.Context, *node.Node) (map[string]string, error) {
	return map[string]string{}, nil
}
func (p *blockingApplyProbe) ApplyConfigForNode(context.Context, *node.Node, service.MediaTuning) error {
	close(p.entered)
	<-p.release
	return nil
}
func (p *blockingApplyProbe) KickSessions(context.Context, *node.Node) (int, error) { return 0, nil }
func (p *blockingApplyProbe) RestartServer(context.Context, *node.Node, int) error  { return nil }

func (m *mockProbe) GetServerConfig(_ context.Context, _ *node.Node) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, "GetServerConfig")
	if m.getServerConfigErr != nil {
		return nil, m.getServerConfigErr
	}
	if m.getServerConfig != nil {
		return m.getServerConfig, nil
	}
	return map[string]string{"api.secret": "x", "http.port": "80"}, nil
}
func (m *mockProbe) ApplyConfigForNode(_ context.Context, n *node.Node, _ service.MediaTuning) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, "SetServerConfig")
	m.lastSetParams = map[string]string{"general.mediaServerId": n.MediaServerUUID}
	return m.setServerConfigErr
}

// KickSessions / RestartServer:ZLMProbe 在 T3.5 扩了两方法,基础 mockProbe 给个 no-op 占位
// 让现有用例继续编译;真正校验 Kick/Restart 行为的用例在 node_service_kick_test.go 用 kickProbe。
func (m *mockProbe) KickSessions(_ context.Context, _ *node.Node) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, "KickSessions")
	return 0, nil
}
func (m *mockProbe) RestartServer(_ context.Context, _ *node.Node, _ int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, "RestartServer")
	return nil
}

func newSvc(repo *memoryRepo, probe *mockProbe) *service.NodeService {
	reg := node.NewRegistry(repo)
	return service.NewNodeService(reg, probe, service.MediaTuning{})
}

func TestNodeService_Create_ProbesZLM_ThenWritesUUID(t *testing.T) {
	repo := newMemoryRepo()
	probe := &mockProbe{}
	svc := newSvc(repo, probe)

	n, err := svc.Create(context.Background(), service.CreateNodeReq{
		Name: "n1", Host: "1.2.3.4", ReceiveHost: "203.0.113.10", PlaybackHost: "play.example.com", APIPort: 18080, APISecret: "s",
	})
	require.NoError(t, err)
	require.NotEmpty(t, n.MediaServerUUID)
	require.Equal(t, "203.0.113.10", n.ReceiveHost)
	require.Equal(t, "play.example.com", n.PlaybackHost)
	require.Equal(t, []string{"GetServerConfig", "SetServerConfig", "GetServerConfig"}, probe.calls)
	require.Equal(t, n.MediaServerUUID, probe.lastSetParams["general.mediaServerId"])
	require.True(t, n.AutoOnDemandReady)
}

func TestNodeService_ProbeCreateReadsZLMWithoutPersistingOrReturningSecret(t *testing.T) {
	repo := newMemoryRepo()
	probe := &mockProbe{getServerConfig: map[string]string{
		"api.secret":            "must-not-leak",
		"general.mediaServerId": "existing-zlm-id",
		"http.port":             "18080",
		"rtsp.port":             "10554",
		"rtmp.port":             "11935",
		"rtp_proxy.port":        "10000",
		"protocol.enable_rtsp":  "1",
		"protocol.enable_rtmp":  "1",
		"protocol.enable_hls":   "0",
		"protocol.enable_ts":    "1",
		"protocol.enable_fmp4":  "1",
	}}
	svc := newSvc(repo, probe)

	preview, err := svc.ProbeCreate(context.Background(), service.CreateNodeReq{
		Name: "edge-a", Host: "10.0.0.8", APIPort: 18080, APISecret: "must-not-leak",
	})

	require.NoError(t, err)
	require.True(t, preview.Online)
	require.Equal(t, "existing-zlm-id", preview.MediaServerID)
	require.Equal(t, 18080, preview.ServerConfig.HTTPPort)
	require.Equal(t, 10554, preview.ServerConfig.RTSPPort)
	require.False(t, preview.ServerConfig.HLSEnabled)
	require.Empty(t, repo.rows, "预检不得登记节点")
	require.Equal(t, []string{"GetServerConfig"}, probe.calls, "预检不得下发 ZLM 配置")
}

func TestNodeService_ApplyActiveConfigsConvergesEveryActiveNode(t *testing.T) {
	repo := newMemoryRepo()
	reg := node.NewRegistry(repo)
	activeA, err := reg.Add(context.Background(), node.Node{Name: "a", MediaServerUUID: "uuid-a", State: node.StateActive})
	require.NoError(t, err)
	activeB, err := reg.Add(context.Background(), node.Node{Name: "b", MediaServerUUID: "uuid-b", State: node.StateActive})
	require.NoError(t, err)
	maintenance, err := reg.Add(context.Background(), node.Node{Name: "m", MediaServerUUID: "uuid-m", State: node.StateMaintenance})
	require.NoError(t, err)
	probe := &mockProbe{}
	svc := service.NewNodeService(reg, probe, service.MediaTuning{})

	results := svc.ApplyActiveConfigs(context.Background())
	require.Len(t, results, 2)
	require.True(t, reg.IsAutoOnDemandReady(activeA.ID))
	require.True(t, reg.IsAutoOnDemandReady(activeB.ID))
	require.False(t, reg.IsAutoOnDemandReady(maintenance.ID))
	require.Len(t, probe.calls, 4)
}

func TestNodeService_ApplyActiveConfigsStartsNodesInParallel(t *testing.T) {
	repo := newMemoryRepo()
	reg := node.NewRegistry(repo)
	for i := 0; i < 3; i++ {
		_, err := reg.Add(context.Background(), node.Node{
			Name: "node", MediaServerUUID: fmt.Sprintf("uuid-%d", i), State: node.StateActive,
		})
		require.NoError(t, err)
	}
	probe := &parallelApplyProbe{
		want: 3, allEntered: make(chan struct{}), release: make(chan struct{}),
	}
	svc := service.NewNodeService(reg, probe, service.MediaTuning{})
	done := make(chan []service.ConfigApplyResult, 1)
	go func() { done <- svc.ApplyActiveConfigs(context.Background()) }()

	select {
	case <-probe.allEntered:
		close(probe.release)
	case <-time.After(time.Second):
		close(probe.release)
		<-done
		t.Fatal("active 节点配置仍为串行下发")
	}
	require.Len(t, <-done, 3)
}

func TestNodeService_ConfigConvergenceFailureRemainsRetryable(t *testing.T) {
	repo := newMemoryRepo()
	reg := node.NewRegistry(repo)
	added, err := reg.Add(context.Background(), node.Node{Name: "a", MediaServerUUID: "uuid-a", State: node.StateActive})
	require.NoError(t, err)
	probe := &mockProbe{setServerConfigErr: errors.New("temporary failure")}
	svc := service.NewNodeService(reg, probe, service.MediaTuning{})

	require.Error(t, svc.ConvergeNodeConfig(context.Background(), added.ID))
	require.False(t, reg.IsAutoOnDemandReady(added.ID))
	probe.setServerConfigErr = nil
	require.NoError(t, svc.ConvergeNodeConfig(context.Background(), added.ID))
	require.True(t, reg.IsAutoOnDemandReady(added.ID))
}

func TestNodeService_ConfigChangeDuringApplyCannotBecomeReady(t *testing.T) {
	repo := newMemoryRepo()
	reg := node.NewRegistry(repo)
	added, err := reg.Add(context.Background(), node.Node{
		Name: "a", Host: "127.0.0.1", APIPort: 18080, APISecret: "old-secret",
		MediaServerUUID: "uuid-a", State: node.StateActive,
	})
	require.NoError(t, err)
	probe := &blockingApplyProbe{entered: make(chan struct{}), release: make(chan struct{})}
	svc := service.NewNodeService(reg, probe, service.MediaTuning{})
	result := make(chan error, 1)
	go func() { result <- svc.ConvergeNodeConfig(context.Background(), added.ID) }()
	<-probe.entered

	changed, ok := reg.Get(added.ID)
	require.True(t, ok)
	changed.APISecret = "new-secret"
	require.NoError(t, reg.Update(context.Background(), *changed))
	close(probe.release)

	require.Error(t, <-result)
	require.False(t, reg.IsAutoOnDemandReady(added.ID))
}

func TestNodeService_Create_ZLMUnreachable_RollsBack(t *testing.T) {
	repo := newMemoryRepo()
	probe := &mockProbe{getServerConfigErr: errors.New("connection refused")}
	svc := newSvc(repo, probe)

	_, err := svc.Create(context.Background(), service.CreateNodeReq{
		Name: "n1", Host: "1.2.3.4", APIPort: 18080, APISecret: "s",
	})
	require.Error(t, err)
	require.Empty(t, repo.rows, "节点不应被写入 DB")
}

func TestNodeService_Create_ApplyConfigFails_RollsBack(t *testing.T) {
	repo := newMemoryRepo()
	probe := &mockProbe{setServerConfigErr: errors.New("ZLM rejected")}
	svc := newSvc(repo, probe)

	_, err := svc.Create(context.Background(), service.CreateNodeReq{
		Name: "n1", Host: "1.2.3.4", APIPort: 18080, APISecret: "s",
	})
	require.Error(t, err)
	require.Empty(t, repo.rows, "Apply 失败应回滚 DB 节点")
}

func TestNodeService_Delete_RequiresMaintenance(t *testing.T) {
	repo := newMemoryRepo()
	probe := &mockProbe{}
	svc := newSvc(repo, probe)

	n, _ := svc.Create(context.Background(), service.CreateNodeReq{
		Name: "n1", Host: "1.2.3.4", APIPort: 18080, APISecret: "s",
	})

	err := svc.Delete(context.Background(), n.ID)
	require.ErrorIs(t, err, service.ErrNodeNotInMaintenance)
}

func TestNodeService_Delete_AfterSetMaintenance_OK(t *testing.T) {
	repo := newMemoryRepo()
	probe := &mockProbe{}
	svc := newSvc(repo, probe)

	n, _ := svc.Create(context.Background(), service.CreateNodeReq{
		Name: "n1", Host: "1.2.3.4", APIPort: 18080, APISecret: "s",
	})
	require.NoError(t, svc.SetMaintenance(context.Background(), n.ID))
	require.NoError(t, svc.Delete(context.Background(), n.ID))
	require.Empty(t, repo.rows)
}

func TestNodeService_SetMaintenance_Activate(t *testing.T) {
	repo := newMemoryRepo()
	probe := &mockProbe{}
	svc := newSvc(repo, probe)

	n, _ := svc.Create(context.Background(), service.CreateNodeReq{
		Name: "n1", Host: "1.2.3.4", APIPort: 18080, APISecret: "s",
	})
	// 初始 active
	got, _ := svc.Get(context.Background(), n.ID)
	require.Equal(t, node.StateActive, got.State)

	require.NoError(t, svc.SetMaintenance(context.Background(), n.ID))
	got, _ = svc.Get(context.Background(), n.ID)
	require.Equal(t, node.StateMaintenance, got.State)

	require.NoError(t, svc.Activate(context.Background(), n.ID))
	got, _ = svc.Get(context.Background(), n.ID)
	require.Equal(t, node.StateActive, got.State)
}

func TestNodeService_List_ReturnsAll(t *testing.T) {
	repo := newMemoryRepo()
	probe := &mockProbe{}
	svc := newSvc(repo, probe)

	for i := 0; i < 3; i++ {
		_, err := svc.Create(context.Background(), service.CreateNodeReq{
			Name: "n", Host: "1.2.3.4", APIPort: 18080 + i, APISecret: "s",
		})
		require.NoError(t, err)
	}
	list, err := svc.List(context.Background())
	require.NoError(t, err)
	require.Len(t, list, 3)
}

func TestNodeService_Update_WeightTags(t *testing.T) {
	repo := newMemoryRepo()
	probe := &mockProbe{}
	svc := newSvc(repo, probe)

	n, _ := svc.Create(context.Background(), service.CreateNodeReq{
		Name: "n1", Host: "1.2.3.4", APIPort: 18080, APISecret: "s",
	})

	weight := 80
	updated, err := svc.Update(context.Background(), n.ID, service.UpdateNodeReq{
		Weight: &weight,
		Tags:   map[string]string{"env": "prod"},
	})
	require.NoError(t, err)
	require.Equal(t, 80, updated.Weight)
	require.Equal(t, "prod", updated.Tags["env"])
}

func TestNodeService_Update_MediaHosts(t *testing.T) {
	repo := newMemoryRepo()
	svc := newSvc(repo, &mockProbe{})
	n, _ := svc.Create(context.Background(), service.CreateNodeReq{Name: "n1", Host: "1.2.3.4", APIPort: 18080, APISecret: "s"})
	receiveHost, playbackHost := "203.0.113.10", "play.example.com"
	updated, err := svc.Update(context.Background(), n.ID, service.UpdateNodeReq{ReceiveHost: &receiveHost, PlaybackHost: &playbackHost})
	require.NoError(t, err)
	require.Equal(t, receiveHost, updated.ReceiveHost)
	require.Equal(t, playbackHost, updated.PlaybackHost)
}

func TestNodeService_Get_NotFound(t *testing.T) {
	repo := newMemoryRepo()
	svc := newSvc(repo, &mockProbe{})
	_, err := svc.Get(context.Background(), 9999)
	require.ErrorIs(t, err, service.ErrNodeNotFound)
}
