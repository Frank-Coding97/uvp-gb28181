package talk

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.com/uvp-gb28181/app/utils/logging"
)

func TestUnpublishedAfterBYETimeoutReportsCompletedCleanup(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	ctx := logging.WithContext(context.Background(), zap.New(core))
	media := &fakeActivationMedia{}
	service, repo, _ := newActivationService(t, media, &fakeTalkInviter{byeErr: context.DeadlineExceeded})
	session := activateSessionForCleanupWithSender(t, repo, "unpublish-bye-timeout", 141, "call-141", time.Now().Add(time.Minute), 40022)

	require.NoError(t, service.OnUnpublished(ctx, session.NodeID, session.App, session.SourceStream))
	stored, err := repo.FindBySession(context.Background(), session.SessionID)
	require.NoError(t, err)
	require.Equal(t, models.TalkSessionEnded, stored.State)
	require.Nil(t, stored.LeaseKey)
	require.Contains(t, stored.Error, "TALK BYE")
	require.Equal(t, 1, media.stops)
	require.Equal(t, 1, media.closes)
	entries := logs.All()
	require.Len(t, entries, 1)
	require.Equal(t, zap.InfoLevel, entries[0].Level)
	require.Equal(t, "TALK BYE", entries[0].ContextMap()["cleanup_step"])
	require.Equal(t, "gb28181.talk.cleanup.step_unconfirmed", entries[0].ContextMap()["event"])
}

func TestCleanupMediaFailureKeepsWarningAndRetryLease(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	ctx := logging.WithContext(context.Background(), zap.New(core))
	service, repo, _ := newActivationService(t, &fakeActivationMedia{closeErr: context.DeadlineExceeded}, &fakeTalkInviter{})
	session := activateSessionForCleanupWithSender(t, repo, "unpublish-close-timeout", 142, "call-142", time.Now().Add(time.Minute), 40022)

	require.ErrorIs(t, service.OnUnpublished(ctx, session.NodeID, session.App, session.SourceStream), context.DeadlineExceeded)
	stored, err := repo.FindBySession(context.Background(), session.SessionID)
	require.NoError(t, err)
	require.Equal(t, models.TalkSessionStopping, stored.State)
	require.NotNil(t, stored.LeaseKey)
	entries := logs.All()
	require.Len(t, entries, 1)
	require.Equal(t, zap.WarnLevel, entries[0].Level)
	require.Equal(t, "close source", entries[0].ContextMap()["cleanup_step"])
	require.Equal(t, "gb28181.talk.cleanup.step_failed", entries[0].ContextMap()["event"])
}

type timeoutBroadcastDialogs struct{}

func (timeoutBroadcastDialogs) ByeBroadcast(context.Context, string) error {
	return context.DeadlineExceeded
}

func TestUnpublishedBroadcastAfterBYETimeoutReportsCompletedCleanup(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	ctx := logging.WithContext(context.Background(), zap.New(core))
	media := &fakeActivationMedia{}
	service, repo, session, _, _ := newBroadcastActivationService(t, media)
	service.activation.deps.BroadcastDialogs = timeoutBroadcastDialogs{}
	callID := "broadcast-cleanup-call"
	changed, err := repo.Transition(ctx, session.SessionID, models.TalkSessionPublishing, models.TalkSessionInviting, TransitionPatch{CallID: &callID})
	require.NoError(t, err)
	require.True(t, changed)

	require.NoError(t, service.OnUnpublished(ctx, session.NodeID, session.App, session.SourceStream))
	stored, err := repo.FindBySession(ctx, session.SessionID)
	require.NoError(t, err)
	require.Equal(t, models.TalkSessionEnded, stored.State)
	require.Nil(t, stored.LeaseKey)
	require.Contains(t, stored.Error, "Broadcast BYE")
	require.Equal(t, 1, media.stops)
	require.Equal(t, 1, media.closes)
	require.Len(t, logs.All(), 1)
	require.Equal(t, zap.InfoLevel, logs.All()[0].Level)
	require.Equal(t, "Broadcast BYE", logs.All()[0].ContextMap()["cleanup_step"])
}

func TestUnpublishedBYETimeoutDoesNotHideMediaFailure(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	ctx := logging.WithContext(context.Background(), zap.New(core))
	service, repo, _ := newActivationService(t, &fakeActivationMedia{closeErr: context.DeadlineExceeded}, &fakeTalkInviter{byeErr: context.DeadlineExceeded})
	session := activateSessionForCleanupWithSender(t, repo, "unpublish-bye-and-close-timeout", 144, "call-144", time.Now().Add(time.Minute), 40022)

	require.Error(t, service.OnUnpublished(ctx, session.NodeID, session.App, session.SourceStream))
	stored, err := repo.FindBySession(ctx, session.SessionID)
	require.NoError(t, err)
	require.Equal(t, models.TalkSessionStopping, stored.State)
	require.NotNil(t, stored.LeaseKey)
	entries := logs.All()
	require.Len(t, entries, 2)
	require.Equal(t, zap.InfoLevel, entries[0].Level)
	require.Equal(t, "TALK BYE", entries[0].ContextMap()["cleanup_step"])
	require.Equal(t, zap.WarnLevel, entries[1].Level)
	require.Equal(t, "close source", entries[1].ContextMap()["cleanup_step"])
}

func TestUnpublishedWaitersDoNotRepeatCleanupFailure(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	ctx := logging.WithContext(context.Background(), zap.New(core))
	service, repo, _ := newActivationService(t, &fakeActivationMedia{}, &fakeTalkInviter{byeErr: context.DeadlineExceeded})
	media := &blockingCleanupMedia{started: make(chan struct{}), release: make(chan struct{})}
	service.activation.deps.ClientFor = func(*node.Node) TalkMediaClient { return media }
	session := activateSessionForCleanupWithSender(t, repo, "unpublish-waiters", 143, "call-143", time.Now().Add(time.Minute), 40022)
	ownerDone := make(chan error, 1)
	go func() { ownerDone <- service.Cleanup(ctx, session.SessionID, models.TalkSessionEnded, "user stopped") }()
	select {
	case <-media.started:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not reach close source")
	}
	t.Cleanup(func() {
		select {
		case <-media.release:
		default:
			close(media.release)
		}
	})

	hookResults := make(chan error, 5)
	for range 5 {
		go func() { hookResults <- service.OnUnpublished(ctx, session.NodeID, session.App, session.SourceStream) }()
	}
	close(media.release)
	require.ErrorIs(t, <-ownerDone, context.DeadlineExceeded)
	for range 5 {
		require.NoError(t, <-hookResults)
	}
	require.Len(t, logs.All(), 1, "shared cleanup must report its failed step once")
}
