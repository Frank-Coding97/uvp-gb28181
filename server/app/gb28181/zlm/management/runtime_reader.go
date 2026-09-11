package management

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
)

const DefaultRuntimeCacheTTL = 250 * time.Millisecond

// ErrMediaNotFound distinguishes an absent media resource from an absent
// management node. RuntimeReader preserves this sentinel through the
// executor's redacted error envelope so callers can map the resource result
// without exposing ZLM's response text.
var ErrMediaNotFound = errors.New("media resource not found")

// RuntimeReader is the typed read facade for ZLM runtime data. It has no
// arbitrary API name, query map or URL forwarding method. All upstream calls
// pass through NodeExecutor for node state, authorization and deadline checks.
type RuntimeReader struct {
	executor *NodeExecutor
	ttl      time.Duration

	mu    sync.RWMutex
	cache map[string]runtimeCacheEntry
	group singleflight.Group
}

type RuntimeReaderOption func(*RuntimeReader)

func WithRuntimeCacheTTL(ttl time.Duration) RuntimeReaderOption {
	return func(reader *RuntimeReader) {
		if ttl > 0 {
			reader.ttl = ttl
		}
	}
}

func NewRuntimeReader(executor *NodeExecutor, options ...RuntimeReaderOption) *RuntimeReader {
	reader := &RuntimeReader{
		executor: executor,
		ttl:      DefaultRuntimeCacheTTL,
		cache:    make(map[string]runtimeCacheEntry),
	}
	for _, option := range options {
		if option != nil {
			option(reader)
		}
	}
	return reader
}

type runtimeCacheEntry struct {
	expiresAt time.Time
	value     interface{}
}

// GetStatistic returns the object-instance counters from one active node.
func (r *RuntimeReader) GetStatistic(ctx context.Context, nodeID int64) (zlm.Statistic, error) {
	var zero zlm.Statistic
	if err := r.guardRead(ctx, nodeID); err != nil {
		return zero, err
	}
	if err := r.rejectUnsupported(nodeID, zlm.CapabilityGetStatistic); err != nil {
		return zero, err
	}
	value, err := r.load(ctx, cacheKey("statistic", nodeID, ""), func(callCtx context.Context) (interface{}, error) {
		var statistic zlm.Statistic
		err := r.executeRead(callCtx, nodeID, func(operationCtx context.Context, client *zlm.Client) error {
			var readErr error
			statistic, readErr = client.GetStatistic(operationCtx)
			return readErr
		})
		if err != nil {
			return nil, err
		}
		return statistic, nil
	})
	if err != nil {
		return zero, err
	}
	statistic, ok := value.(zlm.Statistic)
	if !ok {
		return zero, NewInternalError(nodeIDString(nodeID), "runtime statistic cache type mismatch")
	}
	return statistic, nil
}

// GetMediaTrafficStatistic returns the exact socket-level media rates exposed
// by ZLMediaKit-UVP. Older nodes return an error and callers may fall back to
// the legacy media-source rate.
func (r *RuntimeReader) GetMediaTrafficStatistic(ctx context.Context, nodeID int64) (zlm.MediaTrafficStatistic, error) {
	var zero zlm.MediaTrafficStatistic
	if err := r.guardRead(ctx, nodeID); err != nil {
		return zero, err
	}
	value, err := r.load(ctx, cacheKey("media-traffic", nodeID, ""), func(callCtx context.Context) (interface{}, error) {
		var statistic zlm.MediaTrafficStatistic
		err := r.executeRead(callCtx, nodeID, func(operationCtx context.Context, client *zlm.Client) error {
			var readErr error
			statistic, readErr = client.GetMediaTrafficStatistic(operationCtx)
			return readErr
		})
		if err != nil {
			return nil, err
		}
		return statistic, nil
	})
	if err != nil {
		return zero, err
	}
	statistic, ok := value.(zlm.MediaTrafficStatistic)
	if !ok {
		return zero, NewInternalError(nodeIDString(nodeID), "media traffic cache type mismatch")
	}
	return statistic, nil
}

// GetAllSessions returns typed network sessions. Filter values form part of
// the cache key, so two distinct queries can never reuse each other's result.
func (r *RuntimeReader) GetAllSessions(ctx context.Context, nodeID int64, filter zlm.SessionFilter) ([]zlm.Session, error) {
	if err := r.guardRead(ctx, nodeID); err != nil {
		return nil, err
	}
	if err := r.rejectUnsupported(nodeID, zlm.CapabilityGetAllSession); err != nil {
		return nil, err
	}
	key := cacheKey("sessions", nodeID, sessionFilterKey(filter))
	value, err := r.load(ctx, key, func(callCtx context.Context) (interface{}, error) {
		var sessions []zlm.Session
		err := r.executeRead(callCtx, nodeID, func(operationCtx context.Context, client *zlm.Client) error {
			var readErr error
			sessions, readErr = client.GetAllSessions(operationCtx, filter)
			return readErr
		})
		if err != nil {
			return nil, err
		}
		return cloneSessions(sessions), nil
	})
	if err != nil {
		return nil, err
	}
	sessions, ok := value.([]zlm.Session)
	if !ok {
		return nil, NewInternalError(nodeIDString(nodeID), "runtime session cache type mismatch")
	}
	return cloneSessions(sessions), nil
}

// GetMediaListFiltered returns media sources matching the complete typed
// filter. Schema is part of the cache key and is passed to ZLM; a result from
// one protocol view can therefore never be reused for another.
func (r *RuntimeReader) GetMediaListFiltered(ctx context.Context, nodeID int64, filter zlm.MediaFilter) ([]zlm.MediaInfo, error) {
	if err := r.guardRead(ctx, nodeID); err != nil {
		return nil, err
	}
	if err := r.rejectUnsupported(nodeID, zlm.CapabilityGetMediaList); err != nil {
		return nil, err
	}
	key := cacheKey("media-list", nodeID, mediaFilterKey(filter))
	value, err := r.load(ctx, key, func(callCtx context.Context) (interface{}, error) {
		var media []zlm.MediaInfo
		err := r.executeRead(callCtx, nodeID, func(operationCtx context.Context, client *zlm.Client) error {
			var readErr error
			media, readErr = client.GetMediaListFiltered(operationCtx, filter)
			return readErr
		})
		if err != nil {
			return nil, err
		}
		return cloneMediaInfos(media), nil
	})
	if err != nil {
		return nil, err
	}
	media, ok := value.([]zlm.MediaInfo)
	if !ok {
		return nil, NewInternalError(nodeIDString(nodeID), "runtime media list cache type mismatch")
	}
	return cloneMediaInfos(media), nil
}

// GetMediaList is the typed list-all form used by overview and bootstrap
// adapters. It delegates to the same bounded/cache-aware implementation and
// never exposes a generic query map or API name.
func (r *RuntimeReader) GetMediaList(ctx context.Context, nodeID int64) ([]zlm.MediaInfo, error) {
	return r.GetMediaListFiltered(ctx, nodeID, zlm.MediaFilter{})
}

// GetMediaInfo returns a typed detail snapshot for one complete media target.
// The strict client method is used so NotFound and other ZLM failures are not
// silently converted into an offline-looking value.
func (r *RuntimeReader) GetMediaInfo(ctx context.Context, nodeID int64, target zlm.StreamTarget) (*zlm.MediaInfo, error) {
	if err := target.Validate(); err != nil {
		return nil, NewValidationError(map[string]string{"media": "invalid stream target"})
	}
	if err := r.guardRead(ctx, nodeID); err != nil {
		return nil, err
	}
	if err := r.rejectUnsupported(nodeID, zlm.CapabilityGetMediaInfo); err != nil {
		return nil, err
	}
	key := cacheKey("media-info", nodeID, streamTargetKey(target))
	value, err := r.load(ctx, key, func(callCtx context.Context) (interface{}, error) {
		var media *zlm.MediaInfo
		err := r.executeRead(callCtx, nodeID, func(operationCtx context.Context, client *zlm.Client) error {
			var readErr error
			media, readErr = client.GetMediaInfoStrict(operationCtx, target.Schema, target.VHost, target.App, target.Stream)
			if errors.Is(readErr, zlm.ErrMediaNotFound) {
				return errors.Join(ErrMediaNotFound, readErr)
			}
			return readErr
		})
		if err != nil {
			return nil, err
		}
		return cloneMediaInfo(media), nil
	})
	if err != nil {
		return nil, err
	}
	media, ok := value.(*zlm.MediaInfo)
	if !ok {
		return nil, NewInternalError(nodeIDString(nodeID), "runtime media info cache type mismatch")
	}
	return cloneMediaInfo(media), nil
}

// GetMediaPlayerList returns the active players for one complete media
// target. The target is part of the cache key and every returned slice is
// copied before crossing the management boundary.
func (r *RuntimeReader) GetMediaPlayerList(ctx context.Context, nodeID int64, target zlm.StreamTarget) ([]zlm.MediaPlayer, error) {
	if err := target.Validate(); err != nil {
		return nil, NewValidationError(map[string]string{"media": "invalid stream target"})
	}
	if err := r.guardRead(ctx, nodeID); err != nil {
		return nil, err
	}
	if err := r.rejectUnsupported(nodeID, zlm.CapabilityGetMediaPlayerList); err != nil {
		return nil, err
	}
	key := cacheKey("media-players", nodeID, streamTargetKey(target))
	value, err := r.load(ctx, key, func(callCtx context.Context) (interface{}, error) {
		var players []zlm.MediaPlayer
		err := r.executeRead(callCtx, nodeID, func(operationCtx context.Context, client *zlm.Client) error {
			var readErr error
			players, readErr = client.GetMediaPlayerList(operationCtx, target.Schema, target.VHost, target.App, target.Stream)
			if errors.Is(readErr, zlm.ErrMediaNotFound) {
				return errors.Join(ErrMediaNotFound, readErr)
			}
			return readErr
		})
		if err != nil {
			return nil, err
		}
		return cloneMediaPlayers(players), nil
	})
	if err != nil {
		return nil, err
	}
	players, ok := value.([]zlm.MediaPlayer)
	if !ok {
		return nil, NewInternalError(nodeIDString(nodeID), "runtime media player cache type mismatch")
	}
	return cloneMediaPlayers(players), nil
}

// GetCapabilityProfile probes getApiList and records supported, unsupported
// or unknown states. A failed probe is not cached as an empty profile; callers
// can still perform a controlled typed read while the capability remains
// unknown.
func (r *RuntimeReader) GetCapabilityProfile(ctx context.Context, nodeID int64) (zlm.CapabilityProfile, error) {
	var zero zlm.CapabilityProfile
	if err := r.guardRead(ctx, nodeID); err != nil {
		return zero, err
	}
	key := cacheKey("capability", nodeID, "")
	value, err := r.load(ctx, key, func(callCtx context.Context) (interface{}, error) {
		var profile zlm.CapabilityProfile
		readErr := r.executeRead(callCtx, nodeID, func(operationCtx context.Context, client *zlm.Client) error {
			var err error
			profile, err = client.GetCapabilityProfile(operationCtx)
			return err
		})
		if readErr != nil {
			return profile, readErr
		}
		return cloneCapabilityProfile(profile), nil
	})
	if err != nil {
		// The client returns the partially populated unknown profile alongside a
		// probe error. load deliberately does not retain failed values, but it
		// still returns that profile to make the unknown state observable.
		if profile, ok := value.(zlm.CapabilityProfile); ok {
			return cloneCapabilityProfile(profile), err
		}
		return zero, err
	}
	profile, ok := value.(zlm.CapabilityProfile)
	if !ok {
		return zero, NewInternalError(nodeIDString(nodeID), "runtime capability cache type mismatch")
	}
	return cloneCapabilityProfile(profile), nil
}

// GetThreadsLoad and GetWorkThreadsLoad are typed runtime reads used by node
// overview aggregation. They intentionally do not accept a generic API name.
func (r *RuntimeReader) GetThreadsLoad(ctx context.Context, nodeID int64) (float64, error) {
	loads, err := r.GetThreadsLoadDetail(ctx, nodeID)
	return zlm.AverageThreadLoad(loads), err
}

// GetThreadsLoadDetail preserves the per-event-poller detail and caches a
// defensive copy for the same short runtime TTL as the other node reads.
func (r *RuntimeReader) GetThreadsLoadDetail(ctx context.Context, nodeID int64) ([]zlm.ThreadLoad, error) {
	if err := r.guardRead(ctx, nodeID); err != nil {
		return nil, err
	}
	if err := r.rejectUnsupported(nodeID, "getThreadsLoad"); err != nil {
		return nil, err
	}
	key := cacheKey("getThreadsLoadDetail", nodeID, "")
	value, err := r.load(ctx, key, func(callCtx context.Context) (interface{}, error) {
		var loads []zlm.ThreadLoad
		err := r.executeRead(callCtx, nodeID, func(operationCtx context.Context, client *zlm.Client) error {
			var readErr error
			loads, readErr = client.GetThreadsLoadDetail(operationCtx)
			return readErr
		})
		if err != nil {
			return nil, err
		}
		return append([]zlm.ThreadLoad(nil), loads...), nil
	})
	if err != nil {
		return nil, err
	}
	loads, ok := value.([]zlm.ThreadLoad)
	if !ok {
		return nil, NewInternalError(nodeIDString(nodeID), "runtime thread load cache type mismatch")
	}
	return append([]zlm.ThreadLoad(nil), loads...), nil
}

func (r *RuntimeReader) GetWorkThreadsLoad(ctx context.Context, nodeID int64) (float64, error) {
	return r.readLoad(ctx, nodeID, "getWorkThreadsLoad", func(operationCtx context.Context, client *zlm.Client) (float64, error) {
		return client.GetWorkThreadsLoad(operationCtx)
	})
}

type loadReader func(context.Context, *zlm.Client) (float64, error)

func (r *RuntimeReader) readLoad(ctx context.Context, nodeID int64, api string, read loadReader) (float64, error) {
	if err := r.guardRead(ctx, nodeID); err != nil {
		return 0, err
	}
	if err := r.rejectUnsupported(nodeID, api); err != nil {
		return 0, err
	}
	key := cacheKey(api, nodeID, "")
	value, err := r.load(ctx, key, func(callCtx context.Context) (interface{}, error) {
		var load float64
		err := r.executeRead(callCtx, nodeID, func(operationCtx context.Context, client *zlm.Client) error {
			var readErr error
			load, readErr = read(operationCtx, client)
			return readErr
		})
		if err != nil {
			return nil, err
		}
		return load, nil
	})
	if err != nil {
		return 0, err
	}
	load, ok := value.(float64)
	if !ok {
		return 0, NewInternalError(nodeIDString(nodeID), "runtime load cache type mismatch")
	}
	return load, nil
}

// InvalidateNode drops all cached runtime values for one node. In-flight
// calls are allowed to finish; a later call will observe the empty cache.
func (r *RuntimeReader) InvalidateNode(nodeID int64) {
	if r == nil {
		return
	}
	prefix := nodeIDString(nodeID) + ":"
	r.mu.Lock()
	defer r.mu.Unlock()
	for key := range r.cache {
		if strings.HasPrefix(key, prefix) {
			delete(r.cache, key)
		}
	}
}

func (r *RuntimeReader) rejectUnsupported(nodeID int64, api string) error {
	if r == nil {
		return NewInternalError(nodeIDString(nodeID), "runtime reader is not configured")
	}
	value, ok := r.cached(cacheKey("capability", nodeID, ""))
	if !ok {
		// No successful capability probe exists. Unknown is explicit, so a
		// typed operation may make one controlled attempt.
		return nil
	}
	profile, ok := value.(zlm.CapabilityProfile)
	if !ok {
		return NewInternalError(nodeIDString(nodeID), "runtime capability cache type mismatch")
	}
	if profile.Status(api) == zlm.CapabilityUnsupported {
		return NewUnsupportedCapabilityError(nodeIDString(nodeID), api)
	}
	return nil
}

func (r *RuntimeReader) guardRead(ctx context.Context, nodeID int64) error {
	if r == nil || r.executor == nil {
		return NewInternalError(nodeIDString(nodeID), "runtime reader is not configured")
	}
	_, err := r.executor.guard(ctx, nodeID, NodeOperationRead)
	return err
}

func (r *RuntimeReader) executeRead(ctx context.Context, nodeID int64, operation ClientOperation) error {
	if r == nil || r.executor == nil {
		return NewInternalError(nodeIDString(nodeID), "runtime reader is not configured")
	}
	return r.executor.ExecuteRead(ctx, nodeID, operation)
}

func (r *RuntimeReader) load(ctx context.Context, key string, fetch func(context.Context) (interface{}, error)) (interface{}, error) {
	if r == nil {
		return nil, NewInternalError("", "runtime reader is not configured")
	}
	if fetch == nil {
		return nil, NewInternalError("", "runtime reader fetch is not configured")
	}
	if value, ok := r.cached(key); ok {
		return value, nil
	}
	result := r.group.DoChan(key, func() (interface{}, error) {
		if value, ok := r.cached(key); ok {
			return value, nil
		}
		// A caller that leaves while another caller is waiting must not cancel
		// the shared upstream read. The executor supplies its own bounded
		// deadline; WithoutCancel preserves request values for authorization
		// while detaching cancellation from this singleflight leader.
		value, err := fetch(context.WithoutCancel(nonNilContext(ctx)))
		if err != nil {
			return value, err
		}
		r.store(key, value)
		return value, nil
	})
	select {
	case <-nonNilContext(ctx).Done():
		return nil, nonNilContext(ctx).Err()
	case completed := <-result:
		return completed.Val, completed.Err
	}
}

func (r *RuntimeReader) cached(key string) (interface{}, bool) {
	if r == nil {
		return nil, false
	}
	now := time.Now()
	r.mu.RLock()
	entry, ok := r.cache[key]
	r.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if now.Before(entry.expiresAt) {
		return entry.value, true
	}
	r.mu.Lock()
	if current, exists := r.cache[key]; exists && !now.Before(current.expiresAt) {
		delete(r.cache, key)
	}
	r.mu.Unlock()
	return nil, false
}

func (r *RuntimeReader) store(key string, value interface{}) {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.cache == nil {
		r.cache = make(map[string]runtimeCacheEntry)
	}
	ttl := r.ttl
	if ttl <= 0 {
		ttl = DefaultRuntimeCacheTTL
	}
	r.cache[key] = runtimeCacheEntry{expiresAt: time.Now().Add(ttl), value: value}
	r.mu.Unlock()
}

func cacheKey(kind string, nodeID int64, suffix string) string {
	return nodeIDString(nodeID) + ":" + kind + ":" + suffix
}

func sessionFilterKey(filter zlm.SessionFilter) string {
	values := url.Values{}
	values.Set("local_port", strconv.Itoa(filter.LocalPort))
	values.Set("peer_ip", filter.PeerIP)
	return values.Encode()
}

func mediaFilterKey(filter zlm.MediaFilter) string {
	values := url.Values{}
	values.Set("schema", filter.Schema)
	values.Set("vhost", filter.VHost)
	values.Set("app", filter.App)
	values.Set("stream", filter.Stream)
	return values.Encode()
}

func streamTargetKey(target zlm.StreamTarget) string {
	values := url.Values{}
	values.Set("schema", target.Schema)
	values.Set("vhost", target.VHost)
	values.Set("app", target.App)
	values.Set("stream", target.Stream)
	return values.Encode()
}

func cloneSessions(sessions []zlm.Session) []zlm.Session {
	if sessions == nil {
		return []zlm.Session{}
	}
	return append([]zlm.Session(nil), sessions...)
}

func cloneMediaInfos(media []zlm.MediaInfo) []zlm.MediaInfo {
	if media == nil {
		return []zlm.MediaInfo{}
	}
	cloned := make([]zlm.MediaInfo, len(media))
	for i := range media {
		cloned[i] = *cloneMediaInfo(&media[i])
	}
	return cloned
}

func cloneMediaInfo(media *zlm.MediaInfo) *zlm.MediaInfo {
	if media == nil {
		return nil
	}
	copy := *media
	copy.Tracks = make([]zlm.MediaTrack, len(media.Tracks))
	for i := range media.Tracks {
		copy.Tracks[i] = media.Tracks[i]
		if media.Tracks[i].Loss != nil {
			loss := *media.Tracks[i].Loss
			copy.Tracks[i].Loss = &loss
		}
	}
	if media.OriginSock != nil {
		sock := *media.OriginSock
		copy.OriginSock = &sock
	}
	return &copy
}

func cloneMediaPlayers(players []zlm.MediaPlayer) []zlm.MediaPlayer {
	if players == nil {
		return []zlm.MediaPlayer{}
	}
	return append([]zlm.MediaPlayer(nil), players...)
}

func cloneCapabilityProfile(profile zlm.CapabilityProfile) zlm.CapabilityProfile {
	copy := profile
	copy.APIs = append([]string(nil), profile.APIs...)
	if profile.Reasons != nil {
		copy.Reasons = make(map[string]string, len(profile.Reasons))
		for key, reason := range profile.Reasons {
			copy.Reasons[key] = reason
		}
	}
	return copy
}
