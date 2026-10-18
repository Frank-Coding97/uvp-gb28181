package upgrade

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestAutoUpgradePolicyValidatesTimezoneAndWindow(t *testing.T) {
	policy := AutoUpgradePolicy{Enabled: true, PolicyVersion: 1, Timezone: "Asia/Shanghai", WindowStart: "23:00", WindowEnd: "01:00", Channel: "stable", Deployment: "compose", OS: "linux", Arch: "amd64", PatchOnly: true, AllowRestart: true, RetainVersions: 2}
	if err := policy.Validate(); err != nil {
		t.Fatal(err)
	}
	if !policy.InWindow(time.Date(2026, 10, 16, 0, 30, 0, 0, time.FixedZone("CST", 8*3600))) {
		t.Fatal("cross-midnight window rejected")
	}
	if policy.InWindow(time.Date(2026, 10, 16, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))) {
		t.Fatal("outside window accepted")
	}
	policy.Timezone = "not/a-zone"
	if err := policy.Validate(); err == nil {
		t.Fatal("invalid timezone accepted")
	}
}

func TestAutoUpgradePolicyRequiresExplicitRestartPermissionAndMatchesTarget(t *testing.T) {
	policy := AutoUpgradePolicy{Enabled: true, PolicyVersion: 1, Timezone: "Asia/Shanghai", WindowStart: "02:00", WindowEnd: "04:00", Channel: "stable", Deployment: "compose", OS: "linux", Arch: "amd64", AllowRestart: false}
	if err := policy.Validate(); err != nil {
		t.Fatal(err)
	}
	if policy.AllowsTarget(Target{Deployment: "compose", OS: "linux", Arch: "amd64"}) {
		t.Fatal("policy without restart permission was accepted")
	}
	policy.AllowRestart = true
	if !policy.AllowsTarget(Target{Deployment: "compose", OS: "linux", Arch: "amd64"}) {
		t.Fatal("matching target was rejected")
	}
	if policy.AllowsTarget(Target{Deployment: "standalone", OS: "linux", Arch: "amd64"}) {
		t.Fatal("mismatched deployment was accepted")
	}
}

func TestPolicyStoreDefaultsAndAtomicSave(t *testing.T) {
	store, err := OpenPolicyStore(filepath.Join(t.TempDir(), "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	policy, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if policy.Enabled || policy.Channel != "stable" {
		t.Fatalf("unexpected defaults: %#v", policy)
	}
	policy.Enabled = true
	if err := store.Save(policy); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Enabled || loaded.PolicyVersion != policy.PolicyVersion {
		t.Fatalf("loaded policy mismatch: %#v", loaded)
	}
}

func TestPolicyStoreConcurrentRevisionsAreStrictlyIncreasing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	const count = 12
	results := make(chan AutoUpgradePolicy, count)
	failures := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store, err := OpenPolicyStore(path)
			if err != nil {
				failures <- err
				return
			}
			policy, err := store.Load()
			if err != nil {
				failures <- err
				return
			}
			updated, err := store.SaveNext(policy)
			if err != nil {
				failures <- err
				return
			}
			results <- updated
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	seen := map[uint64]bool{}
	for policy := range results {
		if seen[policy.PolicyVersion] {
			t.Errorf("duplicate revision %d", policy.PolicyVersion)
		}
		seen[policy.PolicyVersion] = true
	}
	if len(seen) != count {
		t.Fatalf("revisions: %v", seen)
	}
}
