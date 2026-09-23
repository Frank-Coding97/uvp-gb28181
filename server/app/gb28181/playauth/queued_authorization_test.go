package playauth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type queuedTestAuthority struct {
	mu sync.Mutex

	epoch       int64
	loadErr     error
	legacyErr   error
	epochErr    error
	loads       int
	legacyCalls int
	epochCalls  int
	afterCall   func(string, int)
	entered     chan struct{}
	release     chan struct{}
	enteredOnce sync.Once
}

func (a *queuedTestAuthority) Load(ctx context.Context, _ string) (DeviceSecurityState, error) {
	if ctx == nil {
		return DeviceSecurityState{}, ErrDeviceSecurityUnavailable
	}
	if err := ctx.Err(); err != nil {
		return DeviceSecurityState{}, err
	}
	a.mu.Lock()
	a.loads++
	epoch, err := a.epoch, a.loadErr
	a.mu.Unlock()
	if err != nil {
		return DeviceSecurityState{}, err
	}
	return DeviceSecurityState{AccessEpoch: epoch}, nil
}

func (a *queuedTestAuthority) AuthorizeLegacy(ctx context.Context, _ string, _ int64) error {
	return a.authorize(ctx, "legacy", 0)
}

func (a *queuedTestAuthority) AuthorizeEpoch(ctx context.Context, _ string, epoch int64) error {
	return a.authorize(ctx, "epoch", epoch)
}

func (a *queuedTestAuthority) authorize(ctx context.Context, kind string, requested int64) error {
	if ctx == nil {
		return ErrDeviceSecurityUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	a.mu.Lock()
	call := 0
	var configuredErr error
	if kind == "legacy" {
		a.legacyCalls++
		call = a.legacyCalls
		configuredErr = a.legacyErr
	} else {
		a.epochCalls++
		call = a.epochCalls
		configuredErr = a.epochErr
	}
	currentEpoch := a.epoch
	afterCall := a.afterCall
	entered := a.entered
	release := a.release
	a.mu.Unlock()

	if entered != nil {
		a.enteredOnce.Do(func() { close(entered) })
		if release != nil {
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	if configuredErr != nil {
		return configuredErr
	}
	if kind == "epoch" && currentEpoch != requested {
		return ErrTokenRevoked
	}
	if afterCall != nil {
		afterCall(kind, call)
	}
	return nil
}

func newQueuedFixture(t *testing.T, generation string, epoch, authorityEpoch int64, lifetime time.Duration) (*AuthorizationService, *AuthorizationRegistry, *queuedTestAuthority, QueuedAuthorization, *time.Time) {
	t.Helper()
	now := time.Unix(1_800_000_000, 0).UTC()
	signer, err := NewSigner([]byte(testRootSecret), WithNow(func() time.Time { return now }))
	require.NoError(t, err)
	registry := NewAuthorizationRegistry(WithAuthorizationRegistryNow(func() time.Time { return now }))
	authority := &queuedTestAuthority{epoch: authorityEpoch}
	service := NewAuthorizationService(signer, registry, WithDeviceSecurityAuthority(authority))
	binding := Binding{
		DeviceID: "37010301021320000014", ChannelID: "37010301021320000001", DeviceEpoch: epoch,
		App: "rtp", Stream: "37010301021320000014_37010301021320000001", MediaServerID: "node-a",
	}
	prepared := Prepared{
		IssuedAt: now, ExpiresAt: now.Add(lifetime), Nonce: "nonce-" + generation,
		AuthorizationGeneration: generation,
	}
	require.NoError(t, registry.Register(prepared, binding))
	queued := QueuedAuthorization{
		AuthorizationGeneration: generation,
		DeviceID:                binding.DeviceID,
		ChannelID:               binding.ChannelID,
		DeviceEpoch:             epoch,
		App:                     binding.App,
		Stream:                  binding.Stream,
		MediaServerID:           binding.MediaServerID,
	}
	return service, registry, authority, queued, &now
}

func queuedRecordState(t *testing.T, registry *AuthorizationRegistry, generation string) authorizationRecord {
	t.Helper()
	registry.mu.Lock()
	defer registry.mu.Unlock()
	record, ok := registry.records[generation]
	require.True(t, ok)
	return record
}

func TestQueuedAuthorizationRequiresCompleteIdentityAndFreshAuthority(t *testing.T) {
	service, registry, authority, queued, _ := newQueuedFixture(t, "queued-full-identity", 7, 7, 2*time.Minute)

	require.NoError(t, service.ValidateQueuedAuthorizationContext(context.Background(), queued))
	authority.mu.Lock()
	require.Zero(t, authority.loads)
	require.Equal(t, 1, authority.epochCalls)
	authority.mu.Unlock()

	for name, mutate := range map[string]func(*QueuedAuthorization){
		"device":  func(q *QueuedAuthorization) { q.DeviceID = "37010301021320000015" },
		"channel": func(q *QueuedAuthorization) { q.ChannelID = "37010301021320000002" },
		"epoch":   func(q *QueuedAuthorization) { q.DeviceEpoch = 6 },
		"app":     func(q *QueuedAuthorization) { q.App = "other" },
		"stream":  func(q *QueuedAuthorization) { q.Stream = "other" },
		"node":    func(q *QueuedAuthorization) { q.MediaServerID = "node-b" },
	} {
		t.Run(name, func(t *testing.T) {
			mutated := queued
			mutate(&mutated)
			require.ErrorIs(t, service.ValidateQueuedAuthorizationContext(context.Background(), mutated), ErrAuthorizationClaimsMismatch)
		})
	}
	unknownGeneration := queued
	unknownGeneration.AuthorizationGeneration = "other"
	require.ErrorIs(t, service.ValidateQueuedAuthorizationContext(context.Background(), unknownGeneration), ErrAuthorizationNotFound)

	require.NoError(t, service.BindAuthorizationContext(context.Background(), queued, 17))
	require.NoError(t, service.ValidateQueuedAuthorizationContext(context.Background(), queued), "valid bound records remain reusable for Bind's same-generation check")
	require.NoError(t, service.BindAuthorizationContext(context.Background(), queued, 17))
	require.ErrorIs(t, service.BindAuthorizationContext(context.Background(), queued, 18), ErrAuthorizationAlreadyBound)
	require.Equal(t, AuthorizationBound, queuedRecordState(t, registry, queued.AuthorizationGeneration).state)
}

func TestQueuedAuthorizationRejectsInvalidGenerationEpochAndMissingRecords(t *testing.T) {
	service, registry, authority, queued, _ := newQueuedFixture(t, "queued-invalid-input", 7, 7, 2*time.Minute)

	require.ErrorIs(t, service.BindAuthorizationContext(context.Background(), queued, 0), ErrAuthorizationClaimsMismatch)
	authority.mu.Lock()
	require.Zero(t, authority.epochCalls, "zero media generation must not preflight authority")
	authority.mu.Unlock()

	for _, epoch := range []int64{0, -1, 6} {
		invalid := queued
		invalid.DeviceEpoch = epoch
		require.ErrorIs(t, service.ValidateQueuedAuthorizationContext(context.Background(), invalid), ErrAuthorizationClaimsMismatch)
	}
	unknown := queued
	unknown.AuthorizationGeneration = "missing"
	require.ErrorIs(t, service.ValidateQueuedAuthorizationContext(context.Background(), unknown), ErrAuthorizationNotFound)
	require.Zero(t, registry.Size()-1)
}

func TestQueuedAuthorizationUsesRecordVersionForLegacyAuthority(t *testing.T) {
	service, registry, authority, queued, now := newQueuedFixture(t, "queued-v2", 0, 9, 2*time.Minute)

	require.NoError(t, service.ValidateQueuedAuthorizationContext(context.Background(), queued))
	authority.mu.Lock()
	require.Equal(t, 1, authority.legacyCalls)
	require.Zero(t, authority.epochCalls)
	authority.mu.Unlock()
	require.NoError(t, service.BindAuthorizationContext(context.Background(), queued, 21))
	authority.mu.Lock()
	require.Equal(t, 3, authority.legacyCalls)
	require.Zero(t, authority.epochCalls)
	authority.mu.Unlock()

	bad := queued
	bad.DeviceEpoch = 1
	require.ErrorIs(t, service.ValidateQueuedAuthorizationContext(context.Background(), bad), ErrAuthorizationClaimsMismatch)

	*now = now.Add(3 * time.Minute)
	require.ErrorIs(t, service.ValidateQueuedAuthorizationContext(context.Background(), queued), ErrAuthorizationExpired)
	_ = registry
}

func TestQueuedAuthorizationChecksExpiryTerminalAndGlobalCutoff(t *testing.T) {
	service, registry, authority, queued, now := newQueuedFixture(t, "queued-expiry", 7, 7, time.Minute)
	*now = now.Add(2 * time.Minute)
	require.ErrorIs(t, service.ValidateQueuedAuthorizationContext(context.Background(), queued), ErrAuthorizationExpired)

	service, registry, authority, queued, now = newQueuedFixture(t, "queued-terminal", 7, 7, 2*time.Minute)
	require.NoError(t, service.BindAuthorizationContext(context.Background(), queued, 31))
	require.Equal(t, 1, registry.TerminateMediaGeneration(31))
	require.ErrorIs(t, service.ValidateQueuedAuthorizationContext(context.Background(), queued), ErrAuthorizationTerminal)
	_ = authority

	service, _, _, queued, now = newQueuedFixture(t, "queued-cutoff", 7, 7, 2*time.Minute)
	previous := RevokedBefore()
	defer revokedBefore.Store(previous)
	BumpRevocation(now.Add(time.Second))
	require.ErrorIs(t, service.ValidateQueuedAuthorizationContext(context.Background(), queued), ErrTokenRevoked)
}

func TestQueuedAuthorizationStartLifetimeAppliesOnlyToUnboundRecords(t *testing.T) {
	service, registry, authority, queued, now := newQueuedFixture(t, "queued-lifetime", 7, 7, time.Minute)
	*now = now.Add(31 * time.Second)
	require.ErrorIs(t, service.ValidateQueuedAuthorizationContext(context.Background(), queued), ErrAuthorizationLifetimeTooShort)
	authority.mu.Lock()
	require.Zero(t, authority.epochCalls, "near-expiry preflight must not reach media-side work")
	authority.mu.Unlock()
	require.ErrorIs(t, service.BindAuthorizationContext(context.Background(), queued, 41), ErrAuthorizationLifetimeTooShort)
	require.Equal(t, AuthorizationUnbound, queuedRecordState(t, registry, queued.AuthorizationGeneration).state)

	service, registry, authority, queued, now = newQueuedFixture(t, "queued-bound-short", 7, 7, 2*time.Minute)
	require.NoError(t, service.BindAuthorizationContext(context.Background(), queued, 42))
	*now = now.Add(91 * time.Second)
	require.NoError(t, service.ValidateQueuedAuthorizationContext(context.Background(), queued))
	require.NoError(t, service.BindAuthorizationContext(context.Background(), queued, 42))
	_ = authority
}

func TestQueuedAuthorizationUsesUnixSecondTokenExpiryForV2AndV4(t *testing.T) {
	for _, epoch := range []int64{0, 7} {
		t.Run(map[int64]string{0: "v2", 7: "v4"}[epoch], func(t *testing.T) {
			service, registry, _, queued, now := newQueuedFixture(t, "queued-second-expiry", epoch, 7, 2*time.Minute)
			registry.mu.Lock()
			record := registry.records[queued.AuthorizationGeneration]
			record.expiresAt = record.issuedAt.Add(60*time.Second + 900*time.Millisecond)
			registry.records[queued.AuthorizationGeneration] = record
			registry.mu.Unlock()
			*now = record.issuedAt.Add(60 * time.Second)
			require.ErrorIs(t, service.ValidateQueuedAuthorizationContext(context.Background(), queued), ErrAuthorizationExpired)
		})
	}
}

func TestQueuedAuthorizationFreshRecheckRejectsTransferAndTerminalizesOnlyCurrentAuthorization(t *testing.T) {
	service, registry, authority, queued, _ := newQueuedFixture(t, "queued-transfer", 7, 7, 2*time.Minute)
	other := queued
	other.AuthorizationGeneration = "queued-other"
	otherRecord := authorizationRecord{
		binding: resourceBinding(Binding{
			DeviceID: other.DeviceID, ChannelID: other.ChannelID, DeviceEpoch: other.DeviceEpoch,
			App: other.App, Stream: other.Stream, MediaServerID: other.MediaServerID,
		}),
		issuedAt: time.Unix(1_800_000_000, 0).UTC(), expiresAt: time.Unix(1_800_000_120, 0).UTC(),
		nonce: "nonce-queued-other", state: AuthorizationBound, mediaGeneration: 77,
	}
	registry.mu.Lock()
	registry.records[other.AuthorizationGeneration] = otherRecord
	registry.addGenerationLocked(77, other.AuthorizationGeneration)
	registry.mu.Unlock()

	require.NoError(t, service.ValidateQueuedAuthorizationContext(context.Background(), queued))
	authority.mu.Lock()
	authority.epoch = 8
	authority.mu.Unlock()
	require.ErrorIs(t, service.BindAuthorizationContext(context.Background(), queued, 77), ErrTokenRevoked)
	require.Equal(t, AuthorizationUnbound, queuedRecordState(t, registry, queued.AuthorizationGeneration).state)

	authority.mu.Lock()
	authority.epoch = 7
	authority.epochCalls = 0
	authority.afterCall = func(kind string, call int) {
		if kind == "epoch" && call == 1 {
			authority.mu.Lock()
			authority.epoch = 8
			authority.mu.Unlock()
		}
	}
	authority.mu.Unlock()
	require.ErrorIs(t, service.BindAuthorizationContext(context.Background(), queued, 77), ErrTokenRevoked)
	require.Equal(t, AuthorizationTerminal, queuedRecordState(t, registry, queued.AuthorizationGeneration).state)
	registry.mu.Lock()
	_, currentIndexed := registry.mediaGenerations[77][queued.AuthorizationGeneration]
	_, otherStillIndexed := registry.mediaGenerations[77][other.AuthorizationGeneration]
	registry.mu.Unlock()
	require.False(t, currentIndexed)
	require.True(t, otherStillIndexed)
}

func TestQueuedAuthorizationDoesNotHoldRegistryLockAcrossAuthority(t *testing.T) {
	service, registry, authority, queued, _ := newQueuedFixture(t, "queued-lock", 7, 7, 2*time.Minute)
	authority.entered = make(chan struct{})
	authority.release = make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- service.ValidateQueuedAuthorizationContext(context.Background(), queued) }()
	select {
	case <-authority.entered:
	case <-time.After(time.Second):
		t.Fatal("authority was not called")
	}
	sizeDone := make(chan struct{})
	go func() {
		_ = registry.Size()
		close(sizeDone)
	}()
	select {
	case <-sizeDone:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("registry lock held while authority was running")
	}
	close(authority.release)
	require.NoError(t, <-done)
}

func TestQueuedAuthorizationRejectsRecordChangeAfterCAS(t *testing.T) {
	service, registry, authority, queued, _ := newQueuedFixture(t, "queued-replaced", 7, 7, 2*time.Minute)
	authority.afterCall = func(kind string, call int) {
		if kind != "epoch" || call != 1 {
			return
		}
		registry.mu.Lock()
		record := registry.records[queued.AuthorizationGeneration]
		record.nonce = "replacement"
		registry.records[queued.AuthorizationGeneration] = record
		registry.mu.Unlock()
	}
	require.ErrorIs(t, service.BindAuthorizationContext(context.Background(), queued, 101), ErrAuthorizationClaimsMismatch)
	require.Equal(t, AuthorizationUnbound, queuedRecordState(t, registry, queued.AuthorizationGeneration).state)
}

func TestQueuedAuthorizationRejectsTerminalizationAfterSecondAuthority(t *testing.T) {
	service, registry, authority, queued, _ := newQueuedFixture(t, "queued-terminal-race", 7, 7, 2*time.Minute)
	authority.afterCall = func(kind string, call int) {
		if kind == "epoch" && call == 2 {
			require.Equal(t, 1, registry.TerminateMediaGeneration(102))
		}
	}
	require.ErrorIs(t, service.BindAuthorizationContext(context.Background(), queued, 102), ErrAuthorizationTerminal)
	require.Equal(t, AuthorizationTerminal, queuedRecordState(t, registry, queued.AuthorizationGeneration).state)
}

func TestQueuedAuthorizationCancellationAndAuthorityFailuresFailClosed(t *testing.T) {
	service, registry, authority, queued, _ := newQueuedFixture(t, "queued-cancel", 7, 7, 2*time.Minute)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, service.ValidateQueuedAuthorizationContext(canceled, queued), context.Canceled)
	authority.mu.Lock()
	require.Zero(t, authority.epochCalls)
	authority.mu.Unlock()

	authority.mu.Lock()
	authority.epochErr = errors.New("authority unavailable")
	authority.mu.Unlock()
	require.Error(t, service.ValidateQueuedAuthorizationContext(context.Background(), queued))
	require.Equal(t, AuthorizationUnbound, queuedRecordState(t, registry, queued.AuthorizationGeneration).state)

	var typedNil *queuedTestAuthority
	typedNilService := NewAuthorizationService(service.signer, registry, WithDeviceSecurityAuthority(typedNil))
	require.ErrorIs(t, typedNilService.ValidateQueuedAuthorizationContext(context.Background(), queued), ErrAuthorizationRegistryUnavailable)

	dbFailed := NewAuthorizationService(service.signer, registry, WithDeviceSecurityAuthority(NewDeviceSecurityStore(nil)))
	require.ErrorIs(t, dbFailed.ValidateQueuedAuthorizationContext(context.Background(), queued), ErrDeviceSecurityUnavailable)
}

func TestQueuedAuthorizationConcurrentCASIsIdempotentOnlyForSameGeneration(t *testing.T) {
	service, registry, _, queued, _ := newQueuedFixture(t, "queued-concurrent", 7, 7, 2*time.Minute)
	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- service.BindAuthorizationContext(context.Background(), queued, 88)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, AuthorizationBound, queuedRecordState(t, registry, queued.AuthorizationGeneration).state)

	service, _, _, queued, _ = newQueuedFixture(t, "queued-concurrent-different", 7, 7, 2*time.Minute)
	first := make(chan error, 1)
	second := make(chan error, 1)
	go func() { first <- service.BindAuthorizationContext(context.Background(), queued, 89) }()
	go func() { second <- service.BindAuthorizationContext(context.Background(), queued, 90) }()
	err1, err2 := <-first, <-second
	require.True(t, (err1 == nil && errors.Is(err2, ErrAuthorizationAlreadyBound)) || (err2 == nil && errors.Is(err1, ErrAuthorizationAlreadyBound)), "err1=%v err2=%v", err1, err2)
}
