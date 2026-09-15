package service_test

import (
	"context"
	"encoding/json"
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
		Streams: 2001, Recordings: 3, Sessions: 4, EvidenceFingerprint: "sha256-test-a",
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
	body, err := json.Marshal(preflight)
	require.NoError(t, err)
	require.NotContains(t, string(body), "sha256-test-a")
}

func TestNodeImpactT13_EvidenceFingerprintMustBeBoundedAndSecretFree(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	n := t13Node(t, reg)
	provider := &t13ImpactProvider{impact: service.NodeImpact{EvidenceFingerprint: "contains space"}}
	svc := service.NewNodeService(reg, &t13Probe{}, service.MediaTuning{})
	svc.SetNodeImpactProvider(provider)

	_, err := svc.PreflightDelete(context.Background(), n.ID)
	require.ErrorIs(t, err, service.ErrNodeImpactInvalid)
	provider.set(service.NodeImpact{EvidenceFingerprint: strings.Repeat("a", service.MaxNodeImpactEvidenceFingerprintLength+1)})
	_, err = svc.PreflightDelete(context.Background(), n.ID)
	require.ErrorIs(t, err, service.ErrNodeImpactInvalid)
}

func TestNodeImpactT13_ChangedFingerprintHasZeroSideEffects(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	n := t13Node(t, reg)
	current, _ := reg.Get(n.ID)
	current.State = node.StateMaintenance
	require.NoError(t, reg.Update(context.Background(), *current))
	provider := &t13ImpactProvider{impact: service.NodeImpact{EvidenceFingerprint: "sha256-test-a"}}
	svc := service.NewNodeService(reg, &t13Probe{}, service.MediaTuning{})
	svc.SetNodeImpactProvider(provider)

	preflight, err := svc.PreflightDelete(context.Background(), n.ID)
	require.NoError(t, err)
	provider.set(service.NodeImpact{Streams: 1, EvidenceFingerprint: "sha256-test-b"})
	err = svc.DeleteConfirmed(context.Background(), n.ID, preflight.Fingerprint)
	require.ErrorIs(t, err, service.ErrNodeImpactChanged)
	got, ok := reg.Get(n.ID)
	require.True(t, ok, "changed impact must not delete the node")
	require.Equal(t, node.StateMaintenance, got.State)
}

func TestNodeImpactT13_EvidenceFingerprintChangesWithSameCounts(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	n := t13Node(t, reg)
	current, _ := reg.Get(n.ID)
	current.State = node.StateMaintenance
	require.NoError(t, reg.Update(context.Background(), *current))
	provider := &t13ImpactProvider{impact: service.NodeImpact{
		Streams: 2, EvidenceFingerprint: "sha256-target-set-a",
	}}
	svc := service.NewNodeService(reg, &t13Probe{}, service.MediaTuning{})
	svc.SetNodeImpactProvider(provider)

	preflight, err := svc.PreflightDelete(context.Background(), n.ID)
	require.NoError(t, err)
	provider.set(service.NodeImpact{Streams: 2, EvidenceFingerprint: "sha256-target-set-b"})

	err = svc.DeleteConfirmed(context.Background(), n.ID, preflight.Fingerprint)
	require.ErrorIs(t, err, service.ErrNodeImpactChanged)
	_, ok := reg.Get(n.ID)
	require.True(t, ok, "same counts with changed target evidence must not delete")
}

func TestNodeImpactT13_EvidenceFingerprintChangesWhenRawCountsAreClamped(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	n := t13Node(t, reg)
	current, _ := reg.Get(n.ID)
	current.State = node.StateMaintenance
	require.NoError(t, reg.Update(context.Background(), *current))
	provider := &t13ImpactProvider{impact: service.NodeImpact{
		Streams: 1500, EvidenceFingerprint: "sha256-target-set-1500",
	}}
	svc := service.NewNodeService(reg, &t13Probe{}, service.MediaTuning{})
	svc.SetNodeImpactProvider(provider)

	preflight, err := svc.PreflightDelete(context.Background(), n.ID)
	require.NoError(t, err)
	require.Equal(t, service.MaxNodeImpactItems, preflight.Impact.Streams)
	provider.set(service.NodeImpact{
		Streams: 1600, EvidenceFingerprint: "sha256-target-set-1600",
	})

	err = svc.DeleteConfirmed(context.Background(), n.ID, preflight.Fingerprint)
	require.ErrorIs(t, err, service.ErrNodeImpactChanged)
	_, ok := reg.Get(n.ID)
	require.True(t, ok, "raw target changes beyond the cap must not be hidden by clamping")
}

func TestNodeImpactT13_MaintenanceAndKickRecheckBeforeMutation(t *testing.T) {
	repo := newT13Repo()
	reg := node.NewRegistry(repo)
	n := t13Node(t, reg)
	provider := &t13ImpactProvider{impact: service.NodeImpact{EvidenceFingerprint: "sha256-test-a"}}
	probe := &t13ImpactKickProbe{}
	svc := service.NewNodeService(reg, probe, service.MediaTuning{})
	svc.SetNodeImpactProvider(provider)

	maintenance, err := svc.PreflightSetMaintenance(context.Background(), n.ID)
	require.NoError(t, err)
	provider.set(service.NodeImpact{Sessions: 1, EvidenceFingerprint: "sha256-test-b"})
	require.ErrorIs(t, svc.SetMaintenanceConfirmed(context.Background(), n.ID, maintenance.Fingerprint), service.ErrNodeImpactChanged)
	got, _ := reg.Get(n.ID)
	require.Equal(t, node.StateActive, got.State)

	provider.set(service.NodeImpact{EvidenceFingerprint: "sha256-test-c"})
	kick, err := svc.PreflightKickSessions(context.Background(), n.ID)
	require.NoError(t, err)
	provider.set(service.NodeImpact{Recordings: 1, EvidenceFingerprint: "sha256-test-d"})
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
	provider := &t13ImpactProvider{impact: service.NodeImpact{Sessions: 1, EvidenceFingerprint: "sha256-test-a"}}
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
