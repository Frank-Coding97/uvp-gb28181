package node_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// memoryRepo 内存版 Repo,只用来测 Registry(repo 包另测真 DB 版)
type memoryRepo struct {
	mu     sync.Mutex
	nextID int64
	rows   map[int64]node.Node
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{rows: make(map[int64]node.Node)}
}

func (r *memoryRepo) List(_ context.Context) ([]node.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]node.Node, 0, len(r.rows))
	for _, n := range r.rows {
		out = append(out, n)
	}
	return out, nil
}

func (r *memoryRepo) Get(_ context.Context, id int64) (*node.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n, ok := r.rows[id]; ok {
		nc := n
		return &nc, nil
	}
	return nil, nil
}

func (r *memoryRepo) Create(_ context.Context, n node.Node) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	n.ID = r.nextID
	r.rows[n.ID] = n
	return n.ID, nil
}

func (r *memoryRepo) Update(_ context.Context, n node.Node) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows[n.ID] = n
	return nil
}

func (r *memoryRepo) Delete(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.rows, id)
	return nil
}

func mustAdd(t *testing.T, r *node.Registry, in node.Node) *node.Node {
	t.Helper()
	n, err := r.Add(context.Background(), in)
	require.NoError(t, err)
	require.NotZero(t, n.ID)
	return n
}

func TestRegistry_AddAndList(t *testing.T) {
	r := node.NewRegistry(newMemoryRepo())
	in := node.Node{Name: "zlm-1", Host: "1.1.1.1", APIPort: 18080, MediaServerUUID: "uuid-1", State: node.StateActive}
	added := mustAdd(t, r, in)
	require.Equal(t, "zlm-1", added.Name)

	list := r.List()
	require.Len(t, list, 1)
	require.Equal(t, "zlm-1", list[0].Name)
}

func TestRegistry_LoadAll(t *testing.T) {
	repo := newMemoryRepo()
	_, _ = repo.Create(context.Background(), node.Node{Name: "a", MediaServerUUID: "u-a", State: node.StateActive})
	_, _ = repo.Create(context.Background(), node.Node{Name: "b", MediaServerUUID: "u-b", State: node.StateMaintenance})

	r := node.NewRegistry(repo)
	require.NoError(t, r.LoadAll(context.Background()))
	require.Len(t, r.List(), 2)
}

func TestRegistry_GetByUUID(t *testing.T) {
	r := node.NewRegistry(newMemoryRepo())
	added := mustAdd(t, r, node.Node{Name: "n1", MediaServerUUID: "uuid-x", State: node.StateActive})

	got, ok := r.GetByUUID("uuid-x")
	require.True(t, ok)
	require.Equal(t, added.ID, got.ID)

	_, ok = r.GetByUUID("nope")
	require.False(t, ok)
}

func TestRegistry_ListActive_SkipsOfflineAndMaintenance(t *testing.T) {
	r := node.NewRegistry(newMemoryRepo())
	mustAdd(t, r, node.Node{Name: "a", MediaServerUUID: "ua", State: node.StateActive})
	mustAdd(t, r, node.Node{Name: "b", MediaServerUUID: "ub", State: node.StateMaintenance})
	mustAdd(t, r, node.Node{Name: "c", MediaServerUUID: "uc", State: node.StateOffline})

	active := r.ListActive()
	require.Len(t, active, 1)
	require.Equal(t, "a", active[0].Name)
}

func TestRegistry_MarkOffline(t *testing.T) {
	r := node.NewRegistry(newMemoryRepo())
	added := mustAdd(t, r, node.Node{Name: "a", MediaServerUUID: "ua", State: node.StateActive})
	require.NoError(t, r.MarkOffline(context.Background(), added.ID))
	got, _ := r.Get(added.ID)
	require.Equal(t, node.StateOffline, got.State)
}

func TestRegistry_UpdateStats_Concurrent(t *testing.T) {
	r := node.NewRegistry(newMemoryRepo())
	added := mustAdd(t, r, node.Node{Name: "a", MediaServerUUID: "ua", State: node.StateActive})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r.UpdateStats("ua", node.Stats{
				LastHeartbeatAt:  time.Now(),
				MediaSourceCount: i,
			})
		}(i)
	}
	wg.Wait()

	got, _ := r.Get(added.ID)
	require.False(t, got.Stats.LastHeartbeatAt.IsZero())
}

func TestRegistry_UpdateStats_UnknownUUID_NoOp(t *testing.T) {
	r := node.NewRegistry(newMemoryRepo())
	r.UpdateStats("nonexistent", node.Stats{MediaSourceCount: 5})
}

func TestRegistry_Delete(t *testing.T) {
	r := node.NewRegistry(newMemoryRepo())
	added := mustAdd(t, r, node.Node{Name: "a", MediaServerUUID: "ua", State: node.StateActive})
	require.NoError(t, r.Delete(context.Background(), added.ID))

	_, ok := r.Get(added.ID)
	require.False(t, ok)
	_, ok = r.GetByUUID("ua")
	require.False(t, ok)
}

func TestRegistry_Update(t *testing.T) {
	r := node.NewRegistry(newMemoryRepo())
	added := mustAdd(t, r, node.Node{Name: "old", MediaServerUUID: "ua", State: node.StateActive, Weight: 50})
	added.Name = "new"
	added.Weight = 80
	require.NoError(t, r.Update(context.Background(), *added))

	got, _ := r.Get(added.ID)
	require.Equal(t, "new", got.Name)
	require.Equal(t, 80, got.Weight)
}
