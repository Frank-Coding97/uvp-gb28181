package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/service"
)

type t13Repo struct {
	mu        sync.Mutex
	rows      map[int64]node.Node
	nextID    int64
	updates   int
	failNext  int
	rejectCAS bool
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

func (r *t13Repo) UpdateCAS(_ context.Context, n node.Node, expectedRevision uint64) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.rejectCAS {
		return false, nil
	}
	current, ok := r.rows[n.ID]
	if !ok || current.Revision != expectedRevision {
		return false, nil
	}
	r.updates++
	if r.failNext > 0 {
		r.failNext--
		return false, errors.New("forced update failure")
	}
	n.Revision = expectedRevision + 1
	r.rows[n.ID] = n
	return true, nil
}

func (r *t13Repo) forceExternalHost(id int64, host string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.rows[id]
	current.Host = host
	current.Revision++
	r.rows[id] = current
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

// commitBarrierRepo makes the first update durable before pausing its return.
// A stale MarkOffline must not be allowed to enter its full-row write during
// that window; otherwise the two callers can commit in the wrong order and
// leave DB and Registry disagreeing.
type commitBarrierRepo struct {
	mu             sync.Mutex
	rows           map[int64]node.Node
	nextID         int64
	firstCommitted chan struct{}
	firstRelease   chan struct{}
	secondEntered  chan struct{}
	updateCount    int
}

func newCommitBarrierRepo() *commitBarrierRepo {
	return &commitBarrierRepo{
		rows:           make(map[int64]node.Node),
		firstCommitted: make(chan struct{}),
		firstRelease:   make(chan struct{}),
		secondEntered:  make(chan struct{}),
	}
}

func (r *commitBarrierRepo) List(_ context.Context) ([]node.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]node.Node, 0, len(r.rows))
	for _, n := range r.rows {
		out = append(out, n)
	}
	return out, nil
}

func (r *commitBarrierRepo) Get(_ context.Context, id int64) (*node.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n, ok := r.rows[id]
	if !ok {
		return nil, nil
	}
	return &n, nil
}

func (r *commitBarrierRepo) Create(_ context.Context, n node.Node) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	n.ID = r.nextID
	r.rows[n.ID] = n
	return n.ID, nil
}

func (r *commitBarrierRepo) Update(_ context.Context, n node.Node) error {
	r.mu.Lock()
	r.updateCount++
	count := r.updateCount
	r.rows[n.ID] = n
	r.mu.Unlock()
	if count == 1 {
		close(r.firstCommitted)
		<-r.firstRelease
	} else if count == 2 {
		close(r.secondEntered)
	}
	return nil
}

func (r *commitBarrierRepo) UpdateCAS(_ context.Context, n node.Node, expectedRevision uint64) (bool, error) {
	r.mu.Lock()
	current, ok := r.rows[n.ID]
	if !ok || current.Revision != expectedRevision {
		r.mu.Unlock()
		return false, nil
	}
	r.updateCount++
	count := r.updateCount
	n.Revision = expectedRevision + 1
	r.rows[n.ID] = n
	r.mu.Unlock()
	if count == 1 {
		close(r.firstCommitted)
		<-r.firstRelease
	} else if count == 2 {
		close(r.secondEntered)
	}
	return true, nil
}

func (r *commitBarrierRepo) Delete(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.rows, id)
	return nil
}

type rollbackRaceProbe struct {
	firstEntered chan struct{}
	release      chan struct{}
	mu           sync.Mutex
	calls        int
}

func (p *rollbackRaceProbe) GetServerConfig(context.Context, *node.Node) (map[string]string, error) {
	return map[string]string{}, nil
}

func (p *rollbackRaceProbe) ApplyConfigForNode(context.Context, *node.Node, service.MediaTuning) error {
	p.mu.Lock()
	p.calls++
	call := p.calls
	p.mu.Unlock()
	if call == 1 {
		close(p.firstEntered)
		<-p.release
		return errors.New("candidate apply failed")
	}
	return nil
}

func (p *rollbackRaceProbe) KickSessions(context.Context, *node.Node) (int, error) { return 0, nil }
func (p *rollbackRaceProbe) RestartServer(context.Context, *node.Node, int) error  { return nil }

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
	require.Equal(t, 2, repo.updates, "endpoint candidate and successful recovery clear are separate durable commits")
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

func TestNodeServiceT13_CandidateCommitCannotBeOverwrittenByStaleMarkOffline(t *testing.T) {
	for attempt := 0; attempt < 100; attempt++ {
		repo := newCommitBarrierRepo()
		reg := node.NewRegistry(repo)
		old := t13Node(t, reg)
		weight := old.Weight + 1
		candidate := *old
		candidate.Weight = weight

		updateDone := make(chan error, 1)
		go func() {
			updateDone <- reg.Update(context.Background(), candidate)
		}()
		<-repo.firstCommitted

		markDone := make(chan error, 1)
		go func() { markDone <- reg.MarkOffline(context.Background(), old.ID) }()
		select {
		case <-repo.secondEntered:
			close(repo.firstRelease)
			<-updateDone
			<-markDone
			t.Fatalf("attempt %d: stale MarkOffline entered a full-row write before the candidate update returned", attempt)
		case <-time.After(50 * time.Millisecond):
		}

		close(repo.firstRelease)
		require.NoError(t, <-updateDone)
		require.NoError(t, <-markDone)
		got, ok := reg.Get(old.ID)
		require.True(t, ok)
		require.Equal(t, node.StateOffline, got.State)
		require.Equal(t, weight, got.Weight)
		row, err := repo.Get(context.Background(), old.ID)
		require.NoError(t, err)
		require.Equal(t, node.StateOffline, row.State)
		require.Equal(t, weight, row.Weight)
	}
}

func TestNodeServiceT13_EndpointFailurePersistsRecoveryAcrossReload(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	old := t13Node(t, reg)
	probe := &t13Probe{
		getErrs:   []error{nil, errors.New("candidate readback uncertain")},
		applyErrs: []error{nil},
	}
	svc := service.NewNodeService(reg, probe, service.MediaTuning{})
	host := "new.example"
	secret := "candidate-secret"

	_, err := svc.Update(context.Background(), old.ID, service.UpdateNodeReq{Host: &host, APISecret: &secret})
	require.ErrorIs(t, err, service.ErrRollbackUncertain)

	row, err := repo.Get(context.Background(), old.ID)
	require.NoError(t, err)
	require.Equal(t, old.Host, row.Host, "failed endpoint update must restore the persisted old endpoint")
	require.Equal(t, old.APISecret, row.APISecret)
	require.True(t, row.RecoveryRequired)
	require.NotEmpty(t, row.RecoveryReason)
	require.Len(t, row.RecoveryFingerprint, 64)
	require.NotContains(t, row.RecoveryReason, secret)
	require.NotContains(t, row.RecoveryFingerprint, secret)

	fresh := node.NewRegistry(repo)
	require.NoError(t, fresh.LoadAll(context.Background()))
	require.Empty(t, fresh.ListSchedulable(), "a reloaded node with recoveryRequired must fail closed")
	require.True(t, fresh.IsAdmissionBlocked(old.ID))
}

func TestNodeServiceT13_PendingCandidateRecoverySurvivesReload(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	old := t13Node(t, reg)
	candidate := *old
	candidate.Host = "candidate.example"
	candidate.APISecret = "candidate-secret"
	candidate.RecoveryRequired = true
	candidate.RecoveryReason = "endpoint convergence pending"
	candidate.RecoveryFingerprint = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	// This is the crash boundary: the candidate row is durable, but no
	// external convergence has completed yet.
	require.NoError(t, reg.Update(context.Background(), candidate))

	fresh := node.NewRegistry(repo)
	require.NoError(t, fresh.LoadAll(context.Background()))
	require.Empty(t, fresh.ListSchedulable())
	require.True(t, fresh.IsAdmissionBlocked(old.ID))
	row, err := repo.Get(context.Background(), old.ID)
	require.NoError(t, err)
	require.True(t, row.RecoveryRequired)
}

func TestNodeServiceT13_PendingCandidateIsDurableBeforeConverge(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	old := t13Node(t, reg)
	probe := &t13Probe{applyEntered: make(chan struct{}), applyRelease: make(chan struct{})}
	svc := service.NewNodeService(reg, probe, service.MediaTuning{})
	host := "candidate.example"
	secret := "candidate-secret"
	updateDone := make(chan error, 1)
	go func() {
		_, err := svc.Update(context.Background(), old.ID, service.UpdateNodeReq{Host: &host, APISecret: &secret})
		updateDone <- err
	}()

	// The apply barrier is reached only after the candidate Registry.Update has
	// committed. A crash at this point therefore leaves a durable quarantine
	// marker for the next process to load.
	<-probe.applyEntered
	row, err := repo.Get(context.Background(), old.ID)
	require.NoError(t, err)
	require.Equal(t, host, row.Host)
	require.True(t, row.RecoveryRequired)

	fresh := node.NewRegistry(repo)
	require.NoError(t, fresh.LoadAll(context.Background()))
	require.Empty(t, fresh.ListSchedulable())
	require.True(t, fresh.IsAdmissionBlocked(old.ID))

	close(probe.applyRelease)
	require.NoError(t, <-updateDone)
}

func TestNodeServiceT13_CASConflictDoesNotReturnSuccess(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	old := t13Node(t, reg)
	repo.mu.Lock()
	repo.rejectCAS = true
	repo.mu.Unlock()
	probe := &t13Probe{}
	svc := service.NewNodeService(reg, probe, service.MediaTuning{})
	weight := old.Weight + 1

	updated, err := svc.Update(context.Background(), old.ID, service.UpdateNodeReq{Weight: &weight})
	require.ErrorIs(t, err, node.ErrRevisionConflict)
	require.Nil(t, updated)
	got, ok := reg.Get(old.ID)
	require.True(t, ok)
	require.Equal(t, old.Weight, got.Weight)
	row, err := repo.Get(context.Background(), old.ID)
	require.NoError(t, err)
	require.Equal(t, old.Weight, row.Weight)
	probe.mu.Lock()
	require.Empty(t, probe.applyNodes)
	probe.mu.Unlock()
}

func TestNodeServiceT13_DTORecoveryFieldsNeverExposeSecret(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	old := t13Node(t, reg)
	old.RecoveryRequired = true
	old.RecoveryReason = old.APISecret + " leaked"
	old.RecoveryFingerprint = old.APISecret
	require.NoError(t, reg.Update(context.Background(), *old))

	svc := service.NewNodeService(reg, &t13Probe{}, service.MediaTuning{})
	dto, err := svc.Get(context.Background(), old.ID)
	require.NoError(t, err)
	payload, err := json.Marshal(dto)
	require.NoError(t, err)
	require.NotContains(t, string(payload), old.APISecret)
	require.Contains(t, string(payload), "recoveryRequired")
	require.NotContains(t, string(payload), "recoveryFingerprint")
}

func TestNodeServiceT13_RollbackPreservesConcurrentOfflineState(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	old := t13Node(t, reg)
	probe := &rollbackRaceProbe{firstEntered: make(chan struct{}), release: make(chan struct{})}
	svc := service.NewNodeService(reg, probe, service.MediaTuning{})
	weight := old.Weight + 1

	updateDone := make(chan error, 1)
	go func() {
		_, err := svc.Update(context.Background(), old.ID, service.UpdateNodeReq{Weight: &weight})
		updateDone <- err
	}()
	<-probe.firstEntered

	latestStats := node.Stats{
		MediaSourceCount:  3,
		SessionCount:      5,
		NetThreadLoadAvg:  0.25,
		WorkThreadLoadAvg: 0.5,
		LastHeartbeatAt:   time.Now(),
	}
	reg.UpdateStats(old.MediaServerUUID, latestStats)
	markDone := make(chan error, 1)
	go func() { markDone <- reg.MarkOffline(context.Background(), old.ID) }()
	require.NoError(t, <-markDone)
	close(probe.release)
	require.Error(t, <-updateDone)

	got, ok := reg.Get(old.ID)
	require.True(t, ok)
	require.Equal(t, node.StateOffline, got.State, "rollback must not restore active over a concurrent offline transition")
	require.Equal(t, latestStats, got.Stats, "rollback must keep the latest in-memory stats")
	row, err := repo.Get(context.Background(), old.ID)
	require.NoError(t, err)
	require.Equal(t, node.StateOffline, row.State)
}

func TestNodeServiceT13_CASRollbackQuarantinesNewerExternalConfig(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	old := t13Node(t, reg)
	probe := &rollbackRaceProbe{firstEntered: make(chan struct{}), release: make(chan struct{})}
	svc := service.NewNodeService(reg, probe, service.MediaTuning{})
	weight := old.Weight + 1

	updateDone := make(chan error, 1)
	go func() {
		_, err := svc.Update(context.Background(), old.ID, service.UpdateNodeReq{Weight: &weight})
		updateDone <- err
	}()
	<-probe.firstEntered

	// Simulate another process committing a newer endpoint while the old
	// process is still trying to converge its snapshot.
	repo.forceExternalHost(old.ID, "newer.example")
	close(probe.release)
	require.ErrorIs(t, <-updateDone, service.ErrRollbackUncertain)

	got, ok := reg.Get(old.ID)
	require.True(t, ok)
	require.Equal(t, "newer.example", got.Host)
	require.True(t, got.RecoveryRequired)
	require.True(t, reg.IsAdmissionBlocked(old.ID))
	require.Empty(t, reg.ListSchedulable())
	row, err := repo.Get(context.Background(), old.ID)
	require.NoError(t, err)
	require.Equal(t, "newer.example", row.Host)
	require.True(t, row.RecoveryRequired)
}

func TestNodeServiceT13_SuccessfulReconcileClearsRecoveryIsolation(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	_, err := repo.Create(context.Background(), node.Node{
		Name:                "n1",
		Host:                "old.example",
		APIPort:             18080,
		APISecret:           "old-secret",
		MediaServerUUID:     "uuid-1",
		State:               node.StateActive,
		Weight:              50,
		RTPPortStart:        30000,
		RTPPortEnd:          35000,
		RecoveryRequired:    true,
		RecoveryReason:      "external state uncertain",
		RecoveryFingerprint: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	})
	require.NoError(t, err)
	require.NoError(t, reg.LoadAll(context.Background()))
	require.Empty(t, reg.ListSchedulable())

	svc := service.NewNodeService(reg, &t13Probe{}, service.MediaTuning{})
	require.NoError(t, svc.ConvergeNodeConfig(context.Background(), 1))

	got, ok := reg.Get(1)
	require.True(t, ok)
	require.False(t, got.RecoveryRequired)
	require.Empty(t, got.RecoveryReason)
	require.Empty(t, got.RecoveryFingerprint)
	require.False(t, reg.IsAdmissionBlocked(1))
	require.Len(t, reg.ListSchedulable(), 1)
	row, err := repo.Get(context.Background(), 1)
	require.NoError(t, err)
	require.False(t, row.RecoveryRequired)
	require.Empty(t, row.RecoveryReason)
	require.Empty(t, row.RecoveryFingerprint)
}
