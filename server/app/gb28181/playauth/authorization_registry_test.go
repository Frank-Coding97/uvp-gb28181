package playauth

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func registryBinding(mediaGeneration uint64) Binding {
	return Binding{
		DeviceID: "37010301021320000014", ChannelID: "37010301021320000001",
		App: "rtp", Stream: "37010301021320000014_37010301021320000001",
		MediaServerID: "node-a", MediaGeneration: mediaGeneration,
	}
}

func registryPrepared(now time.Time, generation string, lifetime time.Duration) Prepared {
	return Prepared{
		IssuedAt: now, ExpiresAt: now.Add(lifetime), Nonce: "nonce-" + generation,
		AuthorizationGeneration: generation,
	}
}

func registryClaims(prepared Prepared, binding Binding) Claims {
	return Claims{
		DeviceID: binding.DeviceID, ChannelID: binding.ChannelID, App: binding.App,
		Stream: binding.Stream, MediaServerID: binding.MediaServerID, MediaGeneration: binding.MediaGeneration,
		IssuedAt: prepared.IssuedAt.Unix(), ExpiresAt: prepared.ExpiresAt.Unix(), Nonce: prepared.Nonce,
		AuthorizationGeneration: prepared.AuthorizationGeneration,
	}
}

func TestAuthorizationRegistryTransitionsUnboundBoundTerminal(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	registry := NewAuthorizationRegistry(WithAuthorizationRegistryNow(func() time.Time { return now }))
	prepared := registryPrepared(now, "auth-1", MinimumAuthorizationStartLifetime)
	binding := registryBinding(0)
	claims := registryClaims(prepared, binding)

	if err := registry.Register(prepared, binding); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := registry.verify(claims, binding, true); err != nil {
		t.Fatalf("verify unbound: %v", err)
	}
	if err := registry.BindAuthorization(prepared.AuthorizationGeneration, 9); err != nil {
		t.Fatalf("bind: %v", err)
	}
	bound := binding
	bound.MediaGeneration = 9
	if err := registry.verify(claims, bound, false); err != nil {
		t.Fatalf("verify bound preauthorization: %v", err)
	}
	if err := registry.BindAuthorization(prepared.AuthorizationGeneration, 9); err != nil {
		t.Fatalf("idempotent bind: %v", err)
	}
	if err := registry.BindAuthorization(prepared.AuthorizationGeneration, 10); !errors.Is(err, ErrAuthorizationAlreadyBound) {
		t.Fatalf("second bind error=%v", err)
	}

	if terminated := registry.TerminateMediaGeneration(9); terminated != 1 {
		t.Fatalf("terminated=%d, want 1", terminated)
	}
	if err := registry.verify(claims, bound, false); !errors.Is(err, ErrAuthorizationTerminal) {
		t.Fatalf("terminated authorization verified: %v", err)
	}
}

func TestAuthorizationRegistryIdempotentBindSurvivesAuthorizationExpiry(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	registry := NewAuthorizationRegistry(WithAuthorizationRegistryNow(func() time.Time { return now }))
	prepared := registryPrepared(now, "auth-expiring-after-bind", MinimumAuthorizationStartLifetime)
	if err := registry.Register(prepared, registryBinding(0)); err != nil {
		t.Fatal(err)
	}
	if err := registry.BindAuthorization(prepared.AuthorizationGeneration, 9); err != nil {
		t.Fatal(err)
	}
	now = now.Add(MinimumAuthorizationStartLifetime)
	if err := registry.BindAuthorization(prepared.AuthorizationGeneration, 9); err != nil {
		t.Fatalf("same-generation bind must remain idempotent after expiry: %v", err)
	}
	if err := registry.BindAuthorization(prepared.AuthorizationGeneration, 10); !errors.Is(err, ErrAuthorizationAlreadyBound) {
		t.Fatalf("expired authorization rebound to a new generation: %v", err)
	}
}

func TestAuthorizationRegistryRegistersBoundGenerationAndRejectsClaimMismatch(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	registry := NewAuthorizationRegistry(WithAuthorizationRegistryNow(func() time.Time { return now }))
	binding := registryBinding(42)
	prepared := registryPrepared(now, "auth-bound", time.Minute)
	claims := registryClaims(prepared, binding)
	if err := registry.Register(prepared, binding); err != nil {
		t.Fatalf("register bound: %v", err)
	}
	if err := registry.verify(claims, binding, false); err != nil {
		t.Fatalf("verify bound: %v", err)
	}

	for name, mutate := range map[string]func(*Claims){
		"nonce":            func(c *Claims) { c.Nonce = "other" },
		"stream":           func(c *Claims) { c.Stream += "-other" },
		"media generation": func(c *Claims) { c.MediaGeneration++ },
		"expiry":           func(c *Claims) { c.ExpiresAt++ },
	} {
		t.Run(name, func(t *testing.T) {
			mutated := claims
			mutate(&mutated)
			if err := registry.verify(mutated, binding, false); !errors.Is(err, ErrAuthorizationClaimsMismatch) {
				t.Fatalf("verify error=%v", err)
			}
		})
	}
}

func TestAuthorizationRegistryRejectsShortLifetimeAndFullCapacity(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	registry := NewAuthorizationRegistry(
		WithAuthorizationRegistryCapacity(1),
		WithAuthorizationRegistryNow(func() time.Time { return now }),
	)
	binding := registryBinding(0)
	short := registryPrepared(now, "short", MinimumAuthorizationStartLifetime-time.Nanosecond)
	if err := registry.Register(short, binding); !errors.Is(err, ErrAuthorizationLifetimeTooShort) {
		t.Fatalf("short lifetime register error=%v", err)
	}
	first := registryPrepared(now, "first", MinimumAuthorizationStartLifetime)
	if err := registry.Register(first, binding); err != nil {
		t.Fatalf("register first: %v", err)
	}
	second := registryPrepared(now, "second", time.Minute)
	if err := registry.Register(second, binding); !errors.Is(err, ErrAuthorizationRegistryFull) {
		t.Fatalf("full register error=%v", err)
	}

	now = now.Add(MinimumAuthorizationStartLifetime)
	if expired := registry.Expire(); expired != 1 {
		t.Fatalf("expired=%d, want 1", expired)
	}
	if err := registry.Register(second, binding); err != nil {
		t.Fatalf("register after expiry: %v", err)
	}
}

func TestAuthorizationServiceBindsPreauthorizationToCurrentMediaGeneration(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	signer, err := NewSigner([]byte(testRootSecret), WithNow(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	registry := NewAuthorizationRegistry(WithAuthorizationRegistryNow(func() time.Time { return now }))
	service := NewAuthorizationService(signer, registry)
	preauthorized := registryBinding(0)
	prepared, err := service.Prepare()
	if err != nil {
		t.Fatal(err)
	}
	grant, err := service.Bind(prepared, preauthorized)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.VerifyForAutoStart(grant.Token, preauthorized); err != nil {
		t.Fatalf("verify auto start: %v", err)
	}

	current := preauthorized
	current.MediaGeneration = 7
	if _, err := service.Verify(grant.Token, current); err != nil {
		t.Fatalf("verify and bind current media: %v", err)
	}
	if _, err := service.Verify(grant.Token, current); err != nil {
		t.Fatalf("verify already bound current media: %v", err)
	}
	if _, err := service.VerifyForAutoStart(grant.Token, preauthorized); !errors.Is(err, ErrAuthorizationAlreadyBound) {
		t.Fatalf("bound token accepted for another auto start: %v", err)
	}
	otherGeneration := current
	otherGeneration.MediaGeneration++
	if _, err := service.Verify(grant.Token, otherGeneration); !errors.Is(err, ErrAuthorizationAlreadyBound) {
		t.Fatalf("old authorization rebound to another generation: %v", err)
	}
	if terminated := service.TerminateMediaGeneration(current.MediaGeneration); terminated != 1 {
		t.Fatalf("terminated=%d", terminated)
	}
	if _, err := service.Verify(grant.Token, current); !errors.Is(err, ErrAuthorizationTerminal) {
		t.Fatalf("terminal generation verified: %v", err)
	}
}

func TestAuthorizationServiceRejectsConflictingPreauthorizationBeforeBinding(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	signer, err := NewSigner([]byte(testRootSecret), WithNow(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	registry := NewAuthorizationRegistry(WithAuthorizationRegistryNow(func() time.Time { return now }))
	service := NewAuthorizationService(signer, registry)
	binding := registryBinding(0)
	prepared, err := service.Prepare()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Bind(prepared, binding); err != nil {
		t.Fatal(err)
	}
	conflicting := prepared
	conflicting.Nonce = "different-nonce"
	grant, err := signer.Bind(conflicting, binding)
	if err != nil {
		t.Fatal(err)
	}
	current := binding
	current.MediaGeneration = 7
	if _, err := service.Verify(grant.Token, current); !errors.Is(err, ErrAuthorizationClaimsMismatch) {
		t.Fatalf("conflicting token error=%v", err)
	}
	claims := registryClaims(prepared, binding)
	if err := registry.verify(claims, binding, true); err != nil {
		t.Fatalf("conflicting token mutated unbound authorization: %v", err)
	}
}

func TestAuthorizationServiceAutoStartRequiresThirtySecondsRemaining(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	signer, err := NewSigner([]byte(testRootSecret), WithTTL(time.Minute), WithNow(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	registry := NewAuthorizationRegistry(WithAuthorizationRegistryNow(func() time.Time { return now }))
	service := NewAuthorizationService(signer, registry)
	binding := registryBinding(0)
	prepared, err := service.Prepare()
	if err != nil {
		t.Fatal(err)
	}
	grant, err := service.Bind(prepared, binding)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(31 * time.Second)
	if _, err := service.VerifyForAutoStart(grant.Token, binding); !errors.Is(err, ErrAuthorizationLifetimeTooShort) {
		t.Fatalf("short remaining lifetime auto start error=%v", err)
	}
}

func TestAuthorizationServiceExplicitAuthorizationRemainsStatelessAcrossRestart(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	signer, err := NewSigner([]byte(testRootSecret), WithNow(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	binding := registryBinding(8)
	registry := NewAuthorizationRegistry(WithAuthorizationRegistryNow(func() time.Time { return now }))
	service := NewAuthorizationService(signer, registry)
	prepared, err := service.Prepare()
	if err != nil {
		t.Fatal(err)
	}
	grant, err := service.Bind(prepared, binding)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Verify(grant.Token, binding); err != nil {
		t.Fatalf("bound explicit token rejected: %v", err)
	}
	if registry.Size() != 0 {
		t.Fatalf("explicit authorization unexpectedly entered lifecycle registry: size=%d", registry.Size())
	}
	restarted := NewAuthorizationService(signer, NewAuthorizationRegistry(WithAuthorizationRegistryNow(func() time.Time { return now })))
	if _, err := restarted.Verify(grant.Token, binding); err != nil {
		t.Fatalf("restart rejected stateless explicit token: %v", err)
	}
	withoutRegistry := NewAuthorizationService(signer, nil)
	preparedWithoutRegistry, err := withoutRegistry.Prepare()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := withoutRegistry.Bind(preparedWithoutRegistry, binding); err != nil {
		t.Fatalf("explicit token issuance unexpectedly depends on lifecycle registry: %v", err)
	}
	if _, err := withoutRegistry.Verify(grant.Token, binding); err != nil {
		t.Fatalf("explicit token unexpectedly depends on lifecycle registry: %v", err)
	}
}

func TestAuthorizationServicePreauthorizationFailsClosedAfterRegistryRestart(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	signer, err := NewSigner([]byte(testRootSecret), WithNow(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	binding := registryBinding(0)
	service := NewAuthorizationService(signer, NewAuthorizationRegistry(WithAuthorizationRegistryNow(func() time.Time { return now })))
	prepared, err := service.Prepare()
	if err != nil {
		t.Fatal(err)
	}
	grant, err := service.Bind(prepared, binding)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.VerifyForAutoStart(grant.Token, binding); err != nil {
		t.Fatalf("registered preauthorization rejected: %v", err)
	}
	restarted := NewAuthorizationService(signer, NewAuthorizationRegistry(WithAuthorizationRegistryNow(func() time.Time { return now })))
	if _, err := restarted.VerifyForAutoStart(grant.Token, binding); !errors.Is(err, ErrAuthorizationNotFound) {
		t.Fatalf("restart accepted untracked preauthorization: %v", err)
	}
}

func TestAuthorizationServiceVerifiedClientAutoStartRequiresFreshOnPlayProof(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	signer, err := NewSigner([]byte(testRootSecret), WithNow(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	service := NewAuthorizationService(signer, NewAuthorizationRegistry(WithAuthorizationRegistryNow(func() time.Time { return now })))
	binding := registryBinding(0)
	binding.BindClientIP = true
	binding.ClientIP = "203.0.113.9"
	grant, err := service.IssueDirect(binding)
	if err != nil {
		t.Fatal(err)
	}
	autoStartBinding := binding
	autoStartBinding.BindClientIP = false
	autoStartBinding.ClientIP = ""
	if _, err := service.VerifyForVerifiedClientAutoStart(grant.Token, autoStartBinding); !errors.Is(err, ErrAuthorizationClientNotVerified) {
		t.Fatalf("unverified client auto start error=%v", err)
	}
	claims, err := service.Verify(grant.Token, binding)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.MarkVerifiedClientSource(grant.Token+"-other", claims, binding); err == nil {
		t.Fatal("tampered token unexpectedly marked as verified")
	}
	if err := service.MarkVerifiedClientSource(grant.Token, claims, binding); err != nil {
		t.Fatalf("mark verified client: %v", err)
	}
	if _, err := service.VerifyForVerifiedClientAutoStart(grant.Token, autoStartBinding); err != nil {
		t.Fatalf("verified client auto start: %v", err)
	}
	if _, err := service.VerifyForVerifiedClientAutoStart(grant.Token+"-other", autoStartBinding); !errors.Is(err, ErrAuthorizationClientNotVerified) {
		t.Fatalf("different token auto start error=%v", err)
	}
	now = now.Add(VerifiedClientSourceLifetime)
	if _, err := service.VerifyForVerifiedClientAutoStart(grant.Token, autoStartBinding); !errors.Is(err, ErrAuthorizationClientNotVerified) {
		t.Fatalf("expired client proof auto start error=%v", err)
	}
}

func TestAuthorizationRegistryConcurrentBind(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	registry := NewAuthorizationRegistry(
		WithAuthorizationRegistryCapacity(128),
		WithAuthorizationRegistryNow(func() time.Time { return now }),
	)
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			prepared := registryPrepared(now, fmt.Sprintf("auth-%d", i), time.Minute)
			if err := registry.Register(prepared, registryBinding(0)); err != nil {
				t.Errorf("register %d: %v", i, err)
				return
			}
			if err := registry.BindAuthorization(prepared.AuthorizationGeneration, uint64(i+1)); err != nil {
				t.Errorf("bind %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	if got := registry.Size(); got != 64 {
		t.Fatalf("size=%d, want 64", got)
	}
}

func TestAuthorizationRegistryConcurrentClaimsOneMediaGeneration(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	registry := NewAuthorizationRegistry(WithAuthorizationRegistryNow(func() time.Time { return now }))
	prepared := registryPrepared(now, "shared-authorization", time.Minute)
	if err := registry.Register(prepared, registryBinding(0)); err != nil {
		t.Fatal(err)
	}

	var successes atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 1; i <= 32; i++ {
		wg.Add(1)
		go func(generation uint64) {
			defer wg.Done()
			<-start
			err := registry.BindAuthorization(prepared.AuthorizationGeneration, generation)
			if err == nil {
				successes.Add(1)
				return
			}
			if !errors.Is(err, ErrAuthorizationAlreadyBound) {
				t.Errorf("generation %d bind error=%v", generation, err)
			}
		}(uint64(i))
	}
	close(start)
	wg.Wait()
	if got := successes.Load(); got != 1 {
		t.Fatalf("successful media generations=%d, want 1", got)
	}
}
