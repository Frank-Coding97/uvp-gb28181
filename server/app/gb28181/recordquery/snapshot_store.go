package recordquery

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"sync"
	"time"

	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
)

type SnapshotInput struct {
	OwnerUserID   uint
	OwnerClientID int64
	ChannelID     uint
	DeviceCode    string
	ChannelCode   string
	QueryID       string
	SegmentStart  time.Time
	SegmentEnd    time.Time
	Record        manscdp.RecordInfoItem
}

type Snapshot struct {
	RecordKey        string
	OwnerUserID      uint
	OwnerClientID    int64
	ChannelID        uint
	DeviceCode       string
	ChannelCode      string
	QueryID          string
	SegmentStart     time.Time
	SegmentEnd       time.Time
	Record           manscdp.RecordInfoItem
	MetadataDigest   string
	ExpiresAt        time.Time
	validationDigest string
	invalidated      bool
}

type ResolveRequest struct {
	RecordKey     string
	OwnerUserID   uint
	OwnerClientID int64
	ChannelID     uint
	PlayFrom      time.Time
}

type ResultSnapshotStore struct {
	mu          sync.Mutex
	ttl         time.Duration
	now         func() time.Time
	snapshots   map[string]Snapshot
	generations map[ownerChannel]queryGeneration
	// invalidatedScopes 记录被显式失效过的查询范围:旧 RecordKey 报"过期"
	invalidatedScopes map[ownerChannel]struct{}
	closed            bool
	// maxSnapshots 快照总量硬上限,0 表示不设限(测试用)
	maxSnapshots int
}

// defaultMaxSnapshots 默认快照总量上限:高频重复查询不会让内存无界增长
const defaultMaxSnapshots = 512

type ownerChannel struct {
	ownerKind     uint8
	ownerUserID   uint
	ownerClientID int64
	channelID     uint
}

type queryGeneration struct {
	queryID   string
	expiresAt time.Time
}

const (
	ownerKindUser uint8 = iota + 1
	ownerKindClient
)

func userOwnerChannel(ownerUserID, channelID uint) (ownerChannel, bool) {
	if ownerUserID == 0 || channelID == 0 {
		return ownerChannel{}, false
	}
	return ownerChannel{ownerKind: ownerKindUser, ownerUserID: ownerUserID, channelID: channelID}, true
}

func clientOwnerChannel(ownerClientID int64, channelID uint) (ownerChannel, bool) {
	if ownerClientID <= 0 || channelID == 0 {
		return ownerChannel{}, false
	}
	return ownerChannel{ownerKind: ownerKindClient, ownerClientID: ownerClientID, channelID: channelID}, true
}

func ownerChannelFor(userID uint, clientID int64, channelID uint) (ownerChannel, bool) {
	if (userID > 0) == (clientID > 0) {
		return ownerChannel{}, false
	}
	if userID > 0 {
		return userOwnerChannel(userID, channelID)
	}
	return clientOwnerChannel(clientID, channelID)
}

func NewResultSnapshotStore(ttl time.Duration, now func() time.Time) (*ResultSnapshotStore, error) {
	if ttl <= 0 {
		return nil, queryError(ErrorCodeInvalidArgument, ErrInvalidArgument)
	}
	if now == nil {
		now = time.Now
	}
	return &ResultSnapshotStore{
		ttl: ttl, now: now, snapshots: make(map[string]Snapshot), generations: make(map[ownerChannel]queryGeneration),
		invalidatedScopes: make(map[ownerChannel]struct{}), maxSnapshots: defaultMaxSnapshots,
	}, nil
}

func (s *ResultSnapshotStore) BeginQuery(ownerUserID, channelID uint, queryID string) error {
	key, ok := userOwnerChannel(ownerUserID, channelID)
	if !ok {
		return queryError(ErrorCodeInvalidArgument, ErrInvalidArgument)
	}
	return s.beginQuery(key, queryID)
}

func (s *ResultSnapshotStore) BeginQueryForClient(ownerClientID int64, channelID uint, queryID string) error {
	key, ok := clientOwnerChannel(ownerClientID, channelID)
	if !ok {
		return queryError(ErrorCodeInvalidArgument, ErrInvalidArgument)
	}
	return s.beginQuery(key, queryID)
}

func (s *ResultSnapshotStore) beginQuery(key ownerChannel, queryID string) error {
	if s == nil || key.channelID == 0 || normalizeIdentity(queryID) == "" {
		return queryError(ErrorCodeInvalidArgument, ErrInvalidArgument)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return queryError(ErrorCodeUnavailable, ErrUnavailable)
	}
	s.invalidateOwnerChannelLocked(key)
	s.generations[key] = queryGeneration{queryID: normalizeIdentity(queryID), expiresAt: s.now().Add(s.ttl)}
	return nil
}

// IsCurrentClientQuery reports whether a continuation still belongs to the
// latest non-expired result generation for this client and channel.
func (s *ResultSnapshotStore) IsCurrentClientQuery(ownerClientID int64, channelID uint, queryID string) bool {
	key, ok := clientOwnerChannel(ownerClientID, channelID)
	if s == nil || !ok || normalizeIdentity(queryID) == "" {
		return false
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	generation, exists := s.generations[key]
	if !exists || generation.queryID != normalizeIdentity(queryID) || !now.Before(generation.expiresAt) {
		if exists && !now.Before(generation.expiresAt) {
			delete(s.generations, key)
		}
		return false
	}
	return true
}

func (s *ResultSnapshotStore) Issue(input SnapshotInput) (Snapshot, error) {
	key, ownerOK := ownerChannelFor(input.OwnerUserID, input.OwnerClientID, input.ChannelID)
	if s == nil || !ownerOK || normalizeIdentity(input.DeviceCode) == "" ||
		normalizeIdentity(input.ChannelCode) == "" || normalizeIdentity(input.QueryID) == "" ||
		input.SegmentStart.IsZero() || !input.SegmentEnd.After(input.SegmentStart) {
		return Snapshot{}, queryError(ErrorCodeInvalidArgument, ErrInvalidArgument)
	}
	recordKey, err := randomToken(32)
	if err != nil {
		return Snapshot{}, queryError(ErrorCodeUnavailable, err)
	}
	now := s.now()
	snapshot := Snapshot{
		RecordKey: recordKey, OwnerUserID: input.OwnerUserID, OwnerClientID: input.OwnerClientID, ChannelID: input.ChannelID,
		DeviceCode: normalizeIdentity(input.DeviceCode), ChannelCode: normalizeIdentity(input.ChannelCode), QueryID: normalizeIdentity(input.QueryID),
		SegmentStart: input.SegmentStart, SegmentEnd: input.SegmentEnd, Record: input.Record,
		MetadataDigest: recordDigest(input.Record), ExpiresAt: now.Add(s.ttl),
	}
	snapshot.validationDigest = snapshotDigest(snapshot)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return Snapshot{}, queryError(ErrorCodeUnavailable, ErrUnavailable)
	}
	generationKey := key
	current, exists := s.generations[generationKey]
	if !exists {
		s.generations[generationKey] = queryGeneration{queryID: snapshot.QueryID, expiresAt: now.Add(s.ttl)}
		delete(s.invalidatedScopes, generationKey)
	} else if current.queryID != snapshot.QueryID || !now.Before(current.expiresAt) {
		s.mu.Unlock()
		return Snapshot{}, queryError(ErrorCodeExpired, ErrSnapshotExpired)
	}
	s.cleanupExpiredLocked(now)
	s.snapshots[recordKey] = snapshot
	// 单次查询可产生数千条快照:写入后强制淘汰到总量上限
	if max := s.maxSnapshots; max > 0 && len(s.snapshots) > max {
		s.evictOldestLocked(len(s.snapshots) - max)
	}
	s.mu.Unlock()
	return publicSnapshot(snapshot), nil
}

func (s *ResultSnapshotStore) Resolve(request ResolveRequest) (Snapshot, error) {
	key, ownerOK := ownerChannelFor(request.OwnerUserID, request.OwnerClientID, request.ChannelID)
	if s == nil || normalizeIdentity(request.RecordKey) == "" || !ownerOK {
		return Snapshot{}, queryError(ErrorCodeSnapshotMissing, ErrSnapshotNotFound)
	}
	now := s.now()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return Snapshot{}, queryError(ErrorCodeSnapshotMissing, ErrSnapshotNotFound)
	}
	snapshot, exists := s.snapshots[request.RecordKey]
	if exists && (snapshot.invalidated || !now.Before(snapshot.ExpiresAt)) {
		delete(s.snapshots, request.RecordKey)
		s.mu.Unlock()
		return Snapshot{}, queryError(ErrorCodeExpired, ErrSnapshotExpired)
	}
	s.mu.Unlock()
	if !exists || snapshot.OwnerUserID != request.OwnerUserID || snapshot.OwnerClientID != request.OwnerClientID || snapshot.ChannelID != request.ChannelID {
		// 被新查询取代/显式失效而删除的旧快照:报"过期"而非"不存在"
		s.mu.Lock()
		genKey := key
		_, hasGeneration := s.generations[genKey]
		_, wasInvalidated := s.invalidatedScopes[genKey]
		s.mu.Unlock()
		if hasGeneration || wasInvalidated {
			return Snapshot{}, queryError(ErrorCodeExpired, ErrSnapshotExpired)
		}
		return Snapshot{}, queryError(ErrorCodeSnapshotMissing, ErrSnapshotNotFound)
	}
	if request.PlayFrom.IsZero() || request.PlayFrom.Before(snapshot.SegmentStart) || !request.PlayFrom.Before(snapshot.SegmentEnd) {
		return Snapshot{}, queryError(ErrorCodeBoundary, ErrSnapshotBoundary)
	}
	return publicSnapshot(snapshot), nil
}

func (s *ResultSnapshotStore) Validate(candidate Snapshot) error {
	if s == nil || candidate.RecordKey == "" {
		return queryError(ErrorCodeSnapshotMissing, ErrSnapshotNotFound)
	}
	now := s.now()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return queryError(ErrorCodeSnapshotMissing, ErrSnapshotNotFound)
	}
	canonical, exists := s.snapshots[candidate.RecordKey]
	if exists && (canonical.invalidated || !now.Before(canonical.ExpiresAt)) {
		delete(s.snapshots, candidate.RecordKey)
		s.mu.Unlock()
		return queryError(ErrorCodeExpired, ErrSnapshotExpired)
	}
	s.mu.Unlock()
	if !exists {
		return queryError(ErrorCodeSnapshotMissing, ErrSnapshotNotFound)
	}
	want := canonical.validationDigest
	got := snapshotDigest(candidate)
	if len(want) != len(got) || subtle.ConstantTimeCompare([]byte(want), []byte(got)) != 1 {
		return queryError(ErrorCodeTampered, ErrSnapshotTampered)
	}
	return nil
}

func (s *ResultSnapshotStore) InvalidateOwnerChannel(ownerUserID, channelID uint) {
	key, ok := userOwnerChannel(ownerUserID, channelID)
	if !ok {
		return
	}
	s.invalidateOwnerChannel(key)
}

func (s *ResultSnapshotStore) InvalidateClientChannel(ownerClientID int64, channelID uint) {
	key, ok := clientOwnerChannel(ownerClientID, channelID)
	if !ok {
		return
	}
	s.invalidateOwnerChannel(key)
}

func (s *ResultSnapshotStore) invalidateOwnerChannel(key ownerChannel) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.invalidateOwnerChannelLocked(key)
	delete(s.generations, key)
	s.invalidatedScopes[key] = struct{}{}
	s.mu.Unlock()
}

func (s *ResultSnapshotStore) invalidateOwnerChannelLocked(key ownerChannel) {
	// 新查询开始即删除同用户/通道的旧快照:只标记失效会持续累积
	// 无效记录,并让每次失效/清理都全表扫描
	for recordKey, snapshot := range s.snapshots {
		if snapshot.OwnerUserID == key.ownerUserID && snapshot.OwnerClientID == key.ownerClientID && snapshot.ChannelID == key.channelID {
			delete(s.snapshots, recordKey)
		}
	}
	// 总量硬上限:删除最旧快照,防高频查询导致内存无界增长
	if max := s.maxSnapshots; max > 0 && len(s.snapshots) >= max {
		s.evictOldestLocked(len(s.snapshots) - max + 1)
	}
}

// evictOldestLocked 删除 n 个最早过期(即最早创建)的快照
func (s *ResultSnapshotStore) evictOldestLocked(n int) {
	for n > 0 {
		var oldestKey string
		var oldestAt time.Time
		first := true
		for key, snapshot := range s.snapshots {
			if first || snapshot.ExpiresAt.Before(oldestAt) {
				oldestKey, oldestAt, first = key, snapshot.ExpiresAt, false
			}
		}
		if first {
			return
		}
		delete(s.snapshots, oldestKey)
		n--
	}
}

func (s *ResultSnapshotStore) Len() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return 0
	}
	s.cleanupExpiredLocked(s.now())
	length := len(s.snapshots)
	s.mu.Unlock()
	return length
}

func (s *ResultSnapshotStore) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.closed = true
	clear(s.snapshots)
	clear(s.generations)
	s.mu.Unlock()
}

func (s *ResultSnapshotStore) cleanupExpiredLocked(now time.Time) {
	for key, snapshot := range s.snapshots {
		if !now.Before(snapshot.ExpiresAt) {
			delete(s.snapshots, key)
		}
	}
}

func publicSnapshot(snapshot Snapshot) Snapshot {
	snapshot.validationDigest = ""
	snapshot.invalidated = false
	return snapshot
}

func snapshotDigest(snapshot Snapshot) string {
	hash := sha256.New()
	fields := []string{
		snapshot.RecordKey, strconv.FormatUint(uint64(snapshot.OwnerUserID), 10), strconv.FormatInt(snapshot.OwnerClientID, 10), strconv.FormatUint(uint64(snapshot.ChannelID), 10),
		snapshot.DeviceCode, snapshot.ChannelCode, snapshot.QueryID,
		snapshot.SegmentStart.UTC().Format(time.RFC3339Nano), snapshot.SegmentEnd.UTC().Format(time.RFC3339Nano),
		snapshot.MetadataDigest, snapshot.ExpiresAt.UTC().Format(time.RFC3339Nano), recordDigest(snapshot.Record),
	}
	for _, field := range fields {
		// hash.Hash.Write 的实现约定总是返回 nil；这里仅写入摘要材料。
		_, _ = fmt.Fprintf(hash, "%d:%s\x00", len(field), field)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func randomToken(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate opaque token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}
