package videoshare

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	basemodels "uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/utils/authoritytx"
)

func newServiceFixture(t *testing.T) (*Service, uint, time.Time) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbChannelVideoShare{}, &basemodels.SysOperationLog{}))
	require.NoError(t, db.AutoMigrate(&authoritytx.Gate{}))
	require.NoError(t, db.Create(&authoritytx.Gate{ID: 1}).Error)
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN access_epoch INTEGER NOT NULL DEFAULT 1").Error)
	device := gbmodels.GbDevice{DeviceID: "34020000002000000001"}
	require.NoError(t, db.Create(&device).Error)
	require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch = 7 WHERE id = ?", device.ID).Error)
	channel := gbmodels.GbChannel{ChannelID: "34020000001320000001", DeviceID: device.DeviceID}
	require.NoError(t, db.Create(&channel).Error)
	signer, err := NewSigner(make([]byte, keyBytes))
	require.NoError(t, err)
	service, err := NewService(db, signer, func() time.Duration { return 5 * time.Minute }, func(ctx context.Context, tx *gorm.DB, userID, channelPK uint) (AuthorizationSnapshot, error) {
		var channel gbmodels.GbChannel
		if err := tx.WithContext(ctx).Where("id = ?", channelPK).Take(&channel).Error; err != nil {
			return AuthorizationSnapshot{}, err
		}
		var device struct {
			ID          uint
			DeviceID    string
			AccessEpoch uint64
		}
		if err := tx.WithContext(ctx).Table("gb_device").Select("id, device_id, access_epoch").Where("device_id = ?", channel.DeviceID).Take(&device).Error; err != nil {
			return AuthorizationSnapshot{}, err
		}
		return AuthorizationSnapshot{DevicePK: device.ID, DeviceID: device.DeviceID, DeviceEpoch: device.AccessEpoch}, nil
	})
	require.NoError(t, err)
	// Keep the frozen business clock current for issued-link timestamps.
	now := time.Now().UTC()
	service.now = func() time.Time { return now }
	var randomSequence atomic.Uint32
	service.random = func(dst []byte) error {
		for i := range dst {
			dst[i] = byte(i + 1)
		}
		dst[len(dst)-1] = byte(randomSequence.Add(1))
		return nil
	}
	return service, channel.ID, now
}

func TestPendingAutoShareActivatesOnceAndDoesNotExtend(t *testing.T) {
	service, channelPK, now := newServiceFixture(t)
	actor := uint(9)
	require.NoError(t, service.EnsurePending(context.Background(), channelPK, 1, "34020000002000000001", 7, nil, 3))
	var pending gbmodels.GbChannelVideoShare
	require.NoError(t, service.db.Where("channel_pk = ?", channelPK).Take(&pending).Error)
	require.True(t, pending.Enabled)
	require.Nil(t, pending.ActivatedAt)
	require.Nil(t, pending.ExpiresAt)

	_, err := service.AcquireLink(context.Background(), channelPK, actor, pending.Revision+1)
	require.ErrorIs(t, err, ErrConflict)
	first, err := service.AcquireLink(context.Background(), channelPK, actor, pending.Revision)
	require.NoError(t, err)
	require.False(t, first.Pending)
	require.Equal(t, now.Add(5*time.Minute), *first.ExpiresAt)
	_, err = service.AcquireLink(context.Background(), channelPK, actor, pending.Revision)
	require.ErrorIs(t, err, ErrConflict)
	second, err := service.AcquireLink(context.Background(), channelPK, uint(10), first.Revision)
	require.NoError(t, err)
	require.Equal(t, first.Signature, second.Signature)
	require.Equal(t, first.ExpiresAt, second.ExpiresAt)
	require.Equal(t, first.Generation, second.Generation)
	resolved, err := service.Resolve(context.Background(), first.PublicID, first.Signature)
	require.NoError(t, err)
	require.Equal(t, actor, *resolved.CreatedBy)
	require.EqualValues(t, 7, resolved.DeviceEpoch)
}

func TestResolvePublicReturnsOnlySharePageMetadata(t *testing.T) {
	service, channelPK, now := newServiceFixture(t)
	require.NoError(t, service.db.Model(&gbmodels.GbChannel{}).Where("id = ?", channelPK).Updates(map[string]any{
		"name": "Lobby", "status": gbmodels.ChannelStatusOnline, "audio_enabled": true,
	}).Error)
	link, err := service.SetEnabled(context.Background(), channelPK, 9, true, 0)
	require.NoError(t, err)
	public, err := service.ResolvePublic(context.Background(), link.PublicID, link.Signature)
	require.NoError(t, err)
	require.Equal(t, "Lobby", public.Name)
	require.True(t, public.Online)
	require.True(t, public.HasAudio)
	require.Equal(t, *link.ExpiresAt, public.ExpiresAt)
	require.Equal(t, now, public.ServerTime)
}

func TestViewerSessionsAreIndependentAndCloseOnlyTheirOwnSourceLease(t *testing.T) {
	service, channelPK, _ := newServiceFixture(t)
	link, err := service.SetEnabled(context.Background(), channelPK, 9, true, 0)
	require.NoError(t, err)
	var released atomic.Int32
	service.SetPlaybackPreparer(playbackPreparerFunc(func(context.Context, *gbmodels.GbChannelVideoShare) (PlaybackSource, error) {
		return PlaybackSource{StreamID: "stream-1", Generation: 3, HTTPFLVURL: "http://zlm/rtp/stream-1.live.flv", Release: func() { released.Add(1) }}, nil
	}))

	first, err := service.CreateViewer(context.Background(), link.PublicID, link.Signature)
	require.NoError(t, err)
	second, err := service.CreateViewer(context.Background(), link.PublicID, link.Signature)
	require.NoError(t, err)
	require.NotEqual(t, first.Token, second.Token)
	require.NotContains(t, first.Token, "stream-1")
	require.NotEmpty(t, first.HTTPFLVPath)
	require.Empty(t, first.HLSPath)

	access, err := service.LookupViewer(context.Background(), first.Token)
	require.NoError(t, err)
	require.Equal(t, "stream-1", access.Source.StreamID)
	require.NoError(t, service.CloseViewer(first.Token))
	require.EqualValues(t, 1, released.Load())
	_, err = service.LookupViewer(context.Background(), first.Token)
	require.ErrorIs(t, err, ErrExpired)
	_, err = service.LookupViewer(context.Background(), second.Token)
	require.NoError(t, err)
	require.NoError(t, service.CloseViewer(second.Token))
	require.EqualValues(t, 2, released.Load())
}

func TestDrainViewersCancelsSessionsReleasesLeasesAndRejectsInflightCreation(t *testing.T) {
	service, channelPK, _ := newServiceFixture(t)
	link, err := service.SetEnabled(context.Background(), channelPK, 9, true, 0)
	require.NoError(t, err)
	prepareStarted := make(chan struct{})
	allowPrepare := make(chan struct{})
	var released atomic.Int32
	var prepares atomic.Int32
	service.SetPlaybackPreparer(playbackPreparerFunc(func(context.Context, *gbmodels.GbChannelVideoShare) (PlaybackSource, error) {
		if prepares.Add(1) == 2 {
			close(prepareStarted)
			<-allowPrepare
		}
		return PlaybackSource{StreamID: "stream-1", Generation: 3, HTTPFLVURL: "http://zlm/stream.flv", Release: func() { released.Add(1) }}, nil
	}))
	viewer, err := service.CreateViewer(context.Background(), link.PublicID, link.Signature)
	require.NoError(t, err)

	created := make(chan error, 1)
	go func() {
		_, createErr := service.CreateViewer(context.Background(), link.PublicID, link.Signature)
		created <- createErr
	}()
	select {
	case <-prepareStarted:
	case <-time.After(time.Second):
		t.Fatal("inflight viewer preparation did not start")
	}
	service.DrainViewers()
	access, err := service.LookupViewer(context.Background(), viewer.Token)
	require.Empty(t, access)
	require.ErrorIs(t, err, ErrExpired)
	close(allowPrepare)
	require.ErrorIs(t, <-created, ErrViewerUnavailable)
	require.EqualValues(t, 2, released.Load())
	_, err = service.CreateViewer(context.Background(), link.PublicID, link.Signature)
	require.ErrorIs(t, err, ErrViewerUnavailable)
}

type playbackPreparerFunc func(context.Context, *gbmodels.GbChannelVideoShare) (PlaybackSource, error)

func (f playbackPreparerFunc) Prepare(ctx context.Context, share *gbmodels.GbChannelVideoShare) (PlaybackSource, error) {
	return f(ctx, share)
}

func TestManualShareDisableAndReenableRetireOldCredential(t *testing.T) {
	service, channelPK, _ := newServiceFixture(t)
	created, err := service.SetEnabled(context.Background(), channelPK, 1, true, 0)
	require.NoError(t, err)
	require.NotEmpty(t, created.Signature)
	_, err = service.Resolve(context.Background(), created.PublicID, created.Signature)
	require.NoError(t, err)

	disabled, err := service.SetEnabled(context.Background(), channelPK, 1, false, created.Revision)
	require.NoError(t, err)
	_, err = service.Resolve(context.Background(), created.PublicID, created.Signature)
	require.ErrorIs(t, err, ErrExpired)

	reopened, err := service.SetEnabled(context.Background(), channelPK, 1, true, disabled.Revision)
	require.NoError(t, err)
	require.NotEqual(t, created.Generation, reopened.Generation)
	require.NotEqual(t, created.Signature, reopened.Signature)
	_, err = service.Resolve(context.Background(), reopened.PublicID, reopened.Signature)
	require.NoError(t, err)
}

func TestConcurrentRevisionOnlyAllowsOneShareMutation(t *testing.T) {
	service, channelPK, _ := newServiceFixture(t)
	var successes atomic.Int32
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func(actor uint) {
			_, err := service.SetEnabled(context.Background(), channelPK, actor, true, 0)
			if err == nil {
				successes.Add(1)
			}
			done <- err
		}(uint(i + 1))
	}
	first, second := <-done, <-done
	require.EqualValues(t, 1, successes.Load())
	require.True(t, (first == nil && second == ErrConflict) || (second == nil && first == ErrConflict), "results: %v / %v", first, second)
}

func TestShareMutationRejectsInvalidGlobalExpiry(t *testing.T) {
	service, channelPK, _ := newServiceFixture(t)
	service.ttl = func() time.Duration { return 90 * time.Second }
	_, err := service.SetEnabled(context.Background(), channelPK, 1, true, 0)
	require.Error(t, err)
	var count int64
	require.NoError(t, service.db.Model(&gbmodels.GbChannelVideoShare{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestResolveRejectsShareAfterDeviceAccessEpochChanges(t *testing.T) {
	service, channelPK, _ := newServiceFixture(t)
	link, err := service.SetEnabled(context.Background(), channelPK, 9, true, 0)
	require.NoError(t, err)
	require.NoError(t, service.db.Exec("UPDATE gb_device SET access_epoch = access_epoch + 1 WHERE id = ?", 1).Error)
	_, err = service.Resolve(context.Background(), link.PublicID, link.Signature)
	require.ErrorIs(t, err, ErrExpired)
}

func TestResolveDoesNotReviveAfterDeviceTransfersBack(t *testing.T) {
	service, channelPK, _ := newServiceFixture(t)
	link, err := service.SetEnabled(context.Background(), channelPK, 9, true, 0)
	require.NoError(t, err)
	require.NoError(t, service.db.Exec("UPDATE gb_device SET owner_dept_id = 20, access_epoch = access_epoch + 1 WHERE id = ?", 1).Error)
	require.NoError(t, service.db.Exec("UPDATE gb_device SET owner_dept_id = 10, access_epoch = access_epoch + 1 WHERE id = ?", 1).Error)

	_, err = service.Resolve(context.Background(), link.PublicID, link.Signature)
	require.ErrorIs(t, err, ErrExpired)
}

func TestCreatorRevocationPermanentlyRetiresExistingCredential(t *testing.T) {
	service, channelPK, _ := newServiceFixture(t)
	link, err := service.SetEnabled(context.Background(), channelPK, 9, true, 0)
	require.NoError(t, err)
	require.NoError(t, service.db.Transaction(func(tx *gorm.DB) error {
		return RevokeCreatorTx(context.Background(), tx, 9, "creator_access_changed", time.Now())
	}))
	_, err = service.Resolve(context.Background(), link.PublicID, link.Signature)
	require.ErrorIs(t, err, ErrExpired)

	status, err := service.Status(context.Background(), channelPK, 9)
	require.NoError(t, err)
	require.False(t, status.Enabled)
	reopened, err := service.SetEnabled(context.Background(), channelPK, 9, true, status.Revision)
	require.NoError(t, err)
	require.NotEqual(t, link.Generation, reopened.Generation)
	_, err = service.Resolve(context.Background(), link.PublicID, link.Signature)
	require.Error(t, err)
}

func TestPermissionLossRevokesOnlyTheAffectedCreatorsShare(t *testing.T) {
	service, channelPK, _ := newServiceFixture(t)
	db := service.db
	require.NoError(t, db.AutoMigrate(&basemodels.User{}, &basemodels.SysRole{}, &basemodels.SysUserRole{}))
	user := basemodels.User{Username: "share-owner", Password: "hash", Status: 1}
	role := basemodels.SysRole{Name: "share-owner-role", Status: 1, DataScope: 1}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&role).Error)
	require.NoError(t, db.Create(&basemodels.SysUserRole{UserID: user.ID, RoleID: role.ID}).Error)
	link, err := service.SetEnabled(context.Background(), channelPK, user.ID, true, 0)
	require.NoError(t, err)

	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return RevokeUnauthorizedSharesTx(context.Background(), tx, func(context.Context, uint, string, string) (bool, error) {
			return false, nil
		}, []uint{user.ID}, "creator_permission_removed", time.Time{})
	}))
	_, err = service.Resolve(context.Background(), link.PublicID, link.Signature)
	require.ErrorIs(t, err, ErrExpired)
}

func TestDepartmentScopeChangeRevokesOnlyUnauthorizedCreators(t *testing.T) {
	service, channelPK, _ := newServiceFixture(t)
	db := service.db
	active := int8(1)
	role := basemodels.SysRole{Name: "department-scope", Status: 1, DataScope: 1}
	require.NoError(t, db.AutoMigrate(&basemodels.User{}, &basemodels.SysDepartment{}, &basemodels.SysRole{}, &basemodels.SysUserRole{}, &gbmodels.GbDeviceGrant{}))
	require.NoError(t, db.Create(&basemodels.SysDepartment{BaseModel: basemodels.BaseModel{ID: 10}, Name: "Dept", Status: &active}).Error)
	require.NoError(t, db.Create(&[]basemodels.User{
		{BaseModel: basemodels.BaseModel{ID: 9}, Username: "creator-a", Password: "x", Status: 1, DeptID: 10},
		{BaseModel: basemodels.BaseModel{ID: 10}, Username: "creator-b", Password: "x", Status: 1, DeptID: 10},
	}).Error)
	require.NoError(t, db.Create(&role).Error)
	require.NoError(t, db.Create(&[]basemodels.SysUserRole{{UserID: 9, RoleID: role.ID}, {UserID: 10, RoleID: role.ID}}).Error)
	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Where("id = ?", 1).Update("owner_dept_id", 10).Error)
	require.NoError(t, db.Model(&gbmodels.GbChannel{}).Where("id = ?", channelPK).Update("owner_dept_id", 10).Error)
	first, err := service.SetEnabled(context.Background(), channelPK, 9, true, 0)
	require.NoError(t, err)
	secondChannel := gbmodels.GbChannel{ChannelID: "34020000001320000002", DeviceID: "34020000002000000001", OwnerDeptID: 10}
	require.NoError(t, service.db.Create(&secondChannel).Error)
	second, err := service.SetEnabled(context.Background(), secondChannel.ID, 10, true, 0)
	require.NoError(t, err)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return RevokeAllUnauthorizedSharesTx(context.Background(), tx, func(_ context.Context, userID uint, _, _ string) (bool, error) {
			return userID == 10, nil
		}, "creator_department_scope_changed", time.Time{})
	}))
	_, err = service.Resolve(context.Background(), first.PublicID, first.Signature)
	require.ErrorIs(t, err, ErrExpired)
	_, err = service.Resolve(context.Background(), second.PublicID, second.Signature)
	require.NoError(t, err)
}

func TestRoleCreatorIDsIncludeDescendants(t *testing.T) {
	service, _, _ := newServiceFixture(t)
	db := service.db
	require.NoError(t, db.AutoMigrate(&basemodels.SysRole{}, &basemodels.SysUserRole{}))
	parent := basemodels.SysRole{Name: "parent"}
	child := basemodels.SysRole{Name: "child"}
	grandchild := basemodels.SysRole{Name: "grandchild"}
	require.NoError(t, db.Create(&parent).Error)
	child.ParentID = parent.ID
	require.NoError(t, db.Create(&child).Error)
	grandchild.ParentID = child.ID
	require.NoError(t, db.Create(&grandchild).Error)
	require.NoError(t, db.Create(&basemodels.SysUserRole{UserID: 11, RoleID: child.ID}).Error)
	require.NoError(t, db.Create(&basemodels.SysUserRole{UserID: 12, RoleID: grandchild.ID}).Error)
	var userIDs []uint
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		var err error
		userIDs, err = RoleCreatorIDsTx(context.Background(), tx, parent.ID)
		return err
	}))
	require.ElementsMatch(t, []uint{11, 12}, userIDs)
}

func TestRegenerateRetiresActiveLinkAndPreservesItOnFailure(t *testing.T) {
	s, channelPK, _ := newServiceFixture(t)
	old, err := s.SetEnabled(context.Background(), channelPK, 9, true, 0)
	require.NoError(t, err)
	next, err := s.Regenerate(context.Background(), channelPK, 9, old.Revision)
	require.NoError(t, err)
	require.NotEqual(t, old.Signature, next.Signature)
	require.NotEqual(t, old.Generation, next.Generation)
	_, err = s.Resolve(context.Background(), old.PublicID, old.Signature)
	require.ErrorIs(t, err, ErrInvalidCredential)
	_, err = s.Regenerate(context.Background(), channelPK, 9, old.Revision)
	require.ErrorIs(t, err, ErrConflict)
	s.random = func([]byte) error { return errors.New("entropy unavailable") }
	_, err = s.Regenerate(context.Background(), channelPK, 9, next.Revision)
	require.Error(t, err)
	_, err = s.Resolve(context.Background(), next.PublicID, next.Signature)
	require.NoError(t, err)
}
