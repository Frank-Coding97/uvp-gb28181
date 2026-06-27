package service_test

import (
	"context"
	"errors"
	"testing"

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
	getServerConfigErr error
	setServerConfigErr error
	calls              []string
	lastSetParams      map[string]string
}

func (m *mockProbe) GetServerConfig(_ context.Context, _ *node.Node) (map[string]string, error) {
	m.calls = append(m.calls, "GetServerConfig")
	if m.getServerConfigErr != nil {
		return nil, m.getServerConfigErr
	}
	return map[string]string{"api.secret": "x", "http.port": "80"}, nil
}
func (m *mockProbe) ApplyConfigForNode(_ context.Context, n *node.Node, _ service.MediaTuning) error {
	m.calls = append(m.calls, "SetServerConfig")
	m.lastSetParams = map[string]string{"general.mediaServerId": n.MediaServerUUID}
	return m.setServerConfigErr
}

// KickSessions / RestartServer:ZLMProbe 在 T3.5 扩了两方法,基础 mockProbe 给个 no-op 占位
// 让现有用例继续编译;真正校验 Kick/Restart 行为的用例在 node_service_kick_test.go 用 kickProbe。
func (m *mockProbe) KickSessions(_ context.Context, _ *node.Node) (int, error) {
	m.calls = append(m.calls, "KickSessions")
	return 0, nil
}
func (m *mockProbe) RestartServer(_ context.Context, _ *node.Node, _ int) error {
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
		Name: "n1", Host: "1.2.3.4", APIPort: 18080, APISecret: "s",
	})
	require.NoError(t, err)
	require.NotEmpty(t, n.MediaServerUUID)
	require.Equal(t, []string{"GetServerConfig", "SetServerConfig"}, probe.calls)
	require.Equal(t, n.MediaServerUUID, probe.lastSetParams["general.mediaServerId"])
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

func TestNodeService_Get_NotFound(t *testing.T) {
	repo := newMemoryRepo()
	svc := newSvc(repo, &mockProbe{})
	_, err := svc.Get(context.Background(), 9999)
	require.ErrorIs(t, err, service.ErrNodeNotFound)
}
