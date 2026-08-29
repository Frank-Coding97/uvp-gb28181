package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/service"
)

type t13Repo struct {
	mu       sync.Mutex
	rows     map[int64]node.Node
	nextID   int64
	updates  int
	failNext int
}

func newT13Repo() *t13Repo { return &t13Repo{rows: make(map[int64]node.Node)} }

func (r *t13Repo) List(_ context.Context) ([]node.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]node.Node, 0, len(r.rows))
	for _, n := range r.rows {
		out = append(out, n)
	}
	return out, nil
}

func (r *t13Repo) Get(_ context.Context, id int64) (*node.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n, ok := r.rows[id]
	if !ok {
		return nil, nil
	}
	return &n, nil
}

func (r *t13Repo) Create(_ context.Context, n node.Node) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	n.ID = r.nextID
	r.rows[n.ID] = n
	return n.ID, nil
}

func (r *t13Repo) Update(_ context.Context, n node.Node) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updates++
	if r.failNext > 0 {
		r.failNext--
		return errors.New("forced update failure")
	}
	r.rows[n.ID] = n
	return nil
}

func (r *t13Repo) Delete(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.rows, id)
	return nil
}

type t13Probe struct {
	mu           sync.Mutex
	getErrs      []error
	applyErrs    []error
	gets         []*node.Node
	applyNodes   []*node.Node
	restartErr   error
	applyEntered chan struct{}
	applyRelease chan struct{}
	restartCalls int
}

func (p *t13Probe) GetServerConfig(_ context.Context, n *node.Node) (map[string]string, error) {
	p.mu.Lock()
	p.gets = append(p.gets, cloneT13Node(n))
	var err error
	if len(p.getErrs) > 0 {
		err, p.getErrs = p.getErrs[0], p.getErrs[1:]
	}
	p.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return map[string]string{"http.port": "80"}, nil
}

func (p *t13Probe) ApplyConfigForNode(_ context.Context, n *node.Node, _ service.MediaTuning) error {
	p.mu.Lock()
	p.applyNodes = append(p.applyNodes, cloneT13Node(n))
	var err error
	if len(p.applyErrs) > 0 {
		err, p.applyErrs = p.applyErrs[0], p.applyErrs[1:]
	}
	entered, release := p.applyEntered, p.applyRelease
	p.mu.Unlock()
	if entered != nil {
		close(entered)
		<-release
	}
	return err
}

func (p *t13Probe) KickSessions(context.Context, *node.Node) (int, error) { return 0, nil }

func (p *t13Probe) RestartServer(context.Context, *node.Node, int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.restartCalls++
	return p.restartErr
}

func cloneT13Node(n *node.Node) *node.Node {
	if n == nil {
		return nil
	}
	c := *n
	if n.Tags != nil {
		c.Tags = make(map[string]string, len(n.Tags))
		for k, v := range n.Tags {
			c.Tags[k] = v
		}
	}
	return &c
}

func t13Node(t *testing.T, reg *node.Registry) *node.Node {
	t.Helper()
	n, err := reg.Add(context.Background(), node.Node{
		Name: "n1", Host: "old.example", APIPort: 18080, APISecret: "old-secret",
		MediaServerUUID: "uuid-1", State: node.StateActive, Weight: 50,
		RTPPortStart: 30000, RTPPortEnd: 35000, Tags: map[string]string{"env": "test"},
	})
	require.NoError(t, err)
	return n
}

func TestNodeServiceT13_UpdateCandidateProbeFailureDoesNotPersist(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	old := t13Node(t, reg)
	probe := &t13Probe{getErrs: []error{errors.New("bad new-secret")}}
	svc := service.NewNodeService(reg, probe, service.MediaTuning{})
	host := "new.example"
	secret := "new-secret"
	_, err := svc.Update(context.Background(), old.ID, service.UpdateNodeReq{Host: &host, APISecret: &secret})
	require.Error(t, err)
	require.NotContains(t, err.Error(), secret)
	got, ok := reg.Get(old.ID)
	require.True(t, ok)
	require.Equal(t, old.Host, got.Host)
	require.Equal(t, old.APISecret, got.APISecret)
	repo.mu.Lock()
	require.Equal(t, 0, repo.updates)
	repo.mu.Unlock()
	probe.mu.Lock()
	require.Empty(t, probe.applyNodes)
	probe.mu.Unlock()
}

func TestNodeServiceT13_UpdateActiveRequiresConvergedReadback(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	old := t13Node(t, reg)
	probe := &t13Probe{}
	svc := service.NewNodeService(reg, probe, service.MediaTuning{})
	host := "new.example"
	port := 19080
	updated, err := svc.Update(context.Background(), old.ID, service.UpdateNodeReq{Host: &host, APIPort: &port})
	require.NoError(t, err)
	require.Equal(t, host, updated.Host)
	require.Equal(t, port, updated.APIPort)
	require.True(t, updated.AutoOnDemandReady)
	repo.mu.Lock()
	require.Equal(t, 1, repo.updates)
	repo.mu.Unlock()
	probe.mu.Lock()
	require.Len(t, probe.gets, 2, "candidate probe and post-apply readback")
	require.Equal(t, host, probe.gets[0].Host)
	require.Len(t, probe.applyNodes, 1)
	probe.mu.Unlock()
}

func TestNodeServiceT13_ConvergeFailureRollsBackOldSnapshot(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	old := t13Node(t, reg)
	probe := &t13Probe{applyErrs: []error{errors.New("set failed"), nil}}
	svc := service.NewNodeService(reg, probe, service.MediaTuning{})
	weight := old.Weight + 1
	_, err := svc.Update(context.Background(), old.ID, service.UpdateNodeReq{Weight: &weight})
	require.Error(t, err)
	require.NotErrorIs(t, err, service.ErrRollbackUncertain)
	got, ok := reg.Get(old.ID)
	require.True(t, ok)
	require.Equal(t, old.Host, got.Host)
	require.Equal(t, old.APISecret, got.APISecret)
	require.False(t, reg.IsAutoOnDemandReady(old.ID))
	require.False(t, reg.IsAdmissionBlocked(old.ID))
}

func TestNodeServiceT13_RollbackFailureClosesAdmission(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	old := t13Node(t, reg)
	probe := &t13Probe{applyErrs: []error{errors.New("set failed"), errors.New("rollback failed")}}
	svc := service.NewNodeService(reg, probe, service.MediaTuning{})
	host := "new.example"
	_, err := svc.Update(context.Background(), old.ID, service.UpdateNodeReq{Host: &host})
	require.ErrorIs(t, err, service.ErrRollbackUncertain)
	require.False(t, reg.IsAutoOnDemandReady(old.ID))
	require.True(t, reg.IsAdmissionBlocked(old.ID))
}

func TestNodeServiceT13_ConnectionChangeFailureIsRollbackUncertain(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	old := t13Node(t, reg)
	probe := &t13Probe{applyErrs: []error{errors.New("candidate set may have applied")}}
	svc := service.NewNodeService(reg, probe, service.MediaTuning{})
	host := "new.example"
	_, err := svc.Update(context.Background(), old.ID, service.UpdateNodeReq{Host: &host})

	require.ErrorIs(t, err, service.ErrRollbackUncertain)
	got, ok := reg.Get(old.ID)
	require.True(t, ok)
	require.Equal(t, old.Host, got.Host, "local snapshot may be restored, but must be gated")
	require.False(t, reg.IsAutoOnDemandReady(old.ID))
	require.True(t, reg.IsAdmissionBlocked(old.ID))
	probe.mu.Lock()
	require.Len(t, probe.applyNodes, 1, "connection changes must not claim old-endpoint external rollback")
	require.Equal(t, host, probe.applyNodes[0].Host)
	probe.mu.Unlock()
}

func TestNodeServiceT13_ConnectionChangeReadbackFailureIsUncertain(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	old := t13Node(t, reg)
	probe := &t13Probe{
		getErrs:   []error{nil, errors.New("candidate readback uncertain")},
		applyErrs: []error{nil},
	}
	svc := service.NewNodeService(reg, probe, service.MediaTuning{})
	host := "new.example"
	_, err := svc.Update(context.Background(), old.ID, service.UpdateNodeReq{Host: &host})

	require.ErrorIs(t, err, service.ErrRollbackUncertain)
	require.True(t, reg.IsAdmissionBlocked(old.ID))
	probe.mu.Lock()
	require.Len(t, probe.applyNodes, 1, "uncertain candidate endpoint must not receive an old-endpoint rollback")
	require.Equal(t, host, probe.applyNodes[0].Host)
	probe.mu.Unlock()
}

func TestNodeServiceT13_NodeLockSerializesUpdateAndState(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	old := t13Node(t, reg)
	entered := make(chan struct{})
	release := make(chan struct{})
	probe := &t13Probe{applyEntered: entered, applyRelease: release}
	svc := service.NewNodeService(reg, probe, service.MediaTuning{})
	host := "new.example"
	updateDone := make(chan error, 1)
	go func() {
		_, err := svc.Update(context.Background(), old.ID, service.UpdateNodeReq{Host: &host})
		updateDone <- err
	}()
	<-entered
	stateDone := make(chan error, 1)
	go func() { stateDone <- svc.SetMaintenance(context.Background(), old.ID) }()
	select {
	case <-stateDone:
		t.Fatal("state update bypassed per-node update lock")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	require.NoError(t, <-updateDone)
	require.NoError(t, <-stateDone)
	got, ok := reg.Get(old.ID)
	require.True(t, ok)
	require.Equal(t, node.StateMaintenance, got.State)
}
