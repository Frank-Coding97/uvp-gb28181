package service_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/service"
)

type t13ImpactProvider struct {
	mu     sync.Mutex
	impact service.NodeImpact
	err    error
	calls  int
}

func (p *t13ImpactProvider) ReadNodeImpact(context.Context, *node.Node) (service.NodeImpact, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return p.impact, p.err
}

func (p *t13ImpactProvider) set(impact service.NodeImpact) {
	p.mu.Lock()
	p.impact = impact
	p.mu.Unlock()
}

type t13ImpactKickProbe struct {
	t13Probe
	kickCalls int
}

func (p *t13ImpactKickProbe) KickSessions(context.Context, *node.Node) (int, error) {
	p.kickCalls++
	return 3, nil
}

func TestNodeImpactT13_PreflightIsBoundedAndSecretFree(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	n := t13Node(t, reg)
	provider := &t13ImpactProvider{impact: service.NodeImpact{
		Streams: 2001, Recordings: 3, Sessions: 4,
	}}
	svc := service.NewNodeService(reg, &t13Probe{}, service.MediaTuning{})
	svc.SetNodeImpactProvider(provider)

	preflight, err := svc.PreflightDelete(context.Background(), n.ID)
	require.NoError(t, err)
	require.Equal(t, service.NodeImpactActionDelete, preflight.Action)
	require.Equal(t, service.MaxNodeImpactItems, preflight.Impact.Streams)
	require.Equal(t, 3, preflight.Impact.Recordings)
	require.Equal(t, 4, preflight.Impact.Sessions)
	require.True(t, preflight.Impact.Truncated)
	require.Len(t, preflight.Fingerprint, 64)
	require.NotContains(t, preflight.Fingerprint, n.APISecret)
}

func TestNodeImpactT13_ChangedFingerprintHasZeroSideEffects(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	n := t13Node(t, reg)
	current, _ := reg.Get(n.ID)
	current.State = node.StateMaintenance
	require.NoError(t, reg.Update(context.Background(), *current))
	provider := &t13ImpactProvider{}
	svc := service.NewNodeService(reg, &t13Probe{}, service.MediaTuning{})
	svc.SetNodeImpactProvider(provider)

	preflight, err := svc.PreflightDelete(context.Background(), n.ID)
	require.NoError(t, err)
	provider.set(service.NodeImpact{Streams: 1})
	err = svc.DeleteConfirmed(context.Background(), n.ID, preflight.Fingerprint)
	require.ErrorIs(t, err, service.ErrNodeImpactChanged)
	got, ok := reg.Get(n.ID)
	require.True(t, ok, "changed impact must not delete the node")
	require.Equal(t, node.StateMaintenance, got.State)
}

func TestNodeImpactT13_MaintenanceAndKickRecheckBeforeMutation(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	n := t13Node(t, reg)
	provider := &t13ImpactProvider{}
	probe := &t13ImpactKickProbe{}
	svc := service.NewNodeService(reg, probe, service.MediaTuning{})
	svc.SetNodeImpactProvider(provider)

	maintenance, err := svc.PreflightSetMaintenance(context.Background(), n.ID)
	require.NoError(t, err)
	provider.set(service.NodeImpact{Sessions: 1})
	require.ErrorIs(t, svc.SetMaintenanceConfirmed(context.Background(), n.ID, maintenance.Fingerprint), service.ErrNodeImpactChanged)
	got, _ := reg.Get(n.ID)
	require.Equal(t, node.StateActive, got.State)

	provider.set(service.NodeImpact{})
	kick, err := svc.PreflightKickSessions(context.Background(), n.ID)
	require.NoError(t, err)
	provider.set(service.NodeImpact{Recordings: 1})
	_, err = svc.KickAllSessionsConfirmed(context.Background(), n.ID, kick.Fingerprint)
	require.ErrorIs(t, err, service.ErrNodeImpactChanged)
	require.Equal(t, 0, probe.kickCalls)
}

func TestNodeImpactT13_DeleteRejectsLiveImpactAndProviderErrorsStayStable(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	n := t13Node(t, reg)
	current, _ := reg.Get(n.ID)
	current.State = node.StateMaintenance
	require.NoError(t, reg.Update(context.Background(), *current))
	provider := &t13ImpactProvider{impact: service.NodeImpact{Sessions: 1}}
	svc := service.NewNodeService(reg, &t13Probe{}, service.MediaTuning{})
	svc.SetNodeImpactProvider(provider)

	preflight, err := svc.PreflightDelete(context.Background(), n.ID)
	require.NoError(t, err)
	require.ErrorIs(t, svc.DeleteConfirmed(context.Background(), n.ID, preflight.Fingerprint), service.ErrNodeImpactConflict)
	_, ok := reg.Get(n.ID)
	require.True(t, ok)

	provider.err = errors.New("upstream secret=" + n.APISecret)
	_, err = svc.PreflightDelete(context.Background(), n.ID)
	require.ErrorIs(t, err, service.ErrNodeImpactUnavailable)
	require.True(t, strings.Contains(err.Error(), "node impact unavailable"))
	require.NotContains(t, err.Error(), n.APISecret)
}
