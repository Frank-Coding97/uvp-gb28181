package workrecording

import (
	"context"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"path/filepath"
	"sync"
	"testing"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func claimDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "claims.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(&models.GbRecorderClaim{}))
	return db
}

func TestClaimConcurrentOwnersAndDurableConflict(t *testing.T) {
	db := claimDB(t)
	ctx := context.Background()
	key := ChannelResource(9)
	owners := []Owner{{Kind: OwnerWork, ID: "a"}, {Kind: OwnerContinuous, ID: "b"}, {Kind: OwnerPlan, ID: "c"}, {Kind: OwnerManagement, ID: "d"}}
	var wg sync.WaitGroup
	won := make(chan Owner, 4)
	errs := make(chan error, 4)
	for _, o := range owners {
		wg.Add(1)
		go func(o Owner) {
			defer wg.Done()
			_, err := NewClaims(db).Acquire(ctx, key, o, 0)
			if err == nil {
				won <- o
			} else {
				errs <- err
			}
		}(o)
	}
	wg.Wait()
	close(won)
	close(errs)
	require.Len(t, won, 1)
	winner := <-won
	for err := range errs {
		require.ErrorIs(t, err, ErrOwnerConflict)
	}
	restored, err := NewClaims(db).Acquire(ctx, key, winner, 0)
	require.NoError(t, err)
	require.Equal(t, uint64(1), restored.Version)
	_, err = NewClaims(db).Acquire(ctx, ChannelResource(10), Owner{Kind: OwnerWork, ID: "other"}, 0)
	require.NoError(t, err)
}

func TestClaimRejectsStaleOrForeignReleaseAndKeepsUnknown(t *testing.T) {
	s := NewClaims(claimDB(t))
	ctx := context.Background()
	key := ChannelResource(9)
	owner := Owner{Kind: OwnerWork, ID: "a"}
	claim, err := s.Acquire(ctx, key, owner, 5)
	require.NoError(t, err)
	require.ErrorIs(t, s.Release(ctx, key, Owner{Kind: OwnerWork, ID: "b"}, claim.Version), ErrOwnerConflict)
	require.ErrorIs(t, s.Release(ctx, key, owner, claim.Version), ErrVersionConflict)
	claim, err = s.Transition(ctx, key, owner, 1, StateUnknown)
	require.NoError(t, err)
	_, err = s.Transition(ctx, key, owner, 1, StateRecording)
	require.ErrorIs(t, err, ErrVersionConflict)
	_, err = NewClaims(s.db).Acquire(ctx, key, Owner{Kind: OwnerPlan, ID: "p"}, 5)
	require.ErrorIs(t, err, ErrOwnerConflict)
	_, err = s.Acquire(ctx, key, owner, 6)
	require.ErrorIs(t, err, ErrVersionConflict)
	claim, err = s.Transition(ctx, key, owner, claim.Version, StateStopping)
	require.NoError(t, err)
	claim, err = s.Transition(ctx, key, owner, claim.Version, StateStopped)
	require.NoError(t, err)
	require.NoError(t, s.Release(ctx, key, owner, claim.Version))
	next, err := s.Acquire(ctx, key, Owner{Kind: OwnerWork, ID: "next"}, 6)
	require.NoError(t, err)
	require.NotNil(t, next)
	require.ErrorIs(t, s.Release(ctx, key, owner, claim.Version), ErrOwnerConflict)
}

func TestMediaResourceIncludesNodeAndTupleWithoutDelimiterCollision(t *testing.T) {
	a := MediaResource(1, "a/b", "c", "d")
	b := MediaResource(1, "a", "b/c", "d")
	require.NotEqual(t, a, b)
	require.NotEqual(t, a, MediaResource(2, "a/b", "c", "d"))
	require.Len(t, a, 64)
	s := NewClaims(claimDB(t))
	ctx := context.Background()
	_, err := s.Acquire(ctx, a, Owner{Kind: OwnerWork, ID: "a"}, 1)
	require.NoError(t, err)
	_, err = s.Acquire(ctx, a, Owner{Kind: OwnerWork, ID: "b"}, 1)
	require.ErrorIs(t, err, ErrOwnerConflict)
}

func TestClaimRejectsInvalidIdentityAndTransition(t *testing.T) {
	s := NewClaims(claimDB(t))
	ctx := context.Background()
	_, err := s.Acquire(ctx, "", Owner{Kind: OwnerWork, ID: "a"}, 1)
	require.ErrorIs(t, err, ErrInvalidRequest)
	_, err = s.Acquire(ctx, ChannelResource(1), Owner{Kind: "unknown", ID: "a"}, 1)
	require.ErrorIs(t, err, ErrInvalidRequest)
	c, err := s.Acquire(ctx, ChannelResource(1), Owner{Kind: OwnerWork, ID: "a"}, 1)
	require.NoError(t, err)
	_, err = s.Transition(ctx, c.ResourceKey, Owner{Kind: OwnerWork, ID: "a"}, c.Version, "banana")
	require.ErrorIs(t, err, ErrInvalidRequest)
}

func TestClaimOldReleaseCannotRemoveNextRunOfSameOwner(t *testing.T) {
	s := NewClaims(claimDB(t))
	ctx := context.Background()
	key := ChannelResource(1)
	o := Owner{Kind: OwnerContinuous, ID: "channel-1"}
	c, err := s.Acquire(ctx, key, o, 1)
	require.NoError(t, err)
	c, err = s.Transition(ctx, key, o, c.Version, StateStopping)
	require.NoError(t, err)
	c, err = s.Transition(ctx, key, o, c.Version, StateStopped)
	require.NoError(t, err)
	oldVersion := c.Version
	require.NoError(t, s.Release(ctx, key, o, oldVersion))
	next, err := s.Acquire(ctx, key, o, 1)
	require.NoError(t, err)
	next, err = s.Transition(ctx, key, o, next.Version, StateStopping)
	require.NoError(t, err)
	next, err = s.Transition(ctx, key, o, next.Version, StateStopped)
	require.NoError(t, err)
	require.ErrorIs(t, s.Release(ctx, key, o, oldVersion), ErrVersionConflict)
	require.Greater(t, next.Version, oldVersion)
}

func TestClaimDatabaseFailureDoesNotReleaseOwnership(t *testing.T) {
	db := claimDB(t)
	s := NewClaims(db)
	ctx := context.Background()
	key := ChannelResource(1)
	o := Owner{Kind: OwnerWork, ID: "a"}
	c, err := s.Acquire(ctx, key, o, 1)
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE TRIGGER reject_claim_update BEFORE UPDATE ON gb_recorder_claim BEGIN SELECT RAISE(ABORT, 'write unavailable'); END").Error)
	_, err = s.Transition(ctx, key, o, c.Version, StateStopping)
	require.Error(t, err)
	require.NoError(t, db.Exec("DROP TRIGGER reject_claim_update").Error)
	restored, err := NewClaims(db).Get(ctx, key)
	require.NoError(t, err)
	require.Equal(t, StateStarting, restored.State)
	require.Equal(t, c.Version, restored.Version)
	_, err = s.Acquire(ctx, key, Owner{Kind: OwnerPlan, ID: "b"}, 1)
	require.ErrorIs(t, err, ErrOwnerConflict)
}

func TestReacquireClearsReleasedMediaIdentity(t *testing.T) {
	s := NewClaims(claimDB(t))
	ctx := context.Background()
	key := ChannelResource(1)
	owner := Owner{Kind: OwnerWork, ID: "old"}
	c, err := s.Acquire(ctx, key, owner, 5)
	require.NoError(t, err)
	require.NoError(t, s.db.Model(c).Updates(map[string]any{"node_id": 1, "v_host": "v", "app": "rtp", "stream": "old"}).Error)
	c, err = s.Transition(ctx, key, owner, c.Version, StateStopping)
	require.NoError(t, err)
	c, err = s.Transition(ctx, key, owner, c.Version, StateStopped)
	require.NoError(t, err)
	require.NoError(t, s.Release(ctx, key, owner, c.Version))
	next, err := s.Acquire(ctx, key, Owner{Kind: OwnerWork, ID: "next"}, 0)
	require.NoError(t, err)
	require.Empty(t, next.Stream)
	require.Zero(t, next.NodeID)
}
