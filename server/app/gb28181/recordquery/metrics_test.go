package recordquery

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRecordQueryMetricsCountsTerminalOutcomeOnce(t *testing.T) {
	metrics := NewMetrics()
	finish := metrics.Begin()
	require.EqualValues(t, 1, metrics.Snapshot().Active)

	finish(QueryStatusPartial, ErrorCodeCapacity, nil, 3, 250*time.Millisecond)
	finish(QueryStatusComplete, "", nil, 9, time.Second)

	snapshot := metrics.Snapshot()
	require.Zero(t, snapshot.Active)
	require.EqualValues(t, 1, snapshot.Total)
	require.EqualValues(t, 1, snapshot.Outcomes[MetricOutcomeCapacity])
	require.EqualValues(t, 3, snapshot.Records)
	require.Equal(t, 250*time.Millisecond, snapshot.Duration)
}

func TestRecordQueryMetricsUsesFixedLowCardinalityOutcomes(t *testing.T) {
	metrics := NewMetrics()
	cases := []struct {
		status QueryStatus
		err    error
		want   MetricOutcome
	}{
		{QueryStatusComplete, nil, MetricOutcomeComplete},
		{QueryStatusEmpty, nil, MetricOutcomeEmpty},
		{QueryStatusPartial, queryError(ErrorCodeTimeout, ErrTimeout), MetricOutcomeTimeout},
		{QueryStatusCanceled, errors.New("request canceled"), MetricOutcomeCanceled},
		{QueryStatusSendFailed, queryError(ErrorCodeSendFailed, ErrSendFailed), MetricOutcomeSendFailed},
		{QueryStatusUnavailable, queryError(ErrorCodeUnavailable, ErrUnavailable), MetricOutcomeUnavailable},
	}
	for _, test := range cases {
		metrics.Begin()(test.status, "", test.err, 0, time.Millisecond)
	}

	snapshot := metrics.Snapshot()
	require.Zero(t, snapshot.Active)
	require.EqualValues(t, len(cases), snapshot.Total)
	require.Len(t, snapshot.Outcomes, len(AllMetricOutcomes))
	for _, test := range cases {
		require.EqualValues(t, 1, snapshot.Outcomes[test.want], test.want)
	}
}
