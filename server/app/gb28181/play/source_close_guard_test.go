package play

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
)

type sourceCloseContextKey string

type guardedZLM struct {
	*mockZLM
	closeContext atomic.Value
}

func (z *guardedZLM) CloseRtpServer(ctx context.Context, streamID string) error {
	z.closeContext.Store(ctx.Value(sourceCloseContextKey("phase")))
	return z.mockZLM.CloseRtpServer(ctx, streamID)
}

type guardedInviter struct {
	*mockInviter
	byeContext atomic.Value
}

func (i *guardedInviter) Bye(ctx context.Context, sessions *uac.SessionManager, streamID string) error {
	i.byeContext.Store(ctx.Value(sourceCloseContextKey("phase")))
	return i.mockInviter.Bye(ctx, sessions, streamID)
}

func TestSourceCloseGuardDenialPreservesReadyAndSkipsMediaCleanup(t *testing.T) {
	z := &mockZLM{}
	inv := &mockInviter{}
	service, notifier, _ := newSvc(t, z, inv, onlineDevice(), aChannel())
	inv.onInvite = func(session *uac.Session) {
		z.online.Store(true)
		notifier.Publish(session.StreamID)
	}
	result, err := service.Start(context.Background(), onlineDevice().DeviceID, aChannel().ChannelID)
	if err != nil {
		t.Fatal(err)
	}
	denyErr := errors.New("work recording owns source")
	var guardSawReady atomic.Bool
	service.SetSourceCloseGuard(func(_ context.Context, _ string, _ func(context.Context) error) error {
		if _, ok := service.CurrentLiveRef(result.StreamID); ok {
			guardSawReady.Store(true)
		}
		return denyErr
	})

	if err := service.Stop(context.Background(), result.StreamID); !errors.Is(err, denyErr) {
		t.Fatalf("stop error=%v, want guard denial", err)
	}
	if got, ok := service.CurrentLiveRef(result.StreamID); !ok || got != resultLiveRef(result) {
		t.Fatalf("guard denial lost ready generation: got=%+v ok=%v", got, ok)
	}
	if inv.byeCalls.Load() != 0 || z.closeCalls.Load() != 0 {
		t.Fatalf("guard denial produced media cleanup: bye=%d close=%d", inv.byeCalls.Load(), z.closeCalls.Load())
	}
	if !guardSawReady.Load() {
		t.Fatal("guard ran after coordinator left Ready state")
	}
}

func TestSourceCloseGuardDenialThroughConditionalStopPreservesReady(t *testing.T) {
	z := &mockZLM{}
	inv := &mockInviter{}
	service, notifier, _ := newSvc(t, z, inv, onlineDevice(), aChannel())
	inv.onInvite = func(session *uac.Session) {
		z.online.Store(true)
		notifier.Publish(session.StreamID)
	}
	result, err := service.Start(context.Background(), onlineDevice().DeviceID, aChannel().ChannelID)
	if err != nil {
		t.Fatal(err)
	}
	denyErr := errors.New("conditional source protection")
	service.SetSourceCloseGuard(func(_ context.Context, _ string, _ func(context.Context) error) error {
		return denyErr
	})

	handled, err := service.StopIfCurrent(context.Background(), resultLiveRef(result))
	if !handled || !errors.Is(err, denyErr) {
		t.Fatalf("conditional stop handled=%v err=%v, want guard denial", handled, err)
	}
	if got, ok := service.CurrentLiveRef(result.StreamID); !ok || got != resultLiveRef(result) {
		t.Fatalf("conditional guard denial lost ready generation: got=%+v ok=%v", got, ok)
	}
	if inv.byeCalls.Load() != 0 || z.closeCalls.Load() != 0 {
		t.Fatalf("conditional guard denial produced media cleanup: bye=%d close=%d", inv.byeCalls.Load(), z.closeCalls.Load())
	}
}

func TestSourceCloseGuardAllowsCleanupAndPassesNestedContext(t *testing.T) {
	baseZLM := &mockZLM{}
	z := &guardedZLM{mockZLM: baseZLM}
	baseInviter := &mockInviter{}
	inv := &guardedInviter{mockInviter: baseInviter}
	service, notifier, _ := newSvc(t, z, inv, onlineDevice(), aChannel())
	baseInviter.onInvite = func(session *uac.Session) {
		baseZLM.online.Store(true)
		notifier.Publish(session.StreamID)
	}
	result, err := service.Start(context.Background(), onlineDevice().DeviceID, aChannel().ChannelID)
	if err != nil {
		t.Fatal(err)
	}
	service.SetSourceCloseGuard(func(ctx context.Context, streamID string, closeFn func(context.Context) error) error {
		if streamID != result.StreamID {
			t.Fatalf("guard streamID=%q, want %q", streamID, result.StreamID)
		}
		return closeFn(context.WithValue(ctx, sourceCloseContextKey("phase"), "guarded"))
	})

	if err := service.Stop(context.Background(), result.StreamID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if inv.byeCalls.Load() != 1 || z.closeCalls.Load() != 1 {
		t.Fatalf("allowed cleanup counts: bye=%d close=%d", inv.byeCalls.Load(), z.closeCalls.Load())
	}
	if got := inv.byeContext.Load(); got != "guarded" {
		t.Fatalf("BYE lost callback context value: %v", got)
	}
	if got := z.closeContext.Load(); got != "guarded" {
		t.Fatalf("CloseRtpServer lost callback context value: %v", got)
	}
}

func TestSourceCloseGuardProtectsPersistedStopBeforeRTPClose(t *testing.T) {
	z := &mockZLM{}
	inv := &mockInviter{}
	channel := aChannel()
	channel.StreamID = "persisted-stream"
	channel.CurrentSSRC = "0200000001"
	service, _, _ := newSvc(t, z, inv, onlineDevice(), channel)
	denyErr := errors.New("work recording owns persisted source")
	service.SetSourceCloseGuard(func(_ context.Context, _ string, _ func(context.Context) error) error {
		return denyErr
	})

	if err := service.StopIfPersistedCurrent(context.Background(), channel.StreamID, channel.CurrentSSRC); !errors.Is(err, denyErr) {
		t.Fatalf("persisted stop error=%v, want guard denial", err)
	}
	if z.closeCalls.Load() != 0 || channel.StreamID == "" || channel.CurrentSSRC == "" {
		t.Fatalf("guard denial changed persisted cleanup state: close=%d channel=%+v", z.closeCalls.Load(), channel)
	}
}

func TestSourceCloseGuardDenialMakesStartRollbackCleanupPending(t *testing.T) {
	z := &mockZLM{}
	inv := &mockInviter{inviteErr: errors.New("invite failed")}
	service, _, channels := newSvc(t, z, inv, onlineDevice(), aChannel())
	denyErr := errors.New("work recording owns start rollback")
	service.SetSourceCloseGuard(func(_ context.Context, _ string, _ func(context.Context) error) error {
		return denyErr
	})

	_, err := service.Start(context.Background(), onlineDevice().DeviceID, aChannel().ChannelID)
	if !errors.Is(err, ErrLiveCleanupPending) || !errors.Is(err, denyErr) {
		t.Fatalf("start rollback error=%v, want cleanup pending and guard denial", err)
	}
	if inv.byeCalls.Load() != 0 || z.closeCalls.Load() != 0 {
		t.Fatalf("guard denial performed rollback cleanup: bye=%d close=%d", inv.byeCalls.Load(), z.closeCalls.Load())
	}
	if channels.c.StreamID == "" || channels.c.CurrentSSRC == "" {
		t.Fatalf("guard denial did not preserve rollback generation: %+v", channels.c)
	}
}
