package assign

import (
	"context"
	"errors"
	"math"
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

	openapilimit "uvplatform.cn/uvp-gb28181/app/openapi/limit"
	openapimodels "uvplatform.cn/uvp-gb28181/app/openapi/models"
)

type recordingTransferRecorder struct {
	calls    int
	deviceID string
	epoch    int64
	err      error
}

func (r *recordingTransferRecorder) RecordDeviceTransfer(_ context.Context, _ *gorm.DB, deviceID string, epoch int64) error {
	r.calls++
	r.deviceID = deviceID
	r.epoch = epoch
	return r.err
}

func fixedTransferClock() time.Time {
	return time.Date(2026, 9, 6, 8, 9, 10, 987654000, time.UTC)
}

type transferSecurityState struct {
	OwnerDeptID         uint
	AccessEpoch         int64
	LegacyRevokedBefore *time.Time
}

func readTransferSecurity(t *testing.T, db *gorm.DB, deviceID uint) transferSecurityState {
	t.Helper()
	var row transferSecurityState
	require.NoError(t, db.Table("gb_device").Select("owner_dept_id, access_epoch, legacy_revoked_before").Where("id = ?", deviceID).Take(&row).Error)
	return row
}

func seedTransferGrantAndViewer(t *testing.T, db *gorm.DB, deviceCode string, now time.Time, grantID string, viewerID int64) {
	t.Helper()
	channelCode := "34020000002000110002"
	nodeUUID := "node-transfer"
	bootNonce := "0123456789abcdef0123456789abcdef"
	schema, vhost, app, stream, protocol := "rtmp", "__defaultVhost__", "live", "transfer", "https-flv"
	generation := uint64(1)
	require.NoError(t, db.Create(&openapimodels.PlayGrant{
		GrantID: grantID, ClientID: 7, Scope: openapilimit.PlayLiveApplyScope,
		DeviceID: &deviceCode, ChannelID: &channelCode, ClientEpoch: 1, ScopeEpoch: 1, DeviceEpoch: 1,
		NodeUUID: &nodeUUID, BootNonce: &bootNonce, Schema: &schema, VHost: &vhost, App: &app,
		Stream: &stream, MediaGeneration: &generation, Protocol: &protocol,
		IssuedAt: now, ExpiresAt: now.Add(time.Minute), State: openapimodels.GrantStateBound,
		CreatedAt: now, UpdatedAt: now,
	}).Error)
	require.NoError(t, db.Create(&openapimodels.Viewer{
		ID: viewerID, GrantID: grantID, NodeUUID: nodeUUID, BootNonce: bootNonce, Identifier: "viewer-transfer",
		Schema: schema, VHost: vhost, App: app, Stream: stream, MediaGeneration: generation,
		State: openapimodels.ViewerStateActive, CreatedAt: now, UpdatedAt: now,
	}).Error)
}

func TestAssignOneWithReceiptCommitsSecurityAndRecordsIntent(t *testing.T) {
	db := newAssignTestDB(t)
	device := seedAssignedDeviceWithCode(t, db, "34020000002000100001")
	oldCutoff := time.Unix(fixedTransferClock().Unix()+5, 0).UTC()
	require.NoError(t, db.Table("gb_device").Where("id = ?", device.ID).Update("legacy_revoked_before", oldCutoff).Error)
	recorder := &recordingTransferRecorder{}
	svc := NewService(db, validatorVisibleDept1,
		WithTransferClock(fixedTransferClock), WithTransferRecorder(recorder))

	receipt, err := svc.AssignOneWithReceipt(context.Background(), device.ID, 2, []uint{1}, true)
	require.NoError(t, err)
	require.Equal(t, TransferReceipt{
		DevicePK:            device.ID,
		DeviceCode:          device.DeviceID,
		OldOwnerDeptID:      1,
		NewOwnerDeptID:      2,
		OldEpoch:            1,
		NewEpoch:            2,
		LegacyRevokedBefore: oldCutoff,
	}, receipt)
	require.Equal(t, 1, recorder.calls)
	require.Equal(t, device.DeviceID, recorder.deviceID)
	require.Equal(t, int64(2), recorder.epoch)

	state := readTransferSecurity(t, db, device.ID)
	require.EqualValues(t, 2, state.OwnerDeptID)
	require.EqualValues(t, 2, state.AccessEpoch)
	require.Equal(t, oldCutoff, *state.LegacyRevokedBefore)
}

func TestAssignOneWithReceiptDefaultRecorderRevokesGrantAndViewer(t *testing.T) {
	db := newAssignTestDB(t)
	device := seedAssignedDeviceWithCode(t, db, "34020000002000100002")
	now := fixedTransferClock()
	grantID := "00000000-0000-4000-8000-000000000101"
	seedTransferGrantAndViewer(t, db, device.DeviceID, now, grantID, 101)

	svc := NewService(db, validatorVisibleDept1, WithTransferClock(func() time.Time { return now }))
	_, err := svc.AssignOneWithReceipt(context.Background(), device.ID, 2, []uint{1}, true)
	require.NoError(t, err)

	var grant openapimodels.PlayGrant
	require.NoError(t, db.First(&grant, "grant_id = ?", grantID).Error)
	require.Equal(t, openapimodels.GrantStateRevoked, grant.State)
	require.Equal(t, "device.transferred", grant.Reason)
	var viewer openapimodels.Viewer
	require.NoError(t, db.First(&viewer, "grant_id = ?", grantID).Error)
	require.Equal(t, openapimodels.ViewerStateRevokePending, viewer.State)
}

func TestAssignOneWithReceiptRevocationFailureRollsBackGrantViewerAndCascade(t *testing.T) {
	db := newAssignTestDB(t)
	device := seedAssignedDeviceWithCode(t, db, "34020000002000100009")
	now := fixedTransferClock()
	grantID := "00000000-0000-4000-8000-000000000109"
	seedTransferGrantAndViewer(t, db, device.DeviceID, now, grantID, 109)
	require.NoError(t, db.Exec("CREATE TRIGGER transfer_viewer_failure BEFORE UPDATE ON gb_openapi_viewer BEGIN SELECT RAISE(ABORT, 'fixture failure'); END").Error)

	svc := NewService(db, validatorVisibleDept1, WithTransferClock(func() time.Time { return now }))
	receipt, err := svc.AssignOneWithReceipt(context.Background(), device.ID, 2, []uint{1}, true)
	require.Error(t, err)
	require.Equal(t, TransferReceipt{}, receipt)
	state := readTransferSecurity(t, db, device.ID)
	require.EqualValues(t, 1, state.OwnerDeptID)
	require.EqualValues(t, 1, state.AccessEpoch)
	require.Nil(t, state.LegacyRevokedBefore)
	var nodeCount int64
	require.NoError(t, db.Model(&struct{}{}).Table("gb_catalog_node").Where("device_id = ?", device.ID).Count(&nodeCount).Error)
	require.EqualValues(t, 1, nodeCount)
	var grant openapimodels.PlayGrant
	require.NoError(t, db.First(&grant, "grant_id = ?", grantID).Error)
	require.Equal(t, openapimodels.GrantStateBound, grant.State)
	var viewer openapimodels.Viewer
	require.NoError(t, db.First(&viewer, "grant_id = ?", grantID).Error)
	require.Equal(t, openapimodels.ViewerStateActive, viewer.State)
}

func TestAssignOneWithReceiptRecorderFailureRollsBackCascadeAndSecurity(t *testing.T) {
	db := newAssignTestDB(t)
	device := seedAssignedDeviceWithCode(t, db, "34020000002000100003")
	recorder := &recordingTransferRecorder{
		err: errors.New("revocation recorder failed"),
	}
	svc := NewService(db, validatorVisibleDept1, WithTransferClock(fixedTransferClock), WithTransferRecorder(recorder))

	receipt, err := svc.AssignOneWithReceipt(context.Background(), device.ID, 2, []uint{1}, true)
	require.ErrorIs(t, err, recorder.err)
	require.Equal(t, TransferReceipt{}, receipt, "receipt is valid only after commit")
	state := readTransferSecurity(t, db, device.ID)
	require.EqualValues(t, 1, state.OwnerDeptID)
	require.EqualValues(t, 1, state.AccessEpoch)
	require.Nil(t, state.LegacyRevokedBefore)
	var nodes int64
	require.NoError(t, db.Model(&struct{}{}).Table("gb_catalog_node").Where("device_id = ?", device.ID).Count(&nodes).Error)
	require.EqualValues(t, 1, nodes, "cascade must roll back with security update")
}

func TestAssignOneWithReceiptSameOwnerIsNoop(t *testing.T) {
	db := newAssignTestDB(t)
	device := seedAssignedDeviceWithCode(t, db, "34020000002000100004")
	recorder := &recordingTransferRecorder{}
	svc := NewService(db, validatorVisibleDept1, WithTransferRecorder(recorder))

	receipt, err := svc.AssignOneWithReceipt(context.Background(), device.ID, 1, []uint{1}, true)
	require.NoError(t, err)
	require.Equal(t, TransferReceipt{}, receipt)
	require.Zero(t, recorder.calls)
	state := readTransferSecurity(t, db, device.ID)
	require.EqualValues(t, 1, state.OwnerDeptID)
	require.EqualValues(t, 1, state.AccessEpoch)
	require.Nil(t, state.LegacyRevokedBefore)
}

func TestAssignBatchV2SameSecondTransferAndReturnNeverRegressesCutoff(t *testing.T) {
	db := newAssignTestDB(t)
	device := seedAssignedDeviceWithCode(t, db, "34020000002000100005")
	recorder := &recordingTransferRecorder{}
	clock := fixedTransferClock
	svc := NewService(db, validatorVisibleDept1, WithTransferClock(clock), WithTransferRecorder(recorder))

	first, err := svc.AssignBatchV2(context.Background(), []AssignmentInput{{DeviceID: device.ID, ExpectedOwnerDeptID: 1}}, 2)
	require.NoError(t, err)
	require.Equal(t, AssignmentChanged, first.Results[0].Status)
	require.NotNil(t, first.Results[0].Receipt)

	second, err := svc.AssignBatchV2(context.Background(), []AssignmentInput{{DeviceID: device.ID, ExpectedOwnerDeptID: 2}}, 1)
	require.NoError(t, err)
	require.Equal(t, AssignmentChanged, second.Results[0].Status)
	require.NotNil(t, second.Results[0].Receipt)
	require.EqualValues(t, 3, second.Results[0].Receipt.NewEpoch)
	require.Equal(t, first.Results[0].Receipt.LegacyRevokedBefore, second.Results[0].Receipt.LegacyRevokedBefore)
	state := readTransferSecurity(t, db, device.ID)
	require.EqualValues(t, 1, state.OwnerDeptID)
	require.EqualValues(t, 3, state.AccessEpoch)
	require.Equal(t, first.Results[0].Receipt.LegacyRevokedBefore, *state.LegacyRevokedBefore)
	require.Equal(t, 2, recorder.calls)
}

func TestAssignBatchV2StaleAndPartialFailurePreserveSecurityBoundary(t *testing.T) {
	db := newAssignTestDB(t)
	d1 := seedAssignedDeviceWithCode(t, db, "34020000002000100006")
	d2 := seedAssignedDeviceWithCode(t, db, "34020000002000100007")
	require.NoError(t, db.Model(&struct{}{}).Table("gb_device").Where("id = ?", d2.ID).Update("owner_dept_id", 9).Error)
	recorder := &recordingTransferRecorder{}
	svc := NewService(db, validatorVisibleDept1, WithTransferClock(fixedTransferClock), WithTransferRecorder(recorder))

	result, err := svc.AssignBatchV2(context.Background(), []AssignmentInput{
		{DeviceID: d1.ID, ExpectedOwnerDeptID: 1},
		{DeviceID: d2.ID, ExpectedOwnerDeptID: 1},
	}, 2)
	require.NoError(t, err)
	require.Equal(t, AssignmentChanged, result.Results[0].Status)
	require.Equal(t, AssignmentFailed, result.Results[1].Status)
	first := readTransferSecurity(t, db, d1.ID)
	second := readTransferSecurity(t, db, d2.ID)
	require.EqualValues(t, 2, first.OwnerDeptID)
	require.EqualValues(t, 2, first.AccessEpoch)
	require.EqualValues(t, 9, second.OwnerDeptID)
	require.EqualValues(t, 1, second.AccessEpoch)
	require.Nil(t, second.LegacyRevokedBefore)

	stale, err := svc.AssignBatchV2(context.Background(), []AssignmentInput{{DeviceID: d1.ID, ExpectedOwnerDeptID: 1}}, 1)
	require.NoError(t, err)
	require.Equal(t, AssignmentFailed, stale.Results[0].Status)
	require.EqualValues(t, 2, readTransferSecurity(t, db, d1.ID).AccessEpoch)
}

func TestAssignOneRejectsEpochOverflow(t *testing.T) {
	db := newAssignTestDB(t)
	device := seedAssignedDeviceWithCode(t, db, "34020000002000100008")
	require.NoError(t, db.Model(&struct{}{}).Table("gb_device").Where("id = ?", device.ID).Update("access_epoch", math.MaxInt64).Error)
	svc := NewService(db, validatorVisibleDept1, WithTransferRecorder(&recordingTransferRecorder{}))

	_, err := svc.AssignOneWithReceipt(context.Background(), device.ID, 2, []uint{1}, true)
	require.ErrorIs(t, err, ErrAssignmentSecurityUnavailable)
	state := readTransferSecurity(t, db, device.ID)
	require.EqualValues(t, 1, state.OwnerDeptID)
	require.EqualValues(t, math.MaxInt64, state.AccessEpoch)
}

func TestAssignOneRejectsSoftDeletedDevice(t *testing.T) {
	db := newAssignTestDB(t)
	device := seedAssignedDeviceWithCode(t, db, "34020000002000100011")
	deletedAt := fixedTransferClock()
	require.NoError(t, db.Model(&struct{}{}).Table("gb_device").Where("id = ?", device.ID).Update("deleted_at", deletedAt).Error)

	_, err := NewService(db, validatorVisibleDept1, WithTransferRecorder(&recordingTransferRecorder{})).AssignOneWithReceipt(context.Background(), device.ID, 2, []uint{1}, true)
	require.ErrorIs(t, err, ErrDeviceNotVisible)
}

func TestAssignOneRejectsMalformedExistingCutoff(t *testing.T) {
	for _, test := range []struct {
		name   string
		cutoff time.Time
	}{
		{name: "non-positive", cutoff: time.Unix(0, 0).UTC()},
		{name: "subsecond", cutoff: fixedTransferClock().Add(500 * time.Millisecond)},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := newAssignTestDB(t)
			device := seedAssignedDeviceWithCode(t, db, "34020000002000100012")
			require.NoError(t, db.Model(&struct{}{}).Table("gb_device").Where("id = ?", device.ID).Update("legacy_revoked_before", test.cutoff).Error)

			_, err := NewService(db, validatorVisibleDept1, WithTransferRecorder(&recordingTransferRecorder{})).AssignOneWithReceipt(context.Background(), device.ID, 2, []uint{1}, true)
			require.ErrorIs(t, err, ErrAssignmentSecurityUnavailable)
			state := readTransferSecurity(t, db, device.ID)
			require.EqualValues(t, 1, state.OwnerDeptID)
			require.EqualValues(t, 1, state.AccessEpoch)
		})
	}
}

func TestAssignOneTypedNilRecorderUsesProductionRecorder(t *testing.T) {
	db := newAssignTestDB(t)
	device := seedAssignedDeviceWithCode(t, db, "34020000002000100013")
	grantID := "00000000-0000-4000-8000-000000000113"
	seedTransferGrantAndViewer(t, db, device.DeviceID, fixedTransferClock(), grantID, 113)
	var recorder *recordingTransferRecorder
	svc := NewService(db, validatorVisibleDept1,
		WithTransferClock(fixedTransferClock), WithTransferRecorder(recorder))

	receipt, err := svc.AssignOneWithReceipt(context.Background(), device.ID, 2, []uint{1}, true)
	require.NoError(t, err)
	require.EqualValues(t, 1, receipt.OldEpoch)
	require.EqualValues(t, 2, receipt.NewEpoch)
	var grant openapimodels.PlayGrant
	require.NoError(t, db.First(&grant, "grant_id = ?", grantID).Error)
	require.Equal(t, openapimodels.GrantStateRevoked, grant.State)
	var viewer openapimodels.Viewer
	require.NoError(t, db.First(&viewer, "grant_id = ?", grantID).Error)
	require.Equal(t, openapimodels.ViewerStateRevokePending, viewer.State)
}

func TestAssignOneRejectsNonPositiveClock(t *testing.T) {
	for _, now := range []time.Time{time.Time{}, time.Unix(0, 0), time.Unix(-1, 0)} {
		t.Run(now.String(), func(t *testing.T) {
			db := newAssignTestDB(t)
			device := seedAssignedDeviceWithCode(t, db, "34020000002000100014")
			recorder := &recordingTransferRecorder{}
			svc := NewService(db, validatorVisibleDept1,
				WithTransferClock(func() time.Time { return now }), WithTransferRecorder(recorder))
			receipt, err := svc.AssignOneWithReceipt(context.Background(), device.ID, 2, []uint{1}, true)
			require.ErrorIs(t, err, ErrAssignmentSecurityUnavailable)
			require.Equal(t, TransferReceipt{}, receipt)
			require.Zero(t, recorder.calls)
			state := readTransferSecurity(t, db, device.ID)
			require.EqualValues(t, 1, state.OwnerDeptID)
			require.EqualValues(t, 1, state.AccessEpoch)
			require.Nil(t, state.LegacyRevokedBefore)
		})
	}
}

func TestAssignOneFailsClosedWhenSecurityColumnsAreMissing(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE gb_device (
		id INTEGER PRIMARY KEY, device_id TEXT NOT NULL, name TEXT NOT NULL DEFAULT '',
		owner_dept_id INTEGER NOT NULL, deleted_at DATETIME NULL
	)`).Error)
	require.NoError(t, db.Exec("INSERT INTO gb_device(id, device_id, owner_dept_id) VALUES (1, ?, 1)", "34020000002000100010").Error)

	receipt, err := NewService(db, validatorVisibleDept1, WithTransferRecorder(&recordingTransferRecorder{})).AssignOneWithReceipt(context.Background(), 1, 2, nil, false)
	require.ErrorIs(t, err, ErrAssignmentSecurityUnavailable)
	require.Equal(t, TransferReceipt{}, receipt)
	var owner int
	require.NoError(t, db.Table("gb_device").Select("owner_dept_id").Where("id = 1").Scan(&owner).Error)
	require.Equal(t, 1, owner)
}

func TestAssignUsesDialectSpecificDeviceLocks(t *testing.T) {
	cases := []struct {
		name   string
		db     *gorm.DB
		needle string
	}{
		{name: "mysql", db: dryRunAssignmentDB(t, mysql.New(mysql.Config{DSN: "runtime:runtime@tcp(localhost:3306)/runtime", SkipInitializeWithVersion: true})), needle: "FOR UPDATE"},
		{name: "postgres", db: dryRunAssignmentDB(t, postgres.New(postgres.Config{DSN: "host=localhost user=runtime dbname=runtime", PreferSimpleProtocol: true})), needle: "FOR UPDATE"},
		{name: "sqlserver", db: dryRunAssignmentDB(t, sqlserver.Open("sqlserver://localhost:1433?database=runtime")), needle: "UPDLOCK"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var rows []assignmentDevice
			stmt := lockedAssignmentDeviceTable(tc.db).Select("id").Where("id = ?", 1).Find(&rows)
			require.NoError(t, stmt.Error)
			require.Contains(t, strings.ToUpper(stmt.Statement.SQL.String()), tc.needle)
		})
	}
}

func dryRunAssignmentDB(t *testing.T, dialector gorm.Dialector) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(dialector, &gorm.Config{DryRun: true, DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}
