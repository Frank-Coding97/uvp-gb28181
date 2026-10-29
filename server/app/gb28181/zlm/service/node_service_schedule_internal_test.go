package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
)

type scheduledConvergenceRepo struct {
	mu   sync.Mutex
	rows map[int64]node.Node
	next int64
}

func newScheduledConvergenceRepo() *scheduledConvergenceRepo {
	return &scheduledConvergenceRepo{rows: make(map[int64]node.Node)}
}

func (r *scheduledConvergenceRepo) List(context.Context) ([]node.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rows := make([]node.Node, 0, len(r.rows))
	for _, row := range r.rows {
		rows = append(rows, row)
	}
	return rows, nil
}

func (r *scheduledConvergenceRepo) Get(_ context.Context, id int64) (*node.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row, ok := r.rows[id]
	if !ok {
		return nil, nil
	}
	return &row, nil
}

func (r *scheduledConvergenceRepo) Create(_ context.Context, row node.Node) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next++
	row.ID = r.next
	r.rows[row.ID] = row
	return row.ID, nil
}

func (r *scheduledConvergenceRepo) Update(_ context.Context, row node.Node) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows[row.ID] = row
	return nil
}

func (r *scheduledConvergenceRepo) Delete(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.rows, id)
	return nil
}

type scheduledConvergenceProbe struct {
	mu      sync.Mutex
	entered chan *node.Node
}

func (p *scheduledConvergenceProbe) GetServerConfig(context.Context, *node.Node) (map[string]string, error) {
	return map[string]string{}, nil
}

func (p *scheduledConvergenceProbe) ApplyConfigForNode(_ context.Context, current *node.Node, _ MediaTuning) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	copy := *current
	p.entered <- &copy
	return nil
}

func (p *scheduledConvergenceProbe) KickSessions(context.Context, *node.Node) (int, error) {
	return 0, nil
}

func (p *scheduledConvergenceProbe) RestartServer(context.Context, *node.Node, int) error {
	return nil
}

func TestScheduledConvergenceUsesLatestSnapshotAtExecution(t *testing.T) {
	reg := node.NewRegistry(newScheduledConvergenceRepo())
	current, err := reg.Add(context.Background(), node.Node{
		Name: "media", Host: "old.example", APIPort: 18080, APISecret: "secret",
		MediaServerUUID: "media-1", State: node.StateActive,
	})
	require.NoError(t, err)
	probe := &scheduledConvergenceProbe{entered: make(chan *node.Node, 1)}
	svc := NewNodeService(reg, probe, MediaTuning{})

	lock := svc.nodeLock(current.ID)
	lock.Lock()
	scheduled := make(chan bool, 1)
	go func() { scheduled <- svc.ScheduleConfigConvergence(current.ID) }()
	select {
	case accepted := <-scheduled:
		require.True(t, accepted)
	case <-time.After(time.Second):
		lock.Unlock()
		t.Fatal("scheduling should not capture a node snapshot while waiting for the keyed lock")
	}

	latest, ok := reg.Get(current.ID)
	require.True(t, ok)
	latest.Host = "new.example"
	require.NoError(t, reg.Update(context.Background(), *latest))
	lock.Unlock()

	select {
	case applied := <-probe.entered:
		require.Equal(t, "new.example", applied.Host, "worker must read the current node after acquiring the keyed lock")
	case <-time.After(time.Second):
		t.Fatal("scheduled convergence did not reach ZLM")
	}
	require.NoError(t, svc.StopContext(context.Background()))
}
