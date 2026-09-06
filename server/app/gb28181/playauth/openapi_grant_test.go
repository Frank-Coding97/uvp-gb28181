package playauth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"uvplatform.cn/uvp-gb28181/app/openapi/limit"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

const (
	testClientID  int64 = 7
	testDeptID          = 17
	testDeviceID        = "34020000001320000001"
	testChannelID       = "34020000001310000001"
	testNodeUUID        = "node-openapi-a"
	testBootNonce       = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func TestOpenAPIGrantIssuePersistsV3BindingAndRejectsUnqualifiedNode(t *testing.T) {
	fixture := newOpenAPIGrantFixture(t)
	defer fixture.close(t)

	quota := limit.NewQuota(fixture.db, func() time.Time { return fixture.now })
	reservation, err := quota.ReservePending(context.Background(), limit.ReservationRequest{
		ClientID: testClientID, Scope: limit.PlayLiveApplyScope, DeviceID: testDeviceID, ChannelID: testChannelID,
	})
	require.NoError(t, err)

	service, err := NewOpenAPIGrantService(fixture.db, fixture.signer, fixture.authority, func() time.Time { return fixture.now })
	require.NoError(t, err)
	issued, err := service.Issue(context.Background(), OpenAPIGrantIssueRequest{
		GrantID: reservation.GrantID, DeviceID: testDeviceID, ChannelID: testChannelID, NodeUUID: testNodeUUID, BootNonce: testBootNonce,
		Schema: "https", VHost: "__defaultVhost__", App: "rtp", Stream: testDeviceID + "_" + testChannelID,
		MediaGeneration: 9, Protocol: "https-flv",
	})
	require.NoError(t, err)
	require.NotEmpty(t, issued.Token)
	require.Equal(t, reservation.GrantID, issued.AuthorizationGeneration)

	claims, err := fixture.signer.AuthenticateOpenAPI(issued.Token)
	require.NoError(t, err)
	require.Equal(t, reservation.GrantID, claims.GrantID)
	require.Equal(t, testClientID, claims.ClientID)
	require.Equal(t, int64(2), claims.ClientEpoch)
	require.Equal(t, int64(3), claims.ScopeEpoch)
	require.Equal(t, int64(4), claims.DeviceEpoch)
	require.Equal(t, "https-flv", claims.Protocol)

	var grant models.PlayGrant
	require.NoError(t, fixture.db.First(&grant, "grant_id = ?", reservation.GrantID).Error)
	require.Equal(t, models.GrantStateIssued, grant.State)
	require.Equal(t, claims.IssuedAt, grant.IssuedAt.Unix())
	require.Equal(t, claims.ExpiresAt, grant.ExpiresAt.Unix())
	require.Equal(t, claims.GrantID, grant.GrantID)
	require.Equal(t, claims.ClientEpoch, grant.ClientEpoch)
	require.Equal(t, claims.ScopeEpoch, grant.ScopeEpoch)
	require.Equal(t, claims.DeviceEpoch, grant.DeviceEpoch)
	require.Equal(t, claims.DeviceID, *grant.DeviceID)
	require.Equal(t, claims.ChannelID, *grant.ChannelID)
	require.Equal(t, claims.NodeUUID, *grant.NodeUUID)
	require.Equal(t, claims.BootNonce, *grant.BootNonce)
	require.Equal(t, claims.Schema, *grant.Schema)
	require.Equal(t, claims.VHost, *grant.VHost)
	require.Equal(t, claims.App, *grant.App)
	require.Equal(t, claims.Stream, *grant.Stream)
	require.Equal(t, claims.MediaGeneration, *grant.MediaGeneration)
	require.Equal(t, claims.Protocol, *grant.Protocol)

	fixture.setNodeUnknown(t)
	reservation2, err := quota.ReservePending(context.Background(), limit.ReservationRequest{
		ClientID: testClientID, Scope: limit.PlayLiveApplyScope, DeviceID: testDeviceID, ChannelID: testChannelID,
	})
	require.NoError(t, err)
	_, err = service.Issue(context.Background(), OpenAPIGrantIssueRequest{
		GrantID: reservation2.GrantID, DeviceID: testDeviceID, ChannelID: testChannelID, NodeUUID: testNodeUUID, BootNonce: testBootNonce,
		Schema: "https", VHost: "__defaultVhost__", App: "rtp", Stream: testDeviceID + "_" + testChannelID,
		MediaGeneration: 10, Protocol: "https-flv",
	})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrOpenAPIGrantUnavailable)
	var rejected models.PlayGrant
	require.NoError(t, fixture.db.First(&rejected, "grant_id = ?", reservation2.GrantID).Error)
	require.Equal(t, models.GrantStatePending, rejected.State, "failed issue must not partially publish a grant")
}

func TestOpenAPIGrantIssueRequiresExactPendingTargetAndCurrentEpochs(t *testing.T) {
	fixture := newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	service, err := NewOpenAPIGrantService(fixture.db, fixture.signer, fixture.authority, func() time.Time { return fixture.now })
	require.NoError(t, err)

	quota := limit.NewQuota(fixture.db, func() time.Time { return fixture.now })
	reservation, err := quota.ReservePending(context.Background(), limit.ReservationRequest{
		ClientID: testClientID, Scope: limit.PlayLiveApplyScope, DeviceID: testDeviceID, ChannelID: testChannelID,
	})
	require.NoError(t, err)
	_, err = service.Issue(context.Background(), OpenAPIGrantIssueRequest{
		GrantID: reservation.GrantID, DeviceID: testDeviceID, ChannelID: "34020000001310000002", NodeUUID: testNodeUUID, BootNonce: testBootNonce,
		Schema: "https", VHost: "__defaultVhost__", App: "rtp", Stream: "wrong-stream",
		MediaGeneration: 9, Protocol: "https-flv",
	})
	require.ErrorIs(t, err, ErrOpenAPIGrantDenied)

	var pending models.PlayGrant
	require.NoError(t, fixture.db.First(&pending, "grant_id = ?", reservation.GrantID).Error)
	require.Equal(t, models.GrantStatePending, pending.State)

	require.NoError(t, fixture.db.Model(&models.Client{}).Where("id = ?", testClientID).Update("auth_epoch", 99).Error)
	_, err = service.Issue(context.Background(), OpenAPIGrantIssueRequest{
		GrantID: reservation.GrantID, DeviceID: testDeviceID, ChannelID: testChannelID, NodeUUID: testNodeUUID, BootNonce: testBootNonce,
		Schema: "https", VHost: "__defaultVhost__", App: "rtp", Stream: testDeviceID + "_" + testChannelID,
		MediaGeneration: 9, Protocol: "https-flv",
	})
	require.ErrorIs(t, err, ErrOpenAPIGrantDenied)
}

func TestOpenAPIGrantIssueRollbackReturnsZeroTokenWhenGrantWriteFails(t *testing.T) {
	fixture := newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	quota := limit.NewQuota(fixture.db, func() time.Time { return fixture.now })
	reservation, err := quota.ReservePending(context.Background(), limit.ReservationRequest{
		ClientID: testClientID, Scope: limit.PlayLiveApplyScope, DeviceID: testDeviceID, ChannelID: testChannelID,
	})
	require.NoError(t, err)
	require.NoError(t, fixture.db.Exec("CREATE TRIGGER reject_openapi_grant_issue BEFORE UPDATE ON gb_openapi_play_grant BEGIN SELECT RAISE(ABORT, 'fixture rejection'); END").Error)

	service, err := NewOpenAPIGrantService(fixture.db, fixture.signer, fixture.authority, func() time.Time { return fixture.now })
	require.NoError(t, err)
	issued, err := service.Issue(context.Background(), OpenAPIGrantIssueRequest{
		GrantID: reservation.GrantID, DeviceID: testDeviceID, ChannelID: testChannelID, NodeUUID: testNodeUUID, BootNonce: testBootNonce,
		Schema: "https", VHost: "__defaultVhost__", App: "rtp", Stream: testDeviceID + "_" + testChannelID,
		MediaGeneration: 9, Protocol: "https-flv",
	})
	require.Error(t, err)
	require.Empty(t, issued.Token)
	require.Empty(t, issued.AuthorizationGeneration)
	var grant models.PlayGrant
	require.NoError(t, fixture.db.First(&grant, "grant_id = ?", reservation.GrantID).Error)
	require.Equal(t, models.GrantStatePending, grant.State)
}

func TestOpenAPIGrantIssueRejectsMissingMandatoryNodeAuthority(t *testing.T) {
	fixture := newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	_, err := NewOpenAPIGrantService(fixture.db, fixture.signer, nil, func() time.Time { return fixture.now })
	require.ErrorIs(t, err, ErrOpenAPIGrantUnavailable)
}

func TestOpenAPIGrantIssueRequiresCurrentOwnerAndDepartment(t *testing.T) {
	fixture := newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	service, err := NewOpenAPIGrantService(fixture.db, fixture.signer, fixture.authority, func() time.Time { return fixture.now })
	require.NoError(t, err)
	quota := limit.NewQuota(fixture.db, func() time.Time { return fixture.now })
	reservation, err := quota.ReservePending(context.Background(), limit.ReservationRequest{
		ClientID: testClientID, Scope: limit.PlayLiveApplyScope, DeviceID: testDeviceID, ChannelID: testChannelID,
	})
	require.NoError(t, err)
	require.NoError(t, fixture.db.Model(&departmentRow{}).Where("id = ?", testDeptID).Update("status", 0).Error)
	_, err = service.Issue(context.Background(), OpenAPIGrantIssueRequest{
		GrantID: reservation.GrantID, DeviceID: testDeviceID, ChannelID: testChannelID, NodeUUID: testNodeUUID, BootNonce: testBootNonce,
		Schema: "https", VHost: "__defaultVhost__", App: "rtp", Stream: testDeviceID + "_" + testChannelID,
		MediaGeneration: 9, Protocol: "https-flv",
	})
	require.ErrorIs(t, err, ErrOpenAPIGrantDenied)
}

func TestOpenAPIGrantIssueFailsClosedWhenResourceSQLDependencyIsMissing(t *testing.T) {
	fixture := newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	quota := limit.NewQuota(fixture.db, func() time.Time { return fixture.now })
	reservation, err := quota.ReservePending(context.Background(), limit.ReservationRequest{ClientID: testClientID, Scope: limit.PlayLiveApplyScope, DeviceID: testDeviceID, ChannelID: testChannelID})
	require.NoError(t, err)
	require.NoError(t, fixture.db.Exec("DROP TABLE gb_channel").Error)
	service, err := NewOpenAPIGrantService(fixture.db, fixture.signer, fixture.authority, func() time.Time { return fixture.now })
	require.NoError(t, err)
	_, err = service.Issue(context.Background(), OpenAPIGrantIssueRequest{
		GrantID: reservation.GrantID, DeviceID: testDeviceID, ChannelID: testChannelID, NodeUUID: testNodeUUID, BootNonce: testBootNonce,
		Schema: "https", VHost: "__defaultVhost__", App: "rtp", Stream: testDeviceID + "_" + testChannelID, MediaGeneration: 9, Protocol: "https-flv",
	})
	require.ErrorIs(t, err, ErrOpenAPIGrantUnavailable)
}

type openAPIGrantFixture struct {
	db        *gorm.DB
	sqlDB     interface{ Close() error }
	signer    *Signer
	authority OpenAPINodeAuthority
	now       time.Time
}

type departmentRow struct {
	ID        uint `gorm:"column:id;primaryKey"`
	Status    int  `gorm:"column:status"`
	DeletedAt *time.Time
}

func (departmentRow) TableName() string { return "sys_department" }

func newOpenAPIGrantFixture(t *testing.T) *openAPIGrantFixture {
	t.Helper()
	path := fmt.Sprintf("%s/openapi-grant-%d.sqlite", t.TempDir(), time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(path+"?_busy_timeout=5000"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(8)
	require.NoError(t, db.Exec("PRAGMA foreign_keys = ON").Error)
	require.NoError(t, db.AutoMigrate(&models.Client{}, &models.ClientScope{}, &models.PlayGrant{}, &models.Viewer{}))
	createOpenAPIResourceFixtureTables(t, db)
	now := time.Date(2026, 9, 6, 1, 0, 0, 123456000, time.UTC)
	require.NoError(t, db.Create(&departmentRow{ID: testDeptID, Status: 1}).Error)
	require.NoError(t, db.Exec("INSERT INTO gb_device (id, device_id, owner_dept_id, access_epoch, deleted_at) VALUES (?, ?, ?, ?, NULL)", 11, testDeviceID, testDeptID, 4).Error)
	require.NoError(t, db.Exec("INSERT INTO gb_channel (id, device_id, channel_id, owner_dept_id, deleted_at) VALUES (?, ?, ?, ?, NULL)", 12, testDeviceID, testChannelID, testDeptID).Error)
	require.NoError(t, db.Exec("INSERT INTO meta_node (id, revision, media_server_uuid, current_boot_nonce, runtime_epoch, runtime_protocol_version, runtime_confirmed_revision, runtime_confirmed_at, runtime_identity_status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)", 3, 9, testNodeUUID, testBootNonce, 1, 1, 9, now, "active").Error)
	require.NoError(t, db.Create(&models.Client{ID: testClientID, AK: "uvp_test_client", Name: "test", OwnerDeptID: testDeptID, Status: models.StatusActive, SecretCiphertext: []byte("ciphertext"), SecretIV: []byte("0123456789ab"), SecretKeyID: "fixture", SecretVersion: 1, AuthEpoch: 2, ViewerQuota: 10, RateLimit: 10, Burst: 20, RowVersion: 1, CreatedAt: now, UpdatedAt: now}).Error)
	require.NoError(t, db.Create(&models.ClientScope{ClientID: testClientID, Scope: limit.PlayLiveApplyScope, Enabled: true, ScopeEpoch: 3, UpdatedAt: now}).Error)
	signer, err := NewSigner([]byte(strings.Repeat("s", 32)), WithNow(func() time.Time { return now }))
	require.NoError(t, err)
	return &openAPIGrantFixture{db: db, sqlDB: sqlDB, signer: signer, authority: fixtureNodeAuthority{}, now: now}
}

// fixtureNodeAuthority is intentionally test-only. It proves the service calls
// a transaction-bound SQL authority; production still has no qualified-node
// fallback until T09 establishes the topology/credential contract.
type fixtureNodeAuthority struct{}

func (fixtureNodeAuthority) AuthorizeOpenAPI(ctx context.Context, tx *gorm.DB, request OpenAPINodeAuthorization) error {
	if ctx == nil || tx == nil || request.NodeUUID == "" || request.BootNonce == "" {
		return ErrOpenAPIGrantUnavailable
	}
	var rows []struct {
		ID                       int64      `gorm:"column:id"`
		Revision                 uint64     `gorm:"column:revision"`
		MediaServerUUID          string     `gorm:"column:media_server_uuid"`
		CurrentBootNonce         string     `gorm:"column:current_boot_nonce"`
		RuntimeProtocolVersion   int64      `gorm:"column:runtime_protocol_version"`
		RuntimeConfirmedRevision uint64     `gorm:"column:runtime_confirmed_revision"`
		RuntimeConfirmedAt       *time.Time `gorm:"column:runtime_confirmed_at"`
		RuntimeIdentityStatus    string     `gorm:"column:runtime_identity_status"`
	}
	result := tx.WithContext(ctx).Table("meta_node").
		Where("media_server_uuid = ? AND current_boot_nonce = ?", request.NodeUUID, request.BootNonce).
		Limit(2).Find(&rows)
	if result.Error != nil || result.RowsAffected != 1 || len(rows) != 1 || rows[0].ID <= 0 || rows[0].Revision == 0 || rows[0].RuntimeProtocolVersion != 1 || rows[0].RuntimeConfirmedRevision != rows[0].Revision || rows[0].RuntimeConfirmedAt == nil || rows[0].RuntimeIdentityStatus != "active" {
		return ErrOpenAPIGrantUnavailable
	}
	return nil
}

func (f *openAPIGrantFixture) close(t *testing.T) {
	t.Helper()
	if f != nil && f.sqlDB != nil {
		require.NoError(t, f.sqlDB.Close())
	}
}

func (f *openAPIGrantFixture) setNodeUnknown(t *testing.T) {
	t.Helper()
	require.NoError(t, f.db.Table("meta_node").Where("id = ?", 3).Update("runtime_identity_status", "unknown").Error)
}

func createOpenAPIResourceFixtureTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, statement := range []string{
		`CREATE TABLE sys_department (id INTEGER PRIMARY KEY, status INTEGER NOT NULL, deleted_at DATETIME NULL)`,
		`CREATE TABLE gb_device (id INTEGER PRIMARY KEY, device_id VARCHAR(20) NOT NULL, owner_dept_id INTEGER NOT NULL, name VARCHAR(100) NOT NULL DEFAULT '', alias VARCHAR(100) NOT NULL DEFAULT '', manufacturer VARCHAR(100) NOT NULL DEFAULT '', model VARCHAR(100) NOT NULL DEFAULT '', status INTEGER NULL, access_epoch INTEGER NOT NULL DEFAULT 1, deleted_at DATETIME NULL)`,
		`CREATE TABLE gb_channel (id INTEGER PRIMARY KEY, device_id VARCHAR(20) NOT NULL, channel_id VARCHAR(20) NOT NULL, owner_dept_id INTEGER NOT NULL, name VARCHAR(100) NOT NULL DEFAULT '', alias VARCHAR(100) NOT NULL DEFAULT '', manufacturer VARCHAR(100) NOT NULL DEFAULT '', model VARCHAR(100) NOT NULL DEFAULT '', status INTEGER NULL, ptz_type INTEGER NOT NULL DEFAULT 0, deleted_at DATETIME NULL)`,
		`CREATE TABLE meta_node (id INTEGER PRIMARY KEY, revision INTEGER NOT NULL, media_server_uuid VARCHAR(64) NOT NULL, current_boot_nonce VARCHAR(32), runtime_epoch INTEGER NOT NULL, runtime_protocol_version INTEGER NOT NULL, runtime_confirmed_revision INTEGER NOT NULL, runtime_confirmed_at DATETIME NULL, runtime_identity_status VARCHAR(16) NOT NULL)`,
	} {
		require.NoError(t, db.Exec(statement).Error)
	}
}

func requireGrantState(t *testing.T, db *gorm.DB, grantID string, state models.GrantState) models.PlayGrant {
	t.Helper()
	var grant models.PlayGrant
	require.NoError(t, db.First(&grant, "grant_id = ?", grantID).Error)
	require.Equal(t, state, grant.State)
	return grant
}

func TestOpenAPIGrantHelpersRejectMalformedInput(t *testing.T) {
	fixture := newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	service, err := NewOpenAPIGrantService(fixture.db, fixture.signer, fixture.authority, func() time.Time { return fixture.now })
	require.NoError(t, err)
	_, err = service.Issue(nil, OpenAPIGrantIssueRequest{})
	require.Error(t, err)
	_, err = service.Issue(context.Background(), OpenAPIGrantIssueRequest{GrantID: uuid.Nil.String(), DeviceID: testDeviceID, ChannelID: testChannelID, NodeUUID: testNodeUUID, BootNonce: testBootNonce, Schema: "https", VHost: "v", App: "a", Stream: "s", MediaGeneration: 1, Protocol: "ftp"})
	require.Error(t, err)
	require.NotErrorIs(t, err, context.Canceled)
}

func TestOpenAPIGrantErrorClassesDoNotExposeDatabaseDetails(t *testing.T) {
	fixture := newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	service, err := NewOpenAPIGrantService(fixture.db, fixture.signer, fixture.authority, func() time.Time { return fixture.now })
	require.NoError(t, err)
	_, err = service.Issue(context.Background(), OpenAPIGrantIssueRequest{GrantID: "not-a-uuid"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "sqlite")
	require.False(t, errors.Is(err, context.Canceled))
}
