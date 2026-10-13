package audit

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRejectedCollectorNormalizesInputsAndCopiesSnapshots(t *testing.T) {
	collector := NewRejectedCollector()
	collector.Record(RejectedInput{
		RequestID: strings.Repeat("a", 32),
		ClientID:  7,
		Scope:     "device:list",
		Source:    "2001:0db8:0:0:0:0:0:1",
		Reason:    string(ReasonAuthenticationFailed),
	})
	collector.Record(RejectedInput{
		RequestID: "sk=do-not-store",
		ClientID:  -1,
		Scope:     "scope-with-signature=do-not-store",
		Source:    "not-an-ip?token=do-not-store",
		Reason:    "raw signature and body must become UNKNOWN",
	})

	snapshot := collector.Snapshot()
	require.Len(t, snapshot.Events, 2)
	require.Equal(t, strings.Repeat("a", 32), snapshot.Events[0].RequestID)
	require.Equal(t, int64(7), snapshot.Events[0].ClientID)
	require.Equal(t, "device:list", snapshot.Events[0].Scope)
	require.Equal(t, "2001:db8::1", snapshot.Events[0].Source)
	require.Equal(t, ReasonAuthenticationFailed, snapshot.Events[0].Reason)
	require.NotZero(t, snapshot.Events[0].At)
	require.Empty(t, snapshot.Events[1].RequestID)
	require.Zero(t, snapshot.Events[1].ClientID)
	require.Empty(t, snapshot.Events[1].Scope)
	require.Empty(t, snapshot.Events[1].Source)
	require.Equal(t, ReasonUnknown, snapshot.Events[1].Reason)
	require.EqualValues(t, 1, snapshot.Counts[ReasonAuthenticationFailed])
	require.EqualValues(t, 1, snapshot.Counts[ReasonUnknown])

	encoded, err := json.Marshal(snapshot)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "do-not-store")
	require.NotContains(t, string(encoded), "signature")
	require.NotContains(t, string(encoded), "body")

	snapshot.Events[0].Scope = "mutated"
	snapshot.Counts[ReasonAuthenticationFailed] = 0
	snapshotAgain := collector.Snapshot()
	require.Equal(t, "device:list", snapshotAgain.Events[0].Scope)
	require.EqualValues(t, 1, snapshotAgain.Counts[ReasonAuthenticationFailed])
}

func TestRejectedCollectorHasFixedReasonCardinality(t *testing.T) {
	collector := NewRejectedCollector()
	for i := 0; i < 100; i++ {
		collector.Record(RejectedInput{Reason: fmt.Sprintf("attacker-controlled-%d", i)})
	}

	snapshot := collector.Snapshot()
	classes := PublishedRejectedReasonClasses()
	require.Len(t, snapshot.Counts, len(classes))
	for _, class := range classes {
		_, ok := snapshot.Counts[class]
		require.True(t, ok, "missing fixed reason class %q", class)
	}
	require.EqualValues(t, 100, snapshot.Counts[ReasonUnknown])
}

func TestRejectedCollectorRingIsBounded(t *testing.T) {
	collector := NewRejectedCollector()
	for i := 0; i < RejectedEventCapacity+17; i++ {
		collector.Record(RejectedInput{
			RequestID: fmt.Sprintf("%032x", i),
			ClientID:  1,
			Scope:     "device:list",
			Reason:    string(ReasonInvalidRequest),
		})
	}

	snapshot := collector.Snapshot()
	require.Len(t, snapshot.Events, RejectedEventCapacity)
	require.Equal(t, fmt.Sprintf("%032x", 17), snapshot.Events[0].RequestID)
	require.Equal(t, fmt.Sprintf("%032x", RejectedEventCapacity+16), snapshot.Events[len(snapshot.Events)-1].RequestID)
	require.EqualValues(t, RejectedEventCapacity+17, snapshot.Counts[ReasonInvalidRequest])
}

func TestRejectedCollectorConcurrentFloodAndSaturatingCounters(t *testing.T) {
	collector := NewRejectedCollector()
	const goroutines = 32
	const perGoroutine = 3125

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for worker := 0; worker < goroutines; worker++ {
		worker := worker
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				collector.Record(RejectedInput{
					RequestID: fmt.Sprintf("%032x", worker*perGoroutine+i),
					Reason:    string(ReasonRateLimited),
				})
			}
		}()
	}
	wg.Wait()

	snapshot := collector.Snapshot()
	require.Len(t, snapshot.Events, RejectedEventCapacity)
	require.EqualValues(t, goroutines*perGoroutine, snapshot.Counts[ReasonRateLimited])

	collector.mu.Lock()
	collector.counts[reasonIndex(ReasonRateLimited)] = math.MaxUint64
	collector.mu.Unlock()
	collector.Record(RejectedInput{Reason: string(ReasonRateLimited)})
	require.Equal(t, uint64(math.MaxUint64), collector.Snapshot().Counts[ReasonRateLimited])
}

func TestRejectedCollectorNilSafe(t *testing.T) {
	var collector *RejectedCollector
	collector.Record(RejectedInput{Reason: string(ReasonInvalidRequest)})
	snapshot := collector.Snapshot()
	require.Len(t, snapshot.Counts, len(PublishedRejectedReasonClasses()))
	require.Empty(t, snapshot.Events)
}
