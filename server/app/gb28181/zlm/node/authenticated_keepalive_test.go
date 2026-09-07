package node_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

func TestAuthenticatedKeepaliveRequiresCollectorUpdate(t *testing.T) {
	repo := newMemoryRepo()
	r := node.NewRegistry(repo)
	added := mustAdd(t, r, node.Node{Name: "local", MediaServerUUID: "keepalive", State: node.StateOffline})
	require.NoError(t, r.MarkActive(context.Background(), added.ID))
	got, _ := r.Get(added.ID)
	require.True(t, got.Stats.LastAuthenticatedKeepaliveAt.IsZero())
	now := time.Now()
	r.UpdateStats("keepalive", node.Stats{LastHeartbeatAt: now, LastAuthenticatedKeepaliveAt: now})
	got, _ = r.Get(added.ID)
	require.True(t, got.Stats.LastAuthenticatedKeepaliveAt.IsZero(), "generic stats must not manufacture Hook evidence")
	r.UpdateHeartbeatFields("keepalive", 1, 2, now)
	got, _ = r.Get(added.ID)
	require.Equal(t, now, got.Stats.LastAuthenticatedKeepaliveAt)
	r.UpdateStats("keepalive", node.Stats{LastHeartbeatAt: now.Add(time.Second)})
	require.NoError(t, r.MarkActive(context.Background(), added.ID))
	got, _ = r.Get(added.ID)
	require.Equal(t, now, got.Stats.LastAuthenticatedKeepaliveAt, "API activity must neither replace nor refresh Hook evidence")
	restarted := node.NewRegistry(repo)
	require.NoError(t, restarted.LoadAll(context.Background()))
	got, _ = restarted.Get(added.ID)
	require.True(t, got.Stats.LastAuthenticatedKeepaliveAt.IsZero(), "process restart requires a new authenticated Hook")
}
