package bootstrap

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Every database round trip consumes a share of the startup deadline, even
// when the server executes a query quickly. Scale remote latency down here
// so an N+1 catalog seed fails without a minute-long test.
//
// The budget is derived from the surface size plus a fixed slack instead of
// being a constant: the seeded row count grows with every published
// capability (two writes per scope), so a fixed deadline silently turns into
// "fails when the machine is busy" — measured at ~1.0s idle with 84 scopes,
// which is nowhere near the 2s constant once the rest of the suite competes
// for CPU. Deriving it keeps the actual guard intact: a seed that writes more
// than roughly twice the expected rows still blows the deadline.
func TestCoreCatalogSeedFitsRemoteRoundTripBudget(t *testing.T) {
	db := newPublishTestDB(t)
	queries := 0
	delay := func(tx *gorm.DB) {
		select {
		case <-time.After(5 * time.Millisecond):
		case <-tx.Statement.Context.Done():
			tx.AddError(tx.Statement.Context.Err())
		}
	}
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:remote_query_delay", func(tx *gorm.DB) { queries++; delay(tx) }))
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:remote_create_delay", delay))
	expectedWrites := 2*len(CoreCatalogScopes()) + len(coreGroups)
	budget := time.Duration(expectedWrites)*5*time.Millisecond + 2*time.Second
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	result, err := EnsureCoreCatalog(ctx, db, SystemActorID)
	require.NoError(t, err, "catalog seeding must fit startup budget with remote database latency")
	require.Equal(t, len(CoreCatalogScopes()), result.CapabilitiesCreated)
	require.Equal(t, len(CoreCatalogScopes()), result.OperationsCreated)
	require.LessOrEqual(t, queries, 10, "identity queries must not grow with capability count")
}

func TestCatalogPublicationBatchesSnapshotWrites(t *testing.T) {
	db := newPublishTestDB(t)
	var writes int
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:count_snapshot_writes", func(tx *gorm.DB) {
		if tx.Statement.Table == "sys_openapi_release_item" {
			writes++
		}
	}))
	outcome, err := EnsurePublishedCatalog(context.Background(), db, SystemActorID, nil)
	require.NoError(t, err)
	require.Equal(t, len(CoreCatalogScopes()), outcome.ItemCount)
	require.Equal(t, 1, writes, "snapshot must use a batch insert rather than one round trip per capability")
}
