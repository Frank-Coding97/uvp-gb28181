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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
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
