package repo_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/repo"
)

func newManagedResourceDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbZLMManagedResource{}))
	return db
}

func managedResourceIdentity() repo.ManagedResourceIdentity {
	return repo.ManagedResourceIdentity{
		NodeID:       7,
		ResourceType: "pull_proxy",
		ResourceKey:  "pull_proxy|camera-1",
		Schema:       "rtsp",
		Vhost:        "__defaultVhost__",
		App:          "proxy",
		Stream:       "camera-1",
	}
}

func TestManagedResourceRepoUsesCompleteMediaIdentityForCRUD(t *testing.T) {
	db := newManagedResourceDB(t)
	managed := repo.NewManagedResourceRepo(db)
	ctx := context.Background()
	firstIdentity := managedResourceIdentity()
	secondIdentity := firstIdentity
	secondIdentity.Schema = "rtmp"
	secondIdentity.Vhost = "tenant-vhost"

	first, err := managed.Register(ctx, repo.ManagedResourceRegistration{Identity: firstIdentity, CreatedBy: 101})
	require.NoError(t, err)
	second, err := managed.Register(ctx, repo.ManagedResourceRegistration{Identity: secondIdentity, CreatedBy: 202})
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID, "同 key 的不同 schema/vhost 必须分别持久化")
	require.Equal(t, firstIdentity.Schema, first.Schema)
	require.Equal(t, firstIdentity.Vhost, first.Vhost)
	require.Equal(t, secondIdentity.Schema, second.Schema)
	require.Equal(t, secondIdentity.Vhost, second.Vhost)

	foundFirst, err := managed.Find(ctx, firstIdentity)
	require.NoError(t, err)
	require.Equal(t, first.ID, foundFirst.ID)
	foundSecond, err := managed.Find(ctx, secondIdentity)
	require.NoError(t, err)
	require.Equal(t, second.ID, foundSecond.ID)

	tombstoned, err := managed.Tombstone(ctx, firstIdentity, time.Date(2026, 8, 30, 1, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.NotNil(t, tombstoned.TombstonedAt)
	remaining, err := managed.List(ctx, repo.ManagedResourceFilter{NodeID: firstIdentity.NodeID, ResourceType: secondIdentity.ResourceType, ResourceKey: secondIdentity.ResourceKey, Schema: secondIdentity.Schema, Vhost: secondIdentity.Vhost, App: secondIdentity.App, Stream: secondIdentity.Stream})
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	require.Equal(t, second.ID, remaining[0].ID)

	_, err = managed.Observe(ctx, firstIdentity, time.Date(2026, 8, 30, 1, 1, 0, 0, time.UTC))
	require.NoError(t, err)
	visible, err := managed.List(ctx, repo.ManagedResourceFilter{NodeID: firstIdentity.NodeID})
	require.NoError(t, err)
	require.Len(t, visible, 2)
}

func TestManagedResourceFingerprintIncludesCompleteMediaIdentity(t *testing.T) {
	identity := managedResourceIdentity()
	bySchema := identity
	bySchema.Schema = "rtmp"
	byVhost := identity
	byVhost.Vhost = "tenant-vhost"

	require.NotEqual(t, repo.FingerprintManagedResource(identity), repo.FingerprintManagedResource(bySchema))
	require.NotEqual(t, repo.FingerprintManagedResource(identity), repo.FingerprintManagedResource(byVhost))
}

func TestManagedResourceRepoRegisterIsIdempotentAndPreservesCreationSource(t *testing.T) {
	db := newManagedResourceDB(t)
	managed := repo.NewManagedResourceRepo(db)
	ctx := context.Background()
	firstObserved := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	secondObserved := firstObserved.Add(5 * time.Minute)
	identity := managedResourceIdentity()

	first, err := managed.Register(ctx, repo.ManagedResourceRegistration{
		Identity:    identity,
		CreatedBy:   101,
		Fingerprint: repo.FingerprintManagedResource(identity),
		Summary:     "source=rtsp://camera.example.invalid/live",
		ObservedAt:  firstObserved,
	})
	require.NoError(t, err)
	require.NotZero(t, first.ID)

	second, err := managed.Register(ctx, repo.ManagedResourceRegistration{
		Identity:    identity,
		CreatedBy:   202,
		Fingerprint: repo.FingerprintManagedResourceParts("rotated-source-secret-url"),
		Summary:     "source=rtsp://camera-2.example.invalid/live",
		ObservedAt:  secondObserved,
	})
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	require.Equal(t, uint64(101), second.CreatedBy, "重复登记不得覆盖最初创建人")
	require.Equal(t, first.CreatedAt, second.CreatedAt, "重复登记不得覆盖创建时间")
	require.Equal(t, secondObserved, second.LastObservedAt.UTC())
	require.Equal(t, repo.FingerprintManagedResourceParts("rotated-source-secret-url"), second.IdentityFingerprint)
	require.Nil(t, second.TombstonedAt)

	var count int64
	require.NoError(t, db.Model(&gbmodels.GbZLMManagedResource{}).Count(&count).Error)
	require.EqualValues(t, 1, count, "复合身份唯一键必须保证只存在一条账本记录")
}

func TestManagedResourceRepoTombstoneAndReappearKeepOneLedgerRow(t *testing.T) {
	db := newManagedResourceDB(t)
	managed := repo.NewManagedResourceRepo(db)
	ctx := context.Background()
	identity := managedResourceIdentity()
	createdAt := time.Date(2026, 8, 29, 13, 0, 0, 0, time.UTC)
	tombstonedAt := createdAt.Add(time.Minute)
	repeatedTombstoneAt := tombstonedAt.Add(time.Minute)
	reappearedAt := repeatedTombstoneAt.Add(time.Minute)

	_, err := managed.Register(ctx, repo.ManagedResourceRegistration{Identity: identity, CreatedBy: 101, ObservedAt: createdAt})
	require.NoError(t, err)

	tombstone, err := managed.Tombstone(ctx, identity, tombstonedAt)
	require.NoError(t, err)
	require.Equal(t, tombstonedAt, tombstone.TombstonedAt.UTC())

	repeated, err := managed.Tombstone(ctx, identity, repeatedTombstoneAt)
	require.NoError(t, err)
	require.Equal(t, tombstonedAt, repeated.TombstonedAt.UTC(), "重复 tombstone 必须保留首次消失时间")

	visible, err := managed.List(ctx, repo.ManagedResourceFilter{NodeID: identity.NodeID})
	require.NoError(t, err)
	require.Empty(t, visible, "tombstone 记录不能被误报为当前存在资源")

	all, err := managed.List(ctx, repo.ManagedResourceFilter{NodeID: identity.NodeID, IncludeTombstoned: true})
	require.NoError(t, err)
	require.Len(t, all, 1)
	require.Equal(t, tombstonedAt, all[0].TombstonedAt.UTC())

	reappeared, err := managed.Observe(ctx, identity, reappearedAt)
	require.NoError(t, err)
	require.Nil(t, reappeared.TombstonedAt, "ZLM 重新观察到资源时应清除 tombstone")
	require.Equal(t, reappearedAt, reappeared.LastObservedAt.UTC())

	visible, err = managed.List(ctx, repo.ManagedResourceFilter{NodeID: identity.NodeID})
	require.NoError(t, err)
	require.Len(t, visible, 1)

	var count int64
	require.NoError(t, db.Model(&gbmodels.GbZLMManagedResource{}).Count(&count).Error)
	require.EqualValues(t, 1, count, "重新出现不得创建第二条身份记录")
}

func TestManagedResourceRepoObserveUnknownDoesNotCreateManagedSource(t *testing.T) {
	db := newManagedResourceDB(t)
	managed := repo.NewManagedResourceRepo(db)
	identity := managedResourceIdentity()

	_, err := managed.Observe(context.Background(), identity, time.Now().UTC())
	require.ErrorIs(t, err, repo.ErrManagedResourceNotFound)

	var count int64
	require.NoError(t, db.Model(&gbmodels.GbZLMManagedResource{}).Count(&count).Error)
	require.Zero(t, count, "仅在 ZLM 列表中观察到的 unknown 资源不能自动成为管理台来源")
}

func TestManagedResourceRepoRejectsSensitiveIdentityAndRedactsSummary(t *testing.T) {
	db := newManagedResourceDB(t)
	managed := repo.NewManagedResourceRepo(db)
	ctx := context.Background()

	for _, mutate := range []func(repo.ManagedResourceIdentity) repo.ManagedResourceIdentity{
		func(identity repo.ManagedResourceIdentity) repo.ManagedResourceIdentity {
			identity.ResourceKey = "rtsp://user:password@example.invalid/live?token=secret"
			return identity
		},
		func(identity repo.ManagedResourceIdentity) repo.ManagedResourceIdentity {
			identity.Vhost = "rtsp://user:password@example.invalid/live"
			return identity
		},
		func(identity repo.ManagedResourceIdentity) repo.ManagedResourceIdentity {
			identity.App = "live?token=secret"
			return identity
		},
	} {
		sensitiveIdentity := mutate(managedResourceIdentity())
		_, err := managed.Register(ctx, repo.ManagedResourceRegistration{Identity: sensitiveIdentity, CreatedBy: 1})
		require.ErrorIs(t, err, repo.ErrManagedResourceSensitiveData)
	}

	identity := managedResourceIdentity()
	row, err := managed.Register(ctx, repo.ManagedResourceRegistration{
		Identity:   identity,
		CreatedBy:  1,
		Summary:    "source=rtsp://user:password@example.invalid/live?token=secret&x=1 password=secret",
		ObservedAt: time.Now().UTC(),
	})
	require.NoError(t, err)
	require.NotContains(t, strings.ToLower(row.Summary), "password")
	require.NotContains(t, strings.ToLower(row.Summary), "secret")
	require.NotContains(t, strings.ToLower(row.Summary), "/live")
	require.NotContains(t, strings.ToLower(row.Summary), "token=")
	require.Contains(t, row.Summary, "rtsp://example.invalid")
	require.Len(t, row.IdentityFingerprint, 64)
}

func TestManagedResourceRepoRejectsInvalidFingerprint(t *testing.T) {
	db := newManagedResourceDB(t)
	managed := repo.NewManagedResourceRepo(db)

	_, err := managed.Register(context.Background(), repo.ManagedResourceRegistration{
		Identity:    managedResourceIdentity(),
		Fingerprint: "not-a-sha256",
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, repo.ErrManagedResourceInvalidInput))
}
