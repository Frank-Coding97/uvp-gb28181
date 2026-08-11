package play

import (
	"context"
	"errors"
	"fmt"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

var (
	ErrLiveRecoveryPending    = errors.New("live recovery pending")
	ErrReaderCountUnavailable = errors.New("reader count unavailable")
)

type RecoveryStats struct {
	Scanned  int
	Restored int
	Cleaned  int
	Skipped  int
	Failed   int
}

func (s *Service) BeginRecovery() {
	s.coordinator().BeginRecovery()
}

func (s *Service) FinishRecovery() {
	s.coordinator().FinishRecovery()
}

func (s *Service) RecoverLiveSessions(ctx context.Context) (RecoveryStats, error) {
	var stats RecoveryStats
	if !s.useMultiNode() {
		return stats, nil
	}
	if s.ssrcAllocatorErr != nil || s.ssrcAllocator == nil {
		return stats, fmt.Errorf("初始化实时 SSRC 分配器失败: %w", s.ssrcAllocatorErr)
	}
	channels, err := s.channels.ListPlayingChannels(ctx)
	if err != nil {
		return stats, err
	}
	stats.Scanned = len(channels)
	coordinator := s.coordinator()
	for _, channel := range channels {
		if ctx.Err() != nil {
			return stats, ctx.Err()
		}
		if channel == nil {
			stats.Skipped++
			continue
		}
		ssrc := CurrentSSRCForChannel(channel)
		if ssrc == "" {
			stats.Skipped++
			continue
		}

		mediaNode, definitelyOffline, probeErr := s.locateRecoveryNode(ctx, channel.StreamID)
		if probeErr != nil {
			stats.Failed++
			continue
		}
		if definitelyOffline {
			cleared, clearErr := s.channels.ClearIfCurrent(ctx, channel.StreamID, ssrc)
			if clearErr != nil {
				stats.Failed++
			} else if cleared {
				stats.Cleaned++
			} else {
				stats.Skipped++
			}
			continue
		}
		if mediaNode == nil {
			stats.Skipped++
			continue
		}

		mode := LiveModeDynamic
		if _, _, parseErr := ParseFixedStreamID(channel.StreamID); parseErr == nil {
			mode = LiveModeFixed
		} else if channel.StreamID != ssrc {
			stats.Skipped++
			continue
		}
		if reserveErr := s.ssrcAllocator.Reserve(ssrc); reserveErr != nil {
			stats.Failed++
			continue
		}
		generation := s.nextGeneration.Add(1)
		ref := stream.LiveRef{StreamID: channel.StreamID, SSRC: ssrc, Generation: generation, NodeID: mediaNode.ID}
		if !s.bindRecoveredLocation(ref) {
			s.ssrcAllocator.Release(ssrc)
			stats.Failed++
			continue
		}
		result := s.buildNodeResult(ctx, channel.StreamID, ssrc, mediaNode, true)
		result.Generation = generation
		result.ModeAtStart = mode
		req := Request{DeviceID: channel.DeviceID, ChannelID: channel.ChannelID, Trigger: "recovery", RequiredNode: mediaNode.ID}
		if !coordinator.Restore(req, result) {
			s.unbindLocation(ref)
			s.ssrcAllocator.Release(ssrc)
			stats.Failed++
			continue
		}
		stats.Restored++
	}
	return stats, nil
}

func (s *Service) locateRecoveryNode(ctx context.Context, streamID string) (*node.Node, bool, error) {
	active := s.registry.ListActive()
	if len(active) == 0 {
		return nil, false, nil
	}
	var owner *node.Node
	for _, candidate := range active {
		online, err := s.clientForNode(candidate).IsMediaOnline(ctx, zlmApp, streamID)
		if err != nil {
			return nil, false, err
		}
		if !online {
			continue
		}
		if owner != nil {
			return nil, false, fmt.Errorf("stream %s is online on multiple nodes", streamID)
		}
		owner = candidate
	}
	if owner == nil {
		return nil, true, nil
	}
	return owner, false, nil
}

func (s *Service) bindRecoveredLocation(ref stream.LiveRef) bool {
	if versioned, ok := s.locationMap.(interface{ BindCurrent(stream.LiveRef) bool }); ok {
		return versioned.BindCurrent(ref)
	}
	s.locationMap.Bind(ref.StreamID, ref.NodeID)
	return true
}

func (s *Service) CurrentLiveRef(streamID string) (stream.LiveRef, bool) {
	result, ok := s.coordinator().CurrentResult(streamID)
	if !ok {
		return stream.LiveRef{}, false
	}
	return resultLiveRef(result), true
}

func resultLiveRef(result *Result) stream.LiveRef {
	if result == nil {
		return stream.LiveRef{}
	}
	var nodeID int64
	if result.Node != nil {
		nodeID = result.Node.ID
	}
	return stream.LiveRef{StreamID: result.StreamID, SSRC: result.SSRC, Generation: result.Generation, NodeID: nodeID}
}

func (s *Service) StopIfCurrent(ctx context.Context, ref stream.LiveRef) (bool, error) {
	return s.coordinator().StopIfCurrent(ctx, ref)
}

func (s *Service) StopCurrent(ctx context.Context, streamID string) error {
	ref, ok := s.CurrentLiveRef(streamID)
	if !ok {
		return nil
	}
	_, err := s.StopIfCurrent(ctx, ref)
	return err
}

type mediaListClient interface {
	GetMediaList(context.Context, string, string, string) ([]zlm.MediaInfo, error)
}

func (s *Service) StopOnNoneReader(ctx context.Context, captured stream.LiveRef) (bool, error) {
	current, ok := s.CurrentLiveRef(captured.StreamID)
	if !ok || current != captured {
		return false, nil
	}
	var client ZLM
	if s.useMultiNode() {
		mediaNode, exists := s.registry.Get(captured.NodeID)
		if !exists {
			return false, fmt.Errorf("stream %s owner node %d not found", captured.StreamID, captured.NodeID)
		}
		client = s.clientForNode(mediaNode)
	} else {
		client = s.zlm
	}
	lister, ok := client.(mediaListClient)
	if !ok {
		return false, ErrReaderCountUnavailable
	}
	media, err := lister.GetMediaList(ctx, "__defaultVhost__", zlmApp, captured.StreamID)
	if err != nil {
		return false, err
	}
	for _, item := range media {
		if item.ReaderCount > 0 {
			return false, nil
		}
	}
	return s.StopIfCurrent(ctx, captured)
}

// StopIfPersistedCurrent is the reconciler-safe stop entry point. When an
// in-memory generation exists it enters the same coordinator barrier; stale
// rows without an owner are only CAS-cleared and never emit a guessed BYE.
func (s *Service) StopIfPersistedCurrent(ctx context.Context, streamID, ssrc string) error {
	if current, ok := s.CurrentLiveRef(streamID); ok {
		if current.SSRC != ssrc {
			return nil
		}
		_, err := s.StopIfCurrent(ctx, current)
		return err
	}
	cleared, err := s.channels.ClearIfCurrent(ctx, streamID, ssrc)
	if err != nil || !cleared {
		return err
	}
	if current, ok := s.lookupLocationRef(streamID); ok && current.SSRC == ssrc {
		s.unbindLocation(current)
	}
	if s.ssrcAllocator != nil {
		s.ssrcAllocator.Release(ssrc)
	}
	return nil
}

func (s *Service) lookupLocationRef(streamID string) (stream.LiveRef, bool) {
	if !s.useMultiNode() {
		return stream.LiveRef{}, false
	}
	if versioned, ok := s.locationMap.(interface {
		LookupCurrent(string) (stream.LiveRef, bool)
	}); ok {
		return versioned.LookupCurrent(streamID)
	}
	nodeID, ok := s.locationMap.Lookup(streamID)
	if !ok {
		return stream.LiveRef{}, false
	}
	return stream.LiveRef{StreamID: streamID, NodeID: nodeID}, true
}

type conditionalInviter interface {
	ByeIfCurrent(context.Context, *uac.SessionManager, uac.SessionRef) (bool, error)
}

func (s *Service) stopCurrentResult(ctx context.Context, result *Result) error {
	if result == nil {
		return nil
	}
	cleanupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ctx = cleanupCtx
	ref := resultLiveRef(result)
	cleared, clearErr := s.channels.ClearIfCurrent(ctx, ref.StreamID, ref.SSRC)
	if clearErr != nil || !cleared {
		return clearErr
	}

	var byeErr error
	if inviter, ok := s.inviter.(conditionalInviter); ok {
		_, byeErr = inviter.ByeIfCurrent(ctx, s.sessions, uac.SessionRef{
			StreamID: ref.StreamID, SSRC: ref.SSRC, Generation: ref.Generation, NodeID: ref.NodeID,
		})
	} else {
		byeErr = s.inviter.Bye(ctx, s.sessions, ref.StreamID)
	}

	var closeErr error
	if s.useMultiNode() {
		mediaNode, ok := s.registry.Get(ref.NodeID)
		if !ok {
			closeErr = fmt.Errorf("stream %s owner node %d not found", ref.StreamID, ref.NodeID)
		} else {
			closeErr = s.clientForNode(mediaNode).CloseRtpServer(ctx, ref.StreamID)
		}
	} else if s.zlm != nil {
		closeErr = s.zlm.CloseRtpServer(ctx, ref.StreamID)
	}
	s.unbindLocation(ref)
	if s.ssrcAllocator != nil {
		s.ssrcAllocator.Release(ref.SSRC)
	}
	if byeErr != nil {
		return byeErr
	}
	return closeErr
}
