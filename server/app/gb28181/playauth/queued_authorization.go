package playauth

import (
	"context"
	"strings"
	"time"
)

// QueuedAuthorization is the immutable identity captured when a cold
// authorization enters the media-start queue. It intentionally contains no
// caller-supplied version, issued-at, or media generation; those values come
// from the trusted registry record.
type QueuedAuthorization struct {
	AuthorizationGeneration string
	DeviceID                string
	ChannelID               string
	DeviceEpoch             int64
	App                     string
	Stream                  string
	MediaServerID           string
}

type queuedAuthorizationSnapshot struct {
	generation string
	record     authorizationRecord
}

// ValidateQueuedAuthorizationContext rechecks a queued authorization against
// its immutable registry record and the current device authority. It never
// loads a newer epoch into the queued identity and never holds the registry
// mutex while calling the authority.
func (s *AuthorizationService) ValidateQueuedAuthorizationContext(ctx context.Context, queued QueuedAuthorization) error {
	snapshot, err := s.captureQueuedAuthorization(ctx, queued, 0, false)
	if err != nil {
		return err
	}
	if err := s.authorizeQueuedRecord(ctx, snapshot.record); err != nil {
		return err
	}
	return s.recheckQueuedAuthorization(ctx, queued, snapshot, 0, false)
}

// BindAuthorizationContext rechecks a queued authorization, atomically binds
// it to mediaGeneration, and performs a second fresh authority check after
// the registry CAS. A failed second check terminalizes only this authorization
// and removes only this authorization's generation index entry.
func (s *AuthorizationService) BindAuthorizationContext(ctx context.Context, queued QueuedAuthorization, mediaGeneration uint64) error {
	if mediaGeneration == 0 {
		return ErrAuthorizationClaimsMismatch
	}
	snapshot, err := s.captureQueuedAuthorization(ctx, queued, mediaGeneration, true)
	if err != nil {
		return err
	}
	if err := s.authorizeQueuedRecord(ctx, snapshot.record); err != nil {
		return err
	}
	if err := requireAuthorizationContext(ctx); err != nil {
		return err
	}
	if err := s.casQueuedAuthorization(ctx, queued, snapshot, mediaGeneration); err != nil {
		return err
	}
	if err := s.authorizeQueuedRecord(ctx, snapshot.record); err != nil {
		s.terminalizeQueuedAuthorization(snapshot, mediaGeneration)
		return err
	}
	if err := requireAuthorizationContext(ctx); err != nil {
		s.terminalizeQueuedAuthorization(snapshot, mediaGeneration)
		return err
	}
	if err := s.recheckBoundQueuedAuthorization(ctx, queued, snapshot, mediaGeneration); err != nil {
		s.terminalizeQueuedAuthorization(snapshot, mediaGeneration)
		return err
	}
	return nil
}

func (s *AuthorizationService) captureQueuedAuthorization(ctx context.Context, queued QueuedAuthorization, mediaGeneration uint64, forBind bool) (queuedAuthorizationSnapshot, error) {
	if err := s.requireAuthorityContext(ctx); err != nil {
		return queuedAuthorizationSnapshot{}, err
	}
	if s.registry == nil {
		return queuedAuthorizationSnapshot{}, ErrAuthorizationRegistryUnavailable
	}
	if !validQueuedAuthorizationIdentity(queued) || (forBind && mediaGeneration == 0) {
		return queuedAuthorizationSnapshot{}, ErrAuthorizationClaimsMismatch
	}

	now := s.registry.currentTime()
	s.registry.mu.Lock()
	if err := ctx.Err(); err != nil {
		s.registry.mu.Unlock()
		return queuedAuthorizationSnapshot{}, err
	}
	record, ok := s.registry.records[queued.AuthorizationGeneration]
	if !ok {
		s.registry.mu.Unlock()
		return queuedAuthorizationSnapshot{}, ErrAuthorizationNotFound
	}
	if err := validateQueuedRecord(record, queued, now, mediaGeneration, forBind); err != nil {
		s.registry.mu.Unlock()
		return queuedAuthorizationSnapshot{}, err
	}
	snapshot := queuedAuthorizationSnapshot{generation: queued.AuthorizationGeneration, record: record}
	s.registry.mu.Unlock()
	return snapshot, nil
}

func validQueuedAuthorizationIdentity(queued QueuedAuthorization) bool {
	return strings.TrimSpace(queued.AuthorizationGeneration) != "" && validGBID(queued.DeviceID) &&
		validGBID(queued.ChannelID) && queued.DeviceEpoch >= 0 && strings.TrimSpace(queued.App) != "" &&
		strings.TrimSpace(queued.Stream) != "" && strings.TrimSpace(queued.MediaServerID) != ""
}

func validateQueuedRecord(record authorizationRecord, queued QueuedAuthorization, now time.Time, mediaGeneration uint64, forBind bool) error {
	if !queuedRecordVersionEpochValid(record) || !queuedRecordMatchesIdentity(record, queued) {
		return ErrAuthorizationClaimsMismatch
	}
	switch record.state {
	case AuthorizationUnbound:
		if record.mediaGeneration != 0 {
			return ErrAuthorizationClaimsMismatch
		}
	case AuthorizationBound:
		if record.mediaGeneration == 0 {
			return ErrAuthorizationClaimsMismatch
		}
	case AuthorizationTerminal:
		return ErrAuthorizationTerminal
	default:
		return ErrAuthorizationClaimsMismatch
	}
	if record.issuedAt.IsZero() || record.issuedAt.Unix() <= 0 || record.expiresAt.IsZero() || !record.expiresAt.After(record.issuedAt) {
		return ErrAuthorizationClaimsMismatch
	}
	issuedAt := time.Unix(record.issuedAt.Unix(), 0).UTC()
	expiresAt := time.Unix(record.expiresAt.Unix(), 0).UTC()
	if !expiresAt.After(issuedAt) {
		return ErrAuthorizationClaimsMismatch
	}
	if issuedAt.After(now.Add(maxClockSkew)) {
		return ErrTokenTampered
	}
	if !now.Before(expiresAt) {
		return ErrAuthorizationExpired
	}
	if cutoff := RevokedBefore(); cutoff > 0 && record.issuedAt.Unix() < cutoff {
		return ErrTokenRevoked
	}
	switch record.state {
	case AuthorizationUnbound:
		if expiresAt.Sub(now) < MinimumAuthorizationStartLifetime {
			return ErrAuthorizationLifetimeTooShort
		}
	case AuthorizationBound:
		if forBind && record.mediaGeneration != mediaGeneration {
			return ErrAuthorizationAlreadyBound
		}
	}
	return nil
}

func queuedRecordVersionEpochValid(record authorizationRecord) bool {
	switch record.binding.version {
	case tokenVersionV2:
		return record.binding.deviceEpoch == 0
	case tokenVersionV4:
		return record.binding.deviceEpoch > 0
	default:
		return false
	}
}

func queuedRecordMatchesIdentity(record authorizationRecord, queued QueuedAuthorization) bool {
	binding := record.binding
	return binding.deviceID == queued.DeviceID && binding.channelID == queued.ChannelID &&
		binding.deviceEpoch == queued.DeviceEpoch && binding.app == queued.App &&
		binding.stream == queued.Stream && binding.mediaServerID == queued.MediaServerID
}

func queuedRecordImmutableEqual(left, right authorizationRecord) bool {
	return left.binding == right.binding && left.issuedAt.Equal(right.issuedAt) &&
		left.expiresAt.Equal(right.expiresAt) && left.nonce == right.nonce
}

func (s *AuthorizationService) authorizeQueuedRecord(ctx context.Context, record authorizationRecord) error {
	if err := requireAuthorizationContext(ctx); err != nil {
		return err
	}
	if !queuedRecordVersionEpochValid(record) {
		return ErrAuthorizationClaimsMismatch
	}
	switch record.binding.version {
	case tokenVersionV2:
		if record.issuedAt.Unix() <= 0 {
			return ErrAuthorizationClaimsMismatch
		}
		return s.authority.AuthorizeLegacy(ctx, record.binding.deviceID, record.issuedAt.Unix())
	case tokenVersionV4:
		return s.authority.AuthorizeEpoch(ctx, record.binding.deviceID, record.binding.deviceEpoch)
	default:
		return ErrAuthorizationClaimsMismatch
	}
}

func (s *AuthorizationService) recheckQueuedAuthorization(ctx context.Context, queued QueuedAuthorization, snapshot queuedAuthorizationSnapshot, mediaGeneration uint64, forBind bool) error {
	if err := requireAuthorizationContext(ctx); err != nil {
		return err
	}
	now := s.registry.currentTime()
	s.registry.mu.Lock()
	defer s.registry.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	record, ok := s.registry.records[snapshot.generation]
	if !ok {
		return ErrAuthorizationNotFound
	}
	if !queuedRecordImmutableEqual(record, snapshot.record) {
		return ErrAuthorizationClaimsMismatch
	}
	if err := validateQueuedRecord(record, queued, now, mediaGeneration, forBind); err != nil {
		return err
	}
	return ctx.Err()
}

func (s *AuthorizationService) recheckBoundQueuedAuthorization(ctx context.Context, queued QueuedAuthorization, snapshot queuedAuthorizationSnapshot, mediaGeneration uint64) error {
	if err := requireAuthorizationContext(ctx); err != nil {
		return err
	}
	now := s.registry.currentTime()
	s.registry.mu.Lock()
	defer s.registry.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	record, ok := s.registry.records[snapshot.generation]
	if !ok {
		return ErrAuthorizationNotFound
	}
	if !queuedRecordImmutableEqual(record, snapshot.record) {
		return ErrAuthorizationClaimsMismatch
	}
	if err := validateQueuedRecord(record, queued, now, mediaGeneration, true); err != nil {
		return err
	}
	if record.state != AuthorizationBound || record.mediaGeneration != mediaGeneration {
		return ErrAuthorizationClaimsMismatch
	}
	return ctx.Err()
}

func (s *AuthorizationService) casQueuedAuthorization(ctx context.Context, queued QueuedAuthorization, snapshot queuedAuthorizationSnapshot, mediaGeneration uint64) error {
	if err := requireAuthorizationContext(ctx); err != nil {
		return err
	}
	now := s.registry.currentTime()
	s.registry.mu.Lock()
	defer s.registry.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	record, ok := s.registry.records[snapshot.generation]
	if !ok {
		return ErrAuthorizationNotFound
	}
	if !queuedRecordImmutableEqual(record, snapshot.record) {
		return ErrAuthorizationClaimsMismatch
	}
	if err := validateQueuedRecord(record, queued, now, mediaGeneration, true); err != nil {
		return err
	}
	switch record.state {
	case AuthorizationUnbound:
		record.state = AuthorizationBound
		record.mediaGeneration = mediaGeneration
		s.registry.records[snapshot.generation] = record
		s.registry.addGenerationLocked(mediaGeneration, snapshot.generation)
		return nil
	case AuthorizationBound:
		if record.mediaGeneration == mediaGeneration {
			return nil
		}
		return ErrAuthorizationAlreadyBound
	default:
		return ErrAuthorizationClaimsMismatch
	}
}

func (s *AuthorizationService) terminalizeQueuedAuthorization(snapshot queuedAuthorizationSnapshot, mediaGeneration uint64) {
	if s == nil || s.registry == nil || mediaGeneration == 0 {
		return
	}
	s.registry.mu.Lock()
	defer s.registry.mu.Unlock()
	record, ok := s.registry.records[snapshot.generation]
	if !ok || !queuedRecordImmutableEqual(record, snapshot.record) || record.state != AuthorizationBound || record.mediaGeneration != mediaGeneration {
		return
	}
	record.state = AuthorizationTerminal
	s.registry.records[snapshot.generation] = record
	keys := s.registry.mediaGenerations[mediaGeneration]
	delete(keys, snapshot.generation)
	if len(keys) == 0 {
		delete(s.registry.mediaGenerations, mediaGeneration)
	}
}
