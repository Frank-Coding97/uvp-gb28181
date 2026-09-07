package talk

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/internal/sqlitebootstrap"
)

func newTalkSQLiteBaselineDB(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "talk.db")
	db, err := gormhelper.NewSQLiteClient(path)
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	result, err := sqlitebootstrap.Initialize(context.Background(), db)
	require.NoError(t, err)
	require.NotEmpty(t, result.Version)
	require.NotEmpty(t, result.Checksum)
	require.NoError(t, sqlitebootstrap.Migrate(context.Background(), db))
	return db, path
}

func TestGormRepoSQLiteBaselineBroadcastLeaseAndFacts(t *testing.T) {
	db, _ := newTalkSQLiteBaselineDB(t)
	repo := NewGormRepo(db)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	now := time.Now().UTC()

	talkSession := newReservedSession("sqlite-talk", 4201, now.Add(time.Minute))
	require.NoError(t, repo.Create(ctx, talkSession, "talk-token"))
	var stored models.GbTalkSession
	require.NoError(t, db.Where("session_id = ?", talkSession.SessionID).First(&stored).Error)
	require.Equal(t, hashPublishToken("talk-token"), stored.PublishTokenHash)
	require.NotContains(t, stored.PublishTokenHash, "talk-token")

	duplicate := newReservedSession("sqlite-talk-duplicate", 4201, now.Add(time.Minute))
	require.ErrorIs(t, repo.Create(ctx, duplicate, "duplicate-token"), ErrLeaseConflict)
	finished, err := repo.FinishAndReleaseLease(ctx, talkSession.SessionID, models.TalkSessionEnded, "released", now)
	require.NoError(t, err)
	require.True(t, finished)
	require.NoError(t, repo.Create(ctx, duplicate, "duplicate-token"))
	resourceOwner := newReservedSession("sqlite-resource-owner", 4203, now.Add(time.Minute))
	resourceOwner.SourceStream = "sqlite-shared-source"
	require.NoError(t, repo.Create(ctx, resourceOwner, "resource-owner-token"))
	resourceConflict := newReservedSession("sqlite-resource-conflict", 4204, now.Add(time.Minute))
	resourceConflict.SourceStream = resourceOwner.SourceStream
	require.ErrorIs(t, repo.Create(ctx, resourceConflict, "resource-conflict-token"), ErrResourceConflict)

	broadcast := newReservedSession("sqlite-broadcast", 4202, now.Add(time.Minute))
	broadcast.Mode = models.TalkSessionModeBroadcast
	broadcast.DeviceID = "34020000001320000001"
	broadcast.TargetID = "34020000001320000002"
	broadcast.BroadcastSN = 17
	broadcast.SignalPhase = models.TalkSignalPhaseWaitingInvite
	require.NoError(t, repo.Create(ctx, broadcast, "broadcast-token"))

	pending, err := repo.FindPendingBroadcast(ctx, broadcast.DeviceID, broadcast.TargetID)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, broadcast.SessionID, pending[0].SessionID)
	wrongTarget, err := repo.FindPendingBroadcast(ctx, broadcast.DeviceID, "34020000001320009999")
	require.NoError(t, err)
	require.Empty(t, wrongTarget)

	updated, err := repo.UpdateBroadcastFacts(ctx, broadcast.SessionID, BroadcastFactsPatch{
		ReplyStatus:   stringPointer("success"),
		SignalPhase:   signalPhasePointer(models.TalkSignalPhaseAnswering),
		RemoteMediaIP: stringPointer("192.0.2.20"),
		RemotePort:    intPointer(30000),
		Transport:     stringPointer("udp"),
	})
	require.NoError(t, err)
	require.True(t, updated)
	found, err := repo.FindByBroadcastSN(ctx, broadcast.BroadcastSN, broadcast.DeviceID, broadcast.TargetID)
	require.NoError(t, err)
	require.NotNil(t, found)
	require.Equal(t, "success", found.BroadcastReplyStatus)
	require.Equal(t, models.TalkSignalPhaseAnswering, found.SignalPhase)
	require.Equal(t, "192.0.2.20", found.RemoteMediaIP)
	require.Equal(t, 30000, found.RemoteMediaPort)

	claimed, err := repo.Transition(ctx, broadcast.SessionID, models.TalkSessionReserved, models.TalkSessionPublishing, TransitionPatch{})
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = repo.ClaimBroadcastDialog(ctx, broadcast.SessionID, "call-sqlite-1", 8)
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = repo.ClaimBroadcastDialog(ctx, broadcast.SessionID, "call-sqlite-2", 9)
	require.NoError(t, err)
	require.False(t, claimed)
}

func TestGormRepoSQLiteBaselineLeaseIsSerializedAcrossClients(t *testing.T) {
	db, path := newTalkSQLiteBaselineDB(t)
	other, err := gormhelper.NewSQLiteClient(path)
	require.NoError(t, err)
	otherRaw, err := other.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = otherRaw.Close() })
	firstRepo, secondRepo := NewGormRepo(db), NewGormRepo(other)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	expiresAt := time.Now().UTC().Add(time.Minute)
	start := make(chan struct{})
	type outcome struct {
		err error
	}
	outcomes := make(chan outcome, 2)
	var wg sync.WaitGroup
	for index, repo := range []*GormRepo{firstRepo, secondRepo} {
		wg.Add(1)
		go func(index int, repo *GormRepo) {
			defer wg.Done()
			<-start
			session := newReservedSession("sqlite-lease-"+string(rune('a'+index)), 4301, expiresAt)
			outcomes <- outcome{err: repo.Create(ctx, session, "lease-token")}
		}(index, repo)
	}
	close(start)
	wg.Wait()
	close(outcomes)

	successes := 0
	for result := range outcomes {
		if result.err == nil {
			successes++
			continue
		}
		require.True(t, errors.Is(result.err, ErrLeaseConflict), "unexpected concurrent lease result: %v", result.err)
	}
	require.Equal(t, 1, successes)
	var activeLeases int64
	require.NoError(t, db.Model(&models.GbTalkSession{}).
		Where("channel_id = ? AND lease_key IS NOT NULL", 4301).Count(&activeLeases).Error)
	require.EqualValues(t, 1, activeLeases)
}

func TestGormRepoSQLiteBaselinePublishAuthorizationIsSingleUse(t *testing.T) {
	db, path := newTalkSQLiteBaselineDB(t)
	other, err := gormhelper.NewSQLiteClient(path)
	require.NoError(t, err)
	otherRaw, err := other.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = otherRaw.Close() })
	firstRepo, secondRepo := NewGormRepo(db), NewGormRepo(other)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	now := time.Now().UTC()
	session := newReservedSession("sqlite-publish", 4302, now.Add(time.Minute))
	require.NoError(t, firstRepo.Create(ctx, session, "publish-token"))

	ok, err := firstRepo.ConsumeTokenForPublish(ctx, session.SessionID, "wrong-token", "publish-wrong", now)
	require.NoError(t, err)
	require.False(t, ok)

	start := make(chan struct{})
	type result struct {
		ok  bool
		err error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for index, repo := range []*GormRepo{firstRepo, secondRepo} {
		publishID := "publish-" + string(rune('a'+index))
		wg.Add(1)
		go func(repo *GormRepo, publishID string) {
			defer wg.Done()
			<-start
			accepted, consumeErr := repo.ConsumeTokenForPublish(ctx, session.SessionID, "publish-token", publishID, now)
			results <- result{ok: accepted, err: consumeErr}
		}(repo, publishID)
	}
	close(start)
	wg.Wait()
	close(results)
	accepted := 0
	for result := range results {
		require.NoError(t, result.err)
		if result.ok {
			accepted++
		}
	}
	require.Equal(t, 1, accepted)

	var stored models.GbTalkSession
	require.NoError(t, db.Where("session_id = ?", session.SessionID).First(&stored).Error)
	acceptedID := stored.PublishID
	require.NotEmpty(t, acceptedID)
	ok, err = firstRepo.ConsumeTokenForPublish(ctx, session.SessionID, "publish-token", acceptedID, now)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = firstRepo.ConsumeTokenForPublish(ctx, session.SessionID, "publish-token", "publish-other", now)
	require.NoError(t, err)
	require.False(t, ok)
}
