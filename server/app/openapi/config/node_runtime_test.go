package config

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
)

const testBootA = "00112233445566778899aabbccddeeff"
const testBootB = "ffeeddccbbaa99887766554433221100"

func TestNodeRuntimeStoreConfirmProbeBindsNodeAndPersistsIdentity(t *testing.T) {
	db := newNodeRuntimeTestDB(t, "confirm")
	insertNodeRuntimeRow(t, db, nodeRuntimeFixture{revision: 7, uuid: "node-a", history: "[]"})
	now := time.Date(2026, 9, 6, 15, 0, 0, 123456789, time.FixedZone("CST", 8*60*60))
	store := NewNodeRuntimeStore(db, func() time.Time { return now })

	got, err := store.ConfirmProbe(context.Background(), NodeRuntimeObservation{
		NodeRuntimeRef:  NodeRuntimeRef{NodeID: 1, NodeUUID: "node-a", ConfigRevision: 7},
		BootNonce:       testBootA,
		ProtocolVersion: 1,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), got.NodeID)
	require.Equal(t, "node-a", got.NodeUUID)
	require.Equal(t, uint64(7), got.ConfigRevision)
	require.Equal(t, testBootA, got.CurrentBootNonce)
	require.Empty(t, got.RetiredBootHistory)
	require.Equal(t, int64(1), got.RuntimeEpoch)
	require.Equal(t, int64(1), got.RuntimeProtocolVersion)
	require.Equal(t, uint64(7), got.RuntimeConfirmedRevision)
	require.Equal(t, NodeRuntimeStatusActive, got.IdentityStatus)
	require.NotNil(t, got.RuntimeConfirmedAt)
	require.Equal(t, now.UTC().Round(0), *got.RuntimeConfirmedAt)

	requireNodeRuntimeRow(t, db, nodeRuntimeFixture{
		revision: 7, uuid: "node-a", current: testBootA, history: "[]", epoch: 1,
		protocol: 1, confirmedRevision: 7, confirmedAt: timePtr(now.UTC().Round(0)), status: NodeRuntimeStatusActive,
	})
}

func TestNodeRuntimeStoreRejectsStaleNodeConfiguration(t *testing.T) {
	db := newNodeRuntimeTestDB(t, "stale")
	insertNodeRuntimeRow(t, db, nodeRuntimeFixture{revision: 8, uuid: "node-a", history: "[]"})
	store := NewNodeRuntimeStore(db, nil)

	for i, tc := range []struct {
		ref  NodeRuntimeRef
		want error
	}{
		{ref: NodeRuntimeRef{NodeID: 1, NodeUUID: "node-a", ConfigRevision: 7}, want: ErrNodeRuntimeStale},
		{ref: NodeRuntimeRef{NodeID: 1, NodeUUID: "node-b", ConfigRevision: 8}, want: ErrNodeRuntimeStale},
		{ref: NodeRuntimeRef{NodeID: 2, NodeUUID: "node-a", ConfigRevision: 8}, want: ErrNodeRuntimeUnavailable},
	} {
		ref := tc.ref
		_, err := store.ConfirmProbe(context.Background(), NodeRuntimeObservation{
			NodeRuntimeRef: ref, BootNonce: testBootA, ProtocolVersion: 1,
		})
		require.ErrorIs(t, err, tc.want, "case %d", i)
	}
	_, err := store.Load(context.Background(), NodeRuntimeRef{NodeID: 1, NodeUUID: "node-a", ConfigRevision: 7})
	require.ErrorIs(t, err, ErrNodeRuntimeStale)
}

func TestNodeRuntimeStoreSameBootIsIdempotentAndNewBootRetiresOld(t *testing.T) {
	db := newNodeRuntimeTestDB(t, "rotation")
	insertNodeRuntimeRow(t, db, nodeRuntimeFixture{revision: 7, uuid: "node-a", history: "[]"})
	now := time.Date(2026, 9, 6, 15, 1, 0, 0, time.UTC)
	store := NewNodeRuntimeStore(db, func() time.Time { return now })
	ref7 := NodeRuntimeRef{NodeID: 1, NodeUUID: "node-a", ConfigRevision: 7}
	first, err := store.ConfirmProbe(context.Background(), NodeRuntimeObservation{NodeRuntimeRef: ref7, BootNonce: testBootA, ProtocolVersion: 1})
	require.NoError(t, err)

	// A repeated probe for the same process does not create a fake runtime epoch.
	repeated, err := store.ConfirmProbe(context.Background(), NodeRuntimeObservation{NodeRuntimeRef: ref7, BootNonce: testBootA, ProtocolVersion: 1})
	require.NoError(t, err)
	require.Equal(t, first.RuntimeEpoch, repeated.RuntimeEpoch)
	require.Equal(t, first.CurrentBootNonce, repeated.CurrentBootNonce)
	require.Empty(t, repeated.RetiredBootHistory)

	// A node config update must be observed with the new revision before the
	// probe may replace the current identity.
	require.NoError(t, db.Model(&nodeRuntimeTestRow{}).Where("id = ?", 1).Update("revision", 8).Error)
	ref8 := NodeRuntimeRef{NodeID: 1, NodeUUID: "node-a", ConfigRevision: 8}
	rotated, err := store.ConfirmProbe(context.Background(), NodeRuntimeObservation{NodeRuntimeRef: ref8, BootNonce: testBootB, ProtocolVersion: 1})
	require.NoError(t, err)
	require.Equal(t, int64(2), rotated.RuntimeEpoch)
	require.Equal(t, testBootB, rotated.CurrentBootNonce)
	require.Equal(t, []string{testBootA}, rotated.RetiredBootHistory)

	// A retired identity can never become current again, even with the latest
	// configuration revision.
	_, err = store.ConfirmProbe(context.Background(), NodeRuntimeObservation{NodeRuntimeRef: ref8, BootNonce: testBootA, ProtocolVersion: 1})
	require.ErrorIs(t, err, ErrNodeRuntimeRetired)
	requireNodeRuntimeRow(t, db, nodeRuntimeFixture{
		revision: 8, uuid: "node-a", current: testBootB, history: `["` + testBootA + `"]`, epoch: 2,
		protocol: 1, confirmedRevision: 8, confirmedAt: timePtr(now.UTC().Round(0)), status: NodeRuntimeStatusActive,
	})
}

func TestNodeRuntimeStoreRecreatedAndUnknownPreserveMapping(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node-runtime.sqlite")
	db := openNodeRuntimeFileDB(t, path)
	require.NoError(t, db.Exec(nodeRuntimeCreateTableSQL).Error)
	insertNodeRuntimeRow(t, db, nodeRuntimeFixture{revision: 7, uuid: "node-a", history: "[]"})
	ref := NodeRuntimeRef{NodeID: 1, NodeUUID: "node-a", ConfigRevision: 7}
	confirmedAt := time.Date(2026, 9, 6, 15, 2, 0, 0, time.UTC)
	first, err := NewNodeRuntimeStore(db, func() time.Time { return confirmedAt }).ConfirmProbe(context.Background(), NodeRuntimeObservation{NodeRuntimeRef: ref, BootNonce: testBootA, ProtocolVersion: 1})
	require.NoError(t, err)
	require.NoError(t, db.Exec("UPDATE meta_node SET revision = 8").Error)
	ref.ConfigRevision = 8
	_, err = NewNodeRuntimeStore(db, func() time.Time { return confirmedAt.Add(time.Minute) }).ConfirmProbe(context.Background(), NodeRuntimeObservation{NodeRuntimeRef: ref, BootNonce: testBootB, ProtocolVersion: 1})
	require.NoError(t, err)

	unknown, err := NewNodeRuntimeStore(db, func() time.Time { return confirmedAt.Add(2 * time.Minute) }).MarkUnknown(context.Background(), ref)
	require.NoError(t, err)
	require.Equal(t, NodeRuntimeStatusUnknown, unknown.IdentityStatus)
	require.Equal(t, testBootB, unknown.CurrentBootNonce)
	require.Equal(t, []string{testBootA}, unknown.RetiredBootHistory)
	require.Equal(t, int64(2), unknown.RuntimeEpoch)

	recovered, err := NewNodeRuntimeStore(db, nil).Load(context.Background(), ref)
	require.NoError(t, err)
	require.Equal(t, unknown, recovered)
	require.Equal(t, first.NodeUUID, recovered.NodeUUID)
}

func TestNodeRuntimeStoreDoesNotTreatOldConfirmationAsCurrentRevision(t *testing.T) {
	db := newNodeRuntimeTestDB(t, "revision-invalidation")
	insertNodeRuntimeRow(t, db, nodeRuntimeFixture{revision: 7, uuid: "node-a", history: "[]"})
	clock := time.Date(2026, 9, 6, 15, 3, 0, 0, time.UTC)
	store := NewNodeRuntimeStore(db, func() time.Time { return clock })
	ref7 := NodeRuntimeRef{NodeID: 1, NodeUUID: "node-a", ConfigRevision: 7}
	_, err := store.ConfirmProbe(context.Background(), NodeRuntimeObservation{NodeRuntimeRef: ref7, BootNonce: testBootA, ProtocolVersion: 1})
	require.NoError(t, err)
	require.NoError(t, db.Model(&nodeRuntimeTestRow{}).Where("id = ?", 1).Update("revision", 8).Error)
	ref8 := NodeRuntimeRef{NodeID: 1, NodeUUID: "node-a", ConfigRevision: 8}

	_, err = store.Load(context.Background(), ref8)
	require.ErrorIs(t, err, ErrNodeRuntimeStale)
	unknown, err := store.MarkUnknown(context.Background(), ref8)
	require.NoError(t, err)
	require.Equal(t, NodeRuntimeStatusUnknown, unknown.IdentityStatus)
	require.Equal(t, uint64(7), unknown.RuntimeConfirmedRevision, "unknown state preserves the last confirmed revision")

	refreshed, err := store.ConfirmProbe(context.Background(), NodeRuntimeObservation{NodeRuntimeRef: ref8, BootNonce: testBootA, ProtocolVersion: 1})
	require.NoError(t, err)
	require.Equal(t, int64(1), refreshed.RuntimeEpoch, "same boot does not create a fake epoch")
	require.Equal(t, uint64(8), refreshed.RuntimeConfirmedRevision)
	loaded, err := store.Load(context.Background(), ref8)
	require.NoError(t, err)
	require.Equal(t, refreshed, loaded)
}

func TestNodeRuntimeStoreMalformedOrMissingStateFailsClosed(t *testing.T) {
	t.Run("missing table", func(t *testing.T) {
		db := newNodeRuntimeBareDB(t, "missing-table")
		_, err := NewNodeRuntimeStore(db, nil).Load(context.Background(), NodeRuntimeRef{NodeID: 1, NodeUUID: "node-a", ConfigRevision: 1})
		require.ErrorIs(t, err, ErrNodeRuntimeUnavailable)
	})

	cases := []struct {
		name string
		row  nodeRuntimeFixture
	}{
		{name: "missing row", row: nodeRuntimeFixture{revision: 1, uuid: "node-a", history: "[]", omit: true}},
		{name: "null revision", row: nodeRuntimeFixture{revisionNull: true, uuid: "node-a", history: "[]"}},
		{name: "null uuid", row: nodeRuntimeFixture{revision: 1, uuidNull: true, history: "[]"}},
		{name: "null epoch", row: nodeRuntimeFixture{revision: 1, uuid: "node-a", history: "[]", epochNull: true}},
		{name: "null status", row: nodeRuntimeFixture{revision: 1, uuid: "node-a", history: "[]", statusNull: true}},
		{name: "broken history", row: nodeRuntimeFixture{revision: 1, uuid: "node-a", history: "{"}},
		{name: "duplicate history", row: nodeRuntimeFixture{revision: 1, uuid: "node-a", history: `["` + testBootA + `","` + testBootA + `"]`}},
		{name: "current in history", row: nodeRuntimeFixture{revision: 1, uuid: "node-a", current: testBootA, history: `["` + testBootA + `"]`, epoch: 1, protocol: 1, confirmedRevision: 1, confirmedAt: timePtr(time.Unix(1, 0).UTC()), status: NodeRuntimeStatusActive}},
		{name: "confirmed revision ahead", row: nodeRuntimeFixture{revision: 1, uuid: "node-a", current: testBootA, history: "[]", epoch: 1, protocol: 1, confirmedRevision: 2, confirmedAt: timePtr(time.Unix(1, 0).UTC()), status: NodeRuntimeStatusActive}},
		{name: "zero confirmed timestamp", row: nodeRuntimeFixture{revision: 1, uuid: "node-a", current: testBootA, history: "[]", epoch: 1, protocol: 1, confirmedRevision: 1, confirmedAt: timePtr(time.Time{}), status: NodeRuntimeStatusActive}},
		{name: "unknown with current but no history", row: nodeRuntimeFixture{revision: 1, uuid: "node-a", current: testBootA, historyNull: true, epoch: 1, protocol: 1, confirmedRevision: 1, confirmedAt: timePtr(time.Unix(1, 0).UTC()), status: NodeRuntimeStatusUnknown}},
		{name: "unknown invalid status", row: nodeRuntimeFixture{revision: 1, uuid: "node-a", history: "[]", status: "maybe"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newNodeRuntimeTestDB(t, "corrupt-"+strings.ReplaceAll(tc.name, " ", "-"))
			if !tc.row.omit {
				insertNodeRuntimeRow(t, db, tc.row)
			}
			_, err := NewNodeRuntimeStore(db, nil).Load(context.Background(), NodeRuntimeRef{NodeID: 1, NodeUUID: "node-a", ConfigRevision: 1})
			require.ErrorIs(t, err, ErrNodeRuntimeUnavailable)
		})
	}
}

func TestNodeRuntimeStoreUpdateFailureDoesNotPartiallyChangeState(t *testing.T) {
	db := newNodeRuntimeTestDB(t, "update-error")
	insertNodeRuntimeRow(t, db, nodeRuntimeFixture{revision: 1, uuid: "node-a", history: "[]"})
	require.NoError(t, db.Exec(`CREATE TRIGGER reject_runtime_update BEFORE UPDATE ON meta_node
WHEN NEW.current_boot_nonce IS NOT NULL
BEGIN SELECT RAISE(ABORT, 'test runtime update rejected'); END`).Error)

	_, err := NewNodeRuntimeStore(db, func() time.Time { return time.Unix(10, 0).UTC() }).ConfirmProbe(context.Background(), NodeRuntimeObservation{
		NodeRuntimeRef: NodeRuntimeRef{NodeID: 1, NodeUUID: "node-a", ConfigRevision: 1}, BootNonce: testBootA, ProtocolVersion: 1,
	})
	require.ErrorIs(t, err, ErrNodeRuntimeUnavailable)
	requireNodeRuntimeRow(t, db, nodeRuntimeFixture{revision: 1, uuid: "node-a", history: "[]"})
}

func TestNodeRuntimeStoreUsesDialectSpecificRowLocks(t *testing.T) {
	cases := []struct {
		name   string
		db     *gorm.DB
		needle string
	}{
		{name: "mysql", db: dryRunNodeRuntimeDB(t, mysqlDialector()), needle: "FOR UPDATE"},
		{name: "postgres", db: dryRunNodeRuntimeDB(t, postgresDialector()), needle: "FOR UPDATE"},
		{name: "sqlserver", db: dryRunNodeRuntimeDB(t, sqlserverDialector()), needle: "UPDLOCK"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var rows []nodeRuntimeProjection
			stmt := lockNodeRuntimeRow(tc.db).Select("id").Where("id = ?", 1).Find(&rows)
			require.NoError(t, stmt.Error)
			require.Contains(t, strings.ToUpper(stmt.Statement.SQL.String()), tc.needle)
		})
	}
}

func newNodeRuntimeTestDB(t *testing.T, name string) *gorm.DB {
	db := newNodeRuntimeBareDB(t, name)
	require.NoError(t, db.Exec(nodeRuntimeCreateTableSQL).Error)
	return db
}

func newNodeRuntimeBareDB(t *testing.T, name string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+name+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("disable_raise_record_not_found", gormhelper.MaskNotDataError))
	return db
}

func openNodeRuntimeFileDB(t *testing.T, path string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+path+"?cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("disable_raise_record_not_found", gormhelper.MaskNotDataError))
	return db
}

const nodeRuntimeCreateTableSQL = `CREATE TABLE meta_node (
 id INTEGER PRIMARY KEY,
 revision INTEGER NULL,
 media_server_uuid TEXT NULL,
 current_boot_nonce TEXT NULL,
 retired_boot_history TEXT NULL,
 runtime_epoch INTEGER NULL,
 runtime_protocol_version INTEGER NULL,
 runtime_confirmed_revision INTEGER NULL,
 runtime_confirmed_at DATETIME NULL,
 runtime_identity_status TEXT NULL
)`

type nodeRuntimeFixture struct {
	revision          uint64
	revisionNull      bool
	uuid              string
	uuidNull          bool
	current           string
	history           string
	historyNull       bool
	epoch             int64
	epochNull         bool
	protocol          int64
	confirmedRevision int64
	confirmedAt       *time.Time
	status            string
	statusNull        bool
	omit              bool
}

type nodeRuntimeTestRow struct {
	ID                       int64      `gorm:"column:id"`
	Revision                 *uint64    `gorm:"column:revision"`
	MediaServerUUID          *string    `gorm:"column:media_server_uuid"`
	CurrentBootNonce         *string    `gorm:"column:current_boot_nonce"`
	RetiredBootHistory       *string    `gorm:"column:retired_boot_history"`
	RuntimeEpoch             *int64     `gorm:"column:runtime_epoch"`
	RuntimeProtocolVersion   *int64     `gorm:"column:runtime_protocol_version"`
	RuntimeConfirmedRevision *int64     `gorm:"column:runtime_confirmed_revision"`
	RuntimeConfirmedAt       *time.Time `gorm:"column:runtime_confirmed_at"`
	RuntimeIdentityStatus    *string    `gorm:"column:runtime_identity_status"`
}

func (nodeRuntimeTestRow) TableName() string { return "meta_node" }

func insertNodeRuntimeRow(t *testing.T, db *gorm.DB, f nodeRuntimeFixture) {
	t.Helper()
	if f.omit {
		return
	}
	revision := any(f.revision)
	if f.revisionNull {
		revision = nil
	}
	uuid := any(f.uuid)
	if f.uuidNull {
		uuid = nil
	}
	history := any(f.history)
	if f.historyNull {
		history = nil
	}
	epoch := any(f.epoch)
	if f.epochNull {
		epoch = nil
	}
	protocol := any(f.protocol)
	confirmedRevision := any(f.confirmedRevision)
	confirmedAt := any(f.confirmedAt)
	status := any(f.status)
	current := any(f.current)
	if f.current == "" {
		current = nil
	}
	if f.status == "" {
		status = NodeRuntimeStatusUnknown
	}
	if f.statusNull {
		status = nil
	}
	require.NoError(t, db.Exec(`INSERT INTO meta_node
	(id, revision, media_server_uuid, current_boot_nonce, retired_boot_history, runtime_epoch,
	 runtime_protocol_version, runtime_confirmed_revision, runtime_confirmed_at, runtime_identity_status)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, 1, revision, uuid, current, history, epoch, protocol, confirmedRevision, confirmedAt, status).Error)
}

func requireNodeRuntimeRow(t *testing.T, db *gorm.DB, want nodeRuntimeFixture) {
	t.Helper()
	var got nodeRuntimeTestRow
	require.NoError(t, db.Where("id = ?", 1).First(&got).Error)
	require.NotNil(t, got.Revision)
	require.Equal(t, want.revision, *got.Revision)
	if want.uuid != "" {
		require.NotNil(t, got.MediaServerUUID)
		require.Equal(t, want.uuid, *got.MediaServerUUID)
	}
	if want.current == "" {
		require.Nil(t, got.CurrentBootNonce)
	} else {
		require.NotNil(t, got.CurrentBootNonce)
		require.Equal(t, want.current, *got.CurrentBootNonce)
	}
	require.NotNil(t, got.RetiredBootHistory)
	require.Equal(t, want.history, *got.RetiredBootHistory)
	if want.epoch == 0 {
		require.NotNil(t, got.RuntimeEpoch)
		require.Equal(t, int64(0), *got.RuntimeEpoch)
	} else {
		require.NotNil(t, got.RuntimeEpoch)
		require.Equal(t, want.epoch, *got.RuntimeEpoch)
	}
	if want.protocol != 0 {
		require.NotNil(t, got.RuntimeProtocolVersion)
		require.Equal(t, want.protocol, *got.RuntimeProtocolVersion)
	}
	if want.confirmedRevision != 0 {
		require.NotNil(t, got.RuntimeConfirmedRevision)
		require.Equal(t, want.confirmedRevision, *got.RuntimeConfirmedRevision)
	}
	if want.confirmedAt != nil {
		require.NotNil(t, got.RuntimeConfirmedAt)
		require.Equal(t, want.confirmedAt.UTC().Round(0), got.RuntimeConfirmedAt.UTC().Round(0))
	}
	if want.status != "" {
		require.NotNil(t, got.RuntimeIdentityStatus)
		require.Equal(t, want.status, *got.RuntimeIdentityStatus)
	}
}

func dryRunNodeRuntimeDB(t *testing.T, dialector gorm.Dialector) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(dialector, &gorm.Config{DryRun: true, DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	return db
}

func mysqlDialector() gorm.Dialector {
	return mysql.New(mysql.Config{DSN: "runtime:runtime@tcp(localhost:3306)/runtime", SkipInitializeWithVersion: true})
}

func postgresDialector() gorm.Dialector {
	return postgres.New(postgres.Config{DSN: "host=localhost user=runtime dbname=runtime", PreferSimpleProtocol: true})
}

func sqlserverDialector() gorm.Dialector {
	return sqlserver.Open("sqlserver://localhost:1433?database=runtime")
}
