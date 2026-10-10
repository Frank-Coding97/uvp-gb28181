package config

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNodeProbeSerializesWholeExchangeAndAllowsCancellation(t *testing.T) {
	db := newNodeRuntimeTestDB(t, "probe-serial")
	insertNodeRuntimeRow(t, db, nodeRuntimeFixture{revision: 7, uuid: "node-a", history: "[]"})
	store := NewNodeRuntimeStore(db, nil)
	ref := NodeRuntimeRef{NodeID: 1, NodeUUID: "node-a", NodeRevision: 7}
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		_, err := NewNodeProbeCoordinator(store).Probe(context.Background(), ref, func(ctx context.Context) (NodeRuntimeObservation, error) {
			close(entered)
			<-release
			return NodeRuntimeObservation{NodeRuntimeRef: ref, BootNonce: testBootA, ProtocolVersion: 1}, nil
		})
		done <- err
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	called := false
	_, err := NewNodeProbeCoordinator(store).Probe(ctx, ref, func(context.Context) (NodeRuntimeObservation, error) {
		called = true
		return NodeRuntimeObservation{}, nil
	})
	close(release)
	require.ErrorIs(t, err, ErrNodeRuntimeUnavailable)
	require.False(t, called, "even separate coordinators must share the per-node gate")
	require.NoError(t, <-done)
}

func TestNodeProbeFailureKeepsHistoryAndUnknown(t *testing.T) {
	db := newNodeRuntimeTestDB(t, "probe-failure")
	insertNodeRuntimeRow(t, db, nodeRuntimeFixture{revision: 7, uuid: "node-a", history: "[]"})
	store := NewNodeRuntimeStore(db, nil)
	ref := NodeRuntimeRef{NodeID: 1, NodeUUID: "node-a", NodeRevision: 7}
	_, err := store.ConfirmProbe(context.Background(), NodeRuntimeObservation{NodeRuntimeRef: ref, BootNonce: testBootA, ProtocolVersion: 1})
	require.NoError(t, err)
	_, err = NewNodeProbeCoordinator(store).Probe(context.Background(), ref, func(ctx context.Context) (NodeRuntimeObservation, error) {
		current, readErr := store.Load(ctx, ref)
		require.NoError(t, readErr)
		require.Equal(t, NodeRuntimeStatusUnknown, current.IdentityStatus)
		return NodeRuntimeObservation{}, errors.New("fixture-secret must not be exposed")
	})
	require.Equal(t, ErrNodeRuntimeUnavailable, err)
	current, err := store.Load(context.Background(), ref)
	require.NoError(t, err)
	require.Equal(t, testBootA, current.CurrentBootNonce)
	require.EqualValues(t, 1, current.RuntimeEpoch)
	require.Equal(t, NodeRuntimeStatusUnknown, current.IdentityStatus)
}

func TestNodeProbeRejectsRevisionChangeAndMismatchedResult(t *testing.T) {
	for _, mode := range []string{"revision", "result", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			db := newNodeRuntimeTestDB(t, "probe-"+mode)
			insertNodeRuntimeRow(t, db, nodeRuntimeFixture{revision: 7, uuid: "node-a", history: "[]"})
			ref := NodeRuntimeRef{NodeID: 1, NodeUUID: "node-a", NodeRevision: 7}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			_, err := NewNodeProbeCoordinator(NewNodeRuntimeStore(db, nil)).Probe(ctx, ref, func(context.Context) (NodeRuntimeObservation, error) {
				got := NodeRuntimeObservation{NodeRuntimeRef: ref, BootNonce: testBootA, ProtocolVersion: 1}
				switch mode {
				case "revision":
					require.NoError(t, db.Exec("UPDATE meta_node SET revision = 8 WHERE id = 1").Error)
				case "result":
					got.NodeUUID = "node-b"
				case "cancelled":
					cancel()
				}
				return got, nil
			})
			require.Error(t, err)
			var row nodeRuntimeTestRow
			require.NoError(t, db.Take(&row).Error)
			require.Empty(t, row.CurrentBootNonce)
			require.Equal(t, NodeRuntimeStatusUnknown, *row.RuntimeIdentityStatus)
		})
	}
}
