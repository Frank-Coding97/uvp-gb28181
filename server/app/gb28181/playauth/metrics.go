package playauth

import (
	"errors"
	"sync/atomic"
)

// MetricOutcome is deliberately a fixed, low-cardinality set. Resource IDs,
// client addresses, tokens, and authorization generations must never become
// metric labels or keys.
type MetricOutcome string

const metricOutcomeCount = 10

const (
	MetricOutcomeIssued        MetricOutcome = "issued"
	MetricOutcomeVerified      MetricOutcome = "verified"
	MetricOutcomeMissing       MetricOutcome = "missing"
	MetricOutcomeExpired       MetricOutcome = "expired"
	MetricOutcomeTampered      MetricOutcome = "tampered"
	MetricOutcomeWrongResource MetricOutcome = "wrong_resource"
	MetricOutcomeIPMismatch    MetricOutcome = "ip_mismatch"
	MetricOutcomeUnavailable   MetricOutcome = "unavailable"
	MetricOutcomeRegistryFull  MetricOutcome = "registry_full"
	MetricOutcomeTerminal      MetricOutcome = "terminal"
)

var AllMetricOutcomes = []MetricOutcome{
	MetricOutcomeIssued,
	MetricOutcomeVerified,
	MetricOutcomeMissing,
	MetricOutcomeExpired,
	MetricOutcomeTampered,
	MetricOutcomeWrongResource,
	MetricOutcomeIPMismatch,
	MetricOutcomeUnavailable,
	MetricOutcomeRegistryFull,
	MetricOutcomeTerminal,
}

type MetricsSnapshot struct {
	Total    int64
	Outcomes map[MetricOutcome]int64
}

type Metrics struct {
	total    atomic.Int64
	outcomes [metricOutcomeCount]atomic.Int64
}

func NewMetrics() *Metrics { return &Metrics{} }

func (m *Metrics) Record(outcome MetricOutcome) {
	if m == nil {
		return
	}
	index, ok := metricOutcomeIndex(outcome)
	if !ok {
		return
	}
	m.total.Add(1)
	m.outcomes[index].Add(1)
}

func (m *Metrics) Snapshot() MetricsSnapshot {
	result := MetricsSnapshot{Outcomes: make(map[MetricOutcome]int64, len(AllMetricOutcomes))}
	if m == nil {
		return result
	}
	result.Total = m.total.Load()
	for index, outcome := range AllMetricOutcomes {
		result.Outcomes[outcome] = m.outcomes[index].Load()
	}
	return result
}

func MetricOutcomeForError(token string, err error) MetricOutcome {
	if token == "" {
		return MetricOutcomeMissing
	}
	switch {
	case errors.Is(err, ErrTokenExpired), errors.Is(err, ErrAuthorizationExpired):
		return MetricOutcomeExpired
	case errors.Is(err, ErrTokenIPMismatch):
		return MetricOutcomeIPMismatch
	case errors.Is(err, ErrTokenBindingMismatch), errors.Is(err, ErrTokenMediaGenerationMismatch), errors.Is(err, ErrAuthorizationClaimsMismatch), errors.Is(err, ErrAuthorizationNotFound), errors.Is(err, ErrAuthorizationAlreadyBound), errors.Is(err, ErrAuthorizationLifetimeTooShort):
		return MetricOutcomeWrongResource
	case errors.Is(err, ErrAuthorizationRegistryFull):
		return MetricOutcomeRegistryFull
	case errors.Is(err, ErrAuthorizationTerminal):
		return MetricOutcomeTerminal
	case errors.Is(err, ErrAuthorizationRegistryUnavailable), errors.Is(err, ErrKeyInvalid):
		return MetricOutcomeUnavailable
	default:
		return MetricOutcomeTampered
	}
}

func metricOutcomeIndex(outcome MetricOutcome) (int, bool) {
	for index, candidate := range AllMetricOutcomes {
		if candidate == outcome {
			return index, true
		}
	}
	return 0, false
}
