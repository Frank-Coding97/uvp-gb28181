package playback

import (
	"context"
	"errors"
	"testing"
	"time"
)

type retainedFailureInviter struct {
	*fakeInvite
	callID          string
	cleanupErr      error
	cleanupCallID   string
	noCleanupMarker bool
}

func (f *retainedFailureInviter) Invite(context.Context, UACInvite) (DialogInfo, error) {
	f.calls.Add(1)
	return DialogInfo{CallID: f.callID, CleanupRequired: !f.noCleanupMarker}, f.err
}

func (f *retainedFailureInviter) Teardown(_ context.Context, callID string) error {
	f.teardown.Add(1)
	f.cleanupCallID = callID
	return f.cleanupErr
}

func TestPlaybackInviteFailureRetainsKnownCleanupHandle(t *testing.T) {
	now := time.Unix(1700000000, 0)
	picker := &fakeNodePicker{node: NodeInfo{ID: "node-1", ServerID: "34020000002000000001", Destination: "192.0.2.20:5060", RecvIP: "192.0.2.10"}}
	rtp := &fakeRTP{}
	want := errors.New("ACK failed after answer")
	pending := errors.New("original dialog still unconfirmed")
	invite := &retainedFailureInviter{fakeInvite: &fakeInvite{err: want}, callID: "original-failed-dialog", cleanupErr: pending}
	registry := NewRegistry(RegistryConfig{Now: func() time.Time { return now }})
	service := NewService(registry, picker, rtp, invite, &fakeMedia{}, ServiceConfig{ServerID: "34020000002000000001"})
	_, err := service.Create(context.Background(), validCreate(now))
	if !errors.Is(err, want) || !errors.Is(err, pending) || invite.teardown.Load() != 1 || invite.cleanupCallID != invite.callID || rtp.unbindCalls.Load() != 0 {
		t.Fatalf("failure lost cleanup: err=%v teardown=%d target=%q unbind=%d", err, invite.teardown.Load(), invite.cleanupCallID, rtp.unbindCalls.Load())
	}
	session, ok := registry.GetByCallID(invite.callID)
	if !ok || session.State != StateStopping || session.Resources == nil {
		t.Fatalf("pending session=%+v", session)
	}
	invite.cleanupErr = nil
	if err := service.Stop(context.Background(), session.ID, "retry"); err != nil {
		t.Fatal(err)
	}
	session, _ = registry.GetByCallID(invite.callID)
	if session.State != StateFailed || invite.teardown.Load() != 2 || invite.calls.Load() != 1 || rtp.unbindCalls.Load() != 1 {
		t.Fatalf("retry changed original failure or reinvited: session=%+v", session)
	}
}

func TestPlaybackInviteFailureCleanupMarkerIsExplicit(t *testing.T) {
	for _, brokenMarker := range []bool{false, true} {
		now := time.Unix(1700000000, 0)
		picker := &fakeNodePicker{node: NodeInfo{ID: "node-1", ServerID: "34020000002000000001", Destination: "192.0.2.20:5060", RecvIP: "192.0.2.10"}}
		rtp := &fakeRTP{}
		invite := &retainedFailureInviter{fakeInvite: &fakeInvite{err: errors.New("invite failed")}, callID: "generated-only", noCleanupMarker: true}
		if brokenMarker {
			invite.callID, invite.noCleanupMarker = "", false
		}
		service := NewService(NewRegistry(RegistryConfig{Now: func() time.Time { return now }}), picker, rtp, invite, &fakeMedia{}, ServiceConfig{ServerID: "34020000002000000001"})
		_, err := service.Create(context.Background(), validCreate(now))
		if err == nil || invite.teardown.Load() != 0 {
			t.Fatal("unusable cleanup handle dispatched")
		}
		wantUnbind := int32(1)
		if brokenMarker {
			wantUnbind = 0
		}
		if rtp.unbindCalls.Load() != wantUnbind {
			t.Fatalf("broken=%v unbind=%d", brokenMarker, rtp.unbindCalls.Load())
		}
	}
}
