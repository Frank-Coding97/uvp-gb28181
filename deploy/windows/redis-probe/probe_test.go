package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestAggregateStatusKeepsUnexecutedEvidenceVisible(t *testing.T) {
	if got := aggregateStatus([]checkResult{{Name: "a", Status: "passed"}, {Name: "b", Status: "not_executed"}}); got != "not_executed" {
		t.Fatalf("aggregate status = %q", got)
	}
	if got := aggregateStatus([]checkResult{{Name: "a", Status: "passed"}, {Name: "b", Status: "failed"}}); got != "failed" {
		t.Fatalf("aggregate status = %q", got)
	}
	if got := aggregateStatus([]checkResult{{Name: "a", Status: "passed"}}); got != "passed" {
		t.Fatalf("aggregate status = %q", got)
	}
}

func TestVerifyLicenseDistinguishesMissingAndEmptyMaterial(t *testing.T) {
	missing := verifyLicense(filepath.Join(t.TempDir(), "COPYING"))
	if missing["status"] != "failed" {
		t.Fatalf("missing license status = %#v", missing)
	}
	emptyPath := filepath.Join(t.TempDir(), "COPYING")
	if err := os.WriteFile(emptyPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	empty := verifyLicense(emptyPath)
	if empty["status"] != "failed" {
		t.Fatalf("empty license status = %#v", empty)
	}
	validPath := filepath.Join(t.TempDir(), "COPYING")
	if err := os.WriteFile(validPath, []byte("Redis license fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	valid := verifyLicense(validPath)
	if valid["status"] != "passed" || valid["bytes"] != 22 {
		t.Fatalf("valid license result = %#v", valid)
	}
}

func TestRealRedisProbeUsesTemporaryServerWhenAvailable(t *testing.T) {
	binary, err := exec.LookPath("redis-server")
	if err != nil {
		t.Skip("redis-server is not available")
	}
	licensePath := filepath.Join(t.TempDir(), "COPYING")
	if err := os.WriteFile(licensePath, []byte("Redis license fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := runProbe(options{redisServer: binary, licenseFile: licensePath})
	if result.Status != "not_executed" {
		t.Fatalf("probe status = %q, want not_executed because disk-full is not injected", result.Status)
	}
	checks := make(map[string]string, len(result.Checks))
	for _, check := range result.Checks {
		checks[check.Name] = check.Status
	}
	if checks["t02-a"] != "passed" || checks["t02-b"] != "passed" || checks["t02-c"] != "not_executed" {
		t.Fatalf("probe checks = %#v", checks)
	}
}
