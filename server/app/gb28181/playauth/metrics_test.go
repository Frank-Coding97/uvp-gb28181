package playauth

import (
	"strings"
	"testing"
	"time"
)

func TestAuthorizationMetricsUseOnlyFixedOutcomes(t *testing.T) {
	metrics := NewMetrics()
	metrics.Record(MetricOutcomeIssued)
	metrics.Record(MetricOutcomeMissing)
	metrics.Record(MetricOutcome("device-37010301021320000014"))

	snapshot := metrics.Snapshot()
	if snapshot.Total != 2 {
		t.Fatalf("total=%d, want 2", snapshot.Total)
	}
	if len(snapshot.Outcomes) != len(AllMetricOutcomes) {
		t.Fatalf("outcome count=%d, want %d", len(snapshot.Outcomes), len(AllMetricOutcomes))
	}
	if snapshot.Outcomes[MetricOutcomeIssued] != 1 || snapshot.Outcomes[MetricOutcomeMissing] != 1 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	if _, exists := snapshot.Outcomes[MetricOutcome("device-37010301021320000014")]; exists {
		t.Fatal("metrics accepted a high-cardinality dynamic outcome")
	}
}

func TestAuthorizationServiceRecordsIssueAndVerificationOutcomes(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	signer, err := NewSigner(
		[]byte("0123456789abcdef0123456789abcdef"),
		WithNow(func() time.Time { return now }),
	)
	if err != nil {
		t.Fatal(err)
	}
	metrics := NewMetrics()
	service := NewAuthorizationService(
		signer,
		NewAuthorizationRegistry(WithAuthorizationRegistryNow(func() time.Time { return now })),
		WithAuthorizationMetrics(metrics),
	)
	binding := versionedBinding()
	binding.BindClientIP = true
	binding.ClientIP = "203.0.113.9"

	grant, err := service.IssueDirect(binding)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Verify(grant.Token, binding); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Verify("", binding); err == nil {
		t.Fatal("missing token unexpectedly verified")
	}
	if _, err := service.Verify(grant.Token+"tampered", binding); err == nil {
		t.Fatal("tampered token unexpectedly verified")
	}
	wrongResource := binding
	wrongResource.Stream = "another-stream"
	if _, err := service.Verify(grant.Token, wrongResource); err == nil {
		t.Fatal("wrong-resource token unexpectedly verified")
	}
	wrongIP := binding
	wrongIP.ClientIP = "203.0.113.10"
	if _, err := service.Verify(grant.Token, wrongIP); err == nil {
		t.Fatal("wrong-IP token unexpectedly verified")
	}
	now = now.Add(DefaultTTL)
	if _, err := service.Verify(grant.Token, binding); err == nil {
		t.Fatal("expired token unexpectedly verified")
	}

	snapshot := metrics.Snapshot()
	want := map[MetricOutcome]int64{
		MetricOutcomeIssued:        1,
		MetricOutcomeVerified:      1,
		MetricOutcomeMissing:       1,
		MetricOutcomeTampered:      1,
		MetricOutcomeWrongResource: 1,
		MetricOutcomeIPMismatch:    1,
		MetricOutcomeExpired:       1,
	}
	for outcome, count := range want {
		if snapshot.Outcomes[outcome] != count {
			t.Fatalf("outcome %s=%d, want %d; snapshot=%+v", outcome, snapshot.Outcomes[outcome], count, snapshot)
		}
	}
}

func TestAuthorizationCorrelationIDDoesNotExposeGeneration(t *testing.T) {
	generation := "authorization-generation-secret"
	correlation := CorrelationID(generation)
	if correlation == "" || correlation == generation || strings.Contains(correlation, generation) {
		t.Fatalf("unsafe correlation id %q", correlation)
	}
	if correlation != CorrelationID(generation) {
		t.Fatal("correlation id must be stable")
	}
}
