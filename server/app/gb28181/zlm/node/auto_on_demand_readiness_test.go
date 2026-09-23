package node_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

func TestAutoOnDemandReadinessIsTransientAndClearedOffline(t *testing.T) {
	repo := newMemoryRepo()
	registry := node.NewRegistry(repo)
	added := mustAdd(t, registry, node.Node{
		Name: "node-a", MediaServerUUID: "uuid-a", State: node.StateActive,
	})

	require.False(t, registry.IsAutoOnDemandReady(added.ID))
	require.True(t, registry.SetAutoOnDemandReady(added.ID, true))
	require.True(t, registry.IsAutoOnDemandReady(added.ID))

	require.NoError(t, registry.MarkOffline(context.Background(), added.ID))
	require.False(t, registry.IsAutoOnDemandReady(added.ID))
	require.NoError(t, registry.MarkActive(context.Background(), added.ID))
	require.False(t, registry.IsAutoOnDemandReady(added.ID), "恢复 active 后必须重新 Apply 并回读")

	require.NoError(t, registry.Delete(context.Background(), added.ID))
	require.False(t, registry.SetAutoOnDemandReady(added.ID, true))
}

func TestResolveAutoOnDemandNodeRequiresReadyActiveCapacity(t *testing.T) {
	repo := newMemoryRepo()
	registry := node.NewRegistry(repo)
	added := mustAdd(t, registry, node.Node{
		Name: "node-a", Host: "192.0.2.10", APISecret: "secret",
		MediaServerUUID: "uuid-a", State: node.StateActive,
		RTPPortStart: 30000, RTPPortEnd: 30100,
	})

	_, ok := registry.ResolveAutoOnDemandNode("uuid-a")
	require.False(t, ok, "配置未回读前必须拒绝")
	require.True(t, registry.SetAutoOnDemandReady(added.ID, true))
	resolved, ok := registry.ResolveAutoOnDemandNode("uuid-a")
	require.True(t, ok)
	require.Equal(t, added.ID, resolved.ID)
	require.Equal(t, "secret", resolved.APISecret)

	resolved.APISecret = "mutated"
	again, ok := registry.ResolveAutoOnDemandNode("uuid-a")
	require.True(t, ok)
	require.Equal(t, "secret", again.APISecret, "返回值必须是快照")

	require.NoError(t, registry.MarkOffline(context.Background(), added.ID))
	_, ok = registry.ResolveAutoOnDemandNode("uuid-a")
	require.False(t, ok)
}

func TestResolveAutoOnDemandNodeRejectsNearCapacity(t *testing.T) {
	registry := node.NewRegistry(newMemoryRepo())
	added := mustAdd(t, registry, node.Node{
		Name: "node-a", Host: "192.0.2.10", APISecret: "secret",
		MediaServerUUID: "uuid-a", State: node.StateActive,
		RTPPortStart: 30000, RTPPortEnd: 30100,
		Stats: node.Stats{MediaSourceCount: 80},
	})
	require.True(t, registry.SetAutoOnDemandReady(added.ID, true))
	_, ok := registry.ResolveAutoOnDemandNode("uuid-a")
	require.False(t, ok)
}
