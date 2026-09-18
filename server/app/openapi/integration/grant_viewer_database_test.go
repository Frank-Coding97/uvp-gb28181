package integration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/openapi/limit"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

const (
	nativeGrantViewerDepartmentID       = uint(2401)
	nativeGrantViewerDeviceID           = "34020000002000000001"
	nativeGrantViewerChannelID          = "34020000001320000001"
	nativeGrantViewerNodeUUID           = "native-grant-viewer-node"
	nativeGrantViewerBootNonce          = "200102030405060708090a0b0c0d0e0f"
	nativeGrantViewerQualificationTable = "openapi_test_node_qualification"
)

// checkNativeGrantViewer runs inside TestOpenAPIDatabaseCoreMigration, after
// that test has proved an explicitly selected empty database and applied the
// real OpenAPI migrations. It deliberately uses only a SQL-backed test
// authority: runtime readback is necessary but the qualification rows below
// are separate test evidence, so active runtime status is never treated as a
// product qualified-node fallback.
func checkNativeGrantViewer(t *testing.T, root *gorm.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	db := root.WithContext(ctx)

	fixture := nativeGrantViewerFixture{}
	defer func() { fixture.cleanup(t, root) }()
	var err error
	fixture, err = prepareNativeGrantViewerFixture(t, db)
	require.NoError(t, err)
	var qualified int64
	require.NoError(t, db.Table("meta_node AS n").Joins("JOIN "+nativeGrantViewerQualificationTable+" AS q ON q.node_uuid = n.media_server_uuid AND q.boot_nonce = n.current_boot_nonce").Count(&qualified).Error, "native fixture qualification must join its exact node identity")
	require.Positive(t, qualified)

	clock := time.Date(2026, 9, 6, 20, 30, 40, 123456789, time.UTC)
	signer, err := playauth.NewSigner([]byte(strings.Repeat("n", 32)), playauth.WithNow(func() time.Time { return clock }))
	require.NoError(t, err)
	authority := nativeGrantViewerNodeAuthority{}
	service, err := playauth.NewOpenAPIGrantService(db, signer, authority, func() time.Time { return clock })
	require.NoError(t, err)
	quota := limit.NewQuota(db, func() time.Time { return clock })

	issue := func(protocol string, generation uint64) (playauth.Grant, playauth.OpenAPIClaims, limit.Reservation) {
		t.Helper()
		reservation, reserveErr := quota.ReservePending(ctx, limit.ReservationRequest{
			ClientID: fixture.client.ID, Scope: limit.PlayLiveApplyScope,
			DeviceID: nativeGrantViewerDeviceID, ChannelID: nativeGrantViewerChannelID,
		})
		require.NoError(t, reserveErr)
		issued, issueErr := service.Issue(ctx, nativeGrantViewerIssueRequest(reservation.GrantID, protocol, generation))
		require.NoError(t, issueErr)
		require.NotEmpty(t, issued.Token)
		claims, authErr := signer.AuthenticateOpenAPI(issued.Token)
		require.NoError(t, authErr)
		return issued, claims, reservation
	}

	first, firstClaims, firstReservation := issue("https-flv", 9)
	assertNativeGrantBinding(t, db, firstClaims, firstReservation.GrantID)
	var firstGrant models.PlayGrant
	require.NoError(t, db.First(&firstGrant, "grant_id = ?", firstReservation.GrantID).Error)
	require.Equal(t, time.Unix(firstClaims.IssuedAt, 0).UTC(), firstGrant.IssuedAt.UTC())
	require.Equal(t, time.Unix(firstClaims.ExpiresAt, 0).UTC(), firstGrant.ExpiresAt.UTC())
	require.Equal(t, "rtmp", *firstGrant.Schema)
	require.Equal(t, "https-flv", *firstGrant.Protocol)

	firstRequest := nativeGrantViewerBindRequest("native-first", "https-flv", 9)
	firstViewer, err := service.BindViewer(ctx, first.Token, firstRequest)
	require.NoError(t, err)
	require.Equal(t, models.ViewerStateActive, firstViewer.State)
	repeated, err := service.BindViewer(ctx, first.Token, firstRequest)
	require.NoError(t, err, "same native timestamp must be an idempotent no-op")
	require.Equal(t, firstViewer.ID, repeated.ID)

	wssGrant, wssClaims, wssReservation := issue("wss-flv", 10)
	assertNativeGrantBinding(t, db, wssClaims, wssReservation.GrantID)
	_, err = service.BindViewer(ctx, wssGrant.Token, nativeGrantViewerBindRequest("wss-wrong", "https-flv", 10))
	require.ErrorIs(t, err, playauth.ErrOpenAPIViewerDenied)
	wssViewer, err := service.BindViewer(ctx, wssGrant.Token, nativeGrantViewerBindRequest("wss-correct", "wss-flv", 10))
	require.NoError(t, err)
	require.Equal(t, models.ViewerStateActive, wssViewer.State)

	clock = clock.Add(playauth.OpenAPIPlayTTL + time.Second)
	recreated, err := playauth.NewOpenAPIGrantService(db, signer, authority, func() time.Time { return clock })
	require.NoError(t, err)
	reconnected, err := recreated.BindViewer(ctx, first.Token, firstRequest)
	require.NoError(t, err, "bound connection survives ordinary token TTL after service recreation")
	require.Equal(t, firstViewer.ID, reconnected.ID)
	_, err = recreated.BindViewer(ctx, first.Token, nativeGrantViewerBindRequest("native-second", "https-flv", 9))
	require.ErrorIs(t, err, playauth.ErrOpenAPIViewerDenied)

	concurrentGrant, _, _ := issue("https-flv", 12)
	requests := []playauth.OpenAPIViewerBindRequest{
		nativeGrantViewerBindRequest("native-race-a", "https-flv", 12),
		nativeGrantViewerBindRequest("native-race-b", "https-flv", 12),
	}
	var wait sync.WaitGroup
	errs := make(chan error, len(requests))
	for _, request := range requests {
		wait.Add(1)
		go func(request playauth.OpenAPIViewerBindRequest) {
			defer wait.Done()
			_, bindErr := recreated.BindViewer(ctx, concurrentGrant.Token, request)
			errs <- bindErr
		}(request)
	}
	wait.Wait()
	close(errs)
	success, denied := 0, 0
	for bindErr := range errs {
		switch {
		case bindErr == nil:
			success++
		case errors.Is(bindErr, playauth.ErrOpenAPIViewerDenied):
			denied++
		default:
			t.Fatalf("native concurrent bind failed with unexpected error class: %v", bindErr)
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, denied)
	var concurrentViewerCount int64
	require.NoError(t, db.Model(&models.Viewer{}).Where("grant_id = ?", concurrentGrant.AuthorizationGeneration).Count(&concurrentViewerCount).Error)
	require.Equal(t, int64(success), concurrentViewerCount)

	require.NoError(t, db.Model(&models.Client{}).Where("id = ?", fixture.client.ID).Update("auth_epoch", fixture.client.AuthEpoch+1).Error)
	_, err = recreated.BindViewer(ctx, first.Token, firstRequest)
	require.ErrorIs(t, err, playauth.ErrOpenAPIViewerDenied)

	t.Logf("%s native grant/viewer: quota→issue→bind, exact claim time/protocol round-trip, same-microsecond idempotency, recreated bound TTL recovery, protocol mismatch, epoch invalidation, and 2-way identifier race passed", db.Dialector.Name())
}

type nativeGrantViewerFixture struct {
	client            models.Client
	createdDepartment bool
	createdChannel    bool
	createdAuthority  bool
	departmentReady   bool
	channelReady      bool
	authorityReady    bool
	originalDeviceOK  bool
	originalNodeOK    bool
	originalDevice    nativeGrantViewerDeviceRow
	originalNode      nativeGrantViewerNodeRow
}

type nativeGrantViewerDeviceRow struct {
	DeviceID    string     `gorm:"column:device_id"`
	OwnerDeptID uint       `gorm:"column:owner_dept_id"`
	AccessEpoch int64      `gorm:"column:access_epoch"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
}

type nativeGrantViewerNodeRow struct {
	Revision                 int64      `gorm:"column:revision"`
	MediaServerUUID          string     `gorm:"column:media_server_uuid"`
	CurrentBootNonce         *string    `gorm:"column:current_boot_nonce"`
	RuntimeEpoch             int64      `gorm:"column:runtime_epoch"`
	RuntimeProtocolVersion   int64      `gorm:"column:runtime_protocol_version"`
	RuntimeConfirmedRevision int64      `gorm:"column:runtime_confirmed_revision"`
	RuntimeConfirmedAt       *time.Time `gorm:"column:runtime_confirmed_at"`
	RuntimeIdentityStatus    string     `gorm:"column:runtime_identity_status"`
}

func prepareNativeGrantViewerFixture(t *testing.T, db *gorm.DB) (nativeGrantViewerFixture, error) {
	t.Helper()
	fixture := nativeGrantViewerFixture{}
	if !db.Migrator().HasColumn("gb_device", "owner_dept_id") {
		if err := db.Exec("ALTER TABLE gb_device ADD owner_dept_id BIGINT NOT NULL DEFAULT 0").Error; err != nil {
			return fixture, err
		}
	}
	ensureNativeNodeRuntimeColumns(t, db)

	var err error
	fixture.createdDepartment, err = ensureNativeGrantViewerTable(db, "sys_department", nativeGrantViewerDepartmentDDL(db.Dialector.Name()))
	if err != nil {
		return fixture, err
	}
	fixture.departmentReady = true
	fixture.createdChannel, err = ensureNativeGrantViewerTable(db, "gb_channel", nativeGrantViewerChannelDDL(db.Dialector.Name()))
	if err != nil {
		return fixture, err
	}
	fixture.channelReady = true
	fixture.createdAuthority, err = ensureNativeGrantViewerTable(db, nativeGrantViewerQualificationTable, nativeGrantViewerQualificationDDL(db.Dialector.Name()))
	if err != nil {
		return fixture, err
	}
	fixture.authorityReady = true

	if err := db.Table("gb_device").Select("device_id, owner_dept_id, access_epoch, deleted_at").Where("id = ?", 1).Take(&fixture.originalDevice).Error; err != nil {
		return fixture, err
	}
	fixture.originalDeviceOK = true
	if err := db.Table("meta_node").Select("revision, media_server_uuid, current_boot_nonce, runtime_epoch, runtime_protocol_version, runtime_confirmed_revision, runtime_confirmed_at, runtime_identity_status").Where("id = ?", 1).Take(&fixture.originalNode).Error; err != nil {
		return fixture, err
	}
	fixture.originalNodeOK = true

	if err := db.Table("sys_department").Create(map[string]any{"id": int64(nativeGrantViewerDepartmentID), "status": 1, "deleted_at": nil}).Error; err != nil {
		return fixture, err
	}
	if err := db.Table("gb_device").Where("id = ?", 1).Updates(map[string]any{
		"device_id": nativeGrantViewerDeviceID, "owner_dept_id": nativeGrantViewerDepartmentID, "deleted_at": nil,
	}).Error; err != nil {
		return fixture, err
	}
	if err := db.Table("gb_channel").Create(map[string]any{
		"id": int64(2402), "channel_id": nativeGrantViewerChannelID, "device_id": nativeGrantViewerDeviceID,
		"name": "native grant viewer", "alias": "", "manufacturer": "fixture", "model": "fixture",
		"status": 1, "ptz_type": 0, "owner_dept_id": nativeGrantViewerDepartmentID, "deleted_at": nil,
	}).Error; err != nil {
		return fixture, err
	}
	nodeNow := time.Date(2026, 9, 6, 20, 30, 40, 123456, time.UTC)
	if err := db.Table("meta_node").Where("id = ?", 1).Updates(map[string]any{
		"revision": 1, "media_server_uuid": nativeGrantViewerNodeUUID, "current_boot_nonce": nativeGrantViewerBootNonce,
		"runtime_epoch": 1, "runtime_protocol_version": 1, "runtime_confirmed_revision": 1,
		"runtime_confirmed_at": nodeNow, "runtime_identity_status": "active",
	}).Error; err != nil {
		return fixture, err
	}
	for _, protocol := range []string{"https-flv", "wss-flv"} {
		if err := db.Table(nativeGrantViewerQualificationTable).Create(map[string]any{
			"node_uuid": nativeGrantViewerNodeUUID, "boot_nonce": nativeGrantViewerBootNonce,
			"protocol": protocol, "is_qualified": 1,
		}).Error; err != nil {
			return fixture, err
		}
	}

	baseNow := time.Date(2026, 9, 6, 20, 30, 40, 123456, time.UTC)
	client := models.Client{
		AK: "uvp_native_grant_viewer_01", Name: "native grant viewer", OwnerDeptID: nativeGrantViewerDepartmentID,
		Status: models.StatusActive, SecretCiphertext: []byte("native-ciphertext"), SecretIV: []byte("0123456789ab"),
		SecretKeyID: "native", SecretVersion: 1, AuthEpoch: 4, RateLimit: 10, Burst: 20, ViewerQuota: 10,
		RowVersion: 1, CreatedBy: 0, UpdatedBy: 0, CreatedAt: baseNow, UpdatedAt: baseNow,
	}
	if err := db.Create(&client).Error; err != nil {
		return fixture, err
	}
	fixture.client = client
	if err := db.Create(&models.ClientScope{ClientID: client.ID, Scope: limit.PlayLiveApplyScope, Enabled: true, ScopeEpoch: 6, UpdatedBy: 0, UpdatedAt: baseNow}).Error; err != nil {
		return fixture, err
	}
	return fixture, nil
}

func (fixture nativeGrantViewerFixture) cleanup(t *testing.T, root *gorm.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := root.WithContext(ctx)
	if fixture.client.ID > 0 {
		if err := db.Where("grant_id IN (?)", db.Model(&models.PlayGrant{}).Select("grant_id").Where("client_id = ?", fixture.client.ID)).Delete(&models.Viewer{}).Error; err != nil {
			t.Errorf("native grant/viewer viewer cleanup failed")
		}
		if err := db.Where("client_id = ?", fixture.client.ID).Delete(&models.PlayGrant{}).Error; err != nil {
			t.Errorf("native grant/viewer grant cleanup failed")
		}
		if err := db.Where("client_id = ?", fixture.client.ID).Delete(&models.ClientScope{}).Error; err != nil {
			t.Errorf("native grant/viewer scope cleanup failed")
		}
		if err := db.Where("id = ?", fixture.client.ID).Delete(&models.Client{}).Error; err != nil {
			t.Errorf("native grant/viewer client cleanup failed")
		}
	}
	if fixture.authorityReady && db.Exec("DELETE FROM "+nativeGrantViewerQualificationTable+" WHERE node_uuid = ? AND boot_nonce = ?", nativeGrantViewerNodeUUID, nativeGrantViewerBootNonce).Error != nil {
		t.Errorf("native grant/viewer authority cleanup failed")
	}
	if fixture.originalNodeOK {
		if err := db.Table("meta_node").Where("id = ?", 1).Updates(map[string]any{
			"revision": fixture.originalNode.Revision, "media_server_uuid": fixture.originalNode.MediaServerUUID,
			"current_boot_nonce": fixture.originalNode.CurrentBootNonce, "runtime_epoch": fixture.originalNode.RuntimeEpoch,
			"runtime_protocol_version":   fixture.originalNode.RuntimeProtocolVersion,
			"runtime_confirmed_revision": fixture.originalNode.RuntimeConfirmedRevision,
			"runtime_confirmed_at":       fixture.originalNode.RuntimeConfirmedAt,
			"runtime_identity_status":    fixture.originalNode.RuntimeIdentityStatus,
		}).Error; err != nil {
			t.Errorf("native grant/viewer node cleanup failed")
		}
	}
	if fixture.originalDeviceOK {
		if err := db.Table("gb_device").Where("id = ?", 1).Updates(map[string]any{
			"device_id": fixture.originalDevice.DeviceID, "owner_dept_id": fixture.originalDevice.OwnerDeptID,
			"access_epoch": fixture.originalDevice.AccessEpoch, "deleted_at": fixture.originalDevice.DeletedAt,
		}).Error; err != nil {
			t.Errorf("native grant/viewer device cleanup failed")
		}
	}
	if fixture.channelReady && db.Exec("DELETE FROM gb_channel WHERE id = ?", 2402).Error != nil {
		t.Errorf("native grant/viewer channel cleanup failed")
	}
	if fixture.departmentReady && db.Exec("DELETE FROM sys_department WHERE id = ?", int64(nativeGrantViewerDepartmentID)).Error != nil {
		t.Errorf("native grant/viewer department cleanup failed")
	}
	if fixture.createdAuthority {
		dropNativeGrantViewerTable(t, db, nativeGrantViewerQualificationTable)
	}
	if fixture.createdChannel {
		dropNativeGrantViewerTable(t, db, "gb_channel")
	}
	if fixture.createdDepartment {
		dropNativeGrantViewerTable(t, db, "sys_department")
	}
}

type nativeGrantViewerNodeAuthority struct{}

func (nativeGrantViewerNodeAuthority) AuthorizeOpenAPI(ctx context.Context, tx *gorm.DB, request playauth.OpenAPINodeAuthorization) error {
	if ctx == nil || tx == nil || request.NodeUUID == "" || request.BootNonce == "" || request.Protocol == "" {
		return playauth.ErrOpenAPIGrantUnavailable
	}
	var rows []struct {
		ID                       int64  `gorm:"column:id"`
		Revision                 int64  `gorm:"column:revision"`
		RuntimeEpoch             int64  `gorm:"column:runtime_epoch"`
		RuntimeProtocolVersion   int64  `gorm:"column:runtime_protocol_version"`
		RuntimeConfirmedRevision int64  `gorm:"column:runtime_confirmed_revision"`
		RuntimeIdentityStatus    string `gorm:"column:runtime_identity_status"`
	}
	result := tx.WithContext(ctx).Table("meta_node AS n").
		Joins("JOIN "+nativeGrantViewerQualificationTable+" AS q ON q.node_uuid = n.media_server_uuid AND q.boot_nonce = n.current_boot_nonce AND q.protocol = ? AND q.is_qualified = ?", request.Protocol, 1).
		Where("n.media_server_uuid = ? AND n.current_boot_nonce = ? AND n.runtime_identity_status = ? AND n.runtime_epoch > 0 AND n.runtime_protocol_version > 0 AND n.runtime_confirmed_revision = n.revision AND n.runtime_confirmed_at IS NOT NULL", request.NodeUUID, request.BootNonce, "active").
		Limit(2).Find(&rows)
	if result.Error != nil || result.RowsAffected != 1 || len(rows) != 1 || rows[0].ID <= 0 || rows[0].Revision <= 0 || rows[0].RuntimeEpoch <= 0 || rows[0].RuntimeProtocolVersion <= 0 || rows[0].RuntimeConfirmedRevision != rows[0].Revision || rows[0].RuntimeIdentityStatus != "active" {
		return playauth.ErrOpenAPIGrantUnavailable
	}
	return nil
}

func nativeGrantViewerIssueRequest(grantID, protocol string, generation uint64) playauth.OpenAPIGrantIssueRequest {
	return playauth.OpenAPIGrantIssueRequest{
		GrantID: grantID, DeviceID: nativeGrantViewerDeviceID, ChannelID: nativeGrantViewerChannelID,
		NodeUUID: nativeGrantViewerNodeUUID, BootNonce: nativeGrantViewerBootNonce, Schema: "rtmp",
		VHost: "__defaultVhost__", App: "rtp", Stream: nativeGrantViewerDeviceID + "_" + nativeGrantViewerChannelID,
		MediaGeneration: generation, Protocol: protocol,
	}
}

func nativeGrantViewerBindRequest(identifier, protocol string, generation uint64) playauth.OpenAPIViewerBindRequest {
	return playauth.OpenAPIViewerBindRequest{
		NodeUUID: nativeGrantViewerNodeUUID, BootNonce: nativeGrantViewerBootNonce, Identifier: identifier,
		Protocol: protocol, Schema: "rtmp", VHost: "__defaultVhost__", App: "rtp",
		Stream: nativeGrantViewerDeviceID + "_" + nativeGrantViewerChannelID, MediaGeneration: generation,
	}
}

func assertNativeGrantBinding(t *testing.T, db *gorm.DB, claims playauth.OpenAPIClaims, grantID string) {
	t.Helper()
	var grant models.PlayGrant
	require.NoError(t, db.First(&grant, "grant_id = ?", grantID).Error)
	require.Equal(t, models.GrantStateIssued, grant.State)
	require.Equal(t, claims.GrantID, grant.GrantID)
	require.Equal(t, claims.ClientID, grant.ClientID)
	require.Equal(t, claims.ClientEpoch, grant.ClientEpoch)
	require.Equal(t, claims.ScopeEpoch, grant.ScopeEpoch)
	require.Equal(t, claims.DeviceEpoch, grant.DeviceEpoch)
	require.NotNil(t, grant.Protocol)
	require.Equal(t, claims.Protocol, *grant.Protocol)
	require.Equal(t, time.Unix(claims.IssuedAt, 0).UTC(), grant.IssuedAt.UTC())
	require.Equal(t, time.Unix(claims.ExpiresAt, 0).UTC(), grant.ExpiresAt.UTC())
}

func ensureNativeGrantViewerTable(db *gorm.DB, table, ddl string) (bool, error) {
	if db.Migrator().HasTable(table) {
		return false, nil
	}
	return true, db.Exec(ddl).Error
}

func nativeGrantViewerTimestampType(dialect string) string {
	switch dialect {
	case "mysql":
		return "DATETIME(6)"
	case "postgres":
		return "TIMESTAMPTZ(6)"
	case "sqlserver":
		return "DATETIME2(6)"
	default:
		return ""
	}
}

func nativeGrantViewerDepartmentDDL(dialect string) string {
	return fmt.Sprintf("CREATE TABLE sys_department (id BIGINT NOT NULL PRIMARY KEY, status SMALLINT NOT NULL, deleted_at %s NULL)", nativeGrantViewerTimestampType(dialect))
}

func nativeGrantViewerChannelDDL(dialect string) string {
	ts := nativeGrantViewerTimestampType(dialect)
	return fmt.Sprintf("CREATE TABLE gb_channel (id BIGINT NOT NULL PRIMARY KEY, channel_id VARCHAR(20) NOT NULL, device_id VARCHAR(20) NOT NULL, name VARCHAR(255) NOT NULL DEFAULT '', alias VARCHAR(255) NOT NULL DEFAULT '', manufacturer VARCHAR(255) NOT NULL DEFAULT '', model VARCHAR(255) NOT NULL DEFAULT '', status SMALLINT NULL, ptz_type SMALLINT NOT NULL DEFAULT 0, owner_dept_id BIGINT NOT NULL, deleted_at %s NULL)", ts)
}

func nativeGrantViewerQualificationDDL(dialect string) string {
	bootType := "CHAR(32)"
	if dialect == "sqlserver" {
		bootType += " COLLATE Latin1_General_100_BIN2"
	}
	return "CREATE TABLE " + nativeGrantViewerQualificationTable + " (node_uuid VARCHAR(64) NOT NULL, boot_nonce " + bootType + " NOT NULL, protocol VARCHAR(16) NOT NULL, is_qualified SMALLINT NOT NULL, PRIMARY KEY (node_uuid, boot_nonce, protocol))"
}

func dropNativeGrantViewerTable(t *testing.T, db *gorm.DB, table string) {
	t.Helper()
	var err error
	if db.Dialector.Name() == "sqlserver" {
		err = db.Exec("IF OBJECT_ID(N'dbo." + table + "', N'U') IS NOT NULL DROP TABLE dbo." + table).Error
	} else {
		err = db.Exec("DROP TABLE IF EXISTS " + table).Error
	}
	if err != nil {
		t.Errorf("drop native grant/viewer fixture table failed")
	}
}
