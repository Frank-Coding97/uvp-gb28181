package videoshare

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"testing"
	"time"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	basemodels "uvplatform.com/uvp-gb28181/app/models"
)

func newAuditFixture(t *testing.T) (*Service, uint, time.Time) {
	t.Helper()
	s, channel, now := newServiceFixture(t)
	require.NoError(t, s.db.AutoMigrate(&basemodels.SysOperationLog{}))
	return s, channel, now
}

func TestVideoShareAuditUsesOnlySafeMetadataAndTransaction(t *testing.T) {
	s, channel, now := newAuditFixture(t)
	share := gbmodels.GbChannelVideoShare{ChannelPK: channel, PublicID: "public-credential-never-log", Generation: "generation-credential-never-log", KeyID: "key-credential-never-log", ExpiresAt: &now}
	ctx := WithAuditTraceID(context.Background(), "trusted-request-42")
	require.Error(t, RecordAuditTx(ctx, s.db, "video_share.enabled", 9, &share))
	require.NoError(t, s.db.Transaction(func(tx *gorm.DB) error { return RecordAuditTx(ctx, tx, "video_share.enabled", 9, &share) }))
	var logs []basemodels.SysOperationLog
	require.NoError(t, s.db.Find(&logs).Error)
	require.Len(t, logs, 1)
	var data map[string]any
	require.NoError(t, json.Unmarshal([]byte(logs[0].RequestData), &data))
	require.Len(t, data, 7)
	require.Equal(t, "video_share.enabled", data["event"])
	require.EqualValues(t, 9, data["operator_id"])
	require.EqualValues(t, channel, data["channel_row_id"])
	require.Equal(t, "trusted-request-42", data["trace_id"])
	require.Equal(t, "success", data["result"])
	require.Equal(t, now.Format(time.RFC3339Nano), data["expires_at"])
	digest := sha256.Sum256([]byte(share.Generation))
	require.Equal(t, hex.EncodeToString(digest[:8]), data["generation"])
	require.EqualValues(t, 9, logs[0].UserID)
	require.Empty(t, logs[0].ResponseData)
	encoded, err := json.Marshal(logs[0])
	require.NoError(t, err)
	for _, secret := range []string{share.PublicID, share.Generation, share.KeyID} {
		require.NotContains(t, string(encoded), secret)
	}
	for _, invalid := range []string{"", "viewer-token", "video_share.enabled secret"} {
		require.Error(t, s.db.Transaction(func(tx *gorm.DB) error { return RecordAuditTx(ctx, tx, invalid, 9, &share) }))
	}
}

func TestVideoShareScanExpiredBoundaryAndIdempotency(t *testing.T) {
	s, channel, now := newAuditFixture(t)
	link, err := s.SetEnabled(context.Background(), channel, 9, true, 0)
	require.NoError(t, err)
	second := gbmodels.GbChannel{ChannelID: "not-expired", DeviceID: "34020000002000000001"}
	third := gbmodels.GbChannel{ChannelID: "pending", DeviceID: "34020000002000000001"}
	require.NoError(t, s.db.Create(&second).Error)
	require.NoError(t, s.db.Create(&third).Error)
	_, err = s.SetEnabled(context.Background(), second.ID, 9, true, 0)
	require.NoError(t, err)
	require.NoError(t, s.EnsurePending(context.Background(), third.ID, 1, second.DeviceID, 7, nil, 1))
	boundary := now.Add(5 * time.Minute)
	require.NoError(t, s.db.Model(&gbmodels.GbChannelVideoShare{}).Where("channel_pk = ?", second.ID).Update("expires_at", boundary.Add(time.Nanosecond)).Error)
	s.now = func() time.Time { return boundary }
	count, err := s.ScanExpired(context.Background(), 100)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	var retired gbmodels.GbChannelVideoShare
	require.NoError(t, s.db.Where("channel_pk = ?", channel).Take(&retired).Error)
	require.False(t, retired.Enabled)
	require.Equal(t, "expired", retired.RevokeReason)
	require.Equal(t, boundary, *retired.RevokedAt)
	require.Equal(t, link.Revision+1, retired.Revision)
	var active []gbmodels.GbChannelVideoShare
	require.NoError(t, s.db.Where("channel_pk IN ?", []uint{second.ID, third.ID}).Find(&active).Error)
	require.Len(t, active, 2)
	for _, share := range active {
		require.True(t, share.Enabled)
	}
	count, err = s.ScanExpired(context.Background(), 100)
	require.NoError(t, err)
	require.Zero(t, count)
	var logs []basemodels.SysOperationLog
	require.NoError(t, s.db.Where("operation = ?", "video_share.expired").Find(&logs).Error)
	require.Len(t, logs, 1)
	require.Contains(t, logs[0].RequestData, `"event":"video_share.expired"`)
	_, err = s.Resolve(context.Background(), link.PublicID, link.Signature)
	require.ErrorIs(t, err, ErrExpired)
}

func TestVideoShareScanExpiredAuditFailureRollsBackAndRetries(t *testing.T) {
	s, channel, now := newAuditFixture(t)
	link, err := s.SetEnabled(context.Background(), channel, 9, true, 0)
	require.NoError(t, err)
	s.now = func() time.Time { return now.Add(5 * time.Minute) }
	boom := errors.New("audit storage unavailable")
	require.NoError(t, s.db.Callback().Create().Before("gorm:create").Register("test:fail_expiry_audit", func(tx *gorm.DB) {
		if tx.Statement.Table == "sys_operation_logs" {
			tx.AddError(boom)
		}
	}))
	count, err := s.ScanExpired(context.Background(), 100)
	require.ErrorIs(t, err, boom)
	require.Zero(t, count)
	var share gbmodels.GbChannelVideoShare
	require.NoError(t, s.db.Where("channel_pk = ?", channel).Take(&share).Error)
	require.True(t, share.Enabled)
	require.Nil(t, share.RevokedAt)
	require.Equal(t, link.Revision, share.Revision)
	require.NoError(t, s.db.Callback().Create().Remove("test:fail_expiry_audit"))
	count, err = s.ScanExpired(context.Background(), 100)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestVideoShareScanExpiredDoesNotRetireRegeneratedGeneration(t *testing.T) {
	s, channel, now := newAuditFixture(t)
	old, err := s.SetEnabled(context.Background(), channel, 9, true, 0)
	require.NoError(t, err)
	s.now = func() time.Time { return now.Add(6 * time.Minute) }
	newLink, err := s.Regenerate(context.Background(), channel, 9, old.Revision)
	require.NoError(t, err)
	count, err := s.ScanExpired(context.Background(), 100)
	require.NoError(t, err)
	require.Zero(t, count)
	_, err = s.Resolve(context.Background(), newLink.PublicID, newLink.Signature)
	require.NoError(t, err)
	_, err = s.Resolve(context.Background(), old.PublicID, old.Signature)
	require.ErrorIs(t, err, ErrInvalidCredential)
}

func TestVideoShareScanExpiredBoundsBatchesAndRequiresAuthorityGate(t *testing.T) {
	s, channel, now := newAuditFixture(t)
	_, err := s.SetEnabled(context.Background(), channel, 9, true, 0)
	require.NoError(t, err)
	second := gbmodels.GbChannel{ChannelID: "second", DeviceID: "34020000002000000001"}
	require.NoError(t, s.db.Create(&second).Error)
	_, err = s.SetEnabled(context.Background(), second.ID, 9, true, 0)
	require.NoError(t, err)
	s.now = func() time.Time { return now.Add(5 * time.Minute) }
	count, err := s.ScanExpired(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	count, err = s.ScanExpired(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.NoError(t, s.db.Exec("DELETE FROM gb_video_share_authority").Error)
	_, err = s.ScanExpired(context.Background(), 1)
	require.Error(t, err)
}
