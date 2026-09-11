package businessreadiness_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/businessreadiness"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

func TestProbeIgnoresRemoteNodesAndDetectsLocalAmbiguity(t *testing.T) {
	registry, added, media, _, client, now, _ := setupProbe(t)
	probe := businessreadiness.NewProbe(businessreadiness.ProbeConfig{Client: client, Now: now})
	input := probeInput(registry, media)

	remote, err := registry.Add(context.Background(), node.Node{
		Host: "192.0.2.20", APIPort: 18080, APISecret: "zlm-secret",
		MediaServerUUID: "remote", State: node.StateActive,
	})
	require.NoError(t, err)
	require.True(t, registry.SetAutoOnDemandReady(remote.ID, true))
	result := probe.Check(context.Background(), input)
	require.Equal(t, businessreadiness.ReasonHookUnconfirmed, result.Reason)

	second, err := registry.Add(context.Background(), node.Node{
		Host: "localhost", APIPort: 18080, APISecret: "zlm-secret",
		MediaServerUUID: "local-second", State: node.StateActive,
	})
	require.NoError(t, err)
	require.True(t, registry.SetAutoOnDemandReady(second.ID, true))
	result = probe.Check(context.Background(), input)
	require.False(t, result.BusinessReady)
	require.Equal(t, businessreadiness.ReasonNodeAmbiguous, result.Reason)

	// Keep the original node reference used by the test explicit so a future
	// change cannot accidentally make the remote row the selected candidate.
	require.Equal(t, int64(1), added.ID)
}

func TestProbeRejectsNonLiteralEndpointAndRecoveryNode(t *testing.T) {
	registry, added, media, _, client, now, _ := setupProbe(t)
	probe := businessreadiness.NewProbe(businessreadiness.ProbeConfig{Client: client, Now: now})
	input := probeInput(registry, media)
	input.ZLM.Host = "zlm.internal.example"
	require.Equal(t, businessreadiness.ReasonNodeMissing, probe.Check(context.Background(), input).Reason)
	input.ZLM.Host = "localhost"

	current, ok := registry.Get(added.ID)
	require.True(t, ok)
	current.RecoveryRequired = true
	require.NoError(t, registry.Update(context.Background(), *current))
	require.Equal(t, businessreadiness.ReasonNodeInactive, probe.Check(context.Background(), input).Reason)
}

func TestResultDoesNotExposeExternalConfiguration(t *testing.T) {
	encoded, err := json.Marshal(businessreadiness.Result{
		BusinessReady: false,
		Reason:        businessreadiness.ReasonConfigNotConverged,
	})
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "secret")
	require.NotContains(t, string(encoded), "cap")
	require.NotContains(t, string(encoded), "hook")
}

func TestProbeRestoresAfterConfigDrift(t *testing.T) {
	registry, added, media, expected, client, now, clock := setupProbe(t)
	probe := businessreadiness.NewProbe(businessreadiness.ProbeConfig{Client: client, Now: now})
	input := probeInput(registry, media)

	require.Equal(t, businessreadiness.ReasonHookUnconfirmed, probe.Check(context.Background(), input).Reason)
	client.mu.Lock()
	client.config["hook.on_play"] = "http://127.0.0.1:8280/index/hook/on_play?node=node-1&cap=wrong"
	client.mu.Unlock()
	require.Equal(t, businessreadiness.ReasonConfigNotConverged, probe.Check(context.Background(), input).Reason)

	client.mu.Lock()
	client.config = make(map[string]string, len(expected))
	for key, value := range expected {
		client.config[key] = value
	}
	client.mu.Unlock()
	registry.UpdateHeartbeatFields(added.MediaServerUUID, 0, 0, now().Add(time.Second))
	*clock = now().Add(2 * time.Second)
	result := probe.Check(context.Background(), input)
	require.False(t, result.BusinessReady, "restored configuration starts a new generation")
	require.Equal(t, businessreadiness.ReasonHookUnconfirmed, result.Reason)

	registry.UpdateHeartbeatFields(added.MediaServerUUID, 0, 0, now().Add(time.Second))
	*clock = now().Add(2 * time.Second)
	result = probe.Check(context.Background(), input)
	require.True(t, result.BusinessReady)
}

func TestProbeConcurrentChecksAreConsistent(t *testing.T) {
	registry, added, media, _, client, now, clock := setupProbe(t)
	probe := businessreadiness.NewProbe(businessreadiness.ProbeConfig{Client: client, Now: now})
	input := probeInput(registry, media)
	require.Equal(t, businessreadiness.ReasonHookUnconfirmed, probe.Check(context.Background(), input).Reason)
	registry.UpdateHeartbeatFields(added.MediaServerUUID, 0, 0, now().Add(time.Second))
	*clock = now().Add(2 * time.Second)

	const workers = 32
	results := make(chan businessreadiness.Result, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- probe.Check(context.Background(), input)
		}()
	}
	wg.Wait()
	close(results)
	for result := range results {
		require.True(t, result.BusinessReady)
		require.Equal(t, businessreadiness.ReasonReady, result.Reason)
	}
}
