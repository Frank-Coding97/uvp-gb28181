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
