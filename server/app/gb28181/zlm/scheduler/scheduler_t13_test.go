package scheduler_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/scheduler"
)

func TestSchedulerT13_AdmissionBlockedExcludesPreferredAndPool(t *testing.T) {
	repo := newFakeSchedulerNodeRepo()
	reg := node.NewRegistry(repo)
	n, err := reg.Add(context.Background(), node.Node{Name: "n", Host: "zlm", APIPort: 18080, APISecret: "s", State: node.StateActive, Weight: 50})
	require.NoError(t, err)
	require.True(t, reg.SetAdmissionBlocked(n.ID, true))
	mgr := scheduler.NewManager(scheduler.NewFactory(reg))
	require.NoError(t, mgr.Switch("roundrobin"))
	_, err = mgr.Pick(context.Background(), scheduler.InviteContext{PreferredNodeID: n.ID})
	require.ErrorIs(t, err, scheduler.ErrNoActiveNode)
}

type fakeSchedulerNodeRepo struct {
	rows   map[int64]node.Node
	nextID int64
}

func newFakeSchedulerNodeRepo() *fakeSchedulerNodeRepo {
	return &fakeSchedulerNodeRepo{rows: make(map[int64]node.Node)}
}

func (r *fakeSchedulerNodeRepo) List(context.Context) ([]node.Node, error) {
	out := make([]node.Node, 0, len(r.rows))
	for _, n := range r.rows {
		out = append(out, n)
	}
	return out, nil
}

func (r *fakeSchedulerNodeRepo) Get(_ context.Context, id int64) (*node.Node, error) {
	n, ok := r.rows[id]
	if !ok {
		return nil, nil
	}
	return &n, nil
}

func (r *fakeSchedulerNodeRepo) Create(_ context.Context, n node.Node) (int64, error) {
	r.nextID++
	n.ID = r.nextID
	r.rows[n.ID] = n
	return n.ID, nil
}

func (r *fakeSchedulerNodeRepo) Update(_ context.Context, n node.Node) error {
	r.rows[n.ID] = n
	return nil
}

func (r *fakeSchedulerNodeRepo) Delete(_ context.Context, id int64) error {
	delete(r.rows, id)
	return nil
}
