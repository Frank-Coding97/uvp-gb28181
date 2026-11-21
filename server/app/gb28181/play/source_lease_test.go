package play

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSourceLeaseRegistryProtectsNewGenerationFromStaleRelease(t *testing.T) {
	registry := NewSourceLeaseRegistry()
	old := registry.Acquire("stream-1", 10, "recording-plan:7")
	require.True(t, registry.HasLease("stream-1"))
	current := registry.Acquire("stream-1", 11, "recording-plan:7")
	require.NoError(t, old.Release())
	require.True(t, registry.HasLease("stream-1"), "old generation must not release current lease")
	require.Equal(t, 1, registry.LeaseCount("stream-1"))
	require.NoError(t, current.Release())
	require.False(t, registry.HasLease("stream-1"))
}

func TestSourceLeaseRegistryKeepsStreamUntilAllConsumersRelease(t *testing.T) {
	registry := NewSourceLeaseRegistry()
	plan := registry.Acquire("stream-1", 3, "recording-plan:7")
	cascade := registry.Acquire("stream-1", 3, "cascade:platform-a")
	require.Equal(t, 2, registry.LeaseCount("stream-1"))
	require.NoError(t, plan.Release())
	require.True(t, registry.HasLease("stream-1"))
	require.NoError(t, cascade.Release())
	require.False(t, registry.HasLease("stream-1"))
	require.NoError(t, cascade.Release(), "release must be idempotent")
}
