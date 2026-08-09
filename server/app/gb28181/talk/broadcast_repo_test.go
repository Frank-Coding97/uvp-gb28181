package talk

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestBroadcastSessionFactsPersistAndMatchExactly(t *testing.T) {
	db := newTalkRepoTestDB(t)
	repo := NewGormRepo(db)
	session := newReservedSession("broadcast-facts", 42, time.Now().Add(time.Minute))
	session.Mode = models.TalkSessionModeBroadcast
	session.DeviceID = "34020000001320000001"
	session.TargetID = "34020000001320000002"
	session.BroadcastSN = 17
	session.SignalPhase = models.TalkSignalPhaseWaitingInvite
	require.NoError(t, repo.Create(context.Background(), session, "token"))

	matched, err := repo.FindPendingBroadcast(context.Background(), session.DeviceID, session.TargetID)
	require.NoError(t, err)
	require.Len(t, matched, 1)
	require.Equal(t, session.SessionID, matched[0].SessionID)

	wrongTarget, err := repo.FindPendingBroadcast(context.Background(), session.DeviceID, "34020000001320009999")
	require.NoError(t, err)
	require.Empty(t, wrongTarget)

	updated, err := repo.UpdateBroadcastFacts(context.Background(), session.SessionID, BroadcastFactsPatch{
		ReplyStatus:   pointer("success"),
		SignalPhase:   signalPhasePointer(models.TalkSignalPhaseAnswering),
		RemoteMediaIP: pointer("192.0.2.20"),
		RemotePort:    intPointer(30000),
		Transport:     pointer("udp"),
		SenderMode:    pointer("udp"),
	})
	require.NoError(t, err)
	require.True(t, updated)

	got, err := repo.FindByBroadcastSN(context.Background(), 17, session.DeviceID, session.TargetID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "success", got.BroadcastReplyStatus)
	require.Equal(t, models.TalkSignalPhaseAnswering, got.SignalPhase)
	require.Equal(t, "192.0.2.20", got.RemoteMediaIP)
	require.Equal(t, 30000, got.RemoteMediaPort)
}

func pointer(value string) *string                                            { return &value }
func intPointer(value int) *int                                               { return &value }
func signalPhasePointer(value models.TalkSignalPhase) *models.TalkSignalPhase { return &value }
