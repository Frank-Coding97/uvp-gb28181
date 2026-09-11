package businessreadiness_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/businessreadiness"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type memoryRepo struct {
	mu     sync.Mutex
	nextID int64
	rows   map[int64]node.Node
}

func newMemoryRepo() *memoryRepo { return &memoryRepo{rows: make(map[int64]node.Node)} }

func (r *memoryRepo) List(context.Context) ([]node.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rows := make([]node.Node, 0, len(r.rows))
	for _, row := range r.rows {
		rows = append(rows, row)
	}
	return rows, nil
}

func (r *memoryRepo) Get(_ context.Context, id int64) (*node.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row, ok := r.rows[id]
	if !ok {
		return nil, node.ErrNotFound
	}
	return &row, nil
}

func (r *memoryRepo) Create(_ context.Context, row node.Node) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	row.ID = r.nextID
	r.rows[row.ID] = row
	return row.ID, nil
}

func (r *memoryRepo) Update(_ context.Context, row node.Node) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.rows[row.ID]; !ok {
		return node.ErrNotFound
	}
	r.rows[row.ID] = row
	return nil
}

func (r *memoryRepo) Delete(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.rows, id)
	return nil
}

type configClient struct {
	mu       sync.Mutex
	config   map[string]string
	err      error
	onGet    func(*node.Node)
	getCount int
}

func (c *configClient) GetServerConfig(_ context.Context, n *node.Node) (map[string]string, error) {
	c.mu.Lock()
	c.getCount++
	onGet := c.onGet
	err := c.err
	config := make(map[string]string, len(c.config))
	for key, value := range c.config {
		config[key] = value
	}
	c.mu.Unlock()
	if onGet != nil {
		onGet(n)
	}
	if err != nil {
		return nil, err
	}
	return config, nil
}

func (c *configClient) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.getCount
}

func setupProbe(t *testing.T) (*node.Registry, *node.Node, gbconfig.MediaConfig, map[string]string, *configClient, func() time.Time, *time.Time) {
	t.Helper()
	media := gbconfig.MediaConfig{HookHost: "127.0.0.1", HookPort: 8280}
	repo := newMemoryRepo()
	registry := node.NewRegistry(repo)
	added, err := registry.Add(context.Background(), node.Node{
		Host: "127.0.0.1", APIPort: 18080, APISecret: "zlm-secret",
		MediaServerUUID: "node-1", State: node.StateActive,
	})
	require.NoError(t, err)
	require.True(t, registry.SetAutoOnDemandReady(added.ID, true))
	expected, err := zlm.ExpectedConfigForNode(added, media)
	require.NoError(t, err)
	clock := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	clientConfig := make(map[string]string, len(expected))
	for key, value := range expected {
		clientConfig[key] = value
	}
	client := &configClient{config: clientConfig}
	return registry, added, media, expected, client, func() time.Time { return clock }, &clock
}

func probeInput(registry *node.Registry, media gbconfig.MediaConfig) businessreadiness.Input {
	return businessreadiness.Input{
		InstallationPhase: "complete",
		SIPState:          "running",
		ZLM:               gbconfig.ZLMConfig{Host: "localhost", HTTPPort: 18080, Secret: "zlm-secret"},
		Media:             media,
		Registry:          registry,
	}
}

func TestProbeRequiresAuthenticatedHeartbeatAfterReadback(t *testing.T) {
	registry, added, media, _, client, now, clock := setupProbe(t)
	registry.UpdateHeartbeatFields(added.MediaServerUUID, 0, 0, now().Add(-time.Second))
	probe := businessreadiness.NewProbe(businessreadiness.ProbeConfig{Client: client, Now: now})

	result := probe.Check(context.Background(), probeInput(registry, media))
	require.False(t, result.BusinessReady)
	require.Equal(t, businessreadiness.ReasonHookUnconfirmed, result.Reason)

	registry.UpdateHeartbeatFields(added.MediaServerUUID, 0, 0, now().Add(time.Second))
	*clock = now().Add(2 * time.Second)
	result = probe.Check(context.Background(), probeInput(registry, media))
	require.True(t, result.BusinessReady)
	require.Equal(t, businessreadiness.ReasonReady, result.Reason)
	require.Equal(t, 2, client.count())
}

func TestProbeDoesNotResetGenerationOnOrdinaryReadback(t *testing.T) {
	registry, added, media, _, client, now, clock := setupProbe(t)
	probe := businessreadiness.NewProbe(businessreadiness.ProbeConfig{Client: client, Now: now})

	result := probe.Check(context.Background(), probeInput(registry, media))
	require.Equal(t, businessreadiness.ReasonHookUnconfirmed, result.Reason)

	observed := now().Add(time.Second)
	registry.UpdateHeartbeatFields(added.MediaServerUUID, 0, 0, observed)
	*clock = now().Add(2 * time.Second)
	result = probe.Check(context.Background(), probeInput(registry, media))
	require.True(t, result.BusinessReady)
	require.Equal(t, businessreadiness.ReasonReady, result.Reason)
}

func TestProbeRejectsStaleHeartbeatAndConfigDrift(t *testing.T) {
	registry, added, media, expected, client, now, _ := setupProbe(t)
	probe := businessreadiness.NewProbe(businessreadiness.ProbeConfig{Client: client, Now: now})

	result := probe.Check(context.Background(), probeInput(registry, media))
	require.Equal(t, businessreadiness.ReasonHookUnconfirmed, result.Reason)
	registry.UpdateHeartbeatFields(added.MediaServerUUID, 0, 0, now().Add(-91*time.Second))
	result = probe.Check(context.Background(), probeInput(registry, media))
	require.Equal(t, businessreadiness.ReasonHookUnconfirmed, result.Reason)

	client.mu.Lock()
	client.config = make(map[string]string, len(expected))
	for key, value := range expected {
		client.config[key] = value
	}
	client.config["hook.on_play"] = "http://127.0.0.1:8280/index/hook/on_play?node=node-1&cap=wrong"
	client.mu.Unlock()
	result = probe.Check(context.Background(), probeInput(registry, media))
	require.Equal(t, businessreadiness.ReasonConfigNotConverged, result.Reason)
}

func TestProbeMapsCandidateAndRuntimeFailuresToFiniteReasons(t *testing.T) {
	registry, added, media, _, client, now, _ := setupProbe(t)
	probe := businessreadiness.NewProbe(businessreadiness.ProbeConfig{Client: client, Now: now})
	input := probeInput(registry, media)

	input.InstallationPhase = "pending_sip"
	require.Equal(t, businessreadiness.ReasonInstallationPending, probe.Check(context.Background(), input).Reason)
	input.InstallationPhase = "complete"
	input.SIPState = "starting"
	require.Equal(t, businessreadiness.ReasonSIPNotRunning, probe.Check(context.Background(), input).Reason)
	input.SIPState = "running"

	input.Registry = nil
	require.Equal(t, businessreadiness.ReasonNodeMissing, probe.Check(context.Background(), input).Reason)
	input.Registry = registry
	input.ZLM.Secret = "wrong-secret"
	require.Equal(t, businessreadiness.ReasonIdentityMismatch, probe.Check(context.Background(), input).Reason)
	input.ZLM.Secret = "zlm-secret"
	client.err = context.DeadlineExceeded
	require.Equal(t, businessreadiness.ReasonMediaUnreachable, probe.Check(context.Background(), input).Reason)
	client.err = nil
	require.True(t, registry.SetAutoOnDemandReady(added.ID, false))
	require.Equal(t, businessreadiness.ReasonConfigNotConverged, probe.Check(context.Background(), input).Reason)
}

func TestProbeDoesNotPublishStaleSnapshotAfterRevisionChanges(t *testing.T) {
	registry, _, media, _, client, now, _ := setupProbe(t)
	client.onGet = func(n *node.Node) {
		_ = registry.MarkOffline(context.Background(), n.ID)
	}
	probe := businessreadiness.NewProbe(businessreadiness.ProbeConfig{Client: client, Now: now})

	result := probe.Check(context.Background(), probeInput(registry, media))
	require.False(t, result.BusinessReady)
	require.Equal(t, businessreadiness.ReasonNodeInactive, result.Reason)
}
