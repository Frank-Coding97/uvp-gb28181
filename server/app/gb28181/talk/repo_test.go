package talk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func newTalkRepoTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:talk_repo_%d?mode=memory&cache=shared&_pragma=busy_timeout(5000)", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{SkipDefaultTransaction: true})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(20)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&models.GbTalkSession{}))
	return db
}

func newReservedSession(id string, channelID uint, expiresAt time.Time) *models.GbTalkSession {
	return &models.GbTalkSession{
		SessionID:    id,
		ChannelID:    channelID,
		DeviceID:     "34020000002000000001",
		ActorID:      7,
		ActorDeptID:  9,
		NodeID:       3,
		App:          "talk",
		SourceStream: "talk-source-" + id,
		State:        models.TalkSessionReserved,
		ExpiresAt:    expiresAt,
	}
}

func TestTalkModelIndexesMatchMigration(t *testing.T) {
	db := newTalkRepoTestDB(t)
	for _, index := range []string{
		"uk_talk_session_session_id",
		"uk_talk_session_lease",
		"uk_talk_session_source_key",
		"uk_talk_session_recv_key",
		"uk_talk_session_ssrc_key",
		"idx_talk_session_state_expires",
	} {
		require.True(t, db.Migrator().HasIndex(&models.GbTalkSession{}, index), index)
	}
	require.True(t, db.Migrator().HasColumn(&models.GbTalkSession{}, "publish_token_hash"))
	require.False(t, db.Migrator().HasColumn(&models.GbTalkSession{}, "publish_token"))
}

func TestGormRepoCreateHashesTokenAndEnforcesOneChannelLease(t *testing.T) {
	db := newTalkRepoTestDB(t)
	repo := NewGormRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()

	first := newReservedSession("session-1", 42, now.Add(time.Minute))
	require.NoError(t, repo.Create(ctx, first, "plain-publish-token"))
	require.NotNil(t, first.LeaseKey)
	require.EqualValues(t, 42, *first.LeaseKey)

	var stored models.GbTalkSession
	require.NoError(t, db.First(&stored, first.ID).Error)
	digest := sha256.Sum256([]byte("plain-publish-token"))
	require.Equal(t, hex.EncodeToString(digest[:]), stored.PublishTokenHash)
	require.NotContains(t, stored.PublishTokenHash, "plain-publish-token")

	duplicate := newReservedSession("session-2", 42, now.Add(time.Minute))
	err := repo.Create(ctx, duplicate, "second-token")
	require.ErrorIs(t, err, ErrLeaseConflict)

	finished, err := repo.FinishAndReleaseLease(ctx, first.SessionID, models.TalkSessionEnded, "released", now)
	require.NoError(t, err)
	require.True(t, finished)
	require.NoError(t, repo.Create(ctx, duplicate, "second-token"))

	var history []models.GbTalkSession
	require.NoError(t, db.Order("id").Find(&history).Error)
	require.Len(t, history, 2)
	require.Nil(t, history[0].LeaseKey)
	require.Equal(t, first.SourceStream, history[0].SourceStream, "终态应保留资源历史字段")
}

func TestGormRepoConcurrentCreateHasSingleLeaseWinner(t *testing.T) {
	db := newTalkRepoTestDB(t)
	repo := NewGormRepo(db)
	ctx := context.Background()
	expiresAt := time.Now().Add(time.Minute)

	const workers = 20
	start := make(chan struct{})
	var wg sync.WaitGroup
	var successes atomic.Int32
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			session := newReservedSession(fmt.Sprintf("session-%02d", i), 88, expiresAt)
			err := repo.Create(ctx, session, fmt.Sprintf("token-%02d", i))
			if err == nil {
				successes.Add(1)
				return
			}
			errs <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)

	require.EqualValues(t, 1, successes.Load())
	for err := range errs {
		require.True(t, errors.Is(err, ErrLeaseConflict), "unexpected create error: %v", err)
	}
	var count int64
	require.NoError(t, db.Model(&models.GbTalkSession{}).Where("lease_key = ?", 88).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestGormRepoTransitionUsesCASAndRejectsStateRegression(t *testing.T) {
	db := newTalkRepoTestDB(t)
	repo := NewGormRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()
	session := newReservedSession("transition", 1, now.Add(time.Minute))
	require.NoError(t, repo.Create(ctx, session, "token"))

	changed, err := repo.Transition(ctx, session.SessionID, models.TalkSessionReserved, models.TalkSessionPublishing, TransitionPatch{})
	require.NoError(t, err)
	require.True(t, changed)

	changed, err = repo.Transition(ctx, session.SessionID, models.TalkSessionReserved, models.TalkSessionPublishing, TransitionPatch{})
	require.NoError(t, err)
	require.False(t, changed, "过期写入不得覆盖新状态")

	_, err = repo.Transition(ctx, session.SessionID, models.TalkSessionPublishing, models.TalkSessionReserved, TransitionPatch{})
	require.ErrorIs(t, err, ErrInvalidTransition)

	recv, ssrc, callID := "talk-recv-transition", "0200000001", "call-transition"
	localTag, remoteTag := "local", "remote"
	cseq := uint(10)
	changed, err = repo.Transition(ctx, session.SessionID, models.TalkSessionPublishing, models.TalkSessionInviting, TransitionPatch{
		RecvStream: &recv,
		SSRC:       &ssrc,
		CallID:     &callID,
		LocalTag:   &localTag,
		RemoteTag:  &remoteTag,
		CSeq:       &cseq,
	})
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = repo.Transition(ctx, session.SessionID, models.TalkSessionInviting, models.TalkSessionActive, TransitionPatch{StartedAt: &now})
	require.NoError(t, err)
	require.True(t, changed)

	got, err := repo.FindBySession(ctx, session.SessionID)
	require.NoError(t, err)
	require.Equal(t, models.TalkSessionActive, got.State)
	require.Equal(t, recv, got.RecvStream)
	require.Equal(t, ssrc, got.SSRC)
	require.Equal(t, callID, got.CallID)
}

func TestGormRepoResourceKeysAreUniqueOnlyWhileNonterminal(t *testing.T) {
	db := newTalkRepoTestDB(t)
	repo := NewGormRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()

	first := newReservedSession("resource-1", 1, now.Add(time.Minute))
	first.SourceStream = "same-source"
	require.NoError(t, repo.Create(ctx, first, "token-1"))

	second := newReservedSession("resource-2", 2, now.Add(time.Minute))
	second.SourceStream = "same-source"
	require.Error(t, repo.Create(ctx, second, "token-2"))

	recv, ssrc := "same-recv", "0200000099"
	changed, err := repo.Transition(ctx, first.SessionID, models.TalkSessionReserved, models.TalkSessionPublishing, TransitionPatch{RecvStream: &recv, SSRC: &ssrc})
	require.NoError(t, err)
	require.True(t, changed)
	_, err = repo.FinishAndReleaseLease(ctx, first.SessionID, models.TalkSessionFailed, "failed", now)
	require.NoError(t, err)

	require.NoError(t, repo.Create(ctx, second, "token-2"), "终态释放 source 占用键后应允许复用")
	changed, err = repo.Transition(ctx, second.SessionID, models.TalkSessionReserved, models.TalkSessionPublishing, TransitionPatch{RecvStream: &recv, SSRC: &ssrc})
	require.NoError(t, err, "终态释放 recv/ssrc 占用键后应允许复用")
	require.True(t, changed)
}

func TestGormRepoConsumeTokenIsSingleUseCAS(t *testing.T) {
	db := newTalkRepoTestDB(t)
	repo := NewGormRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()
	session := newReservedSession("token-session", 1, now.Add(time.Minute))
	require.NoError(t, repo.Create(ctx, session, "correct-token"))

	consumed, err := repo.ConsumeToken(ctx, session.SessionID, "wrong-token", now)
	require.NoError(t, err)
	require.False(t, consumed)
	consumed, err = repo.ConsumeToken(ctx, session.SessionID, "correct-token", now)
	require.NoError(t, err)
	require.True(t, consumed)
	consumed, err = repo.ConsumeToken(ctx, session.SessionID, "correct-token", now)
	require.NoError(t, err)
	require.False(t, consumed)

	expired := newReservedSession("expired-token", 2, now.Add(-time.Second))
	require.NoError(t, repo.Create(ctx, expired, "expired-token"))
	consumed, err = repo.ConsumeToken(ctx, expired.SessionID, "expired-token", now)
	require.NoError(t, err)
	require.False(t, consumed)

	concurrent := newReservedSession("concurrent-token", 3, now.Add(time.Minute))
	require.NoError(t, repo.Create(ctx, concurrent, "one-use-token"))
	const workers = 20
	start := make(chan struct{})
	var wg sync.WaitGroup
	var winners atomic.Int32
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			won, consumeErr := repo.ConsumeToken(ctx, concurrent.SessionID, "one-use-token", now)
			if consumeErr != nil {
				errs <- consumeErr
				return
			}
			if won {
				winners.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for consumeErr := range errs {
		require.NoError(t, consumeErr)
	}
	require.EqualValues(t, 1, winners.Load())
}

func TestGormRepoFindersAndExpiryLists(t *testing.T) {
	db := newTalkRepoTestDB(t)
	repo := NewGormRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()

	active := newReservedSession("active", 1, now.Add(time.Minute))
	active.SourceStream = "source-active"
	require.NoError(t, repo.Create(ctx, active, "active-token"))
	recv, callID := "recv-active", "call-active"
	changed, err := repo.Transition(ctx, active.SessionID, models.TalkSessionReserved, models.TalkSessionPublishing, TransitionPatch{RecvStream: &recv, CallID: &callID})
	require.NoError(t, err)
	require.True(t, changed)

	expired := newReservedSession("expired", 2, now.Add(-time.Second))
	require.NoError(t, repo.Create(ctx, expired, "expired-token"))
	ended := newReservedSession("ended", 3, now.Add(-time.Second))
	require.NoError(t, repo.Create(ctx, ended, "ended-token"))
	finished, err := repo.FinishAndReleaseLease(ctx, ended.SessionID, models.TalkSessionEnded, "", now)
	require.NoError(t, err)
	require.True(t, finished)

	bySession, err := repo.FindBySession(ctx, active.SessionID)
	require.NoError(t, err)
	require.Equal(t, active.SessionID, bySession.SessionID)
	bySource, err := repo.FindBySource(ctx, active.NodeID, active.App, active.SourceStream)
	require.NoError(t, err)
	require.Equal(t, active.SessionID, bySource.SessionID)
	byRecv, err := repo.FindByRecv(ctx, active.NodeID, recv)
	require.NoError(t, err)
	require.Equal(t, active.SessionID, byRecv.SessionID)
	byCallID, err := repo.FindByCallID(ctx, callID)
	require.NoError(t, err)
	require.Equal(t, active.SessionID, byCallID.SessionID)

	nonterminal, err := repo.ListNonterminal(ctx)
	require.NoError(t, err)
	require.Len(t, nonterminal, 2)
	expiredRows, err := repo.ListExpired(ctx, now)
	require.NoError(t, err)
	require.Len(t, expiredRows, 1)
	require.Equal(t, expired.SessionID, expiredRows[0].SessionID)

	missing, err := repo.FindBySession(ctx, "missing")
	require.NoError(t, err)
	require.Nil(t, missing)
}

func TestGormRepoFinishIsTerminalAndIdempotent(t *testing.T) {
	db := newTalkRepoTestDB(t)
	repo := NewGormRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()
	session := newReservedSession("finish", 4, now.Add(time.Minute))
	require.NoError(t, repo.Create(ctx, session, "token"))

	_, err := repo.FinishAndReleaseLease(ctx, session.SessionID, models.TalkSessionActive, "bad", now)
	require.ErrorIs(t, err, ErrInvalidTerminalState)
	changed, err := repo.FinishAndReleaseLease(ctx, session.SessionID, models.TalkSessionExpired, "lease expired", now)
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = repo.FinishAndReleaseLease(ctx, session.SessionID, models.TalkSessionFailed, "late failure", now.Add(time.Second))
	require.NoError(t, err)
	require.False(t, changed)

	got, err := repo.FindBySession(ctx, session.SessionID)
	require.NoError(t, err)
	require.Equal(t, models.TalkSessionExpired, got.State)
	require.Nil(t, got.LeaseKey)
	require.Nil(t, got.SourceKey)
	require.Nil(t, got.RecvKey)
	require.Nil(t, got.SSRCKey)
	require.NotNil(t, got.EndedAt)
}

func TestTalkMigrationMatchesModelAndHasDown(t *testing.T) {
	_, sourceFile, _, _ := runtime.Caller(0)
	serverRoot := filepath.Join(filepath.Dir(sourceFile), "..", "..", "..")
	upPath := filepath.Join(serverRoot, "resource/database/gb28181/migrations/2026-07-22-talk-session.sql")
	downPath := filepath.Join(serverRoot, "resource/database/gb28181/migrations/2026-07-22-talk-session-down.sql")

	up, err := os.ReadFile(upPath)
	require.NoError(t, err)
	down, err := os.ReadFile(downPath)
	require.NoError(t, err)
	ddl := string(up)
	for _, fragment := range []string{
		"CREATE TABLE `gb_talk_session`",
		"`lease_key` int unsigned DEFAULT NULL",
		"UNIQUE KEY `uk_talk_session_lease` (`lease_key`)",
		"UNIQUE KEY `uk_talk_session_source_key` (`source_key`)",
		"UNIQUE KEY `uk_talk_session_recv_key` (`recv_key`)",
		"UNIQUE KEY `uk_talk_session_ssrc_key` (`ssrc_key`)",
		"`publish_token_hash` char(64) NOT NULL",
	} {
		require.Contains(t, ddl, fragment)
	}
	require.NotContains(t, strings.ToLower(ddl), "publish_token`", "迁移不得定义令牌明文字段")
	require.Contains(t, string(down), "DROP TABLE IF EXISTS `gb_talk_session`")
}
