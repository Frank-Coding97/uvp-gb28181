package videoshare

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

type batchStatusConfig struct{ app.YmlConfigInterf }

func (batchStatusConfig) GetString(key string) string {
	switch key {
	case "casbin.modelconfig":
		return activePolicyModel
	case "casbin.tableprefix":
		return "active_"
	case "casbin.tablename":
		return "policy_rules"
	}
	return ""
}
func (batchStatusConfig) GetUintSlice(string) []uint { return nil }

func batchStatusFixture(t *testing.T) (*Service, []uint, time.Time) {
	t.Helper()
	db, first := authorizerFixture(t, 10)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbChannelVideoShare{}))
	require.NoError(t, db.Table("active_policy_rules").AutoMigrate(&gormadapter.CasbinRule{}))
	previous := app.ConfigYml
	app.ConfigYml = batchStatusConfig{}
	t.Cleanup(func() { app.ConfigYml = previous })
	ids := []uint{first.ID}
	for i := 1; i < 7; i++ {
		channel := gbmodels.GbChannel{DeviceID: first.DeviceID, ChannelID: fmt.Sprintf("camera-%d", i), OwnerDeptID: 10}
		require.NoError(t, db.Create(&channel).Error)
		ids = append(ids, channel.ID)
	}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		helper := activePolicyHelper(t, tx)
		require.NoError(t, helper.AddRolesForUserByID(7, []uint{1}))
		require.NoError(t, helper.AddPolicyForRole(1, "/api/gb28181/play/:device/:channel", "POST"))
		for _, id := range ids[:6] {
			require.NoError(t, helper.AddPolicyForRole(1, fmt.Sprintf("/api/gb28181/channel-shares/%d", id), "PUT"))
		}
		return nil
	}))
	signer, err := NewSigner(make([]byte, keyBytes))
	require.NoError(t, err)
	service, err := NewService(db, signer, func() time.Duration { return 5 * time.Minute }, NewTransactionalAuthorizer())
	require.NoError(t, err)
	now := time.Date(2026, 11, 17, 8, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	activated := now.Add(-time.Minute)
	future := now.Add(time.Minute)
	past := now.Add(-time.Second)
	creator := uint(7)
	rows := []gbmodels.GbChannelVideoShare{
		{ChannelPK: ids[0], PublicID: "secret-active", Generation: "secret-generation", Enabled: true, ActivatedAt: &activated, ExpiresAt: &future, CreatedBy: &creator, DevicePK: 1, DeviceID: first.DeviceID, DeviceEpoch: 9, Revision: 8},
		{ChannelPK: ids[1], PublicID: "secret-pending", Generation: "pending-generation", Enabled: true, DevicePK: 1, DeviceID: first.DeviceID, DeviceEpoch: 9, Revision: 9},
		{ChannelPK: ids[2], PublicID: "secret-expired", Enabled: true, ActivatedAt: &activated, ExpiresAt: &past, CreatedBy: &creator, DevicePK: 1, DeviceID: first.DeviceID, DeviceEpoch: 9, Revision: 10},
		{ChannelPK: ids[3], PublicID: "secret-revoked", Enabled: true, RevokedAt: &activated, DevicePK: 1, DeviceID: first.DeviceID, DeviceEpoch: 9, Revision: 11},
		{ChannelPK: ids[5], PublicID: "secret-old-epoch", Enabled: true, ActivatedAt: &activated, ExpiresAt: &future, CreatedBy: &creator, DevicePK: 1, DeviceID: first.DeviceID, DeviceEpoch: 8, Revision: 12},
		{ChannelPK: ids[6], PublicID: "secret-forbidden", Enabled: true, DevicePK: 1, DeviceID: first.DeviceID, DeviceEpoch: 9, Revision: 13},
	}
	require.NoError(t, db.Create(&rows).Error)
	return service, ids, now
}

func TestBatchStatusReturnsOnlyAuthorizedListSafeLifecycleState(t *testing.T) {
	service, ids, now := batchStatusFixture(t)
	states, err := service.BatchStatus(context.Background(), append(ids, ids[0]), 7)
	require.NoError(t, err)
	require.Len(t, states, 6)
	require.NotContains(t, states, ids[6], "missing share permission must omit this row")
	require.True(t, states[ids[0]].Enabled)
	require.False(t, states[ids[0]].Pending)
	require.True(t, states[ids[1]].Enabled)
	require.True(t, states[ids[1]].Pending)
	require.Nil(t, states[ids[1]].ExpiresAt)
	for _, index := range []int{2, 3, 4, 5} {
		require.False(t, states[ids[index]].Enabled)
		require.False(t, states[ids[index]].Pending)
	}
	require.EqualValues(t, 0, states[ids[4]].Revision)
	require.EqualValues(t, 10, states[ids[2]].Revision)
	for _, state := range states {
		require.Equal(t, now, state.ServerTime)
	}
	encoded, err := json.Marshal(states)
	require.NoError(t, err)
	for _, secret := range []string{"signature", "publicId", "generation", "secret-", "createdBy", "deviceEpoch"} {
		require.NotContains(t, string(encoded), secret)
	}
}

func TestBatchStatusLoadsPolicyAndSharesOnceWithoutMutatingLifecycle(t *testing.T) {
	service, ids, _ := batchStatusFixture(t)
	var before []gbmodels.GbChannelVideoShare
	require.NoError(t, service.db.Order("id").Find(&before).Error)
	shareReads, policyReads, writes := 0, 0, 0
	require.NoError(t, service.db.Callback().Query().Before("gorm:query").Register("batch_count_reads", func(query *gorm.DB) {
		_, active := query.Statement.ConnPool.(gorm.TxCommitter)
		require.True(t, active, "every batch authorization and status read must use the same transaction")
		if query.Statement.Table != "active_policy_rules" {
			deadline, ok := query.Statement.Context.Deadline()
			require.True(t, ok)
			require.LessOrEqual(t, time.Until(deadline), 2*time.Second)
		}
		switch query.Statement.Table {
		case "gb_channel_video_share":
			shareReads++
		case "active_policy_rules":
			policyReads++
		}
	}))
	require.NoError(t, service.db.Callback().Create().Before("gorm:create").Register("batch_count_create", func(*gorm.DB) { writes++ }))
	require.NoError(t, service.db.Callback().Update().Before("gorm:update").Register("batch_count_update", func(*gorm.DB) { writes++ }))
	require.NoError(t, service.db.Callback().Delete().Before("gorm:delete").Register("batch_count_delete", func(*gorm.DB) { writes++ }))
	service.SetPlaybackPreparer(playbackPreparerFunc(func(context.Context, *gbmodels.GbChannelVideoShare) (PlaybackSource, error) {
		t.Fatal("status listing must not prepare playback")
		return PlaybackSource{}, nil
	}))
	states, err := service.BatchStatus(context.Background(), ids, 7)
	require.NoError(t, err)
	require.Len(t, states, 6)
	require.Equal(t, 1, shareReads)
	require.Equal(t, 1, policyReads, "a page must not reload Casbin for every channel")
	require.Zero(t, writes)
	require.NoError(t, service.db.Callback().Query().Remove("batch_count_reads"))
	var after []gbmodels.GbChannelVideoShare
	require.NoError(t, service.db.Order("id").Find(&after).Error)
	require.Equal(t, before, after, "pending activation, expiry, revisions and credentials must stay unchanged")
}

func TestBatchStatusOmitsAllRowsWithoutPlaybackPermission(t *testing.T) {
	service, ids, _ := batchStatusFixture(t)
	require.NoError(t, service.db.Table("active_policy_rules").Where("ptype = ? AND v2 = ?", "p", "POST").Delete(&gormadapter.CasbinRule{}).Error)
	shareReads := 0
	require.NoError(t, service.db.Callback().Query().Before("gorm:query").Register("batch_no_permission", func(query *gorm.DB) {
		if query.Statement.Table == "gb_channel_video_share" {
			shareReads++
		}
	}))
	states, err := service.BatchStatus(context.Background(), ids, 7)
	require.NoError(t, err)
	require.Empty(t, states)
	require.Zero(t, shareReads, "no lifecycle state should be read before permission is established")
}

func TestBatchStatusPropagatesDatabaseFailureAndCancellation(t *testing.T) {
	service, ids, _ := batchStatusFixture(t)
	failure := errors.New("share status database unavailable")
	require.NoError(t, service.db.Callback().Query().Before("gorm:query").Register("batch_share_failure", func(query *gorm.DB) {
		if query.Statement.Table == "gb_channel_video_share" {
			query.AddError(failure)
		}
	}))
	states, err := service.BatchStatus(context.Background(), ids, 7)
	require.ErrorIs(t, err, failure)
	require.Nil(t, states, "dependency failure must not look like closed sharing")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	states, err = service.BatchStatus(ctx, ids, 7)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, states)
}

func TestStatusDistinguishesNaturalExpiryFromOtherRevocations(t *testing.T) {
	for _, reason := range []string{"", "expired", "manual", "permission_revoked"} {
		t.Run(reason, func(t *testing.T) {
			service, ids, now := batchStatusFixture(t)
			updates := map[string]interface{}{"revoke_reason": reason}
			if reason != "" {
				updates["revoked_at"] = now
				updates["enabled"] = false
			}
			require.NoError(t, service.db.Model(&gbmodels.GbChannelVideoShare{}).Where("channel_pk = ?", ids[2]).Updates(updates).Error)
			single, err := service.Status(context.Background(), ids[2], 7)
			require.NoError(t, err)
			batch, err := service.BatchStatus(context.Background(), ids, 7)
			require.NoError(t, err)
			for _, value := range []interface{}{single, batch[ids[2]]} {
				encoded, err := json.Marshal(value)
				require.NoError(t, err)
				var fields map[string]interface{}
				require.NoError(t, json.Unmarshal(encoded, &fields))
				require.Equal(t, reason == "" || reason == "expired", fields["expired"])
				require.Equal(t, false, fields["enabled"])
			}
		})
	}
}
