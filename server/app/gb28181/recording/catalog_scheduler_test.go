package recording

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

func TestCatalogSchedulerStartIsAsyncAndZeroIntervalKeepsManualQueue(t *testing.T) {
	db := newRepoTestDB(t)
	repo := NewGormRepo(db)
	seedCatalogCandidate(t, db, repo, 1, "stream-scheduler")
	client := &blockingCatalogRecordClient{started: make(chan struct{}), release: make(chan struct{})}
	reconciler := NewCatalogReconciler(repo, fakeLocationLookup{"stream-scheduler": 1}, fakeCatalogNodes{items: map[int64]*node.Node{1: {ID: 1, State: node.StateActive}}}, func(*node.Node) CatalogRecordClient { return client })
	scheduler := NewCatalogReconcileScheduler(reconciler, fakeCatalogNodes{items: map[int64]*node.Node{1: {ID: 1, State: node.StateActive}}}, 0, time.Second)

	startedAt := time.Now()
	scheduler.Start(context.Background())
	require.Less(t, time.Since(startedAt), 100*time.Millisecond)
	<-client.started
	accepted, err := scheduler.Enqueue(ReconcileTriggerManual, []int64{1}, nil, nil)
	require.NoError(t, err)
	require.Empty(t, accepted, "bootstrap run already owns the node")
	close(client.release)
	require.NoError(t, scheduler.Stop())
}

func TestCatalogSchedulerRejectsQueueAfterStop(t *testing.T) {
	db := newRepoTestDB(t)
	repo := NewGormRepo(db)
	nodes := fakeCatalogNodes{items: map[int64]*node.Node{1: {ID: 1, State: node.StateActive}}}
	reconciler := NewCatalogReconciler(repo, fakeLocationLookup{}, nodes, func(*node.Node) CatalogRecordClient { return &fakeCatalogRecordClient{} })
	scheduler := NewCatalogReconcileScheduler(reconciler, nodes, 0, time.Second)
	scheduler.Start(context.Background())
	require.NoError(t, scheduler.Stop())
	_, err := scheduler.Enqueue(ReconcileTriggerManual, nil, nil, nil)
	require.ErrorIs(t, err, ErrCatalogSchedulerStopped)
}
