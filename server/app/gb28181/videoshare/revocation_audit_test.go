package videoshare

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	basemodels "uvplatform.com/uvp-gb28181/app/models"
)

func revocationAuditFixture(t *testing.T) (*Service, uint, Link) {
	t.Helper()
	s, channel, _ := newAuditFixture(t)
	link, err := s.SetEnabled(context.Background(), channel, 9, true, 0)
	require.NoError(t, err)
	require.NoError(t, s.db.AutoMigrate(&basemodels.User{}))
	return s, channel, link
}

func TestVideoShareCreatorRevocationAuditIsPerShareAndIdempotent(t *testing.T) {
	s, channel, link := revocationAuditFixture(t)
	other := gbmodels.GbChannelVideoShare{ChannelPK: channel + 1, PublicID: "other-public", Generation: "other-generation", Enabled: true, CreatedBy: ptrUint(10), DevicePK: 1, DeviceID: "34020000002000000001", Source: "manual", Revision: 1}
	require.NoError(t, s.db.Create(&other).Error)
	ctx := WithAuditTraceID(context.Background(), "revocation-trace")
	for i := 0; i < 2; i++ {
		require.NoError(t, s.db.Transaction(func(tx *gorm.DB) error { return RevokeCreatorTx(ctx, tx, 9, "account_disabled", s.now()) }))
	}
	var logs []basemodels.SysOperationLog
	require.NoError(t, s.db.Where("operation = ?", "video_share.revoked").Find(&logs).Error)
	require.Len(t, logs, 1)
	require.Zero(t, logs[0].UserID, "creator is not the administrator executing revocation")
	var metadata map[string]any
	require.NoError(t, json.Unmarshal([]byte(logs[0].RequestData), &metadata))
	require.Equal(t, "revocation-trace", metadata["trace_id"])
	require.EqualValues(t, 0, metadata["operator_id"])
	require.EqualValues(t, channel, metadata["channel_row_id"])
	require.NotContains(t, logs[0].RequestData, link.Generation)
	require.NotContains(t, logs[0].RequestData, link.PublicID)
	require.NoError(t, s.db.Where("id = ?", other.ID).Take(&other).Error)
	require.True(t, other.Enabled)
}

func TestVideoShareUnauthorizedRevocationAuditIsPerShareAndIdempotent(t *testing.T) {
	s, _, _ := revocationAuditFixture(t)
	// Missing creator is an authority loss; no policy fixture is needed.
	check := func(context.Context, uint, string, string) (bool, error) { return false, nil }
	for i := 0; i < 2; i++ {
		require.NoError(t, s.db.Transaction(func(tx *gorm.DB) error {
			return RevokeUnauthorizedSharesTx(context.Background(), tx, check, []uint{9}, "permission_removed", s.now())
		}))
	}
	var logs []basemodels.SysOperationLog
	require.NoError(t, s.db.Where("operation = ?", "video_share.revoked").Find(&logs).Error)
	require.Len(t, logs, 1)
	require.Zero(t, logs[0].UserID)
}

func TestVideoShareRevocationAuditFailureRollsBackAndCanRetry(t *testing.T) {
	for _, kind := range []string{"creator", "unauthorized"} {
		t.Run(kind, func(t *testing.T) {
			s, channel, link := revocationAuditFixture(t)
			boom := errors.New("revocation audit unavailable")
			require.NoError(t, s.db.Callback().Create().Before("gorm:create").Register("test:revocation_audit_failure", func(tx *gorm.DB) {
				if tx.Statement.Table == "sys_operation_logs" {
					tx.AddError(boom)
				}
			}))
			revoke := func(tx *gorm.DB) error {
				if kind == "creator" {
					return RevokeCreatorTx(context.Background(), tx, 9, "", time.Time{})
				}
				return RevokeUnauthorizedSharesTx(context.Background(), tx, func(context.Context, uint, string, string) (bool, error) { return false, nil }, []uint{9}, "", time.Time{})
			}
			require.ErrorIs(t, s.db.Transaction(revoke), boom)
			var share gbmodels.GbChannelVideoShare
			require.NoError(t, s.db.Where("channel_pk = ?", channel).Take(&share).Error)
			require.True(t, share.Enabled)
			require.Nil(t, share.RevokedAt)
			require.Equal(t, link.Revision, share.Revision)
			require.NoError(t, s.db.Callback().Create().Remove("test:revocation_audit_failure"))
			require.NoError(t, s.db.Transaction(revoke))
			var count int64
			require.NoError(t, s.db.Model(&basemodels.SysOperationLog{}).Where("operation = ?", "video_share.revoked").Count(&count).Error)
			require.EqualValues(t, 1, count)
		})
	}
}

func TestVideoShareCreatorRevocationAuditHandlesMultipleBatches(t *testing.T) {
	s, channel, _ := revocationAuditFixture(t)
	var shares []gbmodels.GbChannelVideoShare
	for i := 0; i < 101; i++ {
		shares = append(shares, gbmodels.GbChannelVideoShare{ChannelPK: channel + uint(i) + 1, PublicID: "public-" + time.Duration(i).String(), Generation: "generation-" + time.Duration(i).String(), Enabled: true, CreatedBy: ptrUint(9), DevicePK: 1, DeviceID: "34020000002000000001", Source: "manual", Revision: 1})
	}
	require.NoError(t, s.db.Create(&shares).Error)
	require.NoError(t, s.db.Transaction(func(tx *gorm.DB) error { return RevokeCreatorTx(context.Background(), tx, 9, "disabled", time.Time{}) }))
	var count int64
	require.NoError(t, s.db.Model(&basemodels.SysOperationLog{}).Where("operation = ?", "video_share.revoked").Count(&count).Error)
	require.EqualValues(t, 102, count)
}

func ptrUint(v uint) *uint { return &v }

func TestVideoShareRevocationAuditLaterFailureRollsBackEarlierAudit(t *testing.T) {
	s, channel, link := revocationAuditFixture(t)
	second := gbmodels.GbChannelVideoShare{ChannelPK: channel + 1, PublicID: "second-public", Generation: "second-generation", Enabled: true, CreatedBy: ptrUint(9), DevicePK: 1, DeviceID: "34020000002000000001", Source: "manual", Revision: 1}
	require.NoError(t, s.db.Create(&second).Error)
	writes := 0
	boom := errors.New("second audit fails")
	require.NoError(t, s.db.Callback().Create().Before("gorm:create").Register("test:later_revocation_audit_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "sys_operation_logs" {
			writes++
			if writes == 2 {
				tx.AddError(boom)
			}
		}
	}))
	t.Cleanup(func() { _ = s.db.Callback().Create().Remove("test:later_revocation_audit_failure") })
	err := s.db.Transaction(func(tx *gorm.DB) error { return RevokeCreatorTx(context.Background(), tx, 9, "disabled", time.Time{}) })
	require.ErrorIs(t, err, boom)
	var logs int64
	require.NoError(t, s.db.Model(&basemodels.SysOperationLog{}).Where("operation = ?", "video_share.revoked").Count(&logs).Error)
	require.Zero(t, logs)
	var shares []gbmodels.GbChannelVideoShare
	require.NoError(t, s.db.Order("id").Find(&shares).Error)
	require.Len(t, shares, 2)
	for _, share := range shares {
		require.True(t, share.Enabled)
		require.Nil(t, share.RevokedAt)
	}
	require.Equal(t, link.Revision, shares[0].Revision)
}

func TestVideoShareRevocationAuditRejectsNonTransactionBeforeMutation(t *testing.T) {
	for _, kind := range []string{"creator", "unauthorized"} {
		t.Run(kind, func(t *testing.T) {
			s, channel, link := revocationAuditFixture(t)
			var err error
			if kind == "creator" {
				err = RevokeCreatorTx(context.Background(), s.db, 9, "disabled", time.Time{})
			} else {
				err = RevokeUnauthorizedSharesTx(context.Background(), s.db, func(context.Context, uint, string, string) (bool, error) { return false, nil }, []uint{9}, "disabled", time.Time{})
			}
			require.Error(t, err)
			var share gbmodels.GbChannelVideoShare
			require.NoError(t, s.db.Where("channel_pk = ?", channel).Take(&share).Error)
			require.True(t, share.Enabled)
			require.Equal(t, link.Revision, share.Revision)
		})
	}
}
