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

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
)

type SnapshotInput struct {
	OwnerUserID  uint
	ChannelID    uint
	DeviceCode   string
	ChannelCode  string
	QueryID      string
	SegmentStart time.Time
	SegmentEnd   time.Time
	Record       manscdp.RecordInfoItem
}

type Snapshot struct {
	RecordKey        string
	OwnerUserID      uint
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
	RecordKey   string
	OwnerUserID uint
	ChannelID   uint
	PlayFrom    time.Time
}

type ResultSnapshotStore struct {
	mu          sync.Mutex
	ttl         time.Duration
	now         func() time.Time
	snapshots   map[string]Snapshot
	generations map[ownerChannel]string
	closed      bool
}

type ownerChannel struct {
	ownerUserID uint
	channelID   uint
}

func NewResultSnapshotStore(ttl time.Duration, now func() time.Time) (*ResultSnapshotStore, error) {
	if ttl <= 0 {
		return nil, queryError(ErrorCodeInvalidArgument, ErrInvalidArgument)
	}
	if now == nil {
		now = time.Now
	}
	return &ResultSnapshotStore{
		ttl: ttl, now: now, snapshots: make(map[string]Snapshot), generations: make(map[ownerChannel]string),
	}, nil
}

func (s *ResultSnapshotStore) BeginQuery(ownerUserID, channelID uint, queryID string) error {
	if s == nil || ownerUserID == 0 || channelID == 0 || normalizeIdentity(queryID) == "" {
		return queryError(ErrorCodeInvalidArgument, ErrInvalidArgument)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return queryError(ErrorCodeUnavailable, ErrUnavailable)
	}
	s.invalidateOwnerChannelLocked(ownerUserID, channelID)
	s.generations[ownerChannel{ownerUserID: ownerUserID, channelID: channelID}] = normalizeIdentity(queryID)
	return nil
}

func (s *ResultSnapshotStore) Issue(input SnapshotInput) (Snapshot, error) {
	if s == nil || input.OwnerUserID == 0 || input.ChannelID == 0 || normalizeIdentity(input.DeviceCode) == "" ||
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
		RecordKey: recordKey, OwnerUserID: input.OwnerUserID, ChannelID: input.ChannelID,
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
	generationKey := ownerChannel{ownerUserID: input.OwnerUserID, channelID: input.ChannelID}
	currentQueryID, exists := s.generations[generationKey]
	if !exists {
		s.generations[generationKey] = snapshot.QueryID
	} else if currentQueryID != snapshot.QueryID {
		s.mu.Unlock()
		return Snapshot{}, queryError(ErrorCodeExpired, ErrSnapshotExpired)
	}
	s.cleanupExpiredLocked(now)
	s.snapshots[recordKey] = snapshot
	s.mu.Unlock()
	return publicSnapshot(snapshot), nil
}

func (s *ResultSnapshotStore) Resolve(request ResolveRequest) (Snapshot, error) {
	if s == nil || normalizeIdentity(request.RecordKey) == "" || request.OwnerUserID == 0 || request.ChannelID == 0 {
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
	if !exists || snapshot.OwnerUserID != request.OwnerUserID || snapshot.ChannelID != request.ChannelID {
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
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.invalidateOwnerChannelLocked(ownerUserID, channelID)
	delete(s.generations, ownerChannel{ownerUserID: ownerUserID, channelID: channelID})
	s.mu.Unlock()
}

func (s *ResultSnapshotStore) invalidateOwnerChannelLocked(ownerUserID, channelID uint) {
	for key, snapshot := range s.snapshots {
		if snapshot.OwnerUserID == ownerUserID && snapshot.ChannelID == channelID {
			snapshot.invalidated = true
			s.snapshots[key] = snapshot
		}
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
		snapshot.RecordKey, strconv.FormatUint(uint64(snapshot.OwnerUserID), 10), strconv.FormatUint(uint64(snapshot.ChannelID), 10),
		snapshot.DeviceCode, snapshot.ChannelCode, snapshot.QueryID,
		snapshot.SegmentStart.UTC().Format(time.RFC3339Nano), snapshot.SegmentEnd.UTC().Format(time.RFC3339Nano),
		snapshot.MetadataDigest, snapshot.ExpiresAt.UTC().Format(time.RFC3339Nano), recordDigest(snapshot.Record),
	}
	for _, field := range fields {
		fmt.Fprintf(hash, "%d:%s\x00", len(field), field)
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
