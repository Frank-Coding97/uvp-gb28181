package play

import (
	"context"
	"errors"
	"strings"
	"testing"

	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
)

type readerCountZLM struct {
	mockZLM
	readers int
}

func (m *readerCountZLM) GetMediaList(context.Context, string, string, string) ([]zlm.MediaInfo, error) {
	return []zlm.MediaInfo{{ReaderCount: m.readers}}, nil
}

func TestServiceRecoveryGateRejectsStartUntilFinished(t *testing.T) {
	z := &mockZLM{port: 40000}
	inv := &mockInviter{}
	s, _, _ := newSvc(t, z, inv, onlineDevice(), aChannel())
	s.BeginRecovery()

	_, err := s.Start(context.Background(), onlineDevice().DeviceID, aChannel().ChannelID)
	if !errors.Is(err, ErrLiveRecoveryPending) {
		t.Fatalf("start error=%v, want ErrLiveRecoveryPending", err)
	}
	if z.openCalls.Load() != 0 || inv.inviteCalls.Load() != 0 {
		t.Fatalf("recovery gate leaked side effects: open=%d invite=%d", z.openCalls.Load(), inv.inviteCalls.Load())
	}
}

func TestServiceRecoveryRestoresOnlineFixedLiveWithoutInvite(t *testing.T) {
	withFixedAddressPlaybackSettings(t, true, true)
	streamID, err := FixedStreamID(onlineDevice().DeviceID, aChannel().ChannelID)
	if err != nil {
		t.Fatal(err)
	}
	channel := aChannel()
	channel.StreamID = streamID
	channel.CurrentSSRC = "0200000007"
	z := &mockZLM{port: 40000}
	z.online.Store(true)
	inv := &mockInviter{}
	s, _, _ := newFixedSvc(t, z, inv, onlineDevice(), channel)
	s.BeginRecovery()
	stats, err := s.RecoverLiveSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s.FinishRecovery()
	if stats.Scanned != 1 || stats.Restored != 1 {
		t.Fatalf("recovery stats=%+v", stats)
	}

	result, err := s.Start(context.Background(), channel.DeviceID, channel.ChannelID)
	if err != nil {
		t.Fatal(err)
	}
	if result.StreamID != streamID || result.SSRC != channel.CurrentSSRC {
		t.Fatalf("recovered result=%+v", result)
	}
	if z.openCalls.Load() != 0 || inv.inviteCalls.Load() != 0 {
		t.Fatalf("recovered live restarted: open=%d invite=%d", z.openCalls.Load(), inv.inviteCalls.Load())
	}
	ref, ok := s.CurrentLiveRef(streamID)
	if !ok || ref.SSRC != channel.CurrentSSRC || ref.Generation == 0 || ref.NodeID == 0 {
		t.Fatalf("recovered ref=%+v ok=%v", ref, ok)
	}
}

func TestServiceRecoveryClosesPersistedOfflineRTPBeforeClearing(t *testing.T) {
	withFixedAddressPlaybackSettings(t, true, false)
	streamID, err := FixedStreamID(onlineDevice().DeviceID, aChannel().ChannelID)
	if err != nil {
		t.Fatal(err)
	}
	channel := aChannel()
	channel.StreamID = streamID
	channel.CurrentSSRC = "0200000007"
	z := &mockZLM{port: 40000}
	s, _, channels := newFixedSvc(t, z, &mockInviter{}, onlineDevice(), channel)
	s.BeginRecovery()
	stats, err := s.RecoverLiveSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s.FinishRecovery()
	if stats.Scanned != 1 || stats.Cleaned != 1 || stats.Failed != 0 {
		t.Fatalf("recovery stats=%+v", stats)
	}
	if z.closeCalls.Load() != 1 {
		t.Fatalf("offline persisted RTP listener was not closed before cleanup: close=%d", z.closeCalls.Load())
	}
	if channels.c.StreamID != "" || channels.c.CurrentSSRC != "" {
		t.Fatalf("successful recovery cleanup retained persistence: %+v", channels.c)
	}
}

func TestStartRefusesNewGenerationWhenPersistedCleanupCannotClose(t *testing.T) {
	withFixedAddressPlaybackSettings(t, true, false)
	streamID, err := FixedStreamID(onlineDevice().DeviceID, aChannel().ChannelID)
	if err != nil {
		t.Fatal(err)
	}
	channel := aChannel()
	channel.StreamID = streamID
	channel.CurrentSSRC = "0200000007"
	z := &mockZLM{port: 40000, closeErr: errors.New("close unavailable")}
	s, _, channels := newFixedSvc(t, z, &mockInviter{}, onlineDevice(), channel)

	_, err = s.Start(context.Background(), channel.DeviceID, channel.ChannelID)
	if err == nil || !strings.Contains(err.Error(), "close unavailable") {
		t.Fatalf("start error=%v, want persisted cleanup failure", err)
	}
	if z.openCalls.Load() != 0 {
		t.Fatalf("cleanup failure opened a new RTP generation: open=%d", z.openCalls.Load())
	}
	if channels.c.StreamID != streamID || channels.c.CurrentSSRC != "0200000007" {
		t.Fatalf("cleanup failure cleared durable quarantine: %+v", channels.c)
	}
}

func TestServiceStopIfCurrentRejectsStaleAndCleansCurrentOnce(t *testing.T) {
	withFixedAddressPlaybackSettings(t, true, false)
	z := &mockZLM{port: 40000}
	inv := &mockInviter{}
	s, notifier, channels := newFixedSvc(t, z, inv, onlineDevice(), aChannel())
	inv.onInvite = func(sess *uac.Session) {
		z.online.Store(true)
		go notifier.Publish(sess.StreamID)
	}
	result, err := s.Start(context.Background(), onlineDevice().DeviceID, aChannel().ChannelID)
	if err != nil {
		t.Fatal(err)
	}
	current := stream.LiveRef{StreamID: result.StreamID, SSRC: result.SSRC, Generation: result.Generation, NodeID: result.Node.ID}
	stale := current
	stale.Generation--
	stale.SSRC = "0200009999"

	if handled, err := s.StopIfCurrent(context.Background(), stale); err != nil || !handled {
		t.Fatalf("stale stop handled=%v err=%v", handled, err)
	}
	if inv.byeCalls.Load() != 0 || z.closeCalls.Load() != 0 || channels.c.StreamID == "" {
		t.Fatalf("stale stop changed current: bye=%d close=%d channel=%+v", inv.byeCalls.Load(), z.closeCalls.Load(), channels.c)
	}
	if handled, err := s.StopIfCurrent(context.Background(), current); err != nil || !handled {
		t.Fatalf("current stop handled=%v err=%v", handled, err)
	}
	if handled, err := s.StopIfCurrent(context.Background(), current); err != nil || handled {
		t.Fatalf("repeated stop handled=%v err=%v", handled, err)
	}
	if inv.byeCalls.Load() != 1 || z.closeCalls.Load() != 1 {
		t.Fatalf("current cleanup counts bye=%d close=%d", inv.byeCalls.Load(), z.closeCalls.Load())
	}
	if channels.c.StreamID != "" || channels.c.CurrentSSRC != "" {
		t.Fatalf("current DB state not cleared: %+v", channels.c)
	}
}

func TestServiceStopOnNoneReaderRechecksReadersBeforeConditionalStop(t *testing.T) {
	withFixedAddressPlaybackSettings(t, true, false)
	z := &readerCountZLM{mockZLM: mockZLM{port: 40000}, readers: 1}
	inv := &mockInviter{}
	s, notifier, channels := newFixedSvc(t, z, inv, onlineDevice(), aChannel())
	inv.onInvite = func(session *uac.Session) {
		z.online.Store(true)
		go notifier.Publish(session.StreamID)
	}
	result, err := s.Start(context.Background(), onlineDevice().DeviceID, aChannel().ChannelID)
	if err != nil {
		t.Fatal(err)
	}
	ref := resultLiveRef(result)
	lifecycle := &fakePlaybackRecordingLifecycle{onEnd: func() {
		if z.closeCalls.Load() != 0 {
			t.Fatal("无人观看停流必须先收尾录像")
		}
	}}
	s.SetPlaybackRecordingLifecycle(lifecycle)
	if stopped, err := s.StopOnNoneReader(context.Background(), ref); err != nil || stopped {
		t.Fatalf("reader recovery stopped=%v err=%v", stopped, err)
	}
	if z.closeCalls.Load() != 0 || channels.c.StreamID == "" {
		t.Fatalf("reader recovery changed current stream: close=%d channel=%+v", z.closeCalls.Load(), channels.c)
	}
	if lifecycle.endCalls.Load() != 0 {
		t.Fatalf("读者恢复时不应收尾录像,实际 %d", lifecycle.endCalls.Load())
	}
	z.readers = 0
	if stopped, err := s.StopOnNoneReader(context.Background(), ref); err != nil || !stopped {
		t.Fatalf("zero readers stopped=%v err=%v", stopped, err)
	}
	if z.closeCalls.Load() != 1 || channels.c.StreamID != "" {
		t.Fatalf("zero readers cleanup close=%d channel=%+v", z.closeCalls.Load(), channels.c)
	}
	if lifecycle.endCalls.Load() != 1 {
		t.Fatalf("零读者停流应收尾录像一次,实际 %d", lifecycle.endCalls.Load())
	}
}

func TestServicePersistedStopCASMismatchHasNoMediaSideEffects(t *testing.T) {
	withFixedAddressPlaybackSettings(t, true, false)
	z := &mockZLM{port: 40000}
	inv := &mockInviter{}
	s, notifier, channels := newFixedSvc(t, z, inv, onlineDevice(), aChannel())
	inv.onInvite = func(session *uac.Session) {
		z.online.Store(true)
		go notifier.Publish(session.StreamID)
	}
	result, err := s.Start(context.Background(), onlineDevice().DeviceID, aChannel().ChannelID)
	if err != nil {
		t.Fatal(err)
	}
	channels.c.CurrentSSRC = "0200000999"
	if err := s.StopIfPersistedCurrent(context.Background(), result.StreamID, result.SSRC); err != nil {
		t.Fatal(err)
	}
	if inv.byeCalls.Load() != 0 || z.closeCalls.Load() != 0 {
		t.Fatalf("CAS mismatch touched media: bye=%d close=%d", inv.byeCalls.Load(), z.closeCalls.Load())
	}
	if channels.c.StreamID != result.StreamID || channels.c.CurrentSSRC != "0200000999" {
		t.Fatalf("CAS mismatch changed new DB generation: %+v", channels.c)
	}
}
