package playauth

import (
	"context"
	"crypto/hmac"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type backendTestAuthority struct {
	mu        sync.Mutex
	state     DeviceSecurityState
	loadErr   error
	legacyErr error
	epochErr  error
	onLoad    func()
	loads     int
	legacies  int
	epochs    int
}

func (a *backendTestAuthority) Load(ctx context.Context, _ string) (DeviceSecurityState, error) {
	if ctx == nil {
		return DeviceSecurityState{}, ErrDeviceSecurityUnavailable
	}
	if err := ctx.Err(); err != nil {
		return DeviceSecurityState{}, err
	}
	a.mu.Lock()
	a.loads++
	if a.loadErr != nil {
		a.mu.Unlock()
		return DeviceSecurityState{}, a.loadErr
	}
	state := a.state
	onLoad := a.onLoad
	a.mu.Unlock()
	if onLoad != nil {
		onLoad()
	}
	return state, nil
}

func (a *backendTestAuthority) AuthorizeLegacy(ctx context.Context, _ string, _ int64) error {
	if ctx == nil {
		return ErrDeviceSecurityUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.legacies++
	return a.legacyErr
}

func (a *backendTestAuthority) AuthorizeEpoch(ctx context.Context, _ string, epoch int64) error {
	if ctx == nil {
		return ErrDeviceSecurityUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.epochs++
	if a.epochErr != nil {
		return a.epochErr
	}
	if epoch != a.state.AccessEpoch {
		return ErrTokenRevoked
	}
	return nil
}

func backendBinding() Binding {
	return Binding{
		DeviceID: "37010301021320000014", ChannelID: "37010301021320000001",
		DeviceEpoch: 1,
		App:         "rtp", Stream: "37010301021320000014_37010301021320000001", MediaServerID: "node-a",
	}
}

func newBackendService(t *testing.T, authority DeviceSecurityAuthority, now *time.Time) *AuthorizationService {
	t.Helper()
	signer, err := NewSigner([]byte(testRootSecret), WithNow(func() time.Time { return *now }))
	require.NoError(t, err)
	registry := NewAuthorizationRegistry(WithAuthorizationRegistryNow(func() time.Time { return *now }))
	return NewAuthorizationService(signer, registry, WithDeviceSecurityAuthority(authority))
}

func withBackendTestAuthority() AuthorizationServiceOption {
	return WithDeviceSecurityAuthority(&backendTestAuthority{state: DeviceSecurityState{AccessEpoch: 1}})
}

func issueLegacyV2Fixture(t *testing.T, signer *Signer, binding Binding) (string, Prepared) {
	t.Helper()
	prepared, err := signer.Prepare()
	require.NoError(t, err)
	claims := Claims{
		Version: tokenVersionV2, Audience: tokenAudience, Mode: ModeDirect, KeyID: signer.activeID,
		DeviceID: binding.DeviceID, ChannelID: binding.ChannelID, App: binding.App,
		Stream: binding.Stream, MediaServerID: binding.MediaServerID, MediaGeneration: binding.MediaGeneration,
		IssuedAt: prepared.IssuedAt.Unix(), ExpiresAt: prepared.ExpiresAt.Unix(), Nonce: prepared.Nonce,
		AuthorizationGeneration: prepared.AuthorizationGeneration,
	}
	if binding.BindClientIP {
		digest, err := clientIPDigest(signer.keys[signer.activeID].ip, binding.ClientIP)
		require.NoError(t, err)
		claims.ClientIPDigest = digest
	}
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	key := signer.keys[signer.activeID]
	return encoded + "." + base64.RawURLEncoding.EncodeToString(signature(key.sign, []byte(encoded))), prepared
}

func TestBackendSignerIssuesV4AndVerifiesLegacyV2OnlyWithZeroEpoch(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	signer, err := NewSigner([]byte(testRootSecret), WithNow(func() time.Time { return now }))
	require.NoError(t, err)
	binding := backendBinding()
	binding.DeviceEpoch = 7

	grant, err := signer.IssueDirect(binding)
	require.NoError(t, err)
	claims, err := signer.Verify(grant.Token, binding)
	require.NoError(t, err)
	require.Equal(t, tokenVersionV4, claims.Version)
	require.Equal(t, int64(7), claims.DeviceEpoch)

	missingEpoch := binding
	missingEpoch.DeviceEpoch = 0
	_, err = signer.IssueDirect(missingEpoch)
	require.ErrorIs(t, err, ErrTokenInvalid)
	_, err = signer.Verify(grant.Token, missingEpoch)
	require.ErrorIs(t, err, ErrTokenInvalid)

	legacyBinding := binding
	legacyBinding.DeviceEpoch = 0
	legacy, _ := issueLegacyV2Fixture(t, signer, legacyBinding)
	legacyClaims, err := signer.Verify(legacy, legacyBinding)
	require.NoError(t, err)
	require.Equal(t, tokenVersionV2, legacyClaims.Version)
	require.Zero(t, legacyClaims.DeviceEpoch)
	_, err = signer.Verify(legacy, binding)
	require.ErrorIs(t, err, ErrTokenInvalid)
}

func TestBackendV2VerificationSurvivesKeyRotationWithoutV2Issuance(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	previous := []byte("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	oldSigner, err := NewSigner(previous, WithNow(func() time.Time { return now }))
	require.NoError(t, err)
	binding := backendBinding()
	binding.DeviceEpoch = 0
	legacyToken, _ := issueLegacyV2Fixture(t, oldSigner, binding)

	active := []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	rotated, err := NewKeyring(KeyMaterial{Secret: active}, &KeyMaterial{Secret: previous}, WithNow(func() time.Time { return now }))
	require.NoError(t, err)
	claims, err := rotated.Verify(legacyToken, binding)
	require.NoError(t, err)
	require.Equal(t, tokenVersionV2, claims.Version)

	withoutPrevious, err := NewSigner(active, WithNow(func() time.Time { return now }))
	require.NoError(t, err)
	_, err = withoutPrevious.Verify(legacyToken, binding)
	require.ErrorIs(t, err, ErrTokenTampered)
}

func TestBackendV4KeyDomainDoesNotReuseV2VerificationKey(t *testing.T) {
	signer, err := NewSigner([]byte(testRootSecret))
	require.NoError(t, err)
	key := signer.keys[signer.activeID]
	payload := []byte("same-payload")
	require.False(t, hmac.Equal(signature(key.sign, payload), signature(key.v4, payload)))
}

func TestAuthorizationServiceContextRefreshesEpochBeforeIssueAndVerify(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	authority := &backendTestAuthority{state: DeviceSecurityState{AccessEpoch: 7}}
	service := newBackendService(t, authority, &now)
	binding := backendBinding()
	binding.DeviceEpoch = 7
	prepared, err := service.Prepare()
	require.NoError(t, err)
	grant, err := service.BindContext(context.Background(), prepared, binding)
	require.NoError(t, err)
	claims, err := service.VerifyContext(context.Background(), grant.Token, binding)
	require.NoError(t, err)
	require.Equal(t, int64(7), claims.DeviceEpoch)

	hot := binding
	hot.MediaGeneration = 12
	hotGrant, err := service.IssueDirectContext(context.Background(), hot)
	require.NoError(t, err)
	hotClaims, err := service.VerifyContext(context.Background(), hotGrant.Token, hot)
	require.NoError(t, err)
	require.Equal(t, uint64(12), hotClaims.MediaGeneration)

	authority.mu.Lock()
	authority.state.AccessEpoch = 8
	authority.mu.Unlock()
	_, err = service.VerifyContext(context.Background(), grant.Token, binding)
	require.ErrorIs(t, err, ErrTokenRevoked)
	_, err = service.VerifyContext(context.Background(), hotGrant.Token, hot)
	require.ErrorIs(t, err, ErrTokenRevoked)

	other := binding
	other.DeviceID = "37010301021320000015"
	other.DeviceEpoch = 8
	grant, err = service.IssueDirectContext(context.Background(), other)
	require.NoError(t, err)
	require.NotEmpty(t, grant.Token)
	// The authority decides device epoch only; it does not impose personnel or
	// owner restrictions on another valid device identifier.
}

func TestAuthorizationServiceBindFailsClosedWhenEpochChangesBeforeAuthorize(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	authority := &backendTestAuthority{state: DeviceSecurityState{AccessEpoch: 5}}
	authority.onLoad = func() {
		authority.mu.Lock()
		authority.state.AccessEpoch = 6
		authority.mu.Unlock()
	}
	service := newBackendService(t, authority, &now)
	binding := backendBinding()
	binding.DeviceEpoch = 5
	grant, err := service.IssueDirectContext(context.Background(), binding)
	require.ErrorIs(t, err, ErrTokenRevoked)
	require.Empty(t, grant.Token)
	require.Zero(t, service.registry.Size())
}

func TestAuthorizationServiceBindRejectsStaleOrInvalidRequestedEpoch(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	authority := &backendTestAuthority{state: DeviceSecurityState{AccessEpoch: 6}}
	service := newBackendService(t, authority, &now)

	prepared, err := service.Prepare()
	require.NoError(t, err)
	stale := backendBinding()
	stale.DeviceEpoch = 5
	_, err = service.BindContext(context.Background(), prepared, stale)
	require.ErrorIs(t, err, ErrTokenRevoked)
	require.Zero(t, service.registry.Size())

	for _, tc := range []struct {
		name  string
		epoch int64
		want  error
	}{
		{name: "missing", epoch: 0, want: ErrAuthorizationDeviceEpoch},
		{name: "negative", epoch: -1, want: ErrTokenInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prepared, err := service.Prepare()
			require.NoError(t, err)
			invalid := backendBinding()
			invalid.DeviceEpoch = tc.epoch
			_, err = service.BindContext(context.Background(), prepared, invalid)
			require.ErrorIs(t, err, tc.want)
			require.Zero(t, service.registry.Size())
		})
	}
}

func TestAuthorizationServiceContextFailsClosedForMissingTypedNilAndFailedAuthority(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	binding := backendBinding()

	withoutAuthority := newBackendService(t, nil, &now)
	_, err := withoutAuthority.IssueDirectContext(context.Background(), binding)
	require.ErrorIs(t, err, ErrAuthorizationRegistryUnavailable)

	var typedNil *backendTestAuthority
	typedNilService := newBackendService(t, typedNil, &now)
	_, err = typedNilService.IssueDirectContext(context.Background(), binding)
	require.ErrorIs(t, err, ErrAuthorizationRegistryUnavailable)

	loadFailed := &backendTestAuthority{loadErr: errors.New("database unavailable")}
	loadFailedService := newBackendService(t, loadFailed, &now)
	_, err = loadFailedService.IssueDirectContext(context.Background(), binding)
	require.Error(t, err)

	dbStoreService := newBackendService(t, NewDeviceSecurityStore(nil), &now)
	_, err = dbStoreService.IssueDirectContext(context.Background(), binding)
	require.ErrorIs(t, err, ErrDeviceSecurityUnavailable)

	epochFailed := &backendTestAuthority{state: DeviceSecurityState{AccessEpoch: 1}, epochErr: errors.New("authority unavailable")}
	epochFailedService := newBackendService(t, epochFailed, &now)
	grant, err := epochFailedService.IssueDirectContext(context.Background(), binding)
	require.Error(t, err)
	require.Empty(t, grant.Token)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	authority := &backendTestAuthority{state: DeviceSecurityState{AccessEpoch: 1}}
	canceledService := newBackendService(t, authority, &now)
	_, err = canceledService.IssueDirectContext(canceled, binding)
	require.ErrorIs(t, err, context.Canceled)
	authority.mu.Lock()
	require.Zero(t, authority.loads)
	authority.mu.Unlock()
}

func TestAuthorizationServiceVerifiedClientFallbackRechecksSignedClaimsAndAuthority(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	authority := &backendTestAuthority{state: DeviceSecurityState{AccessEpoch: 3}}
	service := newBackendService(t, authority, &now)
	binding := backendBinding()
	binding.DeviceEpoch = 3
	binding.BindClientIP = true
	binding.ClientIP = "203.0.113.9"
	grant, err := service.IssueDirectContext(context.Background(), binding)
	require.NoError(t, err)
	claims, err := service.VerifyContext(context.Background(), grant.Token, binding)
	require.NoError(t, err)
	fallback := binding
	fallback.BindClientIP = false
	fallback.ClientIP = ""
	_, err = service.VerifyForVerifiedClientAutoStartContext(context.Background(), grant.Token, fallback)
	require.ErrorIs(t, err, ErrAuthorizationClientNotVerified)
	require.NoError(t, service.MarkVerifiedClientSourceContext(context.Background(), grant.Token, claims, binding))

	verified, err := service.VerifyForVerifiedClientAutoStartContext(context.Background(), grant.Token, fallback)
	require.NoError(t, err)
	require.Equal(t, claims, verified)
	previousRevokedBefore := RevokedBefore()
	defer revokedBefore.Store(previousRevokedBefore)
	BumpRevocation(now.Add(time.Second))
	_, err = service.VerifyForVerifiedClientAutoStartContext(context.Background(), grant.Token, fallback)
	require.ErrorIs(t, err, ErrAuthorizationClientNotVerified)
	revokedBefore.Store(previousRevokedBefore)

	authority.mu.Lock()
	authority.state.AccessEpoch = 4
	authority.mu.Unlock()
	_, err = service.VerifyForVerifiedClientAutoStartContext(context.Background(), grant.Token, fallback)
	require.ErrorIs(t, err, ErrTokenRevoked)

	badToken := grant.Token + "tampered"
	_, err = service.VerifyForVerifiedClientAutoStartContext(context.Background(), badToken, fallback)
	require.Error(t, err)
	if strings.Contains(err.Error(), binding.ClientIP) {
		t.Fatalf("error leaked client IP: %v", err)
	}

	wrongResource := fallback
	wrongResource.Stream += "-other"
	_, err = service.VerifyForVerifiedClientAutoStartContext(context.Background(), grant.Token, wrongResource)
	require.ErrorIs(t, err, ErrAuthorizationClientNotVerified)

	noIP := backendBinding()
	noIP.DeviceEpoch = 4
	noIPGrant, err := service.IssueDirectContext(context.Background(), noIP)
	require.NoError(t, err)
	_, err = service.VerifyForVerifiedClientAutoStartContext(context.Background(), noIPGrant.Token, noIP)
	require.ErrorIs(t, err, ErrAuthorizationClientNotVerified)

	restarted := newBackendService(t, &backendTestAuthority{state: DeviceSecurityState{AccessEpoch: 4}}, &now)
	_, err = restarted.VerifyForVerifiedClientAutoStartContext(context.Background(), grant.Token, fallback)
	require.ErrorIs(t, err, ErrAuthorizationNotFound)

	now = now.Add(VerifiedClientSourceLifetime)
	_, err = service.VerifyForVerifiedClientAutoStartContext(context.Background(), grant.Token, fallback)
	require.ErrorIs(t, err, ErrAuthorizationClientNotVerified)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = service.VerifyForVerifiedClientAutoStartContext(canceled, grant.Token, fallback)
	require.ErrorIs(t, err, context.Canceled)
}

func TestAuthorizationServiceVerifiedFallbackAcceptsV4PreviousKeyDuringRotation(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	previous := []byte("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	active := []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	authority := &backendTestAuthority{state: DeviceSecurityState{AccessEpoch: 3}}
	registry := NewAuthorizationRegistry(WithAuthorizationRegistryNow(func() time.Time { return now }))
	oldSigner, err := NewSigner(previous, WithNow(func() time.Time { return now }))
	require.NoError(t, err)
	oldService := NewAuthorizationService(oldSigner, registry, WithDeviceSecurityAuthority(authority))
	binding := backendBinding()
	binding.DeviceEpoch = 3
	binding.BindClientIP = true
	binding.ClientIP = "203.0.113.9"
	grant, err := oldService.IssueDirectContext(context.Background(), binding)
	require.NoError(t, err)
	claims, err := oldService.VerifyContext(context.Background(), grant.Token, binding)
	require.NoError(t, err)
	require.NoError(t, oldService.MarkVerifiedClientSourceContext(context.Background(), grant.Token, claims, binding))

	rotatedSigner, err := NewKeyring(KeyMaterial{Secret: active}, &KeyMaterial{Secret: previous}, WithNow(func() time.Time { return now }))
	require.NoError(t, err)
	rotatedService := NewAuthorizationService(rotatedSigner, registry, WithDeviceSecurityAuthority(authority))
	fallback := binding
	fallback.BindClientIP = false
	fallback.ClientIP = ""
	verified, err := rotatedService.VerifyForVerifiedClientAutoStartContext(context.Background(), grant.Token, fallback)
	require.NoError(t, err)
	require.Equal(t, claims, verified)
}

func TestAuthorizationServiceLegacyV2ColdFallbackUsesLegacyAuthority(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	authority := &backendTestAuthority{state: DeviceSecurityState{AccessEpoch: 9}}
	signer, err := NewSigner([]byte(testRootSecret), WithNow(func() time.Time { return now }))
	require.NoError(t, err)
	registry := NewAuthorizationRegistry(WithAuthorizationRegistryNow(func() time.Time { return now }))
	service := NewAuthorizationService(signer, registry, WithDeviceSecurityAuthority(authority))
	binding := backendBinding()
	binding.DeviceEpoch = 0
	binding.BindClientIP = true
	binding.ClientIP = "203.0.113.9"
	token, prepared := issueLegacyV2Fixture(t, signer, binding)
	require.NoError(t, registry.Register(prepared, binding))

	claims, err := service.VerifyContext(context.Background(), token, binding)
	require.NoError(t, err)
	require.Zero(t, claims.DeviceEpoch)
	require.NoError(t, service.MarkVerifiedClientSourceContext(context.Background(), token, claims, binding))
	fallback := binding
	fallback.BindClientIP = false
	fallback.ClientIP = ""
	verified, err := service.VerifyForVerifiedClientAutoStartContext(context.Background(), token, fallback)
	require.NoError(t, err)
	require.Equal(t, claims, verified)
	authority.mu.Lock()
	require.Greater(t, authority.legacies, 0)
	require.Zero(t, authority.epochs)
	authority.mu.Unlock()
}
