package upgrade

import (
	"context"
	"strings"
	"testing"
	"time"
)

type schedulerEvents struct{ events []UpgradeEvent }

func (s *schedulerEvents) Append(_ context.Context, event UpgradeEvent) error {
	s.events = append(s.events, event)
	return nil
}

func TestAutoSchedulerDownloadsOutsideWindowWithoutApplying(t *testing.T) {
	store, err := OpenTaskStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	policyStore, err := OpenPolicyStore(t.TempDir() + "/policy.json")
	if err != nil {
		t.Fatal(err)
	}
	policy := AutoUpgradePolicy{Enabled: true, PolicyVersion: 2, Timezone: "Asia/Shanghai", WindowStart: "02:00", WindowEnd: "03:00", Channel: "stable", PatchOnly: true, AllowRestart: true}
	if err := policyStore.Save(policy); err != nil {
		t.Fatal(err)
	}
	events := &schedulerEvents{}
	s := &AutoScheduler{
		Store: store, PolicyStore: policyStore, Target: Target{Deployment: "standalone", OS: "linux", Arch: "amd64"},
		Check: func(context.Context) (Manifest, error) {
			return automaticManifestForTest(), nil
		},
		CurrentVersion: func() string { return "1.1.0" },
		Preflight: func(context.Context) (PreflightSnapshot, error) {
			return automaticPreflightForTest(), nil
		},
		Prepare: func(context.Context, Manifest, Artifact) (StagedArtifact, error) {
			return StagedArtifact{Path: "/tmp/x"}, nil
		},
		Events: events,
		Now:    func() time.Time { return time.Date(2026, 10, 16, 12, 0, 0, 0, time.FixedZone("CST", 8*3600)) },
	}
	if err := s.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(events.events) != 1 || events.events[0].Event != "downloaded" {
		t.Fatalf("events = %#v", events.events)
	}
	records, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Phase != PhaseAwaitingConfirmation {
		t.Fatalf("records = %#v", records)
	}
	if !CompatibilityEqual(records[0].Compatibility, automaticCompatibilityForTest()) {
		t.Fatalf("compatibility was not persisted: %#v", records[0].Compatibility)
	}
}

func TestAutoSchedulerPreparesWithinWindowForIndependentHelper(t *testing.T) {
	store, err := OpenTaskStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	policyStore, err := OpenPolicyStore(t.TempDir() + "/policy.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := policyStore.Save(AutoUpgradePolicy{Enabled: true, PolicyVersion: 1, Timezone: "Asia/Shanghai", WindowStart: "02:00", WindowEnd: "04:00", Channel: "stable", PatchOnly: true, AllowRestart: true}); err != nil {
		t.Fatal(err)
	}
	applyCount := 0
	s := &AutoScheduler{
		Store: store, PolicyStore: policyStore, Target: Target{Deployment: "standalone", OS: "linux", Arch: "amd64"},
		Check: func(context.Context) (Manifest, error) {
			return automaticManifestForTest(), nil
		},
		CurrentVersion: func() string { return "1.1.0" },
		Preflight: func(context.Context) (PreflightSnapshot, error) {
			return automaticPreflightForTest(), nil
		},
		Prepare: func(context.Context, Manifest, Artifact) (StagedArtifact, error) {
			return StagedArtifact{Path: "/tmp/x"}, nil
		},
		Now: func() time.Time { return time.Date(2026, 10, 16, 3, 0, 0, 0, time.FixedZone("CST", 8*3600)) },
	}
	if err := s.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if applyCount != 0 {
		t.Fatalf("business scheduler applied task %d times", applyCount)
	}
	records, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Phase != PhaseAwaitingConfirmation {
		t.Fatalf("records = %#v", records)
	}
	if records[0].PolicySnapshot == nil || !records[0].PolicySnapshot.AllowRestart {
		t.Fatalf("policy snapshot missing: %#v", records[0])
	}
}

func TestAutoSchedulerCancelsPreparedTaskWhenPolicyChanges(t *testing.T) {
	store, err := OpenTaskStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	policyStore, err := OpenPolicyStore(t.TempDir() + "/policy.json")
	if err != nil {
		t.Fatal(err)
	}
	policy := AutoUpgradePolicy{Enabled: true, PolicyVersion: 1, Timezone: "Asia/Shanghai", WindowStart: "02:00", WindowEnd: "04:00", Channel: "stable", PatchOnly: true, AllowRestart: true}
	if err := policyStore.Save(policy); err != nil {
		t.Fatal(err)
	}
	events := &schedulerEvents{}
	s := &AutoScheduler{
		Store: store, PolicyStore: policyStore, Target: Target{Deployment: "standalone", OS: "linux", Arch: "amd64"},
		Check: func(context.Context) (Manifest, error) {
			return automaticManifestForTest(), nil
		},
		CurrentVersion: func() string { return "1.1.0" },
		Prepare: func(context.Context, Manifest, Artifact) (StagedArtifact, error) {
			return StagedArtifact{Path: "/tmp/x"}, nil
		},
		Preflight: func(context.Context) (PreflightSnapshot, error) { return automaticPreflightForTest(), nil },
		Events:    events,
		Now:       func() time.Time { return time.Date(2026, 10, 16, 3, 0, 0, 0, time.FixedZone("CST", 8*3600)) },
	}
	if err := s.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	records, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Phase != PhaseAwaitingConfirmation {
		t.Fatalf("records = %#v", records)
	}
	policy.PolicyVersion = 2
	if err := policyStore.Save(policy); err != nil {
		t.Fatal(err)
	}
	if err := s.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	updated, err := store.Get(records[0].TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Phase != PhaseFailed || !strings.Contains(updated.Error, "policy version changed") {
		t.Fatalf("updated task = %#v", updated)
	}
	if len(events.events) == 0 || events.events[len(events.events)-1].Event != "blocked" {
		t.Fatalf("events = %#v", events.events)
	}
}

func schedulerFixture(t *testing.T) (*AutoScheduler, *schedulerEvents) {
	t.Helper()
	store, err := OpenTaskStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	policies, err := OpenPolicyStore(t.TempDir() + "/policy.json")
	if err != nil {
		t.Fatal(err)
	}
	policy := AutoUpgradePolicy{Enabled: true, PolicyVersion: 1, Timezone: "Asia/Shanghai", WindowStart: "02:00", WindowEnd: "04:00", Channel: "stable", PatchOnly: true, AllowRestart: true}
	if err := policies.Save(policy); err != nil {
		t.Fatal(err)
	}
	events := &schedulerEvents{}
	return &AutoScheduler{
		Store: store, PolicyStore: policies, Events: events,
		Target: Target{Deployment: "standalone", OS: "linux", Arch: "amd64"},
		Check: func(context.Context) (Manifest, error) {
			return automaticManifestForTest(), nil
		},
		CurrentVersion: func() string { return "1.1.0" },
		Preflight: func(context.Context) (PreflightSnapshot, error) {
			return automaticPreflightForTest(), nil
		},
		Prepare: func(context.Context, Manifest, Artifact) (StagedArtifact, error) {
			return StagedArtifact{Path: "/tmp/x"}, nil
		},
		Now: func() time.Time { return time.Date(2026, 10, 16, 3, 0, 0, 0, time.UTC) },
	}, events
}

func TestAutoSchedulerRefusesUndeclaredAutomaticInstallationsBeforePrepare(t *testing.T) {
	for _, test := range []struct {
		name      string
		manifest  func(*Manifest)
		preflight func(*PreflightSnapshot)
	}{
		{name: "missing compatibility", manifest: func(manifest *Manifest) { manifest.Compatibility = nil }},
		{name: "source build mismatch", manifest: func(manifest *Manifest) { manifest.Compatibility.SourceBuilds[0].BuildID = "other-build" }},
		{name: "database dialect mismatch", manifest: func(manifest *Manifest) { manifest.Compatibility.Database.Dialect = "mysql" }},
		{name: "source migration mismatch", manifest: func(manifest *Manifest) {
			manifest.Compatibility.Database.AllowedSourceMigrationDigests = []string{strings.Repeat("c", 64)}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, events := schedulerFixture(t)
			manifest := automaticManifestForTest()
			if test.manifest != nil {
				test.manifest(&manifest)
			}
			preflight := automaticPreflightForTest()
			if test.preflight != nil {
				test.preflight(&preflight)
			}
			prepareCalls := 0
			s.Check = func(context.Context) (Manifest, error) { return manifest, nil }
			s.Preflight = func(context.Context) (PreflightSnapshot, error) { return preflight, nil }
			s.Prepare = func(context.Context, Manifest, Artifact) (StagedArtifact, error) {
				prepareCalls++
				return StagedArtifact{Path: "/tmp/unreachable"}, nil
			}

			if err := s.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			if prepareCalls != 0 {
				t.Fatalf("prepare calls = %d", prepareCalls)
			}
			records, err := s.Store.List()
			if err != nil {
				t.Fatal(err)
			}
			if len(records) != 0 {
				t.Fatalf("blocked upgrade created task: %#v", records)
			}
			if len(events.events) != 1 || events.events[0].Event != "blocked" || !strings.Contains(events.events[0].Reason, "compatibility") {
				t.Fatalf("events = %#v", events.events)
			}
		})
	}
}

func TestAutoSchedulerCancelsPreparedTaskWhenSignedCompatibilityChanges(t *testing.T) {
	s, events := schedulerFixture(t)
	manifest := automaticManifestForTest()
	s.Check = func(context.Context) (Manifest, error) { return manifest, nil }
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))
	s.Now = func() time.Time { return now }
	if err := s.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	records, err := s.Store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Phase != PhaseAwaitingConfirmation {
		t.Fatalf("prepared records = %#v", records)
	}
	if !CompatibilityEqual(records[0].Compatibility, automaticCompatibilityForTest()) {
		t.Fatalf("prepared compatibility = %#v", records[0].Compatibility)
	}

	manifest.Compatibility = CloneCompatibility(manifest.Compatibility)
	manifest.Compatibility.Database.TargetMigrationDigest = strings.Repeat("c", 64)
	now = time.Date(2026, 10, 16, 3, 0, 0, 0, time.FixedZone("CST", 8*3600))
	if err := s.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	updated, err := s.Store.Get(records[0].TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Phase != PhaseFailed || !strings.Contains(updated.Error, "compatibility declaration changed") {
		t.Fatalf("updated task = %#v", updated)
	}
	if len(events.events) == 0 || events.events[len(events.events)-1].Event != "blocked" {
		t.Fatalf("events = %#v", events.events)
	}
}

func TestAutoSchedulerDoesNotReportSuccessForUnfinishedApply(t *testing.T) {
	s, events := schedulerFixture(t)
	s.Now = func() time.Time { return time.Date(2026, 10, 16, 3, 0, 0, 0, time.FixedZone("CST", 8*3600)) }
	if err := s.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, event := range events.events {
		if event.Event == "succeeded" {
			t.Fatal("nonterminal task was reported as succeeded")
		}
	}
}
