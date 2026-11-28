package talk

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func TestCascadeAudioLeaseSharesWebChannelExclusion(t *testing.T) {
	for _, webFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "cascade first", true: "web first"}[webFirst], func(t *testing.T) {
			repo := NewGormRepo(newTalkRepoTestDB(t))
			ctx := context.Background()
			now := time.Now().UTC()
			web := newReservedSession("web", 42, now.Add(time.Minute))
			reserve := func() error {
				_, err := repo.ReserveCascadeLease(ctx, "cascade-audio-test", 42, "34020000001320000001", "34020000001370000001", now.Add(time.Minute))
				return err
			}
			if webFirst {
				require.NoError(t, repo.Create(ctx, web, "web-token"))
				require.ErrorIs(t, reserve(), ErrLeaseConflict)
				changed, err := repo.FinishAndReleaseLease(ctx, "web", models.TalkSessionEnded, "done", now)
				require.NoError(t, err)
				require.True(t, changed)
				require.NoError(t, reserve())
			} else {
				require.NoError(t, reserve())
				require.ErrorIs(t, repo.Create(ctx, web, "web-token"), ErrLeaseConflict)
				changed, err := repo.ReleaseCascadeLease(ctx, "cascade-audio-test", models.TalkSessionEnded, "done", now)
				require.NoError(t, err)
				require.True(t, changed)
				require.NoError(t, repo.Create(ctx, web, "web-token"))
			}
		})
	}
}

func TestCascadeAudioLeaseCannotBeTakenOverByWebLifecycle(t *testing.T) {
	db := newTalkRepoTestDB(t)
	repo := NewGormRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()
	lease, err := repo.ReserveCascadeLease(ctx, "cascade-audio-owned", 42, "34020000001320000001", "34020000001370000001", now.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, models.TalkSessionModeCascade, lease.Mode)
	require.False(t, lease.Mode.Valid(), "internal ownership must not become a public create mode")
	got, err := repo.FindBySession(ctx, lease.SessionID)
	require.NoError(t, err)
	require.Nil(t, got)
	rows, err := repo.ListNonterminal(ctx)
	require.NoError(t, err)
	require.Empty(t, rows)
	rows, err = repo.ListExpired(ctx, now.Add(2*time.Minute))
	require.NoError(t, err)
	require.Empty(t, rows)
	changed, err := repo.Transition(ctx, lease.SessionID, models.TalkSessionReserved, models.TalkSessionPublishing, TransitionPatch{})
	require.NoError(t, err)
	require.False(t, changed)
	changed, err = repo.ReissuePublishToken(ctx, lease.SessionID, "web-token", now)
	require.NoError(t, err)
	require.False(t, changed)
	changed, err = repo.UpdateBroadcastFacts(ctx, lease.SessionID, BroadcastFactsPatch{SN: new(uint)})
	require.NoError(t, err)
	require.False(t, changed)
	changed, err = repo.FinishAndReleaseLease(ctx, lease.SessionID, models.TalkSessionEnded, "web stop", now)
	require.NoError(t, err)
	require.False(t, changed)
	require.NoError(t, db.Model(&models.GbTalkSession{}).Where("session_id = ?", lease.SessionID).Update("publish_token_hash", hashPublishToken("known-token")).Error)
	changed, err = repo.ConsumeToken(ctx, lease.SessionID, "known-token", now)
	require.NoError(t, err)
	require.False(t, changed)
	changed, err = repo.ConsumeTokenForPublish(ctx, lease.SessionID, "known-token", "publish", now)
	require.NoError(t, err)
	require.False(t, changed)
	service := NewService(repo, nil, nil, nil, nil, time.Now)
	require.NoError(t, service.Recover(ctx))
	require.NoError(t, service.SweepExpired(ctx))
	require.NoError(t, service.Shutdown(ctx))
	require.NoError(t, service.Cleanup(ctx, lease.SessionID, models.TalkSessionEnded, "web delete"))
	got, err = repo.FindCascadeLease(ctx, lease.SessionID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, models.TalkSessionReserved, got.State)
	require.NotNil(t, got.LeaseKey)
	changed, err = repo.ActivateCascadeLease(ctx, lease.SessionID, now, now.Add(time.Minute))
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = repo.RenewActive(ctx, lease.SessionID, now.Add(time.Hour))
	require.NoError(t, err)
	require.False(t, changed)
	changed, err = repo.RenewCascadeLease(ctx, lease.SessionID, now, now.Add(2*time.Minute))
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = repo.ReleaseCascadeLease(ctx, lease.SessionID, models.TalkSessionEnded, "cascade stopped media", now)
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = repo.RenewCascadeLease(ctx, lease.SessionID, now, now.Add(time.Hour))
	require.NoError(t, err)
	require.False(t, changed)
}

func TestCascadeAudioLeaseOwnerCannotReleaseWebLeaseOrReviveExpiredLease(t *testing.T) {
	repo := NewGormRepo(newTalkRepoTestDB(t))
	ctx := context.Background()
	now := time.Now().UTC()
	web := newReservedSession("web", 42, now.Add(time.Minute))
	require.NoError(t, repo.Create(ctx, web, "token"))
	changed, err := repo.ReleaseCascadeLease(ctx, "web", models.TalkSessionEnded, "wrong owner", now)
	require.NoError(t, err)
	require.False(t, changed)
	_, err = repo.ReserveCascadeLease(ctx, "cascade-audio-expired", 43, "34020000001320000001", "34020000001370000001", now.Add(time.Minute))
	require.NoError(t, err)
	changed, err = repo.ActivateCascadeLease(ctx, "cascade-audio-expired", now.Add(2*time.Minute), now.Add(3*time.Minute))
	require.NoError(t, err)
	require.False(t, changed)
	owned, err := repo.ListCascadeLeases(ctx)
	require.NoError(t, err)
	require.Len(t, owned, 1)
	malformed := newReservedSession("injected-internal-mode", 44, now.Add(time.Minute))
	malformed.Mode = models.TalkSessionModeCascade
	require.ErrorIs(t, repo.Create(ctx, malformed, "token"), ErrInvalidTalkSessionMode)
}

func TestCascadeAudioLeaseUsesPublishedSQLiteStructure(t *testing.T) {
	db := newTalkRepoTestDB(t)
	require.NoError(t, db.Migrator().DropTable(&models.GbTalkSession{}))
	body, err := os.ReadFile("../../../resource/database/sqlitebaseline/baseline.sql")
	require.NoError(t, err)
	start := strings.Index(string(body), `CREATE TABLE IF NOT EXISTS "gb_talk_session" (`)
	require.NotEqual(t, -1, start)
	section := string(body)[start:]
	end := strings.Index(section, "\n);")
	require.NotEqual(t, -1, end)
	require.NoError(t, db.Exec(section[:end+3]).Error)
	for _, statement := range strings.Split(string(body), ";") {
		if strings.Contains(statement, `ON "gb_talk_session"`) {
			require.NoError(t, db.Exec(statement).Error)
		}
	}
	repo := NewGormRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()
	lease, err := repo.ReserveCascadeLease(ctx, "cascade-audio-baseline", 42, "34020000001320000001", "34020000001370000001", now.Add(time.Minute))
	require.NoError(t, err)
	_, err = repo.ReserveCascadeLease(ctx, "cascade-audio-baseline-conflict", 42, "34020000001320000001", "34020000001370000001", now.Add(time.Minute))
	require.ErrorIs(t, err, ErrLeaseConflict)
	changed, err := repo.ActivateCascadeLease(ctx, lease.SessionID, now, now.Add(time.Minute))
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = repo.ReleaseCascadeLease(ctx, lease.SessionID, models.TalkSessionEnded, "done", now)
	require.NoError(t, err)
	require.True(t, changed)
	_, err = repo.ReserveCascadeLease(ctx, "cascade-audio-baseline-next", 42, "34020000001320000001", "34020000001370000001", now.Add(time.Minute))
	require.NoError(t, err)
}

func TestCascadeAudioLeaseConcurrentWebAndCascadeHaveOneWinner(t *testing.T) {
	repo := NewGormRepo(newTalkRepoTestDB(t))
	ctx := context.Background()
	now := time.Now().UTC()
	start := make(chan struct{})
	results := make(chan error, 12)
	for i := 0; i < 12; i++ {
		go func(i int) {
			<-start
			if i%2 == 0 {
				_, err := repo.ReserveCascadeLease(ctx, fmt.Sprintf("cascade-audio-race-%d", i), 42, "34020000001320000001", "34020000001370000001", now.Add(time.Minute))
				results <- err
			} else {
				results <- repo.Create(ctx, newReservedSession(fmt.Sprintf("web-%d", i), 42, now.Add(time.Minute)), "web-token")
			}
		}(i)
	}
	close(start)
	winners := 0
	for i := 0; i < 12; i++ {
		err := <-results
		if err == nil {
			winners++
		} else {
			require.ErrorIs(t, err, ErrLeaseConflict)
		}
	}
	require.Equal(t, 1, winners)
}

func TestCascadeAudioLeaseExpiryDoesNotReleaseOrReviveLiveOwner(t *testing.T) {
	repo := NewGormRepo(newTalkRepoTestDB(t))
	ctx := context.Background()
	now := time.Now().UTC()
	lease, err := repo.ReserveCascadeLease(ctx, "cascade-audio-expiry", 42, "34020000001320000001", "34020000001370000001", now.Add(time.Second))
	require.NoError(t, err)
	changed, err := repo.ActivateCascadeLease(ctx, lease.SessionID, now, now.Add(time.Second))
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = repo.RenewCascadeLease(ctx, lease.SessionID, now.Add(time.Second), now.Add(time.Minute))
	require.NoError(t, err)
	require.False(t, changed)
	require.ErrorIs(t, repo.Create(ctx, newReservedSession("web", 42, now.Add(time.Minute)), "token"), ErrLeaseConflict)
	changed, err = repo.ReleaseCascadeLease(ctx, lease.SessionID, models.TalkSessionExpired, "owner stopped expired media", now.Add(time.Second))
	require.NoError(t, err)
	require.True(t, changed)
	require.NoError(t, repo.Create(ctx, newReservedSession("web", 42, now.Add(time.Minute)), "token"))
	changed, err = repo.ReleaseCascadeLease(ctx, lease.SessionID, models.TalkSessionExpired, "late owner", now.Add(2*time.Second))
	require.NoError(t, err)
	require.False(t, changed)
	web, err := repo.FindBySession(ctx, "web")
	require.NoError(t, err)
	require.NotNil(t, web.LeaseKey)
}
