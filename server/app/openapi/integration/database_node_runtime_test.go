package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	openapiconfig "uvplatform.cn/uvp-gb28181/app/openapi/config"
)

const (
	nativeNodeRuntimeFixtureID = int64(2)
	nativeNodeRuntimeUUID      = "native-node-runtime"
	nativeNodeRuntimeBootA     = "000102030405060708090a0b0c0d0e0f"
	nativeNodeRuntimeBootB     = "100102030405060708090a0b0c0d0e0f"
)

// checkNativeNodeRuntime runs only after the parent database gate has
// connected to an explicitly selected empty database and applied migrations.
// The base gate's meta_node fixture intentionally has only id/fixture_label;
// these two identity columns are added here as test-only fixture support so
// the real store can be exercised without changing a migration.
func checkNativeNodeRuntime(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(db.Statement.Context, 20*time.Second)
	defer cancel()

	ensureNativeNodeRuntimeColumns(t, db)
	insertNativeNodeRuntimeFixture(t, db)
	defer func() {
		if err := db.Exec("DELETE FROM meta_node WHERE id = ?", nativeNodeRuntimeFixtureID).Error; err != nil {
			t.Errorf("remove node runtime fixture: %v", err)
		}
	}()

	ref := openapiconfig.NodeRuntimeRef{
		NodeID:       nativeNodeRuntimeFixtureID,
		NodeUUID:     nativeNodeRuntimeUUID,
		NodeRevision: 1,
	}
	confirmedAt := time.Date(2026, 9, 6, 15, 10, 20, 123456789, time.UTC)
	clock := confirmedAt
	store := openapiconfig.NewNodeRuntimeStore(db, func() time.Time { return clock })
	first, err := store.ConfirmProbe(ctx, openapiconfig.NodeRuntimeObservation{
		NodeRuntimeRef:  ref,
		BootNonce:       nativeNodeRuntimeBootA,
		ProtocolVersion: openapiconfig.NodeRuntimeProtocolV1,
	})
	require.NoError(t, err)
	require.Equal(t, nativeNodeRuntimeBootA, first.CurrentBootNonce)
	require.Equal(t, int64(1), first.RuntimeEpoch)
	require.Equal(t, []string{}, first.RetiredBootHistory)
	require.NotNil(t, first.RuntimeConfirmedAt)
	require.Equal(t, confirmedAt.UTC().Truncate(time.Microsecond), *first.RuntimeConfirmedAt)

	// A same-boot replay with the same normalized clock value must take the
	// store's idempotent path. Native MySQL may report changed rows as zero for
	// an identical UPDATE, so this is intentionally repeated against the real DB.
	replayed, err := store.ConfirmProbe(ctx, openapiconfig.NodeRuntimeObservation{
		NodeRuntimeRef:  ref,
		BootNonce:       nativeNodeRuntimeBootA,
		ProtocolVersion: openapiconfig.NodeRuntimeProtocolV1,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), replayed.RuntimeEpoch)
	require.Equal(t, nativeNodeRuntimeBootA, replayed.CurrentBootNonce)
	require.Equal(t, first.RuntimeConfirmedAt, replayed.RuntimeConfirmedAt)

	clock = confirmedAt.Add(time.Second)
	rotated, err := store.ConfirmProbe(ctx, openapiconfig.NodeRuntimeObservation{
		NodeRuntimeRef:  ref,
		BootNonce:       nativeNodeRuntimeBootB,
		ProtocolVersion: openapiconfig.NodeRuntimeProtocolV1,
	})
	require.NoError(t, err)
	require.Equal(t, nativeNodeRuntimeBootB, rotated.CurrentBootNonce)
	require.Equal(t, int64(2), rotated.RuntimeEpoch)
	require.Equal(t, []string{nativeNodeRuntimeBootA}, rotated.RetiredBootHistory)

	_, err = store.ConfirmProbe(ctx, openapiconfig.NodeRuntimeObservation{
		NodeRuntimeRef:  ref,
		BootNonce:       nativeNodeRuntimeBootA,
		ProtocolVersion: openapiconfig.NodeRuntimeProtocolV1,
	})
	require.ErrorIs(t, err, openapiconfig.ErrNodeRuntimeRetired)

	// A newly created store must read the same durable mapping, including the
	// retired history, rather than rebuilding an in-memory identity.
	recreated := openapiconfig.NewNodeRuntimeStore(db, func() time.Time { return clock })
	loaded, err := recreated.Load(ctx, ref)
	require.NoError(t, err)
	require.Equal(t, rotated.CurrentBootNonce, loaded.CurrentBootNonce)
	require.Equal(t, rotated.RetiredBootHistory, loaded.RetiredBootHistory)
	require.Equal(t, rotated.RuntimeEpoch, loaded.RuntimeEpoch)
	require.Equal(t, rotated.RuntimeConfirmedRevision, loaded.RuntimeConfirmedRevision)

	unknown, err := recreated.MarkUnknown(ctx, ref)
	require.NoError(t, err)
	require.Equal(t, openapiconfig.NodeRuntimeStatusUnknown, unknown.IdentityStatus)
	require.Equal(t, rotated.CurrentBootNonce, unknown.CurrentBootNonce)
	require.Equal(t, rotated.RetiredBootHistory, unknown.RetiredBootHistory)
	require.Equal(t, rotated.RuntimeEpoch, unknown.RuntimeEpoch)

	loaded, err = openapiconfig.NewNodeRuntimeStore(db, nil).Load(ctx, ref)
	require.NoError(t, err)
	require.Equal(t, openapiconfig.NodeRuntimeStatusUnknown, loaded.IdentityStatus)
	require.Equal(t, nativeNodeRuntimeBootB, loaded.CurrentBootNonce)
	require.Equal(t, []string{nativeNodeRuntimeBootA}, loaded.RetiredBootHistory)

	// Ordinary node state changes advance meta_node.revision. A probe carrying
	// the old revision must not read or mutate the newer row.
	require.NoError(t, db.Table("meta_node").Where("id = ?", nativeNodeRuntimeFixtureID).Update("revision", 2).Error)
	_, err = recreated.Load(ctx, ref)
	require.ErrorIs(t, err, openapiconfig.ErrNodeRuntimeStale)
	_, err = recreated.ConfirmProbe(ctx, openapiconfig.NodeRuntimeObservation{
		NodeRuntimeRef:  ref,
		BootNonce:       nativeNodeRuntimeBootB,
		ProtocolVersion: openapiconfig.NodeRuntimeProtocolV1,
	})
	require.ErrorIs(t, err, openapiconfig.ErrNodeRuntimeStale)

	t.Log("native node runtime: first confirm, same-microsecond replay, retired boot rejection, recreated Load, unknown preservation, and stale revision rejection passed")
}

func ensureNativeNodeRuntimeColumns(t *testing.T, db *gorm.DB) {
	t.Helper()
	columns := []struct {
		name string
		ddl  string
	}{
		{name: "revision", ddl: "ALTER TABLE meta_node ADD revision BIGINT NOT NULL DEFAULT 1"},
		{name: "media_server_uuid", ddl: "ALTER TABLE meta_node ADD media_server_uuid VARCHAR(64) NOT NULL DEFAULT ''"},
	}
	for _, column := range columns {
		if db.Migrator().HasColumn("meta_node", column.name) {
			continue
		}
		require.NoError(t, db.Exec(column.ddl).Error, column.name)
	}
}

func insertNativeNodeRuntimeFixture(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Table("meta_node").Create(map[string]any{
		"id":                         nativeNodeRuntimeFixtureID,
		"fixture_label":              "node-runtime",
		"revision":                   1,
		"media_server_uuid":          nativeNodeRuntimeUUID,
		"current_boot_nonce":         nil,
		"retired_boot_history":       "[]",
		"runtime_epoch":              0,
		"runtime_protocol_version":   0,
		"runtime_confirmed_revision": 0,
		"runtime_confirmed_at":       nil,
		"runtime_identity_status":    openapiconfig.NodeRuntimeStatusUnknown,
	}).Error)
}
